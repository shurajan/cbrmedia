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

func TestMediaInfoMetaWithoutPlaylist(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "vid"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := NewMediaInfo(base, "vid").Meta(); ok {
		t.Error("a folder without playlist.m3u8 is not a video")
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

// GET /videos берёт метаданные из meta.json; без него видео остаётся в списке с нулями.
func TestMetaFromMetaJSON(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "video")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, media.PlaylistFile), []byte("#EXTM3U\n#EXTINF:9,\n0.ts\n#EXT-X-ENDLIST\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := NewMediaInfo(base, "video").Meta()
	if !ok || got != (media.Meta{}) {
		t.Errorf("Meta without meta.json = %+v, %v; want zero, true", got, ok)
	}

	want := media.Meta{Duration: 3600, Resolution: "1920x1080", SizeMB: 2300, SegmentCount: 600, AvgSegmentDuration: 6}
	if err := media.Write(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok = NewMediaInfo(base, "video").Meta()
	if !ok || got != want {
		t.Errorf("Meta = %+v, %v; want %+v", got, ok, want)
	}
}
