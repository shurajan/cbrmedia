package library

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/grafov/m3u8"
)

// Playlist — HLS-плейлист видео. Метаданные вычисляются лениво и кешируются.
type Playlist struct {
	Path   string
	cached *cachedInfo
}

type cachedInfo struct {
	duration      int
	sizeMB        int
	resolution    string
	segmentCount  int
	avgSegmentDur float64
	loaded        bool
}

// ID — идентификатор по пути и содержимому плейлиста.
func (p *Playlist) ID() string {
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return simpleID(p.Path)
	}
	combined := p.Path + string(data)
	return simpleID(combined)
}

// Name — имя файла плейлиста.
func (p *Playlist) Name() string {
	return filepath.Base(p.Path)
}

// Duration — длительность в секундах: сумма длительностей сегментов.
func (p *Playlist) Duration() int {
	p.ensureCached()
	return p.cached.duration
}

// SizeMB — суммарный размер сегментов в мегабайтах.
func (p *Playlist) SizeMB() int {
	p.ensureCached()
	return p.cached.sizeMB
}

// Resolution — разрешение из master-плейлиста или через ffprobe первого сегмента
// (один раз на видео, см. resolutionCache), например "1920x1080".
func (p *Playlist) Resolution() string {
	p.ensureCached()
	return p.cached.resolution
}

// SegmentCount — количество сегментов.
func (p *Playlist) SegmentCount() int {
	p.ensureCached()
	return p.cached.segmentCount
}

// AvgSegmentDuration — средняя длительность сегмента в секундах.
func (p *Playlist) AvgSegmentDuration() float64 {
	p.ensureCached()
	return p.cached.avgSegmentDur
}

func (p *Playlist) ensureCached() {
	if p.cached != nil && p.cached.loaded {
		return
	}

	size := int64(0)
	segmentCount := 0
	totalSegDuration := 0.0
	avgSegmentDur := 0.0
	firstSegment := "" // путь к первому сегменту, который есть на диске

	pl, err := p.parseMediaPlaylist()
	if err == nil {
		dir := filepath.Dir(p.Path)
		for _, seg := range pl.Segments {
			if seg == nil || seg.URI == "" {
				continue
			}
			totalSegDuration += seg.Duration
			segmentCount++

			tsPath := filepath.Join(dir, filepath.FromSlash(seg.URI))
			if info, err := os.Stat(tsPath); err == nil && !info.IsDir() {
				size += info.Size()
				if firstSegment == "" {
					firstSegment = tsPath
				}
			}
		}
	}

	resolution := p.extractResolutionFromPlaylist()
	if resolution == "" && firstSegment != "" {
		resolution = p.segmentResolution(firstSegment)
	}

	// Длительность VOD-плейлиста — сумма #EXTINF; ffprobe даёт то же число,
	// только запуском процесса на каждое видео.
	if segmentCount > 0 {
		avgSegmentDur = totalSegDuration / float64(segmentCount)
	}

	p.cached = &cachedInfo{
		duration:      int(math.Round(totalSegDuration)),
		sizeMB:        int(math.Round(float64(size) / 1024.0 / 1024.0)),
		resolution:    resolution,
		segmentCount:  segmentCount,
		avgSegmentDur: avgSegmentDur,
		loaded:        true,
	}
}

func (p *Playlist) extractResolutionFromPlaylist() string {
	f, err := os.Open(p.Path)
	if err != nil {
		return ""
	}
	defer f.Close()

	master, listType, err := m3u8.DecodeFrom(f, true)
	if err != nil || listType != m3u8.MASTER {
		return ""
	}

	if mpl, ok := master.(*m3u8.MasterPlaylist); ok {
		for _, variant := range mpl.Variants {
			if variant.Resolution != "" {
				return variant.Resolution
			}
		}
	}
	return ""
}

// resolutionCache — разрешение видео по пути к плейлисту; в ключе mtime и
// размер, чтобы перезаписанное видео пробовалось заново. Playlist создаётся
// заново на каждый GET /videos, а ffprobe стоит десятки миллисекунд на видео:
// без кеша список из сотен видео отвечал бы десятки секунд при каждом
// обновлении и упирался в таймаут клиента. Запуск один раз на видео за время
// жизни процесса; результат «не удалось» тоже кешируется.
var resolutionCache sync.Map // resolutionKey -> string

type resolutionKey struct {
	path    string
	modTime int64
	size    int64
}

// probeTimeout — предел одного запуска ffprobe: зависший процесс не должен
// держать запрос.
const probeTimeout = 10 * time.Second

// probeResolution — разрешение видео в файле через ffprobe. Переменная, чтобы
// тесты подменяли её и считали запуски.
var probeResolution = ffprobeResolution

// segmentResolution — разрешение видео по его первому сегменту, с кешем.
func (p *Playlist) segmentResolution(segmentPath string) string {
	info, err := os.Stat(p.Path)
	if err != nil {
		return ""
	}
	key := resolutionKey{path: p.Path, modTime: info.ModTime().UnixNano(), size: info.Size()}
	if v, ok := resolutionCache.Load(key); ok {
		return v.(string)
	}
	res := probeResolution(segmentPath)
	resolutionCache.Store(key, res)
	return res
}

// ffprobeResolution возвращает "ШИРИНАxВЫСОТА" первой видеодорожки файла или "".
func ffprobeResolution(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe",
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

func (p *Playlist) parseMediaPlaylist() (*m3u8.MediaPlaylist, error) {
	count := estimateSegmentCount(p.Path)

	f, err := os.Open(p.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	pl, err := m3u8.NewMediaPlaylist(uint(count), uint(count))
	if err != nil {
		return nil, err
	}
	err = pl.DecodeFrom(f, true)
	if err != nil {
		return nil, err
	}
	return pl, nil
}

func estimateSegmentCount(path string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		return 100
	}
	count := 0
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "#EXTINF:") {
			count++
		}
	}
	if count < 10 {
		return 50
	}
	return count + 20
}
