package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"

	"cbr/core/home"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func testLayout(t *testing.T) home.Layout {
	t.Helper()
	t.Setenv(home.EnvHome, t.TempDir())
	layout, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	return layout
}

// TestRunStartsAndStops поднимает бота на фейковом Telegram API и проверяет остановку.
func TestRunStartsAndStops(t *testing.T) {
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

	layout := testLayout(t)
	if err := os.WriteFile(layout.ConfigFile("tbot"), []byte(`{"bot_token":"123:test","allowed_user_id":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, layout, Options{
			EventsInterval: 10 * time.Millisecond,
			BotOptions:     []bot.Option{bot.WithServerURL(tg.URL), bot.WithSkipGetMe()},
		}, discard)
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestRunInvalidConfig(t *testing.T) {
	layout := testLayout(t)
	if err := os.WriteFile(layout.ConfigFile("tbot"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), layout, Options{}, discard); err == nil {
		t.Fatal("expected config validation error")
	}
}
