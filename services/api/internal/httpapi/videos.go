package httpapi

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v3"

	"cbr/api/internal/library"
)

// MediaFile — элемент ответа GET /videos.
type MediaFile struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	HLSURL             string  `json:"hlsURL"`
	CreatedAt          string  `json:"createdAt,omitempty"`
	Duration           int     `json:"duration"`
	Resolution         string  `json:"resolution,omitempty"`
	SizeMB             int     `json:"sizeMB,omitempty"`
	SegmentCount       int     `json:"segmentCount"`
	AvgSegmentDuration float64 `json:"avgSegmentDuration"`
}

// contentTypes — Content-Type по расширению; остальное отдаётся как octet-stream.
var contentTypes = map[string]string{
	".m3u8": "application/vnd.apple.mpegurl",
	".ts":   "video/MP2T",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".mp4":  "video/mp4",
	".m4s":  "video/iso.segment",
	".vtt":  "text/vtt",
	".zip":  "application/zip",
}

// param возвращает параметр маршрута с раскодированными %XX: fiber отдаёт их
// как есть, а клиенты экранируют пробелы и не-ASCII символы в имени папки.
func param(c fiber.Ctx, key string) (string, bool) {
	v, err := url.PathUnescape(c.Params(key))
	return v, err == nil
}

// videoName возвращает имя папки видео из параметра маршрута или false, если
// имя недопустимо (пустое, ".", "..", скрытое).
func videoName(c fiber.Ctx) (string, bool) {
	raw, ok := param(c, "videoname")
	if !ok {
		return "", false
	}
	name := filepath.Base(raw)
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return "", false
	}
	return name, true
}

func jsonError(c fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}

// listVideos — GET /videos: все папки медиатеки, в которых есть playlist.m3u8.
func listVideos(mediaDir string) fiber.Handler {
	return func(c fiber.Ctx) error {
		entries, err := os.ReadDir(mediaDir)
		if err != nil {
			return jsonError(c, fiber.StatusInternalServerError, err.Error())
		}

		files := make([]MediaFile, 0, len(entries))
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}

			info := library.NewMediaInfo(mediaDir, entry.Name())
			meta, ok := info.Meta()
			if !ok {
				continue
			}

			files = append(files, MediaFile{
				ID:                 info.ID(),
				Name:               entry.Name(),
				HLSURL:             info.StreamURL(),
				CreatedAt:          info.CreatedAt(),
				Duration:           meta.Duration,
				Resolution:         meta.Resolution,
				SizeMB:             meta.SizeMB,
				SegmentCount:       meta.SegmentCount,
				AvgSegmentDuration: meta.AvgSegmentDuration,
			})
		}

		return c.JSON(files)
	}
}

// streamFile — GET /videos/:videoname/*: файл внутри папки видео.
// Путь не может выйти за пределы папки видео.
func streamFile(mediaDir string) fiber.Handler {
	return func(c fiber.Ctx) error {
		name, ok := videoName(c)
		if !ok {
			return jsonError(c, fiber.StatusForbidden, "invalid path")
		}

		sub, ok := param(c, "*")
		if !ok {
			return jsonError(c, fiber.StatusForbidden, "invalid path")
		}
		videoDir := filepath.Join(mediaDir, name)
		fullPath := filepath.Join(videoDir, sub)
		if rel, err := filepath.Rel(videoDir, fullPath); err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return jsonError(c, fiber.StatusForbidden, "invalid path")
		}

		if info, err := os.Stat(fullPath); err != nil || info.IsDir() {
			return jsonError(c, fiber.StatusNotFound, "file not found")
		}

		contentType, ok := contentTypes[strings.ToLower(filepath.Ext(fullPath))]
		if !ok {
			contentType = fiber.MIMEOctetStream
		}
		c.Set(fiber.HeaderContentType, contentType)

		return c.SendFile(fullPath)
	}
}

// deleteVideo — DELETE /videos/:videoname: удаляет папку видео целиком.
func deleteVideo(mediaDir string, log *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		name, ok := videoName(c)
		if !ok {
			return jsonError(c, fiber.StatusForbidden, "invalid path")
		}

		fullPath := filepath.Join(mediaDir, name)
		if _, err := os.Stat(fullPath); errors.Is(err, fs.ErrNotExist) {
			return jsonError(c, fiber.StatusNotFound, "video not found")
		}
		if err := os.RemoveAll(fullPath); err != nil {
			log.Error("delete video", "path", fullPath, "err", err)
			return jsonError(c, fiber.StatusInternalServerError, "failed to delete")
		}
		log.Info("video deleted", "name", name)

		return c.JSON(fiber.Map{"message": "deleted"})
	}
}
