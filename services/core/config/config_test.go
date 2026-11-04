package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testConfig struct {
	URL  string `json:"url"`
	Addr string `json:"addr"`
}

func (c *testConfig) Normalize() {
	c.URL = strings.TrimSpace(c.URL)
	if c.Addr == "" {
		c.Addr = ":1"
	}
}

func (c *testConfig) Validate() error {
	if c.URL == "" {
		return errors.New("url is required")
	}
	return ValidateURL(c.URL)
}

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	var cfg testConfig
	if err := Load(write(t, `{"url":" https://example.com ","extra":1}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://example.com" || cfg.Addr != ":1" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	for name, body := range map[string]string{
		"invalid json": `{`,
		"validation":   `{}`,
		"relative url": `{"url":"example.com"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := Load(write(t, body), &testConfig{}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if err := Load(filepath.Join(t.TempDir(), "missing.json"), &testConfig{}); err == nil {
		t.Fatal("expected error for missing file")
	}
}
