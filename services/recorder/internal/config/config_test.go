package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recorder.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	cfg, err := Load(write(t, `{"base_url":" https://example.com/api/ctx/ ","proxy":"http://127.0.0.1:8080"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://example.com/api/ctx" || cfg.Proxy != "http://127.0.0.1:8080" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadValidation(t *testing.T) {
	for name, body := range map[string]string{
		"missing base_url": `{}`,
		"relative url":     `{"base_url":"example.com"}`,
		"bad proxy":        `{"base_url":"https://example.com","proxy":"nope"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
