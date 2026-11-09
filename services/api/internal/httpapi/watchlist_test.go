package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"cbr/core/store"
)

func watchlistApp(t *testing.T) (*fiber.App, *store.Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "cbr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// recorder в тестах есть только как heartbeat: проверки не дожидаемся (check = pending).
	if err := db.Heartbeat(context.Background(), store.ServiceRecorder, "test", time.Minute); err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	app.Get("/watchlist", listWatchlist(db))
	app.Post("/watchlist", addToWatchlist(db, 10*time.Millisecond))
	app.Delete("/watchlist/:user", removeFromWatchlist(db))
	app.Get("/events", listEvents(db))
	return app, db
}

func do(t *testing.T, app *fiber.App, method, target, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestWatchlistEndpoints(t *testing.T) {
	app, db := watchlistApp(t)

	if code, _ := do(t, app, http.MethodPost, "/watchlist", `{"user":"alice"}`); code != http.StatusCreated {
		t.Fatalf("add: %d", code)
	}
	if code, _ := do(t, app, http.MethodPost, "/watchlist", `{"user":"alice"}`); code != http.StatusConflict {
		t.Fatalf("duplicate add: %d", code)
	}
	if code, _ := do(t, app, http.MethodPost, "/watchlist", `{"user":"bad name"}`); code != http.StatusBadRequest {
		t.Fatalf("invalid add: %d", code)
	}
	if code, _ := do(t, app, http.MethodPost, "/watchlist", `{`); code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", code)
	}
	// Ссылка вместо имени: имя берётся из последнего сегмента пути и возвращается.
	code, body := do(t, app, http.MethodPost, "/watchlist", `{"user":"https://someadress.com/female/bob/"}`)
	if code != http.StatusCreated || !strings.Contains(body, `"user":"bob"`) || !strings.Contains(body, `"check":"pending"`) {
		t.Fatalf("add by link: %d %s", code, body)
	}
	_ = db.RemoveUser(context.Background(), "bob")

	_ = db.SetState(context.Background(), "alice", "public", true)
	code, body = do(t, app, http.MethodGet, "/watchlist", "")
	var items []WatchEntry
	if err := json.Unmarshal([]byte(body), &items); err != nil || code != http.StatusOK {
		t.Fatalf("list: %d %s", code, body)
	}
	if len(items) != 1 || items[0].User != "alice" || items[0].AddedBy != AddedBy ||
		!items[0].Enabled || !items[0].Recording || items[0].RoomStatus != "public" || items[0].UpdatedAt == "" {
		t.Fatalf("items = %+v", items)
	}

	// Имя в любом регистре — тот же пользователь, что и при добавлении.
	if code, _ := do(t, app, http.MethodDelete, "/watchlist/Alice", ""); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := do(t, app, http.MethodDelete, "/watchlist/bad%20name", ""); code != http.StatusBadRequest {
		t.Fatalf("delete invalid: %d", code)
	}
	if code, _ := do(t, app, http.MethodDelete, "/watchlist/alice", ""); code != http.StatusNotFound {
		t.Fatalf("second delete: %d", code)
	}
	if _, body := do(t, app, http.MethodGet, "/watchlist", ""); strings.TrimSpace(body) != "[]" {
		t.Fatalf("empty list = %s", body)
	}
}

// POST /watchlist отвечает по итогу первой проверки recorder'ом.
func TestAddWaitsForFirstCheck(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "cbr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := fiber.New()
	app.Post("/watchlist", addToWatchlist(db, 3*time.Second))
	ctx := context.Background()

	// Без heartbeat recorder'а ответ сразу: добавлен, но не проверен.
	start := time.Now()
	code, body := doTimeout(t, app, `{"user":"carol"}`)
	if code != http.StatusCreated || !strings.Contains(body, `"check":"recorder_offline"`) {
		t.Errorf("carol: %d %s", code, body)
	}
	if time.Since(start) > time.Second {
		t.Error("carol: must not wait for the recorder that is not running")
	}
	if err := db.Heartbeat(ctx, store.ServiceRecorder, "test", time.Minute); err != nil {
		t.Fatal(err)
	}

	// Имитация recorder: ghost не найден и удалён, alice найдена.
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = db.RemoveUser(ctx, "ghost")
		_ = db.SetState(ctx, "alice", "offline", false)
	}()

	code, body = doTimeout(t, app, `{"user":"ghost"}`)
	if code != http.StatusNotFound || !strings.Contains(body, "User ghost not found, not added") {
		t.Errorf("ghost: %d %s", code, body)
	}
	code, body = doTimeout(t, app, `{"user":"alice"}`)
	if code != http.StatusCreated || !strings.Contains(body, `"check":"watching"`) || !strings.Contains(body, `"roomStatus":"offline"`) {
		t.Errorf("alice: %d %s", code, body)
	}
}

func doTimeout(t *testing.T, app *fiber.App, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/watchlist", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestEventsEndpoint(t *testing.T) {
	app, db := watchlistApp(t)
	ctx := context.Background()
	for _, msg := range []string{"one", "two", "three"} {
		_, _ = db.AddEvent(ctx, store.KindRecordingStarted, "alice", msg)
	}

	code, body := do(t, app, http.MethodGet, "/events?after=1&limit=1", "")
	var items []EventItem
	if err := json.Unmarshal([]byte(body), &items); err != nil || code != http.StatusOK {
		t.Fatalf("events: %d %s", code, body)
	}
	if len(items) != 1 || items[0].ID != 2 || items[0].Message != "two" || items[0].Kind != store.KindRecordingStarted {
		t.Fatalf("items = %+v", items)
	}

	for _, q := range []string{"after=x", "after=-1", "limit=0", "limit=abc"} {
		if code, _ := do(t, app, http.MethodGet, "/events?"+q, ""); code != http.StatusBadRequest {
			t.Errorf("%s: code %d, want 400", q, code)
		}
	}
}
