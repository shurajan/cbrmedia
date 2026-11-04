package library

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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

// Duration — длительность в секундах (ffprobe, иначе сумма сегментов).
func (p *Playlist) Duration() int {
	p.ensureCached()
	return p.cached.duration
}

// SizeMB — суммарный размер сегментов в мегабайтах.
func (p *Playlist) SizeMB() int {
	p.ensureCached()
	return p.cached.sizeMB
}

// Resolution — разрешение из master-плейлиста или через ffprobe, например "1920x1080".
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
	var resolution string
	segmentCount := 0
	totalSegDuration := 0.0
	avgSegmentDur := 0.0

	// parse playlist
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
			}
		}
	}

	resolution = p.extractResolutionFromPlaylist()
	if resolution == "" {
		resolution = p.FFProbeResolution()
	}

	duration := int(math.Round(p.ffprobeDuration()))
	if duration == 0 && segmentCount > 0 {
		duration = int(math.Round(totalSegDuration))
	}
	if segmentCount > 0 {
		avgSegmentDur = totalSegDuration / float64(segmentCount)
	}

	p.cached = &cachedInfo{
		duration:      duration,
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

// FFProbeResolution определяет разрешение первого доступного сегмента через ffprobe.
func (p *Playlist) FFProbeResolution() string {
	pl, err := p.parseMediaPlaylist()
	if err != nil {
		return ""
	}

	dir := filepath.Dir(p.Path)
	for _, seg := range pl.Segments {
		if seg == nil || seg.URI == "" {
			continue
		}

		tsPath := filepath.Join(dir, filepath.FromSlash(seg.URI))
		if _, err := os.Stat(tsPath); err == nil {
			cmd := exec.Command("ffprobe",
				"-v", "error",
				"-select_streams", "v:0",
				"-show_entries", "stream=width,height",
				"-of", "csv=p=0:s=x",
				tsPath,
			)

			out, err := cmd.Output()
			if err != nil {
				return ""
			}

			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					return line
				}
			}
		}
	}
	return ""
}

func (p *Playlist) ffprobeDuration() float64 {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		p.Path,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0
	}

	return duration
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
