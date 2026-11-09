package statusapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var discard = slog.New(slog.DiscardHandler)

func TestStatusClientFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/live/":
			fmt.Fprint(w, `{"room_status":"public","broadcaster_username":"live","hls_source":"https://cdn/x.m3u8"}`)
		case "/api/odd/":
			fmt.Fprint(w, `{"room_status":"offline","hls_source":null,"broadcaster_username":5}`)
		case "/api/bad/":
			fmt.Fprint(w, `{`)
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), srv.URL+"/api")
	ctx := context.Background()

	room, status, err := c.Fetch(ctx, "live")
	if err != nil || status != 200 || room != (Room{Status: "public", User: "live", HLSURL: "https://cdn/x.m3u8"}) {
		t.Errorf("live: %+v %d %v", room, status, err)
	}

	room, _, err = c.Fetch(ctx, "odd")
	if err != nil || room != (Room{Status: "offline"}) {
		t.Errorf("odd types must be tolerated: %+v %v", room, err)
	}

	if _, status, err = c.Fetch(ctx, "bad"); err == nil || status != 200 {
		t.Errorf("bad JSON: status=%d err=%v", status, err)
	}

	if _, status, err = c.Fetch(ctx, "missing"); err == nil || status != 404 {
		t.Errorf("missing: status=%d err=%v", status, err)
	}
}

func TestNewHTTPClientRejectsBadProxy(t *testing.T) {
	if _, err := NewHTTPClient("http://[::1", discard); err == nil {
		t.Error("expected error for invalid proxy")
	}
	if _, err := NewHTTPClient("", discard); err != nil {
		t.Errorf("no proxy: %v", err)
	}
}

func TestStatusClientTruncatesErrorBody(t *testing.T) {
	page := strings.Repeat("<html>", 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, page, http.StatusForbidden)
	}))
	defer srv.Close()

	_, status, err := NewClient(srv.Client(), srv.URL).Fetch(context.Background(), "x")
	if status != http.StatusForbidden || err == nil {
		t.Fatalf("status = %d, err = %v", status, err)
	}
	if len(err.Error()) > errorBodyLimit+200 {
		t.Errorf("error message is %d bytes, body must be truncated", len(err.Error()))
	}
}

func TestStatusClientTimesOutOnStalledBody(t *testing.T) {
	defer func(d time.Duration) { statusTimeout = d }(statusTimeout)
	statusTimeout = 100 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"room_status":`)
		w.(http.Flusher).Flush()
		<-r.Context().Done() // тело так и не дописывается
	}))
	defer srv.Close()

	start := time.Now()
	_, _, err := NewClient(srv.Client(), srv.URL).Fetch(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error for a stalled response")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("Fetch took %v, timeout ignored", took)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"":                              0,
		"120":                           2 * time.Minute,
		"-5":                            0,
		"soon":                          0,
		"Fri, 06 Nov 2026 12:01:30 GMT": 90 * time.Second,
		"Fri, 06 Nov 2026 11:00:00 GMT": 0,
	}
	for v, want := range cases {
		if got := parseRetryAfter(v, now); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestStatusClientReturnsRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, status, err := NewClient(srv.Client(), srv.URL).Fetch(context.Background(), "x")
	if status != http.StatusTooManyRequests || RetryAfter(err) != 42*time.Second {
		t.Errorf("status = %d, retryAfter = %v, err = %v", status, RetryAfter(err), err)
	}
}
