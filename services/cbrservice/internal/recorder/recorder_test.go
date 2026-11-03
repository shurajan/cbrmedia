package recorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cbrservice/internal/hls"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

var fastTiming = timing{
	initialDelay:   time.Millisecond,
	restoreStagger: time.Millisecond,
	techFailure:    5 * time.Millisecond,
	otherFailure:   5 * time.Millisecond,
	publicDelay:    5 * time.Millisecond,
	activeDelay:    5 * time.Millisecond,
	offlineDelay:   5 * time.Millisecond,
}

// fakeStatus отдаёт ответы по очереди; после конца очереди повторяет последний.
type fakeStatus struct {
	mu        sync.Mutex
	responses []statusResponse
	calls     int
}

type statusResponse struct {
	room   Room
	status int
	err    error
}

func (f *fakeStatus) Fetch(_ context.Context, _ string) (Room, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.responses[min(f.calls, len(f.responses)-1)]
	f.calls++
	return r.room, r.status, r.err
}

type recordCall struct {
	url, baseName string
	format        hls.Format
}

type fakeRecorder struct {
	mu    sync.Mutex
	calls []recordCall
	done  chan struct{}
}

func (f *fakeRecorder) DetectFormat(context.Context, string) hls.Format { return hls.FormatMP4 }

func (f *fakeRecorder) Record(_ context.Context, url, baseName string, format hls.Format) error {
	f.mu.Lock()
	f.calls = append(f.calls, recordCall{url, baseName, format})
	first := len(f.calls) == 1
	f.mu.Unlock()
	if first && f.done != nil {
		close(f.done)
	}
	return nil
}

func newTestManager(status StatusFetcher, rec StreamRecorder) *Manager {
	m := NewManager(status, rec, discard)
	m.timing = fastTiming
	return m
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestWatchClientErrorRemovesUserAndNotifies(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{{status: 404, err: errors.New("not found")}}}
	m := newTestManager(status, &fakeRecorder{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx, []string{"alice"})

	select {
	case msg := <-m.Notifications():
		if msg != "User: alice will be deleted till restart. Code: 404" {
			t.Errorf("notification = %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no notification")
	}
	waitFor(t, func() bool { return !m.Watching("alice") })

	cancel()
	m.Wait()
}

func TestWatchRecordsWithFreshURL(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{
		{room: Room{Status: "public", HLSURL: "https://cdn/first.m3u8"}, status: 200},
		{room: Room{Status: "public", HLSURL: "https://cdn/fresh.m3u8"}, status: 200},
		{room: Room{Status: "offline"}, status: 200},
	}}
	rec := &fakeRecorder{done: make(chan struct{})}
	m := newTestManager(status, rec)

	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx, []string{"bob"})

	select {
	case <-rec.done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream was not recorded")
	}
	cancel()
	m.Wait()

	rec.mu.Lock()
	defer rec.mu.Unlock()
	c := rec.calls[0]
	if c.url != "https://cdn/fresh.m3u8" {
		t.Errorf("recorded url = %q, want fresh URL", c.url)
	}
	if !strings.HasPrefix(c.baseName, "bob_") || c.format != hls.FormatMP4 {
		t.Errorf("record call = %+v", c)
	}
}

func TestWatchUnwatchIdempotent(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{{room: Room{Status: "offline"}, status: 200}}}
	m := newTestManager(status, &fakeRecorder{})

	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx, nil)

	m.Watch("carol")
	m.Watch("carol")
	if !m.Watching("carol") {
		t.Fatal("carol must be watched")
	}
	m.Unwatch("carol")
	m.Unwatch("carol")
	if m.Watching("carol") {
		t.Fatal("carol must not be watched")
	}

	cancel()
	m.Wait()
}

func TestStartRestoresUsersAndStopsOnCancel(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{{room: Room{Status: "offline"}, status: 200}}}
	m := newTestManager(status, &fakeRecorder{})

	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx, []string{"a", "b", "c"})
	waitFor(t, func() bool { return m.Watching("a") && m.Watching("b") && m.Watching("c") })

	cancel()
	done := make(chan struct{})
	go func() { m.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not return after cancel")
	}
}

func TestDelayFor(t *testing.T) {
	cases := map[string]time.Duration{
		"public":             defaultTiming.publicDelay,
		"private":            defaultTiming.activeDelay,
		"password protected": defaultTiming.activeDelay,
		"offline":            defaultTiming.offlineDelay,
		"":                   defaultTiming.offlineDelay,
	}
	for status, want := range cases {
		if got := defaultTiming.delayFor(status); got != want {
			t.Errorf("delayFor(%q) = %v, want %v", status, got, want)
		}
	}
}

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
	c := NewStatusClient(srv.Client(), srv.URL+"/api")
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
