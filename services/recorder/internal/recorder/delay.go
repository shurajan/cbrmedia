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
	techFailure    time.Duration // после 5xx или сетевой ошибки
	otherFailure   time.Duration // после прочих ошибок (например, некорректный JSON)
	publicDelay    time.Duration // комната public
	activeDelay    time.Duration // пользователь онлайн, но стрим недоступен
	offlineDelay   time.Duration // все остальные статусы
	dailyDelay     time.Duration // пользователь не найден (404/410) или доступ запрещён (401/403)
}

var defaultTiming = timing{
	initialDelay:   5 * time.Second,
	restoreStagger: 5 * time.Second,
	techFailure:    20 * time.Minute,
	otherFailure:   3 * time.Minute,
	publicDelay:    15 * time.Second,
	activeDelay:    time.Minute,
	offlineDelay:   15 * time.Minute,
	dailyDelay:     24 * time.Hour,
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
