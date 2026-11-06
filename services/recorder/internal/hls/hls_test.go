package hls

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestParseAttributes(t *testing.T) {
	got := parseAttributes(`METHOD=AES-128,URI="https://k.example/key?a=1,b=2",IV=0x0A`)
	want := map[string]string{
		"METHOD": "AES-128",
		"URI":    "https://k.example/key?a=1,b=2",
		"IV":     "0x0A",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseMediaPlaylist(t *testing.T) {
	text := `#EXTM3U
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:100
#EXT-X-MAP:URI="init.mp4"
#EXT-X-KEY:METHOD=AES-128,URI="key.bin",IV=0x000102030405060708090a0b0c0d0e0f
#EXTINF:5.5,
seg100.m4s
#EXTINF:6.0,title
https://cdn.example/abs/seg101.m4s
#EXT-X-ENDLIST
`
	pl, err := parseMediaPlaylist(text, "https://cdn.example/live/media.m3u8?token=x")
	if err != nil {
		t.Fatal(err)
	}
	if pl.targetDuration != 6 || !pl.endList {
		t.Errorf("targetDuration=%v endList=%v", pl.targetDuration, pl.endList)
	}
	if len(pl.segments) != 2 {
		t.Fatalf("segments = %d", len(pl.segments))
	}
	for i, seg := range pl.segments {
		if seg.mapURI != "https://cdn.example/live/init.mp4" {
			t.Errorf("segment %d mapURI = %q", i, seg.mapURI)
		}
	}
	s0, s1 := pl.segments[0], pl.segments[1]
	if s0.uri != "https://cdn.example/live/seg100.m4s" || s0.duration != 5.5 || s0.seq != 100 {
		t.Errorf("segment 0 = %+v", s0)
	}
	if s1.uri != "https://cdn.example/abs/seg101.m4s" || s1.seq != 101 {
		t.Errorf("segment 1 = %+v", s1)
	}
	if s0.key == nil || s0.key.method != "AES-128" || s0.key.uri != "https://cdn.example/live/key.bin" || len(s0.key.iv) != 16 {
		t.Errorf("key = %+v", s0.key)
	}
}

func TestBestVariantPicksHighestBandwidth(t *testing.T) {
	text := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1920x1080
high.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1500000
mid.m3u8
`
	v, ok := bestVariant(text)
	if !ok || v.uri != "high.m3u8" || v.bandwidth != 3000000 {
		t.Errorf("bestVariant = %+v, %v", v, ok)
	}
	if _, ok := bestVariant("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\n"); ok {
		t.Error("variant without URI must not be selected")
	}
}

func encryptAES128(t *testing.T, plain, key, iv []byte) []byte {
	t.Helper()
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out
}

func TestDecryptAES128RoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := seqToIV(7)
	plain := []byte("segment payload of arbitrary length")

	got, err := decryptAES128(encryptAES128(t, plain, key, iv), key, iv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("decrypted = %q", got)
	}
}

func TestDecryptAES128Errors(t *testing.T) {
	iv := make([]byte, 16)
	if _, err := decryptAES128(make([]byte, 16), []byte("short"), iv); err == nil {
		t.Error("expected error for short key")
	}
	if _, err := decryptAES128(make([]byte, 15), make([]byte, 16), iv); err == nil {
		t.Error("expected error for non-block-aligned data")
	}
}

func TestSeqToIV(t *testing.T) {
	iv := seqToIV(0x0102)
	want := append(make([]byte, 14), 0x01, 0x02)
	if !bytes.Equal(iv, want) {
		t.Errorf("seqToIV = %x", iv)
	}
}

func TestPKCS7Unpad(t *testing.T) {
	if _, err := pkcs7Unpad([]byte{1, 2, 3, 0}); err == nil {
		t.Error("zero padding must be rejected")
	}
	if _, err := pkcs7Unpad([]byte{1, 3, 2, 2}); err != nil {
		t.Errorf("valid padding rejected: %v", err)
	}
	if _, err := pkcs7Unpad([]byte{1, 2, 3, 2}); err == nil {
		t.Error("inconsistent padding bytes must be rejected")
	}
}

func TestReloadDelay(t *testing.T) {
	cases := []struct {
		target float64
		newSeg int
		want   time.Duration
	}{
		{6, 3, 6 * time.Second},
		{6, 0, 3 * time.Second},
		{0, 1, 4 * time.Second},
		{1, 0, time.Second},
	}
	for _, c := range cases {
		if got := reloadDelay(c.target, c.newSeg); got != c.want {
			t.Errorf("reloadDelay(%v, %d) = %v, want %v", c.target, c.newSeg, got, c.want)
		}
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestSegmentWriterRotatesAndFinalizes(t *testing.T) {
	dir := t.TempDir()
	w := &segmentWriter{dir: dir, baseName: "rec", ext: "mp4", rotateAfter: 10, log: discard}
	w.setInit("init.mp4", []byte("INIT"))

	for _, chunk := range []string{"a", "b", "c"} {
		if err := w.write([]byte(chunk), 6); err != nil {
			t.Fatal(err)
		}
	}
	// Пока part открыт, он лежит с суффиксом .download.
	if got := listDir(t, dir); fmt.Sprint(got) != "[rec_part1.mp4 rec_part2.mp4.download]" {
		t.Errorf("files while recording = %v", got)
	}
	w.close()

	if got := listDir(t, dir); fmt.Sprint(got) != "[rec_part1.mp4 rec_part2.mp4]" {
		t.Errorf("files after close = %v", got)
	}
	p1, _ := os.ReadFile(filepath.Join(dir, "rec_part1.mp4"))
	p2, _ := os.ReadFile(filepath.Join(dir, "rec_part2.mp4"))
	if string(p1) != "INITab" || string(p2) != "INITc" {
		t.Errorf("part1=%q part2=%q", p1, p2)
	}
}

func TestRecoverPartial(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a_part1.ts.download", "partial")
	write("b_part1.ts", "final")
	write("b_part1.ts.download", "stale")
	write("note.txt", "x")

	RecoverPartial(dir, discard)

	if got := listDir(t, dir); fmt.Sprint(got) != "[a_part1.ts b_part1.ts note.txt]" {
		t.Errorf("files = %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b_part1.ts")); string(b) != "final" {
		t.Errorf("existing file overwritten: %q", b)
	}
}

// fakeStream отдаёт master → media-плейлист с двумя зашифрованными сегментами и ENDLIST.
func fakeStream(t *testing.T) *httptest.Server {
	t.Helper()
	key := []byte("0123456789abcdef")
	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=10\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=99\nhigh.m3u8\n")
	})
	mux.HandleFunc("/high.m3u8", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:5\n"+
			"#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n"+
			"#EXTINF:1,\ns5.ts\n#EXTINF:1,\ns6.ts\n#EXT-X-ENDLIST\n")
	})
	mux.HandleFunc("/key.bin", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(key) })
	mux.HandleFunc("/s5.ts", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(encryptAES128(t, []byte("FIVE"), key, seqToIV(5)))
	})
	mux.HandleFunc("/s6.ts", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(encryptAES128(t, []byte("SIX"), key, seqToIV(6)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRecordEndToEnd(t *testing.T) {
	srv := fakeStream(t)
	dir := t.TempDir()
	r := &Recorder{Client: srv.Client(), Dir: dir, Log: discard}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if f := r.DetectFormat(ctx, srv.URL+"/master.m3u8"); f != FormatTS {
		t.Errorf("DetectFormat = %+v, want ts", f)
	}
	if err := r.Record(ctx, srv.URL+"/master.m3u8", "user_2026", FormatTS); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "user_2026_part1.ts"))
	if err != nil {
		t.Fatalf("recording not finalized: %v (files: %v)", err, listDir(t, dir))
	}
	if string(got) != "FIVESIX" {
		t.Errorf("recording = %q", got)
	}
}

func TestRecordStopsWhenPlaylistGone(t *testing.T) {
	// Первый запрос отдаёт media-плейлист без сегментов, следующие — 404 (стрим закончился).
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	r := &Recorder{Client: srv.Client(), Dir: t.TempDir(), Log: discard}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Стрим закончился, не дав ни одного сегмента, — для recorder это неудачная попытка.
	if err := r.Record(ctx, srv.URL+"/media.m3u8", "x", FormatTS); !errors.Is(err, ErrNoSegments) {
		t.Fatalf("Record returned %v, want ErrNoSegments", err)
	}
	if ctx.Err() != nil {
		t.Fatal("Record did not stop on 404")
	}
}

func TestDetectFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fmp4.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n")
		case "/plain":
			fmt.Fprint(w, "not a playlist")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := &Recorder{Client: srv.Client(), Log: discard}
	ctx := context.Background()

	cases := map[string]Format{
		srv.URL + "/video.MP4?token=1": FormatMP4,
		srv.URL + "/fmp4.m3u8":         FormatMP4,
		srv.URL + "/plain":             FormatTS,
		srv.URL + "/missing.m3u8":      FormatTS,
	}
	for url, want := range cases {
		if got := r.DetectFormat(ctx, url); got != want {
			t.Errorf("DetectFormat(%s) = %+v, want %+v", url, got, want)
		}
	}
}

func TestSegmentWriterInitChange(t *testing.T) {
	dir := t.TempDir()
	w := &segmentWriter{dir: dir, baseName: "rec", ext: "mp4", rotateAfter: 3600, log: discard}

	w.setInit("init.mp4?token=1", []byte("I1"))
	_ = w.write([]byte("a"), 1)
	// Тот же init под новым URI (свежий токен) — part не меняется.
	w.setInit("init.mp4?token=2", []byte("I1"))
	_ = w.write([]byte("b"), 1)
	// Другой init — следующий сегмент начинает новый part.
	w.setInit("init2.mp4", []byte("I2"))
	_ = w.write([]byte("c"), 1)
	w.close()

	p1, _ := os.ReadFile(filepath.Join(dir, "rec_part1.mp4"))
	p2, _ := os.ReadFile(filepath.Join(dir, "rec_part2.mp4"))
	if string(p1) != "I1ab" || string(p2) != "I2c" {
		t.Errorf("part1=%q part2=%q (files: %v)", p1, p2, listDir(t, dir))
	}
}

func TestStallLimit(t *testing.T) {
	if got := stallLimit(time.Minute, 2); got != time.Minute {
		t.Errorf("stallLimit(1m, 2s) = %v, want 1m", got)
	}
	if got := stallLimit(time.Minute, 20); got != 2*time.Minute {
		t.Errorf("stallLimit(1m, 20s) = %v, want 2m", got)
	}
}

func TestRecordStopsOnStalledPlaylist(t *testing.T) {
	// CDN продолжает отдавать один и тот же плейлист без #EXT-X-ENDLIST.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\ns1.ts\n")
		case "/s1.ts":
			fmt.Fprint(w, "ONE")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	r := &Recorder{Client: srv.Client(), Dir: dir, Log: discard, StallTimeout: 200 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := r.Record(ctx, srv.URL+"/media.m3u8", "stall", FormatTS); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("Record did not stop on a stalled playlist")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "stall_part1.ts")); string(got) != "ONE" {
		t.Errorf("recording = %q (files: %v)", got, listDir(t, dir))
	}
}

func TestRecordWaitsForInitSegment(t *testing.T) {
	// Первые запросы init-сегмента падают; сегменты не должны попасть в файл без него.
	var initCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MAP:URI=\"init.mp4\"\n"+
				"#EXTINF:1,\ns1.m4s\n#EXTINF:1,\ns2.m4s\n#EXT-X-ENDLIST\n")
		case "/init.mp4":
			if initCalls.Add(1) <= 2 {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, "INIT")
		case "/s1.m4s":
			fmt.Fprint(w, "A")
		case "/s2.m4s":
			fmt.Fprint(w, "B")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	r := &Recorder{Client: srv.Client(), Dir: dir, Log: discard, retryDelay: 10 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := r.Record(ctx, srv.URL+"/media.m3u8", "rec", FormatMP4); err != nil {
		t.Fatal(err)
	}
	if got := listDir(t, dir); fmt.Sprint(got) != "[rec_part1.mp4]" {
		t.Fatalf("files = %v", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "rec_part1.mp4")); string(got) != "INITAB" {
		t.Errorf("recording = %q, want INITAB", got)
	}
}

func TestRecordStartsNewPartWhenInitChanges(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MAP:URI=\"i1.mp4\"\n#EXTINF:1,\na.m4s\n"+
				"#EXT-X-DISCONTINUITY\n#EXT-X-MAP:URI=\"i2.mp4\"\n#EXTINF:1,\nb.m4s\n#EXT-X-ENDLIST\n")
		case "/i1.mp4":
			fmt.Fprint(w, "I1")
		case "/i2.mp4":
			fmt.Fprint(w, "I2")
		case "/a.m4s":
			fmt.Fprint(w, "a")
		case "/b.m4s":
			fmt.Fprint(w, "b")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	r := &Recorder{Client: srv.Client(), Dir: dir, Log: discard}
	if err := r.Record(context.Background(), srv.URL+"/media.m3u8", "rec", FormatMP4); err != nil {
		t.Fatal(err)
	}
	p1, _ := os.ReadFile(filepath.Join(dir, "rec_part1.mp4"))
	p2, _ := os.ReadFile(filepath.Join(dir, "rec_part2.mp4"))
	if string(p1) != "I1a" || string(p2) != "I2b" {
		t.Errorf("part1=%q part2=%q (files: %v)", p1, p2, listDir(t, dir))
	}
}

func TestDetectFormatFollowsMasterVariant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cmaf/master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=9\nhigh.m3u8\n")
		case "/cmaf/high.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:1,\ns.m4s\n")
		case "/ts/master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=9\nhigh.m3u8\n")
		case "/ts/high.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\ns.ts\n")
		case "/novariant/master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=9\n")
		case "/broken/master.m3u8": // вариант отвечает 404
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=9\nhigh.m3u8\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := &Recorder{Client: srv.Client(), Log: discard}

	cases := map[string]Format{
		"/cmaf/master.m3u8":      FormatMP4,
		"/ts/master.m3u8":        FormatTS,
		"/novariant/master.m3u8": FormatTS,
		"/broken/master.m3u8":    FormatTS,
	}
	for path, want := range cases {
		if got := r.DetectFormat(context.Background(), srv.URL+path); got != want {
			t.Errorf("DetectFormat(%s) = %+v, want %+v", path, got, want)
		}
	}
}

func TestSegmentWriterSkipsExistingParts(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"rec_part1.ts":          "old1",
		"rec_part2.ts.download": "old2",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	w := &segmentWriter{dir: dir, baseName: "rec", ext: "ts", rotateAfter: 3600, log: discard}
	if err := w.write([]byte("new"), 1); err != nil {
		t.Fatal(err)
	}
	w.close()

	if got := listDir(t, dir); fmt.Sprint(got) != "[rec_part1.ts rec_part2.ts.download rec_part3.ts]" {
		t.Fatalf("files = %v", got)
	}
	for name, want := range map[string]string{
		"rec_part1.ts":          "old1",
		"rec_part2.ts.download": "old2",
		"rec_part3.ts":          "new",
	} {
		if got, _ := os.ReadFile(filepath.Join(dir, name)); string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestRecordSameBaseNameDoesNotOverwrite(t *testing.T) {
	// Запись оборвалась и началась снова в ту же минуту — то же baseName.
	srv := fakeStream(t)
	dir := t.TempDir()
	r := &Recorder{Client: srv.Client(), Dir: dir, Log: discard}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for range 2 {
		if err := r.Record(ctx, srv.URL+"/master.m3u8", "user_2026", FormatTS); err != nil {
			t.Fatal(err)
		}
	}

	if got := listDir(t, dir); fmt.Sprint(got) != "[user_2026_part1.ts user_2026_part2.ts]" {
		t.Fatalf("files = %v", got)
	}
	for _, name := range []string{"user_2026_part1.ts", "user_2026_part2.ts"} {
		if got, _ := os.ReadFile(filepath.Join(dir, name)); string(got) != "FIVESIX" {
			t.Errorf("%s = %q", name, got)
		}
	}
}

// TestPartFileIsPrivate: part-файлы записи доступны только владельцу.
func TestPartFileIsPrivate(t *testing.T) {
	w := &segmentWriter{dir: t.TempDir(), baseName: "alice_2026-01-01_00-00", ext: "ts", log: discard}
	f, err := w.createNextPart()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("part file mode = %o, want no group/other access", perm)
	}
}

// stalledBody начинает ответ и не дописывает его, пока клиент не отключится.
func stalledBody(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\nseg")
	w.(http.Flusher).Flush()
	<-r.Context().Done()
}

func TestFetchTimesOutOnStalledBody(t *testing.T) {
	defer func(p, s time.Duration) { playlistTimeout, segmentTimeout = p, s }(playlistTimeout, segmentTimeout)
	playlistTimeout, segmentTimeout = 100*time.Millisecond, 100*time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(stalledBody))
	defer srv.Close()
	ctx := context.Background()

	start := time.Now()
	if _, err := fetch(ctx, srv.Client(), srv.URL); err == nil {
		t.Error("fetch: expected error for a stalled body")
	}
	// Оборванный плейлист нельзя разбирать: последняя строка — обрезанный URI сегмента.
	if text, _, err := fetchText(ctx, srv.Client(), srv.URL); err == nil {
		t.Errorf("fetchText: expected error, got %q", text)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v, timeouts ignored", took)
	}
}

// orderedStream отдаёт плейлист s1, s2 (на третьей перезагрузке — с ENDLIST);
// первые s1Failures запросов s1 падают.
func orderedStream(t *testing.T, s1Failures int32) *httptest.Server {
	t.Helper()
	var s1Calls, reloads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media.m3u8":
			end := ""
			if reloads.Add(1) > s1Failures+1 {
				end = "#EXT-X-ENDLIST\n"
			}
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\ns1.ts\n#EXTINF:1,\ns2.ts\n"+end)
		case "/s1.ts":
			if s1Calls.Add(1) <= s1Failures {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprint(w, "A")
		case "/s2.ts":
			fmt.Fprint(w, "B")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRecordKeepsSegmentOrderOnRetry(t *testing.T) {
	srv := orderedStream(t, 1)
	dir := t.TempDir()
	r := &Recorder{Client: srv.Client(), Dir: dir, Log: discard, retryDelay: 10 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Record(ctx, srv.URL+"/media.m3u8", "order", FormatTS); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "order_part1.ts")); string(got) != "AB" {
		t.Errorf("recording = %q, want segments in playlist order", got)
	}
}

func TestRecordSkipsSegmentAfterRepeatedFailures(t *testing.T) {
	srv := orderedStream(t, maxSegmentAttempts)
	dir := t.TempDir()
	r := &Recorder{Client: srv.Client(), Dir: dir, Log: discard, retryDelay: 10 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Record(ctx, srv.URL+"/media.m3u8", "skip", FormatTS); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "skip_part1.ts")); string(got) != "B" {
		t.Errorf("recording = %q, want the broken segment skipped", got)
	}
}
