// Package recorder следит за пользователями: опрашивает status API и, когда
// стрим начинается, записывает его через hls.Recorder.
package recorder

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"cbrservice/internal/ctxutil"
	"cbrservice/internal/hls"
)

// StatusFetcher получает состояние комнаты пользователя.
type StatusFetcher interface {
	Fetch(ctx context.Context, user string) (Room, int, error)
}

// StreamRecorder определяет формат и записывает стрим.
type StreamRecorder interface {
	DetectFormat(ctx context.Context, url string) hls.Format
	Record(ctx context.Context, url, baseName string, f hls.Format) error
}

// Manager управляет горутинами опроса — по одной на пользователя.
type Manager struct {
	status StatusFetcher
	rec    StreamRecorder
	log    *slog.Logger
	timing timing

	notifications chan string

	mu       sync.Mutex
	ctx      context.Context // базовый контекст, задаётся в Start
	watchers map[string]context.CancelFunc
	wg       sync.WaitGroup
}

// NewManager создаёт менеджер. Перед Watch нужно вызвать Start.
func NewManager(status StatusFetcher, rec StreamRecorder, log *slog.Logger) *Manager {
	return &Manager{
		status:        status,
		rec:           rec,
		log:           log,
		timing:        defaultTiming,
		notifications: make(chan string, 100),
		watchers:      make(map[string]context.CancelFunc),
	}
}

// Notifications — сообщения для владельца (например, пользователь удалён из-за 4xx).
func (m *Manager) Notifications() <-chan string { return m.notifications }

// Start запускает слежение за users с паузой между ними, чтобы не нагружать API
// при старте. Все горутины останавливаются при отмене ctx; дождаться их — Wait.
func (m *Manager) Start(ctx context.Context, users []string) {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()

	m.wg.Go(func() {
		for i, user := range users {
			if i > 0 && ctxutil.Sleep(ctx, m.timing.restoreStagger) {
				return
			}
			m.Watch(user)
		}
	})
}

// Wait ждёт завершения всех горутин менеджера (после отмены контекста Start).
func (m *Manager) Wait() { m.wg.Wait() }

// Watch начинает слежение за пользователем. Повторный вызов для уже
// отслеживаемого пользователя ничего не делает.
func (m *Manager) Watch(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ctx == nil {
		m.log.Error("watch before start", "user", user)
		return
	}
	if m.ctx.Err() != nil {
		return
	}
	if _, ok := m.watchers[user]; ok {
		m.log.Info("already watching", "user", user)
		return
	}

	ctx, cancel := context.WithCancel(m.ctx)
	m.watchers[user] = cancel
	m.log.Info("watch started", "user", user)

	m.wg.Go(func() {
		m.watch(ctx, user)
		m.log.Info("watch stopped", "user", user)
	})
}

// Unwatch прекращает слежение за пользователем (текущая запись завершается).
func (m *Manager) Unwatch(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cancel, ok := m.watchers[user]
	if !ok {
		m.log.Info("no watcher to cancel", "user", user)
		return
	}
	cancel()
	delete(m.watchers, user)
	m.log.Info("watch cancelled", "user", user)
}

// Watching возвращает true, если за пользователем идёт слежение.
func (m *Manager) Watching(user string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.watchers[user]
	return ok
}

// notify отправляет сообщение владельцу; если очередь занята дольше 2 секунд, сообщение теряется.
func (m *Manager) notify(msg string) {
	m.mu.Lock()
	ctx := m.ctx
	m.mu.Unlock()

	select {
	case m.notifications <- msg:
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		m.log.Warn("notification dropped: queue full", "message", msg)
	}
}
