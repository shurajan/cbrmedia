// Command hlsprobe собирает сырые ответы status API и CDN по одному пользователю,
// чтобы по ним разобрать реальное поведение внешних сервисов и подстроить recorder:
// коды ответов (существующий, несуществующий, имя в другом регистре), master- и
// media-плейлисты, как меняются URL сегментов и токены между перезагрузками,
// ключи, init-сегменты и несколько сегментов.
//
// Использование:
//
//	hlsprobe [-out DIR] [-wait 10m] [-reloads 6 | -duration 5m] [-segments 3] [-nonexistent NAME] <user или ссылка>
//
// base_url и proxy берутся из $CBR_HOME/config/recorder.json (как у recorder),
// флаги -base-url и -proxy их переопределяют. Всё сохраняется в DIR:
// файлы ответов, requests.jsonl (URL, код, заголовки, время каждого запроса)
// и summary.txt с кратким разбором. Ссылки во всём сохранённом и напечатанном
// замаскированы (mask.go): хосты, значения query, имя пользователя, cookies.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cbr/core/home"
	"cbr/core/store"
	"cbr/core/svc"
	"cbr/recorder/internal/config"
	"cbr/recorder/internal/recorder"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type options struct {
	out, baseURL, proxy string
	nonexistent         string // имя, которого точно нет в status API
	wait, duration      time.Duration
	reloads, segments   int
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hlsprobe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: hlsprobe [flags] <user or link>")
		fs.PrintDefaults()
	}
	var o options
	fs.StringVar(&o.out, "out", "", "output dir (default ./hlsprobe-<user>-<time>)")
	fs.StringVar(&o.baseURL, "base-url", "", "status API base URL (default from recorder.json)")
	fs.StringVar(&o.proxy, "proxy", "", "HTTP proxy (default from recorder.json)")
	fs.StringVar(&o.nonexistent, "nonexistent", "", "user that surely does not exist (default <user>zz0probe404)")
	fs.DurationVar(&o.wait, "wait", 0, "if the stream is not live, poll every 15s up to this long")
	fs.IntVar(&o.reloads, "reloads", 6, "how many times to reload the media playlist")
	fs.DurationVar(&o.duration, "duration", 0, "reload the media playlist for this long instead of -reloads times")
	fs.IntVar(&o.segments, "segments", 3, "how many latest segments to download")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	log := svc.Logger(stderr, false)

	if err := resolveConfig(&o); err != nil {
		log.Error("config", "err", err)
		return 1
	}
	raw := rawUser(fs.Arg(0))
	user, err := store.UserFromInput(fs.Arg(0))
	if err != nil {
		log.Error("user", "input", fs.Arg(0), "err", err)
		return 2
	}
	if o.out == "" {
		// Без имени пользователя: оно маскируется во всех сохраняемых данных.
		o.out = "hlsprobe-" + time.Now().Format("20060102-150405")
	}
	if err := os.MkdirAll(o.out, 0o750); err != nil {
		log.Error("create output dir", "err", err)
		return 1
	}
	client, err := recorder.NewHTTPClient(o.proxy, slog.New(slog.DiscardHandler))
	if err != nil {
		log.Error("http client", "err", err)
		return 1
	}

	ctx, stop := svc.SignalContext()
	defer stop()

	p, err := newProbe(client, o.baseURL, o.out, newMasker(user))
	if err != nil {
		log.Error("open output", "err", err)
		return 1
	}
	defer p.close()

	p.run(ctx, o, user, raw)
	summary := p.mask.text(p.summary.String())
	if err := os.WriteFile(filepath.Join(o.out, "summary.txt"), []byte(summary), 0o600); err != nil {
		log.Error("write summary", "err", err)
		return 1
	}
	fmt.Fprint(stdout, summary)
	fmt.Fprintf(stdout, "\nsaved to %s\n", o.out)
	return 0
}

// resolveConfig дополняет флаги значениями из recorder.json, если они не заданы.
func resolveConfig(o *options) error {
	if o.baseURL == "" {
		layout, err := home.Resolve()
		if err != nil {
			return err
		}
		cfg, err := config.Load(layout.ConfigFile(config.Name))
		if err != nil {
			return fmt.Errorf("%w (or pass -base-url)", err)
		}
		o.baseURL = cfg.BaseURL
		if o.proxy == "" {
			o.proxy = cfg.Proxy
		}
	}
	o.baseURL = strings.TrimRight(o.baseURL, "/")
	return nil
}

// rawUser — имя из ввода как есть, без приведения регистра: чтобы проверить,
// различает ли status API регистр.
func rawUser(input string) string {
	s := strings.TrimSpace(input)
	if !strings.Contains(s, "/") {
		return s
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return parts[len(parts)-1]
}

// --- запись запросов ----------------------------------------------------------

// probe выполняет запросы и сохраняет каждый ответ в файл и в requests.jsonl.
type probe struct {
	client  *http.Client
	baseURL string // status API
	dir     string
	mask    *masker // всё сохраняемое и печатаемое проходит через него
	index   *os.File
	summary strings.Builder

	mu sync.Mutex
	n  int
}

// record — строка requests.jsonl.
type record struct {
	N          int                 `json:"n"`
	Label      string              `json:"label"`
	URL        string              `json:"url"`
	At         string              `json:"at"`
	DurationMS int64               `json:"durationMs"`
	Status     int                 `json:"status,omitempty"`
	Bytes      int                 `json:"bytes"`
	File       string              `json:"file,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Error      string              `json:"error,omitempty"`
}

func newProbe(client *http.Client, baseURL, dir string, mask *masker) (*probe, error) {
	index, err := os.OpenFile(filepath.Join(dir, "requests.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	return &probe{client: client, baseURL: baseURL, dir: dir, mask: mask, index: index}, nil
}

func (p *probe) close() { _ = p.index.Close() }

func (p *probe) logf(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(&p.summary, format+"\n", args...)
}

// Пределы одного запроса и пауза между проверками при -wait.
const (
	waitPoll       = 15 * time.Second
	requestTimeout = time.Minute
	maxBody        = 64 << 20
)

// get выполняет GET и сохраняет ответ в файл <n>_<label><ext>.
func (p *probe) get(ctx context.Context, label, rawURL, ext string) (int, []byte, error) {
	p.mu.Lock()
	p.n++
	n := p.n
	p.mu.Unlock()

	p.mask.remember(rawURL)
	rec := record{N: n, Label: label, URL: p.mask.text(rawURL), At: time.Now().UTC().Format(time.RFC3339Nano)}
	start := time.Now()
	status, body, headers, err := p.fetch(ctx, rawURL)
	rec.DurationMS = time.Since(start).Milliseconds()
	rec.Status, rec.Bytes, rec.Headers = status, len(body), p.mask.headers(headers)
	if err != nil {
		rec.Error = p.mask.text(err.Error())
	}
	if len(body) > 0 {
		rec.File = fmt.Sprintf("%03d_%s%s", n, label, ext)
		saved := body
		if textFile(ext) {
			saved = []byte(p.mask.text(string(body)))
		}
		if werr := os.WriteFile(filepath.Join(p.dir, rec.File), saved, 0o600); werr != nil && err == nil {
			err = werr
		}
	}

	line, _ := json.Marshal(rec)
	p.mu.Lock()
	_, _ = p.index.Write(append(line, '\n'))
	p.mu.Unlock()
	return status, body, err
}

func (p *probe) fetch(ctx context.Context, rawURL string) (int, []byte, map[string][]string, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	return resp.StatusCode, body, resp.Header, err
}

// --- сценарий -------------------------------------------------------------------

// statusInfo — нужные поля ответа status API.
type statusInfo struct {
	RoomStatus string `json:"room_status"`
	HLSSource  string `json:"hls_source"`
}

func (p *probe) status(ctx context.Context, label, user string) (int, statusInfo) {
	code, body, err := p.get(ctx, label, p.baseURL+"/"+user+"/", ".json")
	var info statusInfo
	_ = json.Unmarshal(body, &info)
	p.logf("%-22s %-28s HTTP %d room_status=%q live=%v err=%v", label, user, code, info.RoomStatus, info.HLSSource != "", err)
	return code, info
}

func (p *probe) run(ctx context.Context, o options, user, raw string) {
	p.logf("hlsprobe %s, user %s (input %q), base_url %s", time.Now().Format(time.RFC3339), user, raw, o.baseURL)
	p.logf("\n== status API")

	_, info := p.status(ctx, "status", user)
	if raw != "" && raw != user {
		p.status(ctx, "status_raw_case", raw)
	}
	if upper := strings.ToUpper(user); upper != user && upper != raw {
		p.status(ctx, "status_upper_case", upper)
	}
	fake := o.nonexistent
	if fake == "" {
		fake = user + "zz0probe404"
	}
	p.status(ctx, "status_nonexistent", fake)

	if info.HLSSource == "" && o.wait > 0 {
		p.logf("not live, waiting (up to %v)…", o.wait)
		fmt.Fprintf(os.Stderr, "not live, polling every %v up to %v…\n", waitPoll, o.wait)
		deadline := time.Now().Add(o.wait)
		for info.HLSSource == "" && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(waitPoll):
			}
			_, info = p.status(ctx, "status_wait", user)
		}
	}
	if info.HLSSource == "" {
		p.logf("\nstream is not live (room_status=%q): no hls_source, nothing to download. Run again when public, or use -wait.", info.RoomStatus)
		return
	}

	mediaURL, renditions := p.master(ctx, "master", info.HLSSource, true)
	if mediaURL == "" {
		return
	}
	last := p.reloads(ctx, mediaURL, o.reloads, o.duration)
	if last != nil {
		p.download(ctx, "", last, o.segments)
	}
	// Альтернативные дорожки (обычно звук отдельно от видео): номера сегментов
	// сравниваются с видео — по ним recorder будет выравнивать дорожки.
	for i, r := range renditions {
		p.rendition(ctx, i, r, last, o.segments)
	}

	// Свежий hls_source: меняется ли токен и URL вариантов между запросами.
	p.logf("\n== fresh status and master")
	if _, fresh := p.status(ctx, "status_fresh", user); fresh.HLSSource != "" {
		p.logf("hls_source changed: %v (same path: %v)", fresh.HLSSource != info.HLSSource, stripQuery(fresh.HLSSource) == stripQuery(info.HLSSource))
		_, _ = p.master(ctx, "master_fresh", fresh.HLSSource, false)
	}
}

// master скачивает плейлист по hls_source и, если это master-плейлист, все его
// варианты и альтернативные дорожки. Возвращает URL media-плейлиста с
// максимальным битрейтом (или сам URL, если это уже media-плейлист) и URL
// альтернативных дорожек (#EXT-X-MEDIA). Текст плейлиста целиком попадает в summary.
func (p *probe) master(ctx context.Context, label, src string, fetchVariants bool) (string, []string) {
	p.logf("\n== %s", label)
	code, body, err := p.get(ctx, label, src, ".m3u8")
	text := string(body)
	p.logf("HTTP %d, %d bytes, err=%v", code, len(body), err)
	if err != nil || code != http.StatusOK {
		return "", nil
	}
	p.logText(label, text)
	if !strings.Contains(text, "#EXT-X-STREAM-INF") {
		p.logf("hls_source is a media playlist")
		return src, nil
	}

	variants, media := parseMaster(src, text)
	best := ""
	bestBW := -1
	for i, v := range variants {
		p.logf("variant %d: BANDWIDTH=%d RESOLUTION=%s CODECS=%s", i, v.bandwidth, v.resolution, v.codecs)
		if v.bandwidth >= bestBW {
			best, bestBW = v.uri, v.bandwidth
		}
	}
	for _, m := range media {
		p.logf("rendition: %s", m)
	}
	if !fetchVariants {
		return best, media
	}
	for i, v := range variants {
		if v.uri != best {
			code, _, err := p.get(ctx, fmt.Sprintf("variant%d", i), v.uri, ".m3u8")
			p.logf("variant %d playlist: HTTP %d err=%v", i, code, err)
		}
	}
	for i, m := range media {
		code, _, err := p.get(ctx, fmt.Sprintf("rendition%d", i), m, ".m3u8")
		p.logf("rendition %d playlist: HTTP %d err=%v", i, code, err)
	}
	return best, media
}

// logText добавляет текст плейлиста в summary целиком.
func (p *probe) logText(label, text string) {
	p.logf("--- %s text:\n%s\n--- end of %s", label, strings.TrimRight(text, "\n"), label)
}

// rendition загружает альтернативную дорожку, сравнивает её номера сегментов
// с видео и скачивает её init и последние count сегментов.
func (p *probe) rendition(ctx context.Context, i int, rawURL string, video *playlist, count int) {
	label := fmt.Sprintf("rendition%d_now", i)
	p.logf("\n== %s", label)
	code, body, err := p.get(ctx, label, rawURL, ".m3u8")
	if err != nil || code != http.StatusOK {
		p.logf("HTTP %d err=%v", code, err)
		return
	}
	text := string(body)
	p.logText(label, text)
	pl := parseMedia(rawURL, text)
	line := fmt.Sprintf("seq=%d target=%ds segments=%d maps=%d keys=%d", pl.seq, pl.target, len(pl.segments), len(pl.maps), len(pl.keys))
	if video != nil {
		line += fmt.Sprintf(" | video seq at last reload=%d (%d segments)", video.seq, len(video.segments))
	}
	p.logf("%s", line)
	p.download(ctx, fmt.Sprintf("audio%d_", i), &pl, count)
}

type variant struct {
	uri, resolution, codecs string
	bandwidth               int
}

var attrRe = regexp.MustCompile(`([A-Z0-9-]+)=("[^"]*"|[^,]*)`)

func attrs(s string) map[string]string {
	m := map[string]string{}
	for _, kv := range attrRe.FindAllStringSubmatch(s, -1) {
		m[kv[1]] = strings.Trim(kv[2], `"`)
	}
	return m
}

func parseMaster(base, text string) (variants []variant, media []string) {
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			a := attrs(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			bw, _ := strconv.Atoi(a["BANDWIDTH"])
			for i+1 < len(lines) {
				i++
				if uri := strings.TrimSpace(lines[i]); uri != "" && !strings.HasPrefix(uri, "#") {
					variants = append(variants, variant{uri: resolve(base, uri), bandwidth: bw, resolution: a["RESOLUTION"], codecs: a["CODECS"]})
					break
				}
			}
		case strings.HasPrefix(line, "#EXT-X-MEDIA:"):
			if uri := attrs(strings.TrimPrefix(line, "#EXT-X-MEDIA:"))["URI"]; uri != "" {
				media = append(media, resolve(base, uri))
			}
		}
	}
	return variants, media
}

// playlist — то, что нужно для разбора media-плейлиста.
type playlist struct {
	seq, target, discontinuities int
	endList                      bool
	segments, keys, maps         []string
}

func parseMedia(base, text string) playlist {
	var pl playlist
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			pl.seq, _ = strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"))
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			pl.target, _ = strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"))
		case strings.HasPrefix(line, "#EXT-X-DISCONTINUITY"):
			pl.discontinuities++
		case strings.HasPrefix(line, "#EXT-X-ENDLIST"):
			pl.endList = true
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			if uri := attrs(strings.TrimPrefix(line, "#EXT-X-KEY:"))["URI"]; uri != "" {
				pl.keys = appendNew(pl.keys, resolve(base, uri))
			}
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			if uri := attrs(strings.TrimPrefix(line, "#EXT-X-MAP:"))["URI"]; uri != "" {
				pl.maps = appendNew(pl.maps, resolve(base, uri))
			}
		case strings.HasPrefix(line, "#"):
		default:
			pl.segments = append(pl.segments, resolve(base, line))
		}
	}
	return pl
}

// reloads перезагружает media-плейлист count раз (или, если duration > 0, в течение
// duration — видно, живут ли токены дольше) с интервалом target duration и
// сравнивает URL сегментов соседних перезагрузок — целиком и без query
// (так видно, меняются ли токены у тех же сегментов). Возвращает последний плейлист.
func (p *probe) reloads(ctx context.Context, mediaURL string, count int, duration time.Duration) *playlist {
	p.logf("\n== media playlist reloads")
	if duration > 0 {
		fmt.Fprintf(os.Stderr, "reloading the media playlist for %v…\n", duration)
	}
	start := time.Now()
	finished := func(i int) bool {
		if duration > 0 {
			return time.Since(start) >= duration
		}
		return i >= count-1
	}
	var prev, last *playlist
	for i := 0; ; i++ {
		code, body, err := p.get(ctx, fmt.Sprintf("media_reload%d", i), mediaURL, ".m3u8")
		if err != nil || code != http.StatusOK {
			p.logf("reload %d +%ds: HTTP %d err=%v", i, int(time.Since(start).Seconds()), code, err)
			return last
		}
		if i == 0 {
			p.logText("media_reload0", string(body))
		}
		pl := parseMedia(mediaURL, string(body))
		line := fmt.Sprintf("reload %d +%ds: seq=%d target=%ds segments=%d disc=%d endlist=%v keys=%d maps=%d",
			i, int(time.Since(start).Seconds()), pl.seq, pl.target, len(pl.segments), pl.discontinuities, pl.endList, len(pl.keys), len(pl.maps))
		if prev != nil {
			line += fmt.Sprintf(" | vs prev: same URL %d, same path %d, seq +%d",
				overlap(prev.segments, pl.segments, false), overlap(prev.segments, pl.segments, true), pl.seq-prev.seq)
		}
		p.logf("%s", line)
		prev, last = &pl, &pl
		if pl.endList || finished(i) {
			break
		}
		delay := time.Duration(max(pl.target, 1)) * time.Second
		select {
		case <-ctx.Done():
			return last
		case <-time.After(delay):
		}
	}
	if last != nil && len(last.segments) > 0 {
		p.logf("first segment: %s", last.segments[0])
		p.logf("last segment:  %s", last.segments[len(last.segments)-1])
	}
	return last
}

// download параллельно скачивает ключи, init-сегменты и последние count сегментов;
// prefix отличает файлы дорожек.
func (p *probe) download(ctx context.Context, prefix string, pl *playlist, count int) {
	p.logf("\n== segments, keys, init")
	type job struct{ label, url, ext string }
	var jobs []job
	for i, k := range pl.keys {
		jobs = append(jobs, job{fmt.Sprintf("%skey%d", prefix, i), k, ".key"})
	}
	for i, m := range pl.maps {
		jobs = append(jobs, job{fmt.Sprintf("%sinit%d", prefix, i), m, ext(m, ".mp4")})
	}
	for i, s := range pl.segments[max(len(pl.segments)-count, 0):] {
		jobs = append(jobs, job{fmt.Sprintf("%ssegment%d", prefix, i), s, ext(s, ".ts")})
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Go(func() {
			start := time.Now()
			code, body, err := p.get(ctx, j.label, j.url, j.ext)
			p.logf("%-10s HTTP %d %8d bytes %5dms head=%x err=%v", j.label, code, len(body),
				time.Since(start).Milliseconds(), body[:min(len(body), 8)], err)
		})
	}
	wg.Wait()
}

// --- помощники -------------------------------------------------------------------

func resolve(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func stripQuery(s string) string {
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		return s[:i]
	}
	return s
}

func ext(rawURL, def string) string {
	if e := filepath.Ext(stripQuery(rawURL)); e != "" && len(e) <= 5 {
		return e
	}
	return def
}

func appendNew(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// overlap — сколько сегментов b встречается в a: по полному URL или по пути без query.
func overlap(a, b []string, pathOnly bool) int {
	key := func(s string) string {
		if pathOnly {
			return stripQuery(s)
		}
		return s
	}
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[key(s)] = true
	}
	n := 0
	for _, s := range b {
		if set[key(s)] {
			n++
		}
	}
	return n
}
