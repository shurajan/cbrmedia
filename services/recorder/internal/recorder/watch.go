package recorder

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"cbr/core/ctxutil"
	"cbr/core/store"
)

// watch — цикл опроса одного пользователя. Когда у комнаты появляется
// hls_source, стрим записывается до конца, затем опрос продолжается.
// Ошибки status API разбирает classifyFailure: 404/410 и 401/403/451 переводят
// на проверку раз в сутки (после notFoundLimit ответов 404/410 подряд пользователь
// удаляется из списка слежения), прочие
// 4xx выключают пользователя (AddUser включит снова), остальное — повтор позже.
func (m *Manager) watch(ctx context.Context, user string, initialDelay time.Duration) {
	log := m.log.With("user", user)

	if ctxutil.Sleep(ctx, initialDelay) {
		return
	}

	// verified — status API хоть раз ответил о пользователе успешно (хранится в базе).
	// Неподтверждённого 404/410 удаляет сразу: такого пользователя, скорее всего, нет вовсе.
	verified := m.verified(ctx, user)
	notFound := 0         // ответов 404/410 подряд
	failedRecordings := 0 // неудачных попыток записи подряд
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

			var backoff time.Duration
			switch classifyFailure(status) {
			case actionNotFound:
				if !verified {
					m.reject(ctx, user, status)
					return
				}
				notFound++
				if notFound >= notFoundLimit {
					m.remove(ctx, user, status, notFound)
					return
				}
				backoff = m.timing.dailyDelay
				m.setState(ctx, user, store.RoomNotFound, false)
				log.Warn("user not found, checking again later", "status", status,
					"attempt", notFound, "limit", notFoundLimit, "backoff", backoff)
			case actionDisable:
				m.disable(ctx, user, status)
				return
			case actionBlocked:
				backoff = m.timing.dailyDelay
				m.setState(ctx, user, store.RoomBlocked, false)
				log.Warn("access denied, checking again later", "status", status, "backoff", backoff)
			default:
				switch {
				case status == http.StatusTooManyRequests:
					backoff = m.timing.rateLimitDelay(retryAfter(err))
					log.Warn("rate limited, backing off", "backoff", backoff)
				case status >= http.StatusInternalServerError || status == 0:
					backoff = m.timing.techFailure
					log.Warn("technical failure, backing off", "status", status, "backoff", backoff)
				default:
					backoff = m.timing.otherFailure
				}
			}
			if ctxutil.Sleep(ctx, backoff) {
				return
			}
			continue
		}

		notFound = 0
		if !verified {
			verified = m.markVerified(ctx, user)
		}
		log.Info("room status", "status", room.Status)
		m.setState(ctx, user, room.Status, false)

		if room.HLSURL == "" {
			failedRecordings = 0
		} else {
			switch m.record(ctx, user, room.HLSURL) {
			case recordSkipped:
				// Свежий URL получить не удалось — сразу опрашиваем снова, чтобы не пропустить начало стрима.
				continue
			case recordDone:
				// После долгой записи сразу опрашиваем снова: она могла оборваться
				// посреди стрима (например, истёк CDN-токен), продолжение пойдёт в новый файл.
				failedRecordings = 0
				continue
			case recordShort:
				failedRecordings = 0
			case recordFailed:
				failedRecordings++
				delay := m.timing.recordRetryDelay(failedRecordings)
				log.Warn("recording failed, retrying later", "attempt", failedRecordings, "delay", delay)
				if ctxutil.Sleep(ctx, delay) {
					return
				}
				continue
			}
		}

		delay := m.timing.delayFor(room.Status)
		log.Debug("next poll", "delay", delay)
		if ctxutil.Sleep(ctx, delay) {
			return
		}
	}
}

// verified читает из базы, подтверждён ли пользователь. При ошибке чтения
// считает подтверждённым, чтобы не удалить существующего пользователя по ошибке.
func (m *Manager) verified(ctx context.Context, user string) bool {
	ok, err := m.sink.Verified(ctx, user)
	if err != nil {
		m.log.Warn("read verified failed", "user", user, "err", err)
		return true
	}
	return ok
}

// markVerified отмечает пользователя подтверждённым. Возвращает false, если
// записать не удалось (попробуем при следующем успешном ответе).
func (m *Manager) markVerified(ctx context.Context, user string) bool {
	if err := m.sink.MarkVerified(ctx, user); err != nil {
		m.log.Warn("mark verified failed", "user", user, "err", err)
		return false
	}
	return true
}

// reject удаляет только что добавленного пользователя, которого status API не знает
// (404/410 до первого успешного ответа), и сообщает об этом.
func (m *Manager) reject(ctx context.Context, user string, status int) {
	m.removeUser(ctx, user)
	m.event(ctx, store.KindUserRejected, user,
		fmt.Sprintf("User %s not found (status API returned HTTP %d), not added.", user, status))
	m.log.Warn("user not found, rejected", "user", user, "status", status)
}

func (m *Manager) removeUser(ctx context.Context, user string) {
	sctx, cancel := sinkCtx(ctx)
	defer cancel()
	if err := m.sink.RemoveUser(sctx, user); err != nil && !errors.Is(err, store.ErrUserNotFound) {
		m.log.Error("remove user failed", "user", user, "err", err)
	}
}

// remove убирает из списка слежения пользователя, которого нет (404/410 attempts раз подряд), и сообщает об этом.
func (m *Manager) remove(ctx context.Context, user string, status, attempts int) {
	m.removeUser(ctx, user)
	m.event(ctx, store.KindUserRemoved, user,
		fmt.Sprintf("User %s removed: status API returned HTTP %d (user not found) %d times in a row.", user, status, attempts))
	m.log.Warn("user removed", "user", user, "status", status)
}

// disable выключает пользователя после прочих ответов 4xx и сообщает об этом.
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

// recordResult — итог попытки записи.
type recordResult int

const (
	recordSkipped recordResult = iota // свежий URL получить не удалось, запись не начиналась
	recordDone                        // запись не короче longRecording завершилась (или слежение остановлено)
	recordShort                       // запись завершилась быстрее longRecording
	recordFailed                      // запись не удалась (в том числе hls.ErrNoSegments)
)

// record определяет формат по первому URL, затем берёт свежий URL (чтобы
// CDN-токен не был использован к началу записи) и записывает стрим.
func (m *Manager) record(ctx context.Context, user, streamURL string) recordResult {
	log := m.log.With("user", user)
	baseName := fmt.Sprintf("%s_%s", user, time.Now().Format("2006-01-02_15-04"))

	// Формат определяется по тексту манифеста, без чтения медиапотока.
	format := m.rec.DetectFormat(ctx, streamURL)

	fresh, status, err := m.status.Fetch(ctx, user)
	if err != nil {
		log.Warn("refresh stream URL failed", "status", status, "err", err)
		return recordSkipped
	}
	if fresh.HLSURL == "" {
		log.Info("stream ended before download could start")
		return recordSkipped
	}

	file := baseName + "." + format.Ext
	log.Info("downloading stream", "file", file)
	m.setState(ctx, user, fresh.Status, true)
	m.event(ctx, store.KindRecordingStarted, user, fmt.Sprintf("Recording %s started: %s", user, file))

	started := time.Now()
	err = m.rec.Record(ctx, fresh.HLSURL, baseName, format)
	m.setState(ctx, user, fresh.Status, false)
	switch {
	case err != nil:
		log.Error("download failed", "file", file, "err", err)
		m.event(ctx, store.KindRecordingFailed, user, fmt.Sprintf("Recording %s failed: %v", user, err))
		return recordFailed
	case ctx.Err() != nil:
		log.Info("download stopped", "file", file)
		m.event(ctx, store.KindRecordingFinished, user, fmt.Sprintf("Recording %s stopped: %s", user, file))
	default:
		log.Info("download finished", "file", file)
		m.event(ctx, store.KindRecordingFinished, user, fmt.Sprintf("Recording %s finished: %s", user, file))
	}
	if ctx.Err() == nil && time.Since(started) < m.timing.longRecording {
		return recordShort
	}
	return recordDone
}
