package recorder

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"cbrservice/internal/ctxutil"
)

// watch — цикл опроса одного пользователя. Когда у комнаты появляется
// hls_source, стрим записывается до конца, затем опрос продолжается.
// Ответ 4xx снимает пользователя со слежения до перезапуска сервиса.
func (m *Manager) watch(ctx context.Context, user string) {
	log := m.log.With("user", user)

	if ctxutil.Sleep(ctx, m.timing.initialDelay) {
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
				m.notify(fmt.Sprintf("User: %s will be deleted till restart. Code: %d", user, status))
				m.Unwatch(user)
				log.Warn("user removed from watch list until restart", "status", status)
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
	if err := m.rec.Record(ctx, fresh.HLSURL, baseName, format); err != nil {
		log.Error("download failed", "file", file, "err", err)
		return true
	}
	log.Info("download finished", "file", file)
	return true
}
