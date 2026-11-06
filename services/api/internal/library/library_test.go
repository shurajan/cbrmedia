package library

import (
	"os"
	"path/filepath"
	"testing"
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
