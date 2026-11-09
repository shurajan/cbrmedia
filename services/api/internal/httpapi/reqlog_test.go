package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cbr/api/internal/auth"
	"cbr/core/store"
)

// newLoggedServer — сервер с логгером уровня level, пишущим в буфер.
func newLoggedServer(t *testing.T, level slog.Level) (*Server, *bytes.Buffer) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "cbr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level}))
	svc := auth.NewService(filepath.Join(t.TempDir(), "auth.json"))
	return New(Config{MediaDir: t.TempDir()}, svc, db, nil, log), &buf
}

func TestRequestsLoggedOnlyInDebug(t *testing.T) {
	srv, buf := newLoggedServer(t, slog.LevelDebug)
	for _, target := range []string{"/videos/show/seg001.ts", "/auth"} {
		method := http.MethodGet
		if target == "/auth" {
			method = http.MethodPost
		}
		resp, err := srv.app.Test(httptest.NewRequest(method, target, strings.NewReader("{")))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	out := buf.String()
	// Без токена — 401 от bearerAuth; битый JSON в /auth — 400 через fiber.Error.
	for _, want := range []string{
		`msg="http request" method=GET path=/videos/show/seg001.ts status=401`,
		`msg="http request" method=POST path=/auth status=400`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("debug log has no %q:\n%s", want, out)
		}
	}

	srv, buf = newLoggedServer(t, slog.LevelInfo)
	resp, err := srv.app.Test(httptest.NewRequest(http.MethodGet, "/videos/show/seg001.ts", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if strings.Contains(buf.String(), "http request") {
		t.Errorf("requests must not be logged without debug:\n%s", buf.String())
	}
}
