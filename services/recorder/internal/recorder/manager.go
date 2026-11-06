// Package recorder следит за пользователями: опрашивает status API и, когда
// стрим начинается, записывает его через hls.Recorder.
//
// Кого записывать, Manager узнаёт не из команд, а сверкой: Sync получает
// желаемый список (watchlist из общей базы) и запускает или останавливает
// слежения. Состояние и события Manager отдаёт в Sink (общая база).
package recorder

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"cbr/recorder/internal/hls"
)

// StatusFetcher получает состояние комнаты пользователя.
type StatusFetcher interface {
	Fetch(ctx context.Context, user string) (Room, int, error)
}

// StreamRecorder определяет формат и записывает стрим.
type StreamRecorder interface {
	DetectFormat(ctx context.Context, url string) hls.Format
	// Record пишет стрим с места, где закончилась прошлая запись (opts.After),
	// и возвращает длительность до последнего сегмента и его ключ (hls.Result).
	Record(ctx context.Context, url, baseName string, f hls.Format, opts hls.Options) (hls.Result, error)
}

// Sink принимает состояние слежения и события (реализация — core/store).
type Sink interface {
	SetState(ctx context.Context, user, roomStatus string, recording bool) error
	ClearState(ctx context.Context, user string) error
	AddEvent(ctx context.Context, kind, user, message string) (int64, error)
	DisableUser(ctx context.Context, user, reason string) error
	RemoveUser(ctx context.Context, user string) error
	Verified(ctx context.Context, user string) (bool, error)
	MarkVerified(ctx context.Context, user string) error
}

// sinkTimeout — сколько ждать записи в Sink, когда контекст слежения уже отменён.
const sinkTimeout = 5 * time.Second

// watcher — запущенное слежение за одним пользователем.
type watcher struct {
	cancel context.CancelFunc
}

// Manager управляет горутинами опроса — по одной на пользователя.
type Manager struct {
	status StatusFetcher
	rec    StreamRecorder
	sink   Sink
	log    *slog.Logger
	timing timing

	mu       sync.Mutex
	watchers map[string]*watcher
	wg       sync.WaitGroup
}

// NewManager создаёт менеджер.
func NewManager(status StatusFetcher, rec StreamRecorder, sink Sink, log *slog.Logger) *Manager {
	return &Manager{
		status:   status,
		rec:      rec,
		sink:     sink,
		log:      log,
		timing:   defaultTiming,
		watchers: make(map[string]*watcher),
	}
}

// Sync приводит слежения к списку desired: лишние останавливает (текущая
// запись завершается), недостающие запускает с паузой между ними, чтобы не
// нагружать API. Горутины живут до отмены ctx; дождаться их — Wait.
func (m *Manager) Sync(ctx context.Context, desired []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ctx.Err() != nil {
		return
	}

	want := make(map[string]bool, len(desired))
	for _, user := range desired {
		want[user] = true
	}
	for user, w := range m.watchers {
		if !want[user] {
			w.cancel()
			delete(m.watchers, user)
			m.log.Info("watch cancelled", "user", user)
		}
	}

	started := 0
	for _, user := range desired {
		if _, ok := m.watchers[user]; ok {
			continue
		}
		wctx, cancel := context.WithCancel(ctx)
		w := &watcher{cancel: cancel}
		m.watchers[user] = w
		delay := m.timing.initialDelay + time.Duration(started)*m.timing.restoreStagger
		started++
		m.log.Info("watch started", "user", user)

		m.wg.Go(func() {
			m.watch(wctx, user, delay)
			m.forget(user, w)
			m.clearState(wctx, user)
			m.log.Info("watch stopped", "user", user)
		})
	}
}

// forget убирает слежение из списка, если там всё ещё именно оно.
func (m *Manager) forget(user string, w *watcher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.watchers[user] == w {
		w.cancel()
		delete(m.watchers, user)
	}
}

// Wait ждёт завершения всех горутин менеджера (после отмены контекста Sync).
func (m *Manager) Wait() { m.wg.Wait() }

// Watching возвращает true, если за пользователем идёт слежение.
func (m *Manager) Watching(user string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.watchers[user]
	return ok
}

// sinkCtx — контекст для записи в Sink, который переживает отмену слежения
// (событие о конце записи должно сохраниться и при остановке службы).
func sinkCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), sinkTimeout)
}

func (m *Manager) setState(ctx context.Context, user, roomStatus string, recording bool) {
	if err := m.sink.SetState(ctx, user, roomStatus, recording); err != nil && ctx.Err() == nil {
		m.log.Warn("save state failed", "user", user, "err", err)
	}
}

func (m *Manager) clearState(ctx context.Context, user string) {
	sctx, cancel := sinkCtx(ctx)
	defer cancel()
	if err := m.sink.ClearState(sctx, user); err != nil {
		m.log.Warn("clear state failed", "user", user, "err", err)
	}
}

func (m *Manager) event(ctx context.Context, kind, user, message string) {
	sctx, cancel := sinkCtx(ctx)
	defer cancel()
	if _, err := m.sink.AddEvent(sctx, kind, user, message); err != nil {
		m.log.Warn("save event failed", "user", user, "kind", kind, "err", err)
	}
}
