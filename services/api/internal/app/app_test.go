package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"cbr/api/internal/auth"
	"cbr/core/home"
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

// TestRunEndToEnd поднимает API, логинится, проверяет /videos и /watchlist и
// корректную остановку по отмене контекста.
func TestRunEndToEnd(t *testing.T) {
	t.Setenv(home.EnvHome, t.TempDir())
	layout, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}

	addr := freeAddr(t)
	cfg := fmt.Sprintf(`{"addr": %q}`, addr)
	if err := os.WriteFile(layout.ConfigFile("api"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := auth.NewService(layout.AuthFile()).SetPassword("pw"); err != nil {
		t.Fatal(err)
	}
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

	req, _ = http.NewRequest(http.MethodPost, "http://"+addr+"/watchlist", strings.NewReader(`{"user":"alice"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("POST /watchlist = %d", resp.StatusCode)
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
	err := Run(context.Background(), layout, Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "hash-password") {
		t.Fatalf("err = %v, want hint about hash-password", err)
	}
}
