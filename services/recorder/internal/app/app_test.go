package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"cbr/core/home"
	"cbr/core/store"
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

// parallelLayout — как testLayout, но без t.Setenv, чтобы тест мог идти параллельно.
func parallelLayout(t *testing.T) home.Layout {
	t.Helper()
	root := t.TempDir()
	layout := home.Layout{Root: root}
	for dir, name := range map[*string]string{
		&layout.Bin: "bin", &layout.Config: "config", &layout.Data: "data",
		&layout.Recordings: "recordings", &layout.Media: "media", &layout.Logs: "logs",
	} {
		*dir = filepath.Join(root, name)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	return layout
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRunFollowsWatchlist: recorder подхватывает пользователя из базы,
// публикует его состояние и при остановке очищает его.
func TestRunFollowsWatchlist(t *testing.T) {
	status := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"room_status":"offline"}`)
	}))
	defer status.Close()

	layout := testLayout(t)
	cfg := `{"base_url":"` + status.URL + `"}`
	if err := os.WriteFile(layout.ConfigFile("recorder"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// Отдельное подключение к базе — как у другой службы.
	db, err := store.Open(layout.DBFile())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.AddUser(ctx, "alice", "test"); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- Run(runCtx, layout, Options{SyncInterval: 20 * time.Millisecond}, discard) }()

	state := func() store.Entry {
		entries, err := db.Users(ctx)
		if err != nil || len(entries) == 0 {
			return store.Entry{}
		}
		return entries[0]
	}
	waitFor(t, "state published", func() bool { return state().RoomStatus == "offline" })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	if st := state(); !st.StateAt.IsZero() {
		t.Errorf("state must be cleared on shutdown: %+v", st)
	}
}

// TestRunCDNConnection: общий proxy действует только на status API, к CDN recorder
// подключается напрямую или через прокси — по секции "cdn".
func TestRunCDNConnection(t *testing.T) {
	for name, tc := range map[string]struct {
		cdn      string
		viaProxy bool
	}{
		"no cdn section": {cdn: ``, viaProxy: false},
		"direct":         {cdn: `,"cdn":{"connection":"direct"}`, viaProxy: false},
		"shared proxy":   {cdn: `,"cdn":{"connection":"proxy"}`, viaProxy: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel() // каждый случай ждёт первой проверки (initialDelay)
			var cdnDirect, cdnProxied atomic.Int32
			cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				cdnDirect.Add(1)
				http.NotFound(w, nil)
			}))
			defer cdn.Close()

			// Прокси: status API доступен только через него (выдуманный хост),
			// запросы к CDN считает и не пропускает.
			const statusHost = "status.invalid"
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Host == statusHost {
					_, _ = io.WriteString(w, `{"room_status":"public","hls_source":"`+cdn.URL+`/master.m3u8"}`)
					return
				}
				cdnProxied.Add(1)
				http.NotFound(w, r)
			}))
			defer proxy.Close()

			layout := parallelLayout(t)
			cfg := `{"base_url":"http://` + statusHost + `","proxy":"` + proxy.URL + `"` + tc.cdn + `}`
			if err := os.WriteFile(layout.ConfigFile("recorder"), []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}
			db, err := store.Open(layout.DBFile())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx := context.Background()
			if err := db.AddUser(ctx, "alice", "test"); err != nil {
				t.Fatal(err)
			}

			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- Run(runCtx, layout, Options{SyncInterval: 20 * time.Millisecond}, discard) }()
			waitFor(t, "CDN request", func() bool { return cdnDirect.Load()+cdnProxied.Load() > 0 })
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("Run returned %v", err)
			}

			if tc.viaProxy && (cdnProxied.Load() == 0 || cdnDirect.Load() != 0) {
				t.Errorf("want CDN via proxy: direct=%d proxied=%d", cdnDirect.Load(), cdnProxied.Load())
			}
			if !tc.viaProxy && (cdnDirect.Load() == 0 || cdnProxied.Load() != 0) {
				t.Errorf("want direct CDN: direct=%d proxied=%d", cdnDirect.Load(), cdnProxied.Load())
			}
		})
	}
}

func TestRunInvalidConfig(t *testing.T) {
	layout := testLayout(t)
	if err := os.WriteFile(layout.ConfigFile("recorder"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), layout, Options{}, discard); err == nil {
		t.Fatal("expected config validation error")
	}
}
