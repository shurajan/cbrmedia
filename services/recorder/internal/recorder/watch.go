package recorder

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"cbr/core/ctxutil"
	"cbr/core/store"
)

// watch — цикл опроса одного пользователя. Когда у комнаты появляется
// hls_source, стрим записывается до конца, затем опрос продолжается.
// Ответ 4xx выключает пользователя в списке слежения (AddUser включит снова).
func (m *Manager) watch(ctx context.Context, user string, initialDelay time.Duration) {
	log := m.log.With("user", user)

	if ctxutil.Sleep(ctx, initialDelay) {
		return
	}

	for {
		if ctx.Err() != nil {
			return
		}

		room, status, err := m.status.Fetch(ctx, user)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("status fetch failed", "status", status, "err", err)

			if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
				m.disable(ctx, user, status)
				return
			}

			backoff := m.timing.otherFailure
			if status >= http.StatusInternalServerError || status == 0 {
				backoff = m.timing.techFailure
				log.Warn("technical failure, backing off", "status", status, "backoff", backoff)
			}
			if ctxutil.Sleep(ctx, backoff) {
				return
			}
			continue
		}

		log.Info("room status", "status", room.Status)
		m.setState(ctx, user, room.Status, false)

		if room.HLSURL != "" && !m.record(ctx, user, room.HLSURL) {
			// Свежий URL получить не удалось — сразу опрашиваем снова, чтобы не пропустить начало стрима.
			continue
		}

		delay := m.timing.delayFor(room.Status)
		log.Debug("next poll", "delay", delay)
		if ctxutil.Sleep(ctx, delay) {
			return
		}
	}
}

// disable выключает пользователя после ответа 4xx и сообщает об этом.
func (m *Manager) disable(ctx context.Context, user string, status int) {
	reason := fmt.Sprintf("status API returned HTTP %d", status)

	sctx, cancel := sinkCtx(ctx)
	defer cancel()
	if err := m.sink.DisableUser(sctx, user, reason); err != nil {
		m.log.Error("disable user failed", "user", user, "err", err)
	}
	m.event(ctx, store.KindUserDisabled, user,
		fmt.Sprintf("User %s disabled: %s. Add it again to resume.", user, reason))
	m.log.Warn("user disabled", "user", user, "status", status)
}

// record определяет формат по первому URL, затем берёт свежий URL (чтобы
// CDN-токен не был использован к началу записи) и записывает стрим.
// Возвращает false, если запись не началась из-за неудачи с получением свежего URL.
func (m *Manager) record(ctx context.Context, user, streamURL string) bool {
	log := m.log.With("user", user)
	baseName := fmt.Sprintf("%s_%s", user, time.Now().Format("2006-01-02_15-04"))

	// Формат определяется по тексту манифеста, без чтения медиапотока.
	format := m.rec.DetectFormat(ctx, streamURL)

	fresh, status, err := m.status.Fetch(ctx, user)
	if err != nil {
		log.Warn("refresh stream URL failed", "status", status, "err", err)
		return false
	}
	if fresh.HLSURL == "" {
		log.Info("stream ended before download could start")
		return false
	}

	file := baseName + "." + format.Ext
	log.Info("downloading stream", "file", file)
	m.setState(ctx, user, fresh.Status, true)
	m.event(ctx, store.KindRecordingStarted, user, fmt.Sprintf("Recording %s started: %s", user, file))

	err = m.rec.Record(ctx, fresh.HLSURL, baseName, format)
	m.setState(ctx, user, fresh.Status, false)
	switch {
	case err != nil:
		log.Error("download failed", "file", file, "err", err)
		m.event(ctx, store.KindRecordingFailed, user, fmt.Sprintf("Recording %s failed: %v", user, err))
	case ctx.Err() != nil:
		log.Info("download stopped", "file", file)
		m.event(ctx, store.KindRecordingFinished, user, fmt.Sprintf("Recording %s stopped: %s", user, file))
	default:
		log.Info("download finished", "file", file)
		m.event(ctx, store.KindRecordingFinished, user, fmt.Sprintf("Recording %s finished: %s", user, file))
	}
	return true
}
