package recorder

import (
	"net/http"
	"slices"
	"time"
)

// timing — интервалы цикла опроса. В тестах подменяются короткими.
type timing struct {
	initialDelay   time.Duration // пауза перед первым запросом
	restoreStagger time.Duration // пауза между восстановлением пользователей при старте
	techRetry      time.Duration // после первой 5xx или сетевой ошибки, дальше вдвое больше
	techFailure    time.Duration // предел паузы после 5xx и сетевых ошибок и для Retry-After
	rateLimited    time.Duration // после 429 без Retry-After
	otherFailure   time.Duration // после прочих ошибок (например, некорректный JSON)
	publicDelay    time.Duration // комната public
	activeDelay    time.Duration // пользователь онлайн, но стрим недоступен
	offlineDelay   time.Duration // все остальные статусы
	dailyDelay     time.Duration // пользователь не найден (404/410) или доступ запрещён (401/403)
	// longRecording — если сегменты шли не меньше этого времени (ожидание застоя
	// в конце не считается), опрашиваем сразу: стрим мог оборваться на середине;
	// после короткой — обычная пауза, иначе законченный плейлист, который CDN ещё
	// отдаёт, скачивался бы снова и снова без перерыва.
	longRecording time.Duration
}

var defaultTiming = timing{
	initialDelay:   5 * time.Second,
	restoreStagger: 5 * time.Second,
	techRetry:      15 * time.Second,
	techFailure:    20 * time.Minute,
	rateLimited:    2 * time.Minute,
	otherFailure:   3 * time.Minute,
	publicDelay:    15 * time.Second,
	activeDelay:    time.Minute,
	offlineDelay:   15 * time.Minute,
	dailyDelay:     24 * time.Hour,
	longRecording:  time.Minute,
}

// failureAction — что делать после ошибки status API.
type failureAction int

const (
	actionRetry    failureAction = iota // 5xx, сеть, 429, некорректный ответ — повторить позже
	actionNotFound                      // пользователя нет — проверять раз в сутки, после notFoundLimit подряд удалить
	actionBlocked                       // доступ запрещён — проверять раз в сутки
	actionDisable                       // прочие 4xx — выключить до повторного добавления
)

// notFoundLimit — после стольких ответов 404/410 подряд (раз в сутки) пользователь
// удаляется из списка слежения. Одиночный 404 может быть сбоем API или ошибкой
// в base_url, поэтому сразу не удаляем.
const notFoundLimit = 3

// classifyFailure разбирает HTTP-статус неудачного ответа (0 — сетевая ошибка).
func classifyFailure(status int) failureAction {
	switch status {
	case http.StatusNotFound, http.StatusGone:
		return actionNotFound
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusUnavailableForLegalReasons:
		return actionBlocked
	case http.StatusTooManyRequests:
		return actionRetry
	}
	if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
		return actionDisable
	}
	return actionRetry
}

// techRetryDelay — пауза после n-й подряд сетевой ошибки или 5xx от status API:
// techRetry, затем вдвое больше, но не дольше techFailure. Короткий обрыв сети
// посреди эфира стоит секунды, а не techFailure; долгий сбой — не чаще techFailure.
func (t timing) techRetryDelay(n int) time.Duration {
	d := t.techRetry
	for i := 1; i < n && d < t.techFailure; i++ {
		d *= 2
	}
	return min(d, t.techFailure)
}

// rateLimitDelay — пауза после 429: Retry-After, но не дольше techFailure; без него — rateLimited.
func (t timing) rateLimitDelay(retryAfter time.Duration) time.Duration {
	if retryAfter <= 0 {
		return t.rateLimited
	}
	return min(retryAfter, t.techFailure)
}

// recordRetryDelay — пауза после n-й подряд неудачной попытки записи (стрим
// объявлен, но плейлист недоступен): publicDelay, затем вдвое больше, но не
// дольше offlineDelay. Без неё recorder пробовал бы каждые publicDelay.
func (t timing) recordRetryDelay(n int) time.Duration {
	d := t.publicDelay
	for i := 1; i < n && d < t.offlineDelay; i++ {
		d *= 2
	}
	return min(d, t.offlineDelay)
}

// activeStatuses — статусы, при которых пользователь онлайн, но публичного стрима нет.
var activeStatuses = []string{"private", "away", "hidden", "password protected"}

// delayFor возвращает паузу до следующего опроса по статусу комнаты.
func (t timing) delayFor(roomStatus string) time.Duration {
	switch {
	case roomStatus == "public":
		return t.publicDelay
	case slices.Contains(activeStatuses, roomStatus):
		return t.activeDelay
	default:
		return t.offlineDelay
	}
}
