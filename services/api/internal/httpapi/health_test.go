package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestHealth(t *testing.T) {
	_, db := watchlistApp(t)
	app := fiber.New()
	app.Get("/health", health(db, "1.3.0"))

	ctx := t.Context()
	// Живой recorder, устаревший converter; tbot остановился и удалил запись.
	if err := db.Heartbeat(ctx, "recorder", "1.3.0", 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := db.Heartbeat(ctx, "converter", "1.2.1", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := db.Heartbeat(ctx, "tbot", "1.3.0", time.Second); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearHeartbeat(ctx, "tbot"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)

	code, body := do(t, app, http.MethodGet, "/health", "")
	if code != http.StatusOK {
		t.Fatalf("GET /health: %d %s", code, body)
	}
	var got Health
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.3.0" || len(got.Services) != 2 {
		t.Fatalf("health = %+v", got)
	}
	conv, rec := got.Services[0], got.Services[1]
	if conv.Name != "converter" || conv.Alive || conv.Version != "1.2.1" {
		t.Errorf("converter = %+v, want stale", conv)
	}
	if rec.Name != "recorder" || !rec.Alive || rec.Interval != 3 || rec.SeenAt == "" {
		t.Errorf("recorder = %+v, want alive", rec)
	}
}
