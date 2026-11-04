package home

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveUsesEnvOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvHome, root)

	l, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if l.Root != root {
		t.Fatalf("Root = %q, want %q", l.Root, root)
	}
	if l.ConfigFile("recorder") != filepath.Join(root, "config", "recorder.json") {
		t.Errorf("unexpected ConfigFile: %s", l.ConfigFile("recorder"))
	}
	if l.AuthFile() != filepath.Join(root, "config", "auth.json") {
		t.Errorf("unexpected AuthFile: %s", l.AuthFile())
	}
	if l.DBFile() != filepath.Join(root, "data", "cbr.db") {
		t.Errorf("unexpected DBFile: %s", l.DBFile())
	}
}

func TestResolveDefaultsToDotCbr(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv(EnvHome, "")
	t.Setenv("HOME", userHome)

	l, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(userHome, ".cbr"); l.Root != want {
		t.Fatalf("Root = %q, want %q", l.Root, want)
	}
}

func TestEnsureCreatesAllDirs(t *testing.T) {
	t.Setenv(EnvHome, filepath.Join(t.TempDir(), "cbr"))
	l, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{l.Root, l.Bin, l.Config, l.Data, l.Recordings, l.Media, l.Logs} {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Errorf("%s was not created", dir)
		}
	}
}
