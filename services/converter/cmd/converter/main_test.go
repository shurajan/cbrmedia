package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cbr/core/home"
)

func TestVersionFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code %d: %s", code, errOut.String())
	}
	if strings.TrimSpace(out.String()) != version {
		t.Errorf("output = %q", out.String())
	}
}

func TestInvalidInterval(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-interval", "0s"}, &out, &errOut); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestMissingFFmpeg(t *testing.T) {
	t.Setenv(home.EnvHome, t.TempDir())
	var out, errOut bytes.Buffer
	code := run([]string{"-once", "-ffmpeg", filepath.Join(t.TempDir(), "no-such-ffmpeg")}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "ffmpeg not found") {
		t.Errorf("exit code = %d, stderr = %q", code, errOut.String())
	}
}

func TestLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	unlock, err := lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock(path); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("second lock err = %v, want 'already running'", err)
	}
	unlock()

	unlock2, err := lock(path)
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	unlock2()
}

func TestOnceConvertsRecordings(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvHome, root)

	// Поддельный ffmpeg: пишет готовый плейлист в последний аргумент.
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\nfor last; do :; done\nprintf '#EXTM3U\\n#EXTINF:6,\\n00000.ts\\n#EXT-X-ENDLIST\\n' > \"$last\"\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(root, "recordings", "u_part1.ts")
	if err := os.MkdirAll(filepath.Dir(rec), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rec, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"-once", "-min-age", "0s", "-ffmpeg", ffmpeg}, &out, &errOut); code != 0 {
		t.Fatalf("exit code %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(root, "media", "u_part1", "playlist.m3u8")); err != nil {
		t.Errorf("video not published: %v\n%s", err, errOut.String())
	}
	if _, err := os.Stat(rec); !os.IsNotExist(err) {
		t.Errorf("source must be removed, stat err = %v", err)
	}
}
