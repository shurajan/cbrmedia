package recorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"cbr/core/store"
	"cbr/recorder/internal/hls"
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
	dailyDelay:     5 * time.Millisecond,
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

// fakeSink запоминает события, выключенных пользователей и последнее состояние.
type fakeSink struct {
	mu       sync.Mutex
	events   []store.Event
	disabled map[string]string
	removed  []string
	states   map[string]bool   // user -> recording
	rooms    map[string]string // user -> последний room_status
	verified map[string]bool
}

func newFakeSink() *fakeSink {
	return &fakeSink{disabled: map[string]string{}, states: map[string]bool{}, rooms: map[string]string{}, verified: map[string]bool{}}
}

func (f *fakeSink) SetState(_ context.Context, user, roomStatus string, recording bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[user] = recording
	f.rooms[user] = roomStatus
	return nil
}

func (f *fakeSink) RemoveUser(_ context.Context, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, user)
	return nil
}

func (f *fakeSink) Verified(_ context.Context, user string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verified[user], nil
}

func (f *fakeSink) MarkVerified(_ context.Context, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verified[user] = true
	return nil
}

func (f *fakeSink) isVerified(user string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verified[user]
}

func (f *fakeSink) room(user string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rooms[user]
}

func (f *fakeSink) ClearState(_ context.Context, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.states, user)
	return nil
}

func (f *fakeSink) AddEvent(_ context.Context, kind, user, message string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, store.Event{ID: int64(len(f.events) + 1), Kind: kind, User: user, Message: message})
	return int64(len(f.events)), nil
}

func (f *fakeSink) DisableUser(_ context.Context, user, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disabled[user] = reason
	return nil
}

func (f *fakeSink) kinds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var kinds []string
	for _, e := range f.events {
		kinds = append(kinds, e.Kind)
	}
	return kinds
}

func newTestManager(status StatusFetcher, rec StreamRecorder) (*Manager, *fakeSink) {
	sink := newFakeSink()
	m := NewManager(status, rec, sink, discard)
	m.timing = fastTiming
	return m, sink
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

func TestWatchNotFoundRemovesUserAfterLimit(t *testing.T) {
	for _, code := range []int{404, 410} {
		status := &fakeStatus{responses: []statusResponse{{status: code, err: errors.New("not found")}}}
		m, sink := newTestManager(status, &fakeRecorder{})
		sink.verified["alice"] = true // пользователь уже отвечал успешно

		ctx, cancel := context.WithCancel(context.Background())
		m.Sync(ctx, []string{"alice"})
		waitFor(t, func() bool { return !m.Watching("alice") })
		cancel()
		m.Wait()

		if calls := status.calls; calls != notFoundLimit {
			t.Errorf("HTTP %d: status calls = %d, want %d", code, calls, notFoundLimit)
		}
		if !slices.Equal(sink.removed, []string{"alice"}) || len(sink.disabled) != 0 {
			t.Errorf("HTTP %d: removed = %v, disabled = %v", code, sink.removed, sink.disabled)
		}
		if kinds := sink.kinds(); len(kinds) != 1 || kinds[0] != store.KindUserRemoved {
			t.Errorf("HTTP %d: events = %v", code, kinds)
		}
	}
}

// Неподтверждённый пользователь (ещё ни одного успешного ответа) на 404 удаляется сразу.
func TestWatchNotFoundRejectsUnverifiedUser(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{{status: 404, err: errors.New("not found")}}}
	m, sink := newTestManager(status, &fakeRecorder{})

	ctx, cancel := context.WithCancel(context.Background())
	m.Sync(ctx, []string{"ghost"})
	waitFor(t, func() bool { return !m.Watching("ghost") })
	cancel()
	m.Wait()

	if status.calls != 1 || !slices.Equal(sink.removed, []string{"ghost"}) {
		t.Errorf("calls = %d, removed = %v", status.calls, sink.removed)
	}
	if len(sink.events) != 1 || sink.events[0].Kind != store.KindUserRejected ||
		!strings.Contains(sink.events[0].Message, "not added") {
		t.Errorf("events = %+v", sink.events)
	}
}

// Первый успешный ответ подтверждает пользователя, после этого 404 считаются по счётчику.
func TestWatchSuccessMarksVerified(t *testing.T) {
	nf := statusResponse{status: 404, err: errors.New("not found")}
	ok := statusResponse{room: Room{Status: "offline"}, status: 200}
	status := &fakeStatus{responses: []statusResponse{ok, nf}}
	m, sink := newTestManager(status, &fakeRecorder{})

	ctx, cancel := context.WithCancel(context.Background())
	m.Sync(ctx, []string{"alice"})
	waitFor(t, func() bool { return !m.Watching("alice") })
	cancel()
	m.Wait()

	if !sink.isVerified("alice") || status.calls != 1+notFoundLimit {
		t.Errorf("verified = %v, calls = %d", sink.isVerified("alice"), status.calls)
	}
}

// Успешный ответ между 404 сбрасывает счётчик: удаление только после notFoundLimit подряд.
func TestWatchNotFoundCounterResetsOnSuccess(t *testing.T) {
	nf := statusResponse{status: 404, err: errors.New("not found")}
	ok := statusResponse{room: Room{Status: "offline"}, status: 200}
	status := &fakeStatus{responses: []statusResponse{nf, nf, ok, nf, nf, nf}}
	m, sink := newTestManager(status, &fakeRecorder{})
	sink.verified["alice"] = true // пользователь уже отвечал успешно

	ctx, cancel := context.WithCancel(context.Background())
	m.Sync(ctx, []string{"alice"})
	waitFor(t, func() bool { return !m.Watching("alice") })
	cancel()
	m.Wait()

	if status.calls != 6 || !slices.Equal(sink.removed, []string{"alice"}) {
		t.Errorf("calls = %d, removed = %v", status.calls, sink.removed)
	}
}

func TestWatchNotFoundKeepsWatchingWithState(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{{status: 404, err: errors.New("not found")}}}
	m, sink := newTestManager(status, &fakeRecorder{})
	sink.verified["alice"] = true // пользователь уже отвечал успешно
	m.timing.dailyDelay = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	m.Sync(ctx, []string{"alice"})
	waitFor(t, func() bool { return sink.room("alice") == store.RoomNotFound })
	if !m.Watching("alice") || len(sink.removed) != 0 || len(sink.kinds()) != 0 {
		t.Errorf("watching = %v, removed = %v, events = %v", m.Watching("alice"), sink.removed, sink.kinds())
	}
	cancel()
	m.Wait()
}

func TestWatchAccessDeniedKeepsWatchingWithBlockedState(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{
		{status: 401, err: errors.New("unauthorized")},
		{room: Room{Status: "offline"}, status: 200},
	}}
	m, sink := newTestManager(status, &fakeRecorder{})
	m.timing.dailyDelay = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Sync(ctx, []string{"alice"})

	waitFor(t, func() bool { return sink.room("alice") == store.RoomBlocked })
	if !m.Watching("alice") {
		t.Fatal("blocked user must stay watched")
	}
	// После паузы опрос продолжается, и состояние обновляется.
	waitFor(t, func() bool { return sink.room("alice") == "offline" })
	cancel()
	m.Wait()

	if len(sink.removed) != 0 || len(sink.disabled) != 0 || len(sink.kinds()) != 0 {
		t.Errorf("removed = %v, disabled = %v, events = %v", sink.removed, sink.disabled, sink.kinds())
	}
}

func TestClassifyFailure(t *testing.T) {
	cases := map[int]failureAction{
		0: actionRetry, 400: actionDisable, 401: actionBlocked, 403: actionBlocked, 404: actionNotFound,
		410: actionNotFound, 422: actionDisable, 429: actionRetry, 451: actionBlocked, 500: actionRetry, 503: actionRetry,
	}
	for code, want := range cases {
		if got := classifyFailure(code); got != want {
			t.Errorf("classifyFailure(%d) = %d, want %d", code, got, want)
		}
	}
}

func TestWatchClientErrorDisablesUser(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{{status: 400, err: errors.New("bad request")}}}
	m, sink := newTestManager(status, &fakeRecorder{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Sync(ctx, []string{"alice"})

	waitFor(t, func() bool { return !m.Watching("alice") })
	cancel()
	m.Wait()

	if reason := sink.disabled["alice"]; reason != "status API returned HTTP 400" {
		t.Errorf("disable reason = %q", reason)
	}
	if kinds := sink.kinds(); len(kinds) != 1 || kinds[0] != store.KindUserDisabled {
		t.Errorf("events = %v", kinds)
	}
}

func TestWatchRecordsWithFreshURL(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{
		{room: Room{Status: "public", HLSURL: "https://cdn/first.m3u8"}, status: 200},
		{room: Room{Status: "public", HLSURL: "https://cdn/fresh.m3u8"}, status: 200},
		{room: Room{Status: "offline"}, status: 200},
	}}
	rec := &fakeRecorder{done: make(chan struct{})}
	m, sink := newTestManager(status, rec)

	ctx, cancel := context.WithCancel(context.Background())
	m.Sync(ctx, []string{"bob"})

	select {
	case <-rec.done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream was not recorded")
	}
	waitFor(t, func() bool { return len(sink.kinds()) >= 2 })
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
	if kinds := sink.kinds(); kinds[0] != store.KindRecordingStarted || kinds[1] != store.KindRecordingFinished {
		t.Errorf("events = %v", kinds)
	}
	if _, ok := sink.states["bob"]; ok {
		t.Error("state must be cleared after the watch stops")
	}
}

func TestSyncStartsAndStopsWatches(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{{room: Room{Status: "offline"}, status: 200}}}
	m, _ := newTestManager(status, &fakeRecorder{})

	ctx, cancel := context.WithCancel(context.Background())
	m.Sync(ctx, []string{"a", "b", "c"})
	m.Sync(ctx, []string{"a", "b", "c"}) // повторная сверка ничего не меняет
	if !m.Watching("a") || !m.Watching("b") || !m.Watching("c") {
		t.Fatal("all users must be watched")
	}

	m.Sync(ctx, []string{"b"})
	if m.Watching("a") || !m.Watching("b") || m.Watching("c") {
		t.Fatal("only b must be watched")
	}

	cancel()
	done := make(chan struct{})
	go func() { m.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not return after cancel")
	}

	m.Sync(ctx, []string{"d"}) // после отмены новые слежения не запускаются
	if m.Watching("d") {
		t.Fatal("no watches after cancel")
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
