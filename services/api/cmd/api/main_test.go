package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cbr/api/internal/auth"
	"cbr/api/internal/config"
	"cbr/core/home"
)

func TestVersionFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-version"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit code %d: %s", code, errOut.String())
	}
	if strings.TrimSpace(out.String()) != version {
		t.Errorf("output = %q", out.String())
	}
}

func TestHashPassword(t *testing.T) {
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) }) // home.Prepare делает chdir

	root := t.TempDir()
	t.Setenv(home.EnvHome, root)
	authFile := filepath.Join(root, "config", "auth.json")

	var out, errOut bytes.Buffer
	if code := run([]string{"hash-password", "--password", "first"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit code %d: %s", code, errOut.String())
	}
	if !checkPassword(t, authFile, "first") {
		t.Fatal("password not saved")
	}

	// Перезапись без подтверждения отменяется.
	out.Reset()
	if code := run([]string{"hash-password", "--password", "second"}, strings.NewReader("no\n"), &out, &errOut); code == 0 {
		t.Fatal("overwrite without confirmation must fail")
	}
	if !checkPassword(t, authFile, "first") {
		t.Fatal("password changed without confirmation")
	}

	// С подтверждением — перезаписывается.
	if code := run([]string{"hash-password", "--password", "second"}, strings.NewReader("yes\n"), &out, &errOut); code != 0 {
		t.Fatalf("exit code %d: %s", code, errOut.String())
	}
	if !checkPassword(t, authFile, "second") {
		t.Fatal("password not overwritten after confirmation")
	}
}

func TestHashPasswordRequiresPassword(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"hash-password"}, strings.NewReader(""), &out, &errOut); code == 0 {
		t.Fatal("missing --password must fail")
	}
}

func checkPassword(t *testing.T, path, password string) bool {
	t.Helper()
	svc := auth.NewService(path)
	if err := svc.Load(); err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return svc.CheckPassword(password)
}

// TestConfigCommand: файл, записанный "api config", служба читает без ошибок;
// пустой флаг очищает необязательный параметр.
func TestConfigCommand(t *testing.T) {
	t.Setenv(home.EnvHome, t.TempDir())
	layout, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	path := layout.ConfigFile(config.Name)

	var out, errOut bytes.Buffer
	args := []string{"config", "--status-base-url", "https://example.com/api/ctx/", "--status-api", "http://127.0.0.1:8080"}
	if code := run(args, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit code %d: %s", code, errOut.String())
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != config.DefaultAddr || cfg.StatusBaseURL != "https://example.com/api/ctx" || cfg.StatusAPI.ProxyURL() != "http://127.0.0.1:8080" {
		t.Fatalf("cfg = %+v", cfg)
	}

	if code := run([]string{"config", "--status-base-url", ""}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit code %d: %s", code, errOut.String())
	}
	if cfg, err = config.Load(path); err != nil || cfg.StatusBaseURL != "" || cfg.StatusAPI.ProxyURL() == "" {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
}
