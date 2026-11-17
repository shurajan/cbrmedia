package main

import (
	"bytes"
	"strings"
	"testing"

	"cbr/core/home"
	"cbr/tbot/internal/config"
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

// TestConfigCommand: файл, записанный "tbot config" в диалоге, служба читает без ошибок.
func TestConfigCommand(t *testing.T) {
	t.Setenv(home.EnvHome, t.TempDir())
	layout, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	answers := "123:abc\n42\nhttp://127.0.0.1:8080\n"
	if code := run([]string{"config"}, strings.NewReader(answers), &out, &errOut); code != 0 {
		t.Fatalf("exit code %d: %s", code, errOut.String())
	}
	cfg, err := config.Load(layout.ConfigFile(config.Name))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotToken != "123:abc" || cfg.AllowedUserID != 42 || cfg.Telegram.ProxyURL() != "http://127.0.0.1:8080" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if strings.Contains(out.String(), "123:abc") {
		t.Errorf("token is printed: %s", out.String())
	}
}
