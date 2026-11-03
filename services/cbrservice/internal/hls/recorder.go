// Package hls записывает живые HLS-стримы без ffmpeg: выбирает вариант с
// максимальным битрейтом, непрерывно скачивает новые сегменты, расшифровывает
// AES-128, добавляет init-сегменты fMP4 и пишет результат в part-файлы,
// которые ротируются каждый час записанного контента.
package hls

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cbrservice/internal/ctxutil"
)

// DefaultRotateAfter — длительность контента в одном part-файле.
const DefaultRotateAfter = time.Hour

// DefaultStallTimeout — сколько ждать нового записанного сегмента, прежде чем
// считать стрим завершённым. Нужен для CDN, которые после конца эфира
// продолжают отдавать застывший плейлист без #EXT-X-ENDLIST.
const DefaultStallTimeout = time.Minute

// stallTargetMultiplier: таймаут не меньше этого числа target duration, чтобы
// стримы с длинными сегментами не обрывались между обновлениями плейлиста.
const stallTargetMultiplier = 6

// Format — формат выходного файла.
type Format struct {
	Name string // mpegts | mp4
	Ext  string // ts | mp4
}

// Поддерживаемые форматы.
var (
	FormatTS  = Format{Name: "mpegts", Ext: "ts"}
	FormatMP4 = Format{Name: "mp4", Ext: "mp4"}
)

// Recorder записывает стримы в Dir.
type Recorder struct {
	Client *http.Client
	Dir    string
	// RotateAfter — длительность контента в одном part; 0 означает DefaultRotateAfter.
	RotateAfter time.Duration
	Log         *slog.Logger

	// StallTimeout — запись завершается, если столько времени не удалось записать
	// ни одного нового сегмента; 0 означает DefaultStallTimeout.
	StallTimeout time.Duration

	// retryDelay — пауза после ошибки загрузки или разбора плейлиста (по умолчанию 3s).
	retryDelay time.Duration
}

func (r *Recorder) logger() *slog.Logger {
	if r.Log == nil {
		return slog.Default()
	}
	return r.Log
}

// DetectFormat определяет расширение выходного файла по расширению URL или по
// содержимому HLS-манифеста. Для master-плейлиста проверяется media-плейлист
// лучшего варианта: признаки fMP4 (#EXT-X-MAP, .m4s) есть только в нём.
// Читается только текст плейлистов, а не медиапоток, поэтому CDN-токен
// остаётся доступным для скачивания.
func (r *Recorder) DetectFormat(ctx context.Context, rawURL string) Format {
	log := r.logger()

	// Прямая ссылка на видеофайл (расширение до query string).
	path := strings.SplitN(rawURL, "?", 2)[0]
	lower := strings.ToLower(path)
	for _, ext := range []string{".mp4", ".mov", ".m4v"} {
		if strings.HasSuffix(lower, ext) {
			log.Info("format detected", "format", FormatMP4.Name, "source", "url extension")
			return FormatMP4
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	manifest, _, err := fetchText(ctx, r.Client, rawURL)
	if err != nil {
		log.Warn("format detection failed, defaulting to ts", "err", err)
		return FormatTS
	}

	if !strings.HasPrefix(strings.TrimSpace(manifest), "#EXTM3U") {
		log.Info("format detected", "format", FormatTS.Name, "source", "not an m3u8 response")
		return FormatTS
	}

	if isMasterPlaylist(manifest) {
		mediaURL, ok := variantURL(rawURL, manifest)
		if !ok {
			log.Warn("format detection: master playlist has no usable variant, defaulting to ts")
			return FormatTS
		}
		manifest, _, err = fetchText(ctx, r.Client, mediaURL)
		if err != nil {
			log.Warn("format detection: variant playlist fetch failed, defaulting to ts", "err", err)
			return FormatTS
		}
	}

	// fMP4/CMAF HLS использует #EXT-X-MAP и сегменты .m4s/.mp4.
	if strings.Contains(manifest, "#EXT-X-MAP") ||
		strings.Contains(manifest, ".m4s") ||
		strings.Contains(manifest, ".mp4") {
		log.Info("format detected", "format", FormatMP4.Name, "source", "fMP4/CMAF HLS")
		return FormatMP4
	}

	log.Info("format detected", "format", FormatTS.Name, "source", "MPEG-TS HLS")
	return FormatTS
}

// Record записывает живой стрим masterURL в файлы <baseName>_part<N>.<ext>
// до конца стрима или отмены ctx. Отмена ctx не считается ошибкой.
func (r *Recorder) Record(ctx context.Context, masterURL, baseName string, f Format) error {
	log := r.logger().With("recording", baseName)

	mediaURL, v, err := resolveMediaPlaylist(ctx, r.Client, masterURL)
	if err != nil {
		return fmt.Errorf("resolving media playlist: %w", err)
	}
	if v.uri != "" {
		log.Info("selected variant", "bandwidth", v.bandwidth, "uri", v.uri)
	}

	rotateAfter := r.RotateAfter
	if rotateAfter <= 0 {
		rotateAfter = DefaultRotateAfter
	}
	retryDelay := r.retryDelay
	if retryDelay <= 0 {
		retryDelay = 3 * time.Second
	}

	stallTimeout := r.StallTimeout
	if stallTimeout <= 0 {
		stallTimeout = DefaultStallTimeout
	}

	keys := make(map[string][]byte) // URI ключа -> ключ
	w := &segmentWriter{
		dir:         r.Dir,
		baseName:    baseName,
		ext:         f.Ext,
		rotateAfter: rotateAfter.Seconds(),
		log:         log,
	}
	defer w.close()

	log.Info("native HLS recorder started", "playlist", mediaURL, "ext", f.Ext)

	seen := make(map[string]bool)
	errStreak := 0
	lastProgress := time.Now() // время последнего записанного сегмента
	var targetDuration float64

	for {
		if ctx.Err() != nil {
			log.Info("download stopped")
			return nil
		}

		if stalled := time.Since(lastProgress); stalled >= stallLimit(stallTimeout, targetDuration) {
			log.Warn("no new segments, assuming stream ended", "stalled", stalled.Round(time.Second))
			return nil
		}

		text, status, err := fetchText(ctx, r.Client, mediaURL)
		if err != nil {
			// 4xx (кроме 408/429) означает, что живой стрим закончился.
			if status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests {
				log.Info("playlist gone", "status", status)
				return nil
			}
			errStreak++
			if errStreak >= 5 {
				log.Warn("giving up after repeated playlist errors", "err", err)
				return nil
			}
			if ctxutil.Sleep(ctx, retryDelay) {
				return nil
			}
			continue
		}
		errStreak = 0

		pl, err := parseMediaPlaylist(text, mediaURL)
		if err != nil {
			log.Warn("parse playlist", "err", err)
			if ctxutil.Sleep(ctx, retryDelay) {
				return nil
			}
			continue
		}

		targetDuration = pl.targetDuration

		newCount := 0
		initFailed := false
		for _, seg := range pl.segments {
			if seen[seg.uri] {
				continue
			}
			if ctx.Err() != nil {
				log.Info("download stopped")
				return nil
			}

			// Init-сегмент fMP4 (общий заголовок для последующих фрагментов).
			// Без него фрагменты непроигрываемы, поэтому при ошибке сегменты не
			// пишутся и не помечаются как увиденные — повтор при следующей
			// перезагрузке плейлиста.
			if seg.mapURI != "" && seg.mapURI != w.initURI {
				initData, err := fetch(ctx, r.Client, seg.mapURI)
				if err != nil {
					log.Warn("fetch init segment", "uri", seg.mapURI, "err", err)
					initFailed = true
					break
				}
				w.setInit(seg.mapURI, initData)
			}
			seen[seg.uri] = true

			data, err := downloadSegment(ctx, r.Client, keys, seg)
			if err != nil {
				// Снимаем отметку, чтобы повторить сегмент, пока он остаётся
				// в окне живого плейлиста.
				delete(seen, seg.uri)
				log.Warn("segment download failed", "uri", seg.uri, "err", err)
				continue
			}
			if err := w.write(data, seg.duration); err != nil {
				return fmt.Errorf("writing segment: %w", err)
			}
			newCount++
			lastProgress = time.Now()
		}

		if initFailed {
			// Оставшиеся сегменты ждут init — повторяем, даже если плейлист
			// закрыт #EXT-X-ENDLIST; бесконечный повтор ограничен таймаутом застоя.
			if ctxutil.Sleep(ctx, retryDelay) {
				log.Info("download stopped")
				return nil
			}
			continue
		}

		if pl.endList {
			log.Info("stream ended (#EXT-X-ENDLIST)")
			return nil
		}

		if ctxutil.Sleep(ctx, reloadDelay(pl.targetDuration, newCount)) {
			log.Info("download stopped")
			return nil
		}
	}
}

// stallLimit — таймаут застоя с учётом target duration плейлиста.
func stallLimit(timeout time.Duration, targetDuration float64) time.Duration {
	if byTarget := time.Duration(targetDuration * stallTargetMultiplier * float64(time.Second)); byTarget > timeout {
		return byTarget
	}
	return timeout
}

// reloadDelay: по спецификации HLS плейлист перезагружается через target
// duration; если новых сегментов не было — через половину, чтобы догнать быстрее.
func reloadDelay(targetDuration float64, newSegments int) time.Duration {
	reload := time.Duration(targetDuration * float64(time.Second))
	if reload <= 0 {
		reload = 4 * time.Second
	}
	if newSegments == 0 {
		reload /= 2
	}
	if reload < time.Second {
		reload = time.Second
	}
	return reload
}

// downloadSegment скачивает и при необходимости расшифровывает сегмент; ключи кешируются в keys.
func downloadSegment(ctx context.Context, client *http.Client, keys map[string][]byte, seg playlistSegment) ([]byte, error) {
	data, err := fetch(ctx, client, seg.uri)
	if err != nil {
		return nil, err
	}

	if seg.key == nil || seg.key.method == "" || seg.key.method == "NONE" {
		return data, nil
	}
	if seg.key.method != "AES-128" {
		return nil, fmt.Errorf("unsupported encryption method %q", seg.key.method)
	}

	key, ok := keys[seg.key.uri]
	if !ok {
		key, err = fetch(ctx, client, seg.key.uri)
		if err != nil {
			return nil, fmt.Errorf("fetching key: %w", err)
		}
		keys[seg.key.uri] = key
	}

	iv := seg.key.iv
	if len(iv) == 0 {
		iv = seqToIV(seg.seq)
	}
	return decryptAES128(data, key, iv)
}

// fetch загружает тело ответа целиком; не-2xx считается ошибкой.
func fetch(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// fetchText загружает текст (до 4 МБ) и возвращает HTTP-статус для анализа ошибок.
func fetchText(ctx context.Context, client *http.Client, rawURL string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return string(body), resp.StatusCode, nil
}
