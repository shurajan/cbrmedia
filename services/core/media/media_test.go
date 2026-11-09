package media

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanMPEGTS(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, PlaylistFile, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n"+
		"#EXTINF:6.000,\n00000.ts\n#EXTINF:4.500,\n00001.ts\n#EXTINF:1.500,\nmissing.ts\n#EXT-X-ENDLIST\n")
	write(t, dir, "00000.ts", strings.Repeat("x", 1024*1024))
	write(t, dir, "00001.ts", strings.Repeat("x", 512*1024))

	m, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Meta{Duration: 12, SizeMB: 2, SegmentCount: 3, AvgSegmentDuration: 4}
	if m != want {
		t.Errorf("Scan = %+v, want %+v", m, want)
	}
}

func TestScanFMP4CountsInit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, PlaylistFile, "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:2.0,\n00000.m4s\n#EXTINF:2.0,\n00001.m4s\n#EXT-X-ENDLIST\n")
	write(t, dir, "init.mp4", strings.Repeat("i", 1024*1024))
	write(t, dir, "00000.m4s", strings.Repeat("x", 1024*1024))
	write(t, dir, "00001.m4s", strings.Repeat("x", 1024*1024))

	m, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.SizeMB != 3 || m.SegmentCount != 2 || m.Duration != 4 {
		t.Errorf("Scan = %+v", m)
	}
}

func TestScanErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Scan(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("no playlist: err = %v", err)
	}
	write(t, dir, PlaylistFile, "#EXTM3U\n#EXT-X-ENDLIST\n")
	if _, err := Scan(dir); err == nil {
		t.Error("empty playlist: want error")
	}
}

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read(empty dir) err = %v, want ErrNotExist", err)
	}
	want := Meta{Duration: 3600, Resolution: "1920x1080", SizeMB: 2300, SegmentCount: 600, AvgSegmentDuration: 6}
	if err := Write(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if err != nil || got != want {
		t.Fatalf("Read = %+v, %v; want %+v", got, err, want)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
	write(t, dir, MetaFile, "{broken")
	if _, err := Read(dir); err == nil {
		t.Error("broken JSON: want error")
	}
}

func TestProbeFile(t *testing.T) {
	dir := t.TempDir()
	if got := ProbeFile(dir); got != "" {
		t.Errorf("ProbeFile(no playlist) = %q", got)
	}
	write(t, dir, PlaylistFile, "#EXTM3U\n#EXTINF:6,\n00000.ts\n#EXTINF:6,\n00001.ts\n#EXT-X-ENDLIST\n")
	if got := ProbeFile(dir); got != filepath.Join(dir, "00000.ts") {
		t.Errorf("ProbeFile(ts) = %q", got)
	}
	write(t, dir, PlaylistFile, "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:2,\n00000.m4s\n#EXT-X-ENDLIST\n")
	if got := ProbeFile(dir); got != filepath.Join(dir, "init.mp4") {
		t.Errorf("ProbeFile(fmp4) = %q", got)
	}
}

func TestAttribute(t *testing.T) {
	cases := map[string]string{
		`#EXT-X-MAP:URI="init.mp4"`:                   "init.mp4",
		`#EXT-X-MAP:URI="init.mp4",BYTERANGE="700@0"`: "init.mp4",
		`#EXT-X-MAP:BYTERANGE="700@0",URI="a b.mp4"`:  "a b.mp4",
		`#EXT-X-MAP:BYTERANGE="700@0"`:                "",
	}
	for line, want := range cases {
		if got := attribute(line, "URI"); got != want {
			t.Errorf("attribute(%q) = %q, want %q", line, got, want)
		}
	}
}
