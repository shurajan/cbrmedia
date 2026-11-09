// Package converter переносит завершённые записи из recordings/ в медиатеку
// media/ в виде HLS без перекодирования (ffmpeg -c copy) и удаляет исходники.
//
// Завершённой считается запись с расширением .ts или .mp4: пока рекордер пишет
// part, у файла суффикс .download, и в .ts/.mp4 он переименовывается только
// после закрытия.
//
// Видео собирается во временной скрытой папке media/.convert-<name> (API
// скрывает папки с точкой) и публикуется атомарным переименованием, поэтому
// плеер никогда не видит недорезанный плейлист. Исходник удаляется, когда видео
// во временной папке готово и проверено, но до публикации: тогда падение службы
// в любой момент не даёт дубликата — на следующем запуске cleanupTemp публикует
// готовую временную папку, у которой исходника уже нет, а недоделанную или ту,
// чей исходник ещё на месте, удаляет (запись преобразуется заново). Запись,
// которую не удалось преобразовать, переносится в recordings/failed/, чтобы не
// пробовать её снова при каждом проходе.
package converter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"cbr/core/ctxutil"
	"cbr/core/media"
)

// Значения по умолчанию.
const (
	DefaultInterval   = 5 * time.Minute
	DefaultMinAge     = time.Minute
	DefaultSegmentSec = 6

	// FailedDir — подпапка recordings/ для записей, которые не удалось преобразовать.
	FailedDir = "failed"

	tmpPrefix    = ".convert-"
	playlistName = "playlist.m3u8"
	stderrLimit  = 4 << 10
)

// Converter переносит записи из Recordings в Media.
type Converter struct {
	Recordings string
	Media      string
	FFmpeg     string        // путь к ffmpeg; пусто — "ffmpeg" из PATH
	FFprobe    string        // путь к ffprobe для разрешения в meta.json; пусто — разрешение не определяется
	MinAge     time.Duration // не трогать файлы моложе; защита от файлов, которые ещё копируются вручную
	SegmentSec int           // целевая длительность HLS-сегмента; 0 — DefaultSegmentSec
	Log        *slog.Logger
}

// Run обрабатывает записи сразу и затем каждые interval до отмены ctx.
func (c *Converter) Run(ctx context.Context, interval time.Duration) {
	for {
		c.RunOnce(ctx)
		if ctxutil.Sleep(ctx, interval) {
			return
		}
	}
}

// RunOnce выполняет один проход: разбирает временные папки прерванных запусков
// (готовые публикует, остальные удаляет) и преобразует все завершённые записи.
// Возвращает число опубликованных видео.
func (c *Converter) RunOnce(ctx context.Context) int {
	published := c.cleanupTemp()

	files, err := c.pending(time.Now())
	if err != nil {
		c.Log.Error("scan recordings", "dir", c.Recordings, "err", err)
		return 0
	}
	if len(files) > 0 {
		c.Log.Info("recordings to convert", "count", len(files))
	}

	for _, src := range files {
		if ctx.Err() != nil {
			break
		}
		if c.convert(ctx, src) {
			published++
		}
	}
	return published
}

// pending возвращает завершённые записи (верхний уровень Recordings), отсортированные по имени.
func (c *Converter) pending(now time.Time) ([]string, error) {
	entries, err := os.ReadDir(c.Recordings)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || strings.HasPrefix(name, ".") || formatOf(name) == "" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // файл исчез между ReadDir и Info
		}
		if now.Sub(info.ModTime()) < c.MinAge {
			continue
		}
		files = append(files, filepath.Join(c.Recordings, name))
	}
	slices.Sort(files)
	return files, nil
}

// formatOf возвращает тип HLS-сегментов для записи или "", если файл не запись.
func formatOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ts":
		return "mpegts"
	case ".mp4":
		return "fmp4"
	default:
		return ""
	}
}

// convert преобразует одну запись. Возвращает true, если видео опубликовано.
func (c *Converter) convert(ctx context.Context, src string) bool {
	base := filepath.Base(src)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	log := c.Log.With("recording", base)

	tmp := filepath.Join(c.Media, tmpPrefix+name)
	if err := os.RemoveAll(tmp); err != nil {
		log.Error("clean temp dir", "path", tmp, "err", err)
		return false
	}
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		log.Error("create temp dir", "path", tmp, "err", err)
		return false
	}

	started := time.Now()
	log.Info("converting")
	err := c.remux(ctx, src, tmp)
	if err == nil {
		err = validatePlaylist(filepath.Join(tmp, playlistName))
	}
	if err == nil {
		// meta.json — для GET /videos без разбора плейлиста; без него api посчитает сам.
		if merr := c.writeMeta(ctx, src, tmp); merr != nil {
			log.Warn("write meta.json", "err", merr)
		}
	}
	if err != nil {
		removeAll(log, tmp)
		if ctx.Err() != nil {
			log.Info("conversion interrupted, recording kept")
			return false
		}
		log.Error("conversion failed", "err", err)
		c.moveToFailed(log, src)
		return false
	}

	// Сначала исходник, потом публикация: см. комментарий к пакету. Если исходник
	// удалить не удалось, видео всё равно публикуется — при следующем проходе он
	// даст дубликат с суффиксом, поэтому ошибка важна.
	if err := os.Remove(src); err != nil {
		log.Error("remove converted recording", "err", err)
	}
	dest, err := c.publish(tmp, name)
	if err != nil {
		log.Error("publish video", "err", err)
		removeAll(log, tmp)
		return false
	}
	log.Info("video published", "video", filepath.Base(dest), "took", time.Since(started).Round(time.Second))
	return true
}

// publish переносит готовую временную папку в медиатеку под свободным именем.
func (c *Converter) publish(tmp, name string) (string, error) {
	dest, err := freeName(c.Media, name, "")
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// remux нарезает src в HLS в папке dir без перекодирования.
func (c *Converter) remux(ctx context.Context, src, dir string) error {
	ffmpeg := c.FFmpeg
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	cmd := exec.CommandContext(ctx, ffmpeg, ffmpegArgs(src, dir, c.segmentSec())...)
	cmd.WaitDelay = 5 * time.Second
	stderr := &tailBuffer{limit: stderrLimit}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("ffmpeg: %w: %s", err, msg)
		}
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}

func (c *Converter) segmentSec() int {
	if c.SegmentSec > 0 {
		return c.SegmentSec
	}
	return DefaultSegmentSec
}

// ffmpegArgs — аргументы ffmpeg для переупаковки в VOD-HLS.
// Берутся только видео и аудио: живые стримы часто несут дорожку данных
// (timed ID3), которую HLS-муксер не принимает.
func ffmpegArgs(src, dir string, segmentSec int) []string {
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-fflags", "+genpts",
		"-i", src,
		"-map", "0:v?", "-map", "0:a?",
		"-c", "copy",
		"-f", "hls",
		"-hls_time", strconv.Itoa(segmentSec),
		"-hls_playlist_type", "vod",
		"-hls_list_size", "0",
	}
	if formatOf(src) == "fmp4" {
		args = append(args,
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", "init.mp4",
			"-hls_segment_filename", filepath.Join(dir, "%05d.m4s"),
		)
	} else {
		args = append(args,
			"-hls_segment_type", "mpegts",
			"-hls_segment_filename", filepath.Join(dir, "%05d.ts"),
		)
	}
	return append(args, filepath.Join(dir, playlistName))
}

// writeMeta считает метаданные готового видео в dir и сохраняет их в meta.json.
// Разрешение берётся ffprobe'ом из исходника src (он цельный, в отличие от
// сегментов fMP4); без FFprobe или при его ошибке остаётся пустым.
func (c *Converter) writeMeta(ctx context.Context, src, dir string) error {
	m, err := media.Scan(dir)
	if err != nil {
		return err
	}
	if c.FFprobe != "" {
		m.Resolution = probeResolution(ctx, c.FFprobe, src)
	}
	return media.Write(dir, m)
}

// probeTimeout — предел одного запуска ffprobe.
const probeTimeout = 30 * time.Second

// probeResolution возвращает "ШИРИНАxВЫСОТА" первой видеодорожки файла или "".
func probeResolution(ctx context.Context, ffprobe, path string) string {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		path,
	)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// validatePlaylist проверяет, что ffmpeg дописал плейлист до конца.
func validatePlaylist(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}
	text := string(data)
	if !strings.Contains(text, "#EXTINF:") {
		return errors.New("playlist has no segments")
	}
	if !strings.Contains(text, "#EXT-X-ENDLIST") {
		return errors.New("playlist is not finalized (no #EXT-X-ENDLIST)")
	}
	return nil
}

// freeName возвращает путь dir/<name><ext> или, если он занят,
// dir/<name>_2<ext>, dir/<name>_3<ext>…
func freeName(dir, name, ext string) (string, error) {
	for i := 1; ; i++ {
		candidate := name + ext
		if i > 1 {
			candidate = fmt.Sprintf("%s_%d%s", name, i, ext)
		}
		path := filepath.Join(dir, candidate)
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
}

// moveToFailed убирает запись из очереди, сохраняя её для ручного разбора.
func (c *Converter) moveToFailed(log *slog.Logger, src string) {
	dir := filepath.Join(c.Recordings, FailedDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		log.Error("create failed dir", "err", err)
		return
	}
	base := filepath.Base(src)
	ext := filepath.Ext(base)
	dest, err := freeName(dir, strings.TrimSuffix(base, ext), ext)
	if err != nil {
		log.Error("move to failed", "err", err)
		return
	}
	if err := os.Rename(src, dest); err != nil {
		log.Error("move to failed", "err", err)
		return
	}
	log.Warn("recording moved to failed", "path", dest)
}

// cleanupTemp удаляет временные папки, оставшиеся после прерванного запуска.
// cleanupTemp разбирает временные папки, оставшиеся от прерванного запуска:
// готовое видео (плейлист дописан до конца), исходник которого уже удалён,
// публикует — служба упала между удалением исходника и публикацией; остальные
// удаляет: недоделанные, и те, чей исходник на месте, — он будет преобразован
// заново. Возвращает число опубликованных видео.
func (c *Converter) cleanupTemp() int {
	entries, err := os.ReadDir(c.Media)
	if err != nil {
		c.Log.Error("scan media", "dir", c.Media, "err", err)
		return 0
	}
	published := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), tmpPrefix) {
			continue
		}
		tmp := filepath.Join(c.Media, e.Name())
		name := strings.TrimPrefix(e.Name(), tmpPrefix)
		if c.hasSource(name) || validatePlaylist(filepath.Join(tmp, playlistName)) != nil {
			removeAll(c.Log, tmp)
			continue
		}
		dest, err := c.publish(tmp, name)
		if err != nil {
			c.Log.Error("publish video left by an interrupted run", "path", tmp, "err", err)
			continue
		}
		c.Log.Info("video published (left by an interrupted run)", "video", filepath.Base(dest))
		published++
	}
	return published
}

// hasSource сообщает, лежит ли в Recordings запись с таким именем (любого формата).
func (c *Converter) hasSource(name string) bool {
	for _, ext := range []string{".ts", ".mp4"} {
		if _, err := os.Lstat(filepath.Join(c.Recordings, name+ext)); err == nil {
			return true
		}
	}
	return false
}

func removeAll(log *slog.Logger, path string) {
	if err := os.RemoveAll(path); err != nil {
		log.Error("remove temp dir", "path", path, "err", err)
	}
}

// tailBuffer хранит последние limit байт вывода (stderr ffmpeg может быть большим).
type tailBuffer struct {
	limit int
	buf   bytes.Buffer
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	t.buf.Write(p)
	if extra := t.buf.Len() - t.limit; extra > 0 {
		t.buf.Next(extra)
	}
	return n, nil
}

func (t *tailBuffer) String() string { return t.buf.String() }
