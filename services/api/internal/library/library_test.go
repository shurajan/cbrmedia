package library

import (
	"os"
	"path/filepath"
	"testing"

	"cbr/core/media"
)

func TestSimpleID(t *testing.T) {
	if simpleID("ABC") != simpleID("abc") {
		t.Error("simpleID should be case-insensitive")
	}
	if simpleID("alpha") == simpleID("beta") {
		t.Error("different inputs should produce different ids")
	}
}

func TestMediaInfoStreamURL(t *testing.T) {
	info := NewMediaInfo("/base", "myvid")
	const want = "/videos/myvid/playlist.m3u8"
	if got := info.StreamURL(); got != want {
		t.Errorf("StreamURL() = %q, want %q", got, want)
	}
}

func TestMediaInfoStreamURLEscapesFolder(t *testing.T) {
	info := NewMediaInfo("/base", "My Video#1")
	const want = "/videos/My%20Video%231/playlist.m3u8"
	if got := info.StreamURL(); got != want {
		t.Errorf("StreamURL() = %q, want %q", got, want)
	}
}

func TestMediaInfoPlaylist(t *testing.T) {
	base := t.TempDir()
	folder := "vid"
	dir := filepath.Join(base, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	info := NewMediaInfo(base, folder)
	if info.Playlist() != nil {
		t.Error("expected nil playlist when no playlist.m3u8 exists")
	}

	if err := os.WriteFile(filepath.Join(dir, "playlist.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if info.Playlist() == nil {
		t.Error("expected a playlist once playlist.m3u8 exists")
	}
}

func TestMediaInfoCreatedAt(t *testing.T) {
	base := t.TempDir()
	folder := "vid"
	if err := os.MkdirAll(filepath.Join(base, folder), 0o755); err != nil {
		t.Fatal(err)
	}

	info := NewMediaInfo(base, folder)
	if info.CreatedAt() == "" {
		t.Error("expected a non-empty RFC3339 timestamp for an existing folder")
	}

	missing := NewMediaInfo(base, "no-such-folder")
	if missing.CreatedAt() != "" {
		t.Error("expected an empty timestamp for a missing folder")
	}
}

func TestPlaylistParsesSegments(t *testing.T) {
	dir := t.TempDir()
	writeSampleHLS(t, dir)

	pl := &Playlist{Path: filepath.Join(dir, "playlist.m3u8")}
	if got := pl.SegmentCount(); got != 2 {
		t.Errorf("SegmentCount() = %d, want 2", got)
	}
	if got := pl.AvgSegmentDuration(); got <= 0 {
		t.Errorf("AvgSegmentDuration() = %v, want > 0", got)
	}
}

// writeSampleHLS creates a minimal media playlist with two segments on disk.
func writeSampleHLS(t *testing.T, dir string) {
	t.Helper()
	playlist := "#EXTM3U\n" +
		"#EXT-X-VERSION:3\n" +
		"#EXT-X-TARGETDURATION:10\n" +
		"#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXTINF:9.000,\n" +
		"segment0.ts\n" +
		"#EXTINF:9.000,\n" +
		"segment1.ts\n" +
		"#EXT-X-ENDLIST\n"
	if err := os.WriteFile(filepath.Join(dir, "playlist.m3u8"), []byte(playlist), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"segment0.ts", "segment1.ts"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("ts-bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlaylistDurationIsSegmentSum(t *testing.T) {
	dir := t.TempDir()
	writeSampleHLS(t, dir)

	pl := &Playlist{Path: filepath.Join(dir, "playlist.m3u8")}
	if got := pl.Duration(); got != 18 {
		t.Errorf("Duration() = %d, want 18 (sum of #EXTINF)", got)
	}
}

func TestResolutionProbedOncePerVideo(t *testing.T) {
	defer func(f func(string) string) { probeResolution = f }(probeResolution)
	calls := 0
	probeResolution = func(string) string { calls++; return "1280x720" }

	dir := t.TempDir()
	writeSampleHLS(t, dir)
	path := filepath.Join(dir, "playlist.m3u8")

	// Каждый GET /videos создаёт новый Playlist: ffprobe должен запуститься один раз.
	for i := 0; i < 3; i++ {
		if got := (&Playlist{Path: path}).Resolution(); got != "1280x720" {
			t.Fatalf("Resolution() = %q", got)
		}
	}
	if calls != 1 {
		t.Errorf("ffprobe runs = %d, want 1 (cached by playlist path, mtime and size)", calls)
	}

	// Перезаписанный плейлист (другой размер) пробуется заново.
	if err := os.WriteFile(path, []byte("#EXTM3U\n#EXTINF:9.000,\nsegment0.ts\n#EXT-X-ENDLIST\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	(&Playlist{Path: path}).Resolution()
	if calls != 2 {
		t.Errorf("ffprobe runs = %d, want 2 after the playlist changed", calls)
	}
}

// meta.json от converter избавляет GET /videos от разбора плейлиста и ffprobe.
func TestMetaPrefersMetaJSON(t *testing.T) {
	defer func(f func(string) string) { probeResolution = f }(probeResolution)
	probeResolution = func(string) string { t.Error("ffprobe must not run when meta.json exists"); return "" }

	base := t.TempDir()
	dir := filepath.Join(base, "video")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSampleHLS(t, dir)
	want := media.Meta{Duration: 3600, Resolution: "1920x1080", SizeMB: 2300, SegmentCount: 600, AvgSegmentDuration: 6}
	if err := media.Write(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok := NewMediaInfo(base, "video").Meta()
	if !ok || got != want {
		t.Errorf("Meta = %+v, %v; want %+v", got, ok, want)
	}

	// Без meta.json — по плейлисту (два сегмента по 9 с), разрешение через ffprobe.
	if err := os.Remove(filepath.Join(dir, media.MetaFile)); err != nil {
		t.Fatal(err)
	}
	probeResolution = func(string) string { return "640x480" }
	got, ok = NewMediaInfo(base, "video").Meta()
	if !ok || got.Duration != 18 || got.SegmentCount != 2 || got.Resolution != "640x480" {
		t.Errorf("Meta without meta.json = %+v, %v", got, ok)
	}
	if _, ok := NewMediaInfo(base, "missing").Meta(); ok {
		t.Error("Meta of a folder without playlist must be false")
	}
}
