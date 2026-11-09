package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"cbr/core/statusapi"
	"cbr/core/store"
)

// fakeLive отдаёт заранее заданный ответ status API и запоминает пользователя.
type fakeLive struct {
	room   statusapi.Room
	status int
	err    error
	asked  string
}

func (f *fakeLive) Fetch(_ context.Context, user string) (statusapi.Room, int, error) {
	f.asked = user
	return f.room, f.status, f.err
}

func liveApp(t *testing.T, live LiveFetcher) *fiber.App {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "cbr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.AddUser(context.Background(), "alice", AddedBy); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Get("/watchlist/:user/live", liveStream(db, live, discard))
	return app
}

func TestLiveStreamPublic(t *testing.T) {
	live := &fakeLive{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/x.m3u8?session=1"}, status: 200}
	app := liveApp(t, live)

	// Имя приводится к нижнему регистру, как при добавлении.
	code, body := do(t, app, http.MethodGet, "/watchlist/Alice/live", "")
	var got LiveStream
	if err := json.Unmarshal([]byte(body), &got); err != nil || code != http.StatusOK {
		t.Fatalf("live: %d %s", code, body)
	}
	if got != (LiveStream{User: "alice", RoomStatus: "public", HLSURL: "https://cdn/x.m3u8?session=1"}) || live.asked != "alice" {
		t.Errorf("got %+v, asked %q", got, live.asked)
	}
}

func TestLiveStreamErrors(t *testing.T) {
	cases := []struct {
		name   string
		live   *fakeLive
		target string
		want   int
		body   string
	}{
		{"not public", &fakeLive{room: statusapi.Room{Status: "private"}, status: 200}, "/watchlist/alice/live", http.StatusConflict, `"roomStatus":"private"`},
		{"public without link", &fakeLive{room: statusapi.Room{Status: "public"}, status: 200}, "/watchlist/alice/live", http.StatusConflict, "not live now"},
		{"not in watchlist", &fakeLive{}, "/watchlist/bob/live", http.StatusNotFound, "not in the watchlist"},
		{"invalid name", &fakeLive{}, "/watchlist/bad%20name/live", http.StatusBadRequest, "error"},
		{"status API 404", &fakeLive{status: 404, err: errors.New("HTTP 404")}, "/watchlist/alice/live", http.StatusNotFound, "not found"},
		{"status API down", &fakeLive{status: 503, err: errors.New("HTTP 503")}, "/watchlist/alice/live", http.StatusBadGateway, "unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := do(t, liveApp(t, tc.live), http.MethodGet, tc.target, "")
			if code != tc.want || !strings.Contains(body, tc.body) {
				t.Errorf("%d %s, want %d with %q", code, body, tc.want, tc.body)
			}
		})
	}
}

func TestLiveStreamNotConfigured(t *testing.T) {
	if code, _ := do(t, liveApp(t, nil), http.MethodGet, "/watchlist/alice/live", ""); code != http.StatusServiceUnavailable {
		t.Errorf("code = %d", code)
	}
}
