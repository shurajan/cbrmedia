package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "api.json"))
	if err != nil || cfg.Addr != DefaultAddr {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.json")
	if err := os.WriteFile(path, []byte(`{"addr":"127.0.0.1:9000"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.Addr != "127.0.0.1:9000" {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}

	if err := os.WriteFile(path, []byte(`{"status_base_url":" https://example.com/api/ctx/ ","proxy":"http://127.0.0.1:8080"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil || cfg.Addr != DefaultAddr || cfg.StatusBaseURL != "https://example.com/api/ctx" || cfg.Proxy != "http://127.0.0.1:8080" {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}

	if err := os.WriteFile(path, []byte(`{"status_base_url":"not a url"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected status_base_url validation error")
	}

	if err := os.WriteFile(path, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected parse error")
	}
}
