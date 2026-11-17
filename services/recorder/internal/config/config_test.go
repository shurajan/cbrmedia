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
	// Пустой "proxy" прежней нотации (как в старом шаблоне) не мешает.
	cfg, err := Load(write(t, `{"base_url":" https://example.com/api/ctx/ ","proxy":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://example.com/api/ctx" {
		t.Fatalf("cfg = %+v", cfg)
	}
	// Без секций — оба подключения прямые.
	if cfg.StatusAPI.ProxyURL() != "" || cfg.CDN.ProxyURL() != "" {
		t.Fatalf("status_api = %+v, cdn = %+v", cfg.StatusAPI, cfg.CDN)
	}
}

// У status API и CDN свои секции, одна на другую не влияет.
func TestLoadConnections(t *testing.T) {
	cfg, err := Load(write(t, `{"base_url":"https://e.com",
		"status_api":{"connection":"proxy","proxy":"http://a:1"},
		"cdn":{"connection":"proxy","proxy":"http://b:2"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StatusAPI.ProxyURL() != "http://a:1" || cfg.CDN.ProxyURL() != "http://b:2" {
		t.Fatalf("status_api = %+v, cdn = %+v", cfg.StatusAPI, cfg.CDN)
	}
}

func TestLoadValidation(t *testing.T) {
	for name, body := range map[string]string{
		"missing base_url":    `{}`,
		"relative url":        `{"base_url":"example.com"}`,
		"legacy proxy":        `{"base_url":"https://example.com","proxy":"http://127.0.0.1:8080"}`,
		"bad status_api":      `{"base_url":"https://example.com","status_api":{"connection":"proxy"}}`,
		"bad cdn":             `{"base_url":"https://example.com","cdn":{"connection":"socks"}}`,
		"cdn has no fallback": `{"base_url":"https://example.com","status_api":{"connection":"proxy","proxy":"http://a:1"},"cdn":{"connection":"proxy"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
