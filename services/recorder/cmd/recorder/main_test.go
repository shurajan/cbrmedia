package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"cbr/core/home"
	"cbr/recorder/internal/config"
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

// TestConfigCommand: файл, записанный "recorder config", служба читает без ошибок;
// старый ключ proxy переносится в секцию status_api.
func TestConfigCommand(t *testing.T) {
	t.Setenv(home.EnvHome, t.TempDir())
	layout, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	path := layout.ConfigFile(config.Name)
	if err := os.MkdirAll(layout.Config, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"base_url":"https://old.example.com","proxy":"http://old:1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("old notation must not load")
	}

	var out, errOut bytes.Buffer
	args := []string{"config", "--base-url", "https://example.com/api/ctx", "--cdn", "http://cdn-proxy:2"}
	if code := run(args, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit code %d: %s", code, errOut.String())
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://example.com/api/ctx" || cfg.StatusAPI.ProxyURL() != "http://old:1" || cfg.CDN.ProxyURL() != "http://cdn-proxy:2" {
		t.Fatalf("cfg = %+v", cfg)
	}

	out.Reset()
	if code := run([]string{"config", "--help"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "--status-api") {
		t.Fatalf("help: exit %d, out %s", code, out.String())
	}
}
