package recorder

import (
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
}

var defaultTiming = timing{
	initialDelay:   5 * time.Second,
	restoreStagger: 5 * time.Second,
	techFailure:    20 * time.Minute,
	otherFailure:   3 * time.Minute,
	publicDelay:    15 * time.Second,
	activeDelay:    time.Minute,
	offlineDelay:   15 * time.Minute,
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
