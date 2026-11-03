package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validJSON = `{
  "base_url": "https://example.com/api/ctx/",
  "storage": {"db": "data.db"},
  "url_poller": {"cert_path": "legacy.pem", "proxy": "http://127.0.0.1:8080"},
  "telegram": {"bot_token": "123:abc", "allowed_user_id": 42}
}`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeConfig(t, validJSON), "/srv/cbr/data")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://example.com/api/ctx/" {
		t.Errorf("BaseURL = %q, must be kept as is", cfg.BaseURL)
	}
	if cfg.Storage.DB != "/srv/cbr/data/data.db" {
		t.Errorf("Storage.DB = %q", cfg.Storage.DB)
	}
	if cfg.Poller.Proxy != "http://127.0.0.1:8080" {
		t.Errorf("Poller.Proxy = %q", cfg.Poller.Proxy)
	}
	if cfg.Telegram.AllowedUserID != 42 || cfg.Telegram.BotToken != "123:abc" {
		t.Errorf("Telegram = %+v", cfg.Telegram)
	}
	if cfg.HTTP.Addr != DefaultHTTPAddr {
		t.Errorf("HTTP.Addr = %q, want default", cfg.HTTP.Addr)
	}
}

func TestLoadDefaultsAndAbsoluteDB(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
	  "base_url": "https://example.com",
	  "storage": {"db": "/var/lib/cbr.db"},
	  "telegram": {"bot_token": "t", "allowed_user_id": 1},
	  "http": {"addr": "127.0.0.1:9000"}
	}`), "/srv/cbr/data")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.DB != "/var/lib/cbr.db" {
		t.Errorf("absolute Storage.DB changed: %q", cfg.Storage.DB)
	}
	if cfg.HTTP.Addr != "127.0.0.1:9000" {
		t.Errorf("HTTP.Addr = %q", cfg.HTTP.Addr)
	}
}

func TestLoadEmptyDBUsesDefault(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
	  "base_url": "https://example.com",
	  "telegram": {"bot_token": "t", "allowed_user_id": 1}
	}`), "/data")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.DB != filepath.Join("/data", DefaultDBFile) {
		t.Errorf("Storage.DB = %q", cfg.Storage.DB)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	_, err := Load(writeConfig(t, `{"url_poller": {"proxy": "::bad"}}`), "/data")
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"base_url", "bot_token", "allowed_user_id", "url_poller.proxy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json"), "/data"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	if _, err := Load(writeConfig(t, `{`), "/data"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
