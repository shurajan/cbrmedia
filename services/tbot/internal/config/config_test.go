package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tbot.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	cfg, err := Load(write(t, `{"bot_token":" 123:abc ","allowed_user_id":42}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotToken != "123:abc" || cfg.AllowedUserID != 42 || cfg.Telegram.ProxyURL() != "" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadTelegramProxy(t *testing.T) {
	cfg, err := Load(write(t, `{"bot_token":"t","allowed_user_id":42,"proxy":"",
		"telegram":{"connection":"proxy","proxy":"http://127.0.0.1:8080"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.ProxyURL() != "http://127.0.0.1:8080" {
		t.Fatalf("telegram = %+v", cfg.Telegram)
	}
}

func TestLoadValidation(t *testing.T) {
	for name, body := range map[string]string{
		"missing token": `{"allowed_user_id":42}`,
		"missing user":  `{"bot_token":"t"}`,
		"legacy proxy":  `{"bot_token":"t","allowed_user_id":42,"proxy":"http://127.0.0.1:8080"}`,
		"bad telegram":  `{"bot_token":"t","allowed_user_id":42,"telegram":{"connection":"proxy"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
