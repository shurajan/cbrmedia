package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cbr/api/internal/auth"
	"cbr/core/store"
)

func TestServerServeAndShutdown(t *testing.T) {
	media := t.TempDir()
	makeVideoDir(t, media, "show")

	svc := auth.NewService(filepath.Join(t.TempDir(), "auth.json"))
	if err := svc.SetPassword("pw"); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "cbr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := New(Config{MediaDir: media}, svc, db, discard)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()

	base := "http://" + ln.Addr().String()
	resp, err := http.Post(base+"/auth", "application/json", strings.NewReader(`{"password":"pw"}`))
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ Token string }
	_ = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if body.Token == "" {
		t.Fatal("no token")
	}

	req, _ := http.NewRequest(http.MethodGet, base+"/videos/show/playlist.m3u8", nil)
	req.Header.Set("Authorization", "Bearer "+body.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("playlist status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("server did not shut down")
	}
}
