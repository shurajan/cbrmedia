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
	// Без секции cdn — прямое подключение, общий proxy на CDN не действует.
	if cfg.CDN.Connection != ConnectionDirect || cfg.CDNProxy() != "" {
		t.Fatalf("cdn = %+v, CDNProxy = %q", cfg.CDN, cfg.CDNProxy())
	}
}

func TestCDNProxy(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"direct with proxies set": {
			`{"base_url":"https://e.com","proxy":"http://a:1","cdn":{"connection":"direct","proxy":"http://b:2"}}`, ""},
		"own proxy": {
			`{"base_url":"https://e.com","proxy":"http://a:1","cdn":{"connection":"proxy","proxy":" http://b:2 "}}`, "http://b:2"},
		"shared proxy": {
			`{"base_url":"https://e.com","proxy":"http://a:1","cdn":{"connection":" Proxy "}}`, "http://a:1"},
		"cdn proxy only": {
			`{"base_url":"https://e.com","cdn":{"connection":"proxy","proxy":"http://b:2"}}`, "http://b:2"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(write(t, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.CDNProxy(); got != tc.want {
				t.Fatalf("CDNProxy = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadValidation(t *testing.T) {
	for name, body := range map[string]string{
		"missing base_url":      `{}`,
		"relative url":          `{"base_url":"example.com"}`,
		"bad proxy":             `{"base_url":"https://example.com","proxy":"nope"}`,
		"bad cdn connection":    `{"base_url":"https://example.com","cdn":{"connection":"socks"}}`,
		"cdn proxy without url": `{"base_url":"https://example.com","cdn":{"connection":"proxy"}}`,
		"bad cdn proxy":         `{"base_url":"https://example.com","cdn":{"connection":"proxy","proxy":"nope"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
