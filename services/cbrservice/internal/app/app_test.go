package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"

	"cbrservice/internal/auth"
	"cbrservice/internal/home"
	"cbrservice/internal/storage"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// TestRunEndToEnd поднимает весь сервис на фейковых Telegram и status API,
// проверяет HTTP API и корректную остановку по отмене контекста.
func TestRunEndToEnd(t *testing.T) {
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			select { // long polling: держим запрос, пока клиент не уйдёт
			case <-r.Context().Done():
			case <-time.After(200 * time.Millisecond):
			}
			_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer tg.Close()
	telegramOptions = []bot.Option{bot.WithServerURL(tg.URL), bot.WithSkipGetMe()}
	t.Cleanup(func() { telegramOptions = nil })

	status := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"room_status":"offline"}`)
	}))
	defer status.Close()

	t.Setenv(home.EnvHome, t.TempDir())
	layout, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}

	addr := freeAddr(t)
	cfg := fmt.Sprintf(`{
	  "base_url": %q,
	  "storage": {"db": "users.db"},
	  "telegram": {"bot_token": "123:test", "allowed_user_id": 1},
	  "http": {"addr": %q}
	}`, status.URL, addr)
	if err := os.WriteFile(layout.ConfigFile(), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := auth.NewService(layout.AuthFile()).SetPassword("pw"); err != nil {
		t.Fatal(err)
	}
	// Сохранённый пользователь должен подхватиться при старте.
	users, err := storage.OpenUsers(layout.Data + "/users.db")
	if err != nil {
		t.Fatal(err)
	}
	_ = users.Add("alice")
	_ = users.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { done <- Run(ctx, layout, Options{}, log) }()

	// Ждём HTTP API и логинимся.
	var token string
	deadline := time.Now().Add(10 * time.Second)
	for token == "" {
		if time.Now().After(deadline) {
			t.Fatal("http api did not start")
		}
		resp, err := http.Post("http://"+addr+"/auth", "application/json", strings.NewReader(`{"password":"pw"}`))
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		var body struct{ Token string }
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		token = body.Token
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/videos", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(b)) != "[]" {
		t.Errorf("GET /videos = %d %s", resp.StatusCode, b)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestRunMissingAuthFile(t *testing.T) {
	t.Setenv(home.EnvHome, t.TempDir())
	layout, _ := home.Resolve()
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	cfg := `{"base_url":"https://example.com","telegram":{"bot_token":"t","allowed_user_id":1}}`
	if err := os.WriteFile(layout.ConfigFile(), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Run(context.Background(), layout, Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "hash-password") {
		t.Fatalf("err = %v, want hint about hash-password", err)
	}
}

func TestRunInvalidConfig(t *testing.T) {
	t.Setenv(home.EnvHome, t.TempDir())
	layout, _ := home.Resolve()
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.ConfigFile(), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), layout, Options{}, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("expected config validation error")
	}
}
