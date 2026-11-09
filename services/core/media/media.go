// Package media — метаданные видео медиатеки (~/.cbr/media/<имя>/meta.json).
//
// converter считает их один раз при публикации видео, api отдаёт в GET /videos
// без разбора плейлиста и ffprobe. Плейлист VOD после публикации не меняется,
// поэтому файл не устаревает; у видео без meta.json (опубликованных до него)
// api считает метаданные сам.
package media

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Имена файлов в папке видео.
const (
	PlaylistFile = "playlist.m3u8"
	MetaFile     = "meta.json"
)

// Meta — метаданные видео. Поля совпадают с ответом GET /videos (api MediaFile).
type Meta struct {
	Duration           int     `json:"duration"`             // секунды, сумма #EXTINF
	Resolution         string  `json:"resolution,omitempty"` // "1920x1080"; пусто, если не определено
	SizeMB             int     `json:"sizeMB,omitempty"`     // сегменты и init вместе
	SegmentCount       int     `json:"segmentCount"`
	AvgSegmentDuration float64 `json:"avgSegmentDuration"`
}

// Read читает meta.json из папки видео. Нет файла — fs.ErrNotExist.
func Read(dir string) (Meta, error) {
	data, err := os.ReadFile(filepath.Join(dir, MetaFile))
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, fmt.Errorf("decode %s: %w", MetaFile, err)
	}
	return m, nil
}

// Write сохраняет meta.json в папку видео: через временный файл и rename,
// чтобы api не прочитал недописанный JSON.
func Write(dir string, m Meta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode %s: %w", MetaFile, err)
	}
	path := filepath.Join(dir, MetaFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Scan считает метаданные по плейлисту в папке видео: длительность и число
// сегментов — из #EXTINF, размер — по файлам сегментов и init (#EXT-X-MAP).
// Разрешение не определяет (нужен ffprobe) — его ставит вызывающий.
func Scan(dir string) (Meta, error) {
	f, err := os.Open(filepath.Join(dir, PlaylistFile))
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()

	var (
		total    float64
		size     int64
		count    int
		pending  bool // прочитан #EXTINF, следующая строка без # — сегмент
		initSeen = map[string]bool{}
	)
	addSize := func(name string) {
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err == nil && !info.IsDir() {
			size += info.Size()
		}
	}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXTINF:"):
			dur, err := strconv.ParseFloat(strings.SplitN(strings.TrimPrefix(line, "#EXTINF:"), ",", 2)[0], 64)
			if err != nil {
				return Meta{}, fmt.Errorf("bad %q: %w", line, err)
			}
			total += dur
			pending = true
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			if uri := attribute(line, "URI"); uri != "" && !initSeen[uri] {
				initSeen[uri] = true
				addSize(uri)
			}
		case strings.HasPrefix(line, "#"):
		case pending:
			count++
			pending = false
			addSize(line)
		}
	}
	if err := sc.Err(); err != nil {
		return Meta{}, err
	}
	if count == 0 {
		return Meta{}, errors.New("playlist has no segments")
	}
	return Meta{
		Duration:           int(math.Round(total)),
		SizeMB:             int(math.Round(float64(size) / 1024 / 1024)),
		SegmentCount:       count,
		AvgSegmentDuration: total / float64(count),
	}, nil
}

// attribute возвращает значение атрибута key из строки тега (URI="init.mp4").
func attribute(line, key string) string {
	i := strings.Index(line, key+"=")
	if i < 0 {
		return ""
	}
	v := line[i+len(key)+1:]
	if strings.HasPrefix(v, `"`) {
		if end := strings.Index(v[1:], `"`); end >= 0 {
			return v[1 : end+1]
		}
		return ""
	}
	if end := strings.IndexByte(v, ','); end >= 0 {
		return v[:end]
	}
	return v
}
