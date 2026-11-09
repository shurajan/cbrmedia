package recorder

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"cbr/core/statusapi"
	"cbr/core/store"
	"cbr/recorder/internal/hls"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

var fastTiming = timing{
	initialDelay:   time.Millisecond,
	restoreStagger: time.Millisecond,
	techFailure:    5 * time.Millisecond,
	rateLimited:    5 * time.Millisecond,
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
	room   statusapi.Room
	status int
	err    error
}

func (f *fakeStatus) Fetch(_ context.Context, _ string) (statusapi.Room, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.responses[min(f.calls, len(f.responses)-1)]
	f.calls++
	return r.room, r.status, r.err
}

type recordCall struct {
	url, baseName string
	format        hls.Format
	after         string
}

type fakeRecorder struct {
	mu     sync.Mutex
	calls  []recordCall
	done   chan struct{}
	err    error         // что возвращает Record
	active time.Duration // время до последнего сегмента, которое возвращает Record
	sleep  time.Duration // сколько длится Record
	last   string        // ключ последнего сегмента, который возвращает Record
	// stale — Record отвечает ErrNoNewSegments (не вызывая OnStart), если opts.After == stale.
	stale string
}

func (f *fakeRecorder) DetectFormat(context.Context, string) hls.Format { return hls.FormatMP4 }

func (f *fakeRecorder) Record(ctx context.Context, url, baseName string, format hls.Format, opts hls.Options) (hls.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, recordCall{url, baseName, format, opts.After})
	first := len(f.calls) == 1
	f.mu.Unlock()
	if first && f.done != nil {
		close(f.done)
	}
	if f.stale != "" && opts.After == f.stale {
		return hls.Result{}, hls.ErrNoNewSegments
	}
	// Как настоящий Record: первый сегмент записан сразу, дальше идёт запись.
	if opts.OnStart != nil {
		opts.OnStart()
	}
	if f.sleep > 0 {
		select {
		case <-time.After(f.sleep):
		case <-ctx.Done():
		}
	}
	return hls.Result{Active: f.active, Last: f.last}, f.err
}

// afters возвращает opts.After каждого вызова Record по порядку.
func (f *fakeRecorder) afters() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.after)
	}
	return out
}

func (f *fakeRecorder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
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
	if slices.Contains(f.removed, user) {
		return store.ErrUserNotFound
	}
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

func (f *fakeSink) DisableUser(_ context.Context, user, reason string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, already := f.disabled[user]; already {
		return false, nil
	}
	f.disabled[user] = reason
	return true, nil
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
	ok := statusResponse{room: statusapi.Room{Status: "offline"}, status: 200}
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
	ok := statusResponse{room: statusapi.Room{Status: "offline"}, status: 200}
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
		{room: statusapi.Room{Status: "offline"}, status: 200},
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
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/first.m3u8"}, status: 200},
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/fresh.m3u8"}, status: 200},
		{room: statusapi.Room{Status: "offline"}, status: 200},
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
	status := &fakeStatus{responses: []statusResponse{{room: statusapi.Room{Status: "offline"}, status: 200}}}
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

func TestWatchTechnicalFailuresKeepUser(t *testing.T) {
	// 5xx, сетевая ошибка, 429 и битый JSON — временные: пользователь остаётся, затем опрос идёт дальше.
	status := &fakeStatus{responses: []statusResponse{
		{status: 503, err: errors.New("unavailable")},
		{status: 0, err: errors.New("connection refused")},
		{status: 429, err: errors.New("too many requests")},
		{status: 200, err: errors.New("decode JSON")},
		{room: statusapi.Room{Status: "offline"}, status: 200},
	}}
	m, sink := newTestManager(status, &fakeRecorder{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Sync(ctx, []string{"alice"})

	waitFor(t, func() bool { return sink.room("alice") == "offline" })
	cancel()
	m.Wait()

	if len(sink.removed) != 0 || len(sink.disabled) != 0 || len(sink.kinds()) != 0 {
		t.Errorf("removed = %v, disabled = %v, events = %v", sink.removed, sink.disabled, sink.kinds())
	}
	if !sink.isVerified("alice") {
		t.Error("user must be verified after the first successful response")
	}
}

func TestWatchSkipsRecordingWhenRefreshFails(t *testing.T) {
	// Свежий URL получить не удалось или стрим уже закончился — запись не начинается.
	status := &fakeStatus{responses: []statusResponse{
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/a.m3u8"}, status: 200},
		{status: 502, err: errors.New("bad gateway")},
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/b.m3u8"}, status: 200},
		{room: statusapi.Room{Status: "public"}, status: 200},
		{room: statusapi.Room{Status: "offline"}, status: 200},
	}}
	rec := &fakeRecorder{}
	m, sink := newTestManager(status, rec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Sync(ctx, []string{"bob"})

	waitFor(t, func() bool { return sink.room("bob") == "offline" })
	cancel()
	m.Wait()

	if n := rec.callCount(); n != 0 {
		t.Errorf("Record called %d times, want 0", n)
	}
	if kinds := sink.kinds(); len(kinds) != 0 {
		t.Errorf("events = %v", kinds)
	}
}

func TestWatchRecordingFailureEmitsEvent(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/a.m3u8"}, status: 200},
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/a.m3u8"}, status: 200},
		{room: statusapi.Room{Status: "offline"}, status: 200},
	}}
	rec := &fakeRecorder{err: errors.New("disk full")}
	m, sink := newTestManager(status, rec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Sync(ctx, []string{"bob"})

	waitFor(t, func() bool { return len(sink.kinds()) >= 2 })
	cancel()
	m.Wait()

	if kinds := sink.kinds(); kinds[0] != store.KindRecordingStarted || kinds[1] != store.KindRecordingFailed {
		t.Errorf("events = %v", kinds)
	}
	if sink.events[1].Message != "Recording bob failed: disk full" {
		t.Errorf("message = %q", sink.events[1].Message)
	}
}

// liveStatus — комната всё время public с hls_source.
func liveStatus() *fakeStatus {
	return &fakeStatus{responses: []statusResponse{
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/a.m3u8"}, status: 200},
	}}
}

// recordCallsWithin запускает слежение за bob и возвращает число вызовов Record за d.
func recordCallsWithin(t *testing.T, m *Manager, rec *fakeRecorder, d time.Duration) int {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	m.Sync(ctx, []string{"bob"})
	time.Sleep(d)
	cancel()
	m.Wait()
	return rec.callCount()
}

func TestWatchBacksOffAfterFailedRecordings(t *testing.T) {
	// Стрим объявлен, но записать не выходит: паузы 20, 40, 80, 160 мс… вместо 20 мс каждый раз.
	rec := &fakeRecorder{err: errors.New("playlist 403")}
	m, _ := newTestManager(liveStatus(), rec)
	m.timing.publicDelay = 20 * time.Millisecond
	m.timing.offlineDelay = time.Hour
	m.timing.longRecording = time.Hour

	if n := recordCallsWithin(t, m, rec, 400*time.Millisecond); n < 2 || n > 6 {
		t.Errorf("Record called %d times in 400ms, want 2..6 with backoff", n)
	}
}

func TestWatchRepollsRightAfterLongRecording(t *testing.T) {
	rec := &fakeRecorder{}
	m, _ := newTestManager(liveStatus(), rec)
	m.timing.publicDelay = time.Hour // обычная пауза не наступит
	m.timing.longRecording = 0       // любая запись считается долгой

	if n := recordCallsWithin(t, m, rec, 200*time.Millisecond); n < 2 {
		t.Errorf("Record called %d times, want an immediate new recording after a long one", n)
	}
}

func TestWatchWaitsAfterShortRecording(t *testing.T) {
	rec := &fakeRecorder{}
	m, _ := newTestManager(liveStatus(), rec)
	m.timing.publicDelay = time.Hour
	m.timing.longRecording = time.Hour

	if n := recordCallsWithin(t, m, rec, 200*time.Millisecond); n != 1 {
		t.Errorf("Record called %d times, want 1: short recording waits for the usual delay", n)
	}
}

func TestWatchWaitsAfterRecordingEndedByStall(t *testing.T) {
	// Застывший после эфира плейлист: Record идёт дольше longRecording (ждёт застой),
	// но последний сегмент записан сразу — это короткая запись, опрос не сразу.
	rec := &fakeRecorder{sleep: 50 * time.Millisecond, active: time.Millisecond}
	m, _ := newTestManager(liveStatus(), rec)
	m.timing.publicDelay = time.Hour
	m.timing.longRecording = 20 * time.Millisecond

	if n := recordCallsWithin(t, m, rec, 300*time.Millisecond); n != 1 {
		t.Errorf("Record called %d times, want 1: stall wait must not count as recording time", n)
	}
}

func TestWatchRateLimitUsesRetryAfter(t *testing.T) {
	status := &fakeStatus{responses: []statusResponse{
		{status: 429, err: &statusapi.StatusError{Code: 429, RetryAfter: time.Hour, Message: "slow down"}},
		{room: statusapi.Room{Status: "offline"}, status: 200},
	}}
	m, sink := newTestManager(status, &fakeRecorder{})
	m.timing.techFailure = 30 * time.Millisecond // предел для Retry-After

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Sync(ctx, []string{"alice"})
	waitFor(t, func() bool { return sink.room("alice") == "offline" })
}

func TestRecordRetryDelay(t *testing.T) {
	want := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute,
		4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for i, w := range want {
		if got := defaultTiming.recordRetryDelay(i + 1); got != w {
			t.Errorf("recordRetryDelay(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestRateLimitDelay(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		0:                defaultTiming.rateLimited,
		30 * time.Second: 30 * time.Second,
		time.Hour:        defaultTiming.techFailure,
	}
	for ra, want := range cases {
		if got := defaultTiming.rateLimitDelay(ra); got != want {
			t.Errorf("rateLimitDelay(%v) = %v, want %v", ra, got, want)
		}
	}
}

func TestWatchResumesAfterLastSegment(t *testing.T) {
	// Запись оборвалась посреди стрима: следующая продолжается после последнего записанного сегмента.
	rec := &fakeRecorder{last: "https://cdn/s9.ts"}
	m, _ := newTestManager(liveStatus(), rec)
	m.timing.publicDelay = time.Hour
	m.timing.longRecording = 0 // любая запись долгая: опрос и новая запись сразу

	recordCallsWithin(t, m, rec, 100*time.Millisecond)
	afters := rec.afters()
	if len(afters) < 2 || afters[0] != "" || afters[1] != "https://cdn/s9.ts" {
		t.Errorf("Record After = %q, want \"\" then the last recorded segment", afters)
	}
}

func TestWatchDoesNotRecordStalePlaylistAgain(t *testing.T) {
	// После эфира CDN отдаёт застывший плейлист, а status API ещё отдаёт hls_source:
	// вторая попытка видит, что новее записанного ничего нет, — ни файла, ни событий.
	rec := &fakeRecorder{last: "https://cdn/s9.ts", stale: "https://cdn/s9.ts"}
	m, sink := newTestManager(liveStatus(), rec)
	m.timing.publicDelay = time.Hour
	m.timing.longRecording = 0

	if n := recordCallsWithin(t, m, rec, 100*time.Millisecond); n != 2 {
		t.Errorf("Record called %d times, want 2: the stale attempt waits for the usual delay", n)
	}
	if kinds := sink.kinds(); !slices.Equal(kinds, []string{store.KindRecordingStarted, store.KindRecordingFinished}) {
		t.Errorf("events = %v, want one started/finished pair", kinds)
	}
	if sink.states["bob"] {
		t.Error("recording state left on after a stale attempt")
	}
}

func TestWatchForgetsLastSegmentWhenStreamEnds(t *testing.T) {
	// Стрим закончился по данным status API — новый стрим пишется с начала.
	status := &fakeStatus{responses: []statusResponse{
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/a.m3u8"}, status: 200},
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/a.m3u8"}, status: 200}, // свежий URL
		{room: statusapi.Room{Status: "offline"}, status: 200},
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/b.m3u8"}, status: 200},
		{room: statusapi.Room{Status: "public", HLSURL: "https://cdn/b.m3u8"}, status: 200}, // свежий URL
		{room: statusapi.Room{Status: "offline"}, status: 200},
	}}
	rec := &fakeRecorder{last: "https://cdn/s9.ts"}
	m, _ := newTestManager(status, rec)
	m.timing.longRecording = time.Hour // короткая запись: обычная пауза, затем опрос

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Sync(ctx, []string{"bob"})
	waitFor(t, func() bool { return rec.callCount() >= 2 })
	cancel()
	m.Wait()

	if afters := rec.afters(); !slices.Equal(afters[:2], []string{"", ""}) {
		t.Errorf("Record After = %q, want empty for a new stream", afters)
	}
}
