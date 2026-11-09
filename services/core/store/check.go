package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"cbr/core/ctxutil"
)

// CheckOutcome — итог первой проверки только что добавленного пользователя (см. WaitFirstCheck).
type CheckOutcome int

// Значения CheckOutcome.
const (
	CheckPending    CheckOutcome = iota // recorder ещё не проверил (не запущен, занят или status API недоступен)
	CheckWatching                       // пользователь найден, слежение идёт
	CheckBlocked                        // доступ запрещён (RoomBlocked), проверка раз в сутки
	CheckNotFound                       // не найден (RoomNotFound) — только у ранее подтверждённых
	CheckDisabled                       // recorder выключил пользователя (прочие 4xx)
	CheckRejected                       // не найден при первой проверке — recorder удалил его из списка
	CheckNoRecorder                     // recorder не запущен (нет живого heartbeat): проверит, когда запустится
)

// CheckResult — итог первой проверки.
type CheckResult struct {
	Outcome    CheckOutcome
	RoomStatus string // для CheckWatching
	Reason     string // для CheckDisabled
}

// Параметры ожидания первой проверки. recorder подхватывает нового пользователя
// за SyncInterval (3 с) и проверяет после initialDelay (5 с), так что обычно
// ответ есть через 5–8 с; с запасом на нескольких добавленных одновременно.
const (
	FirstCheckTimeout = 30 * time.Second
	firstCheckPoll    = 500 * time.Millisecond
)

// WaitFirstCheck ждёт, пока recorder впервые проверит только что добавленного
// (или снова включённого) пользователя, но не дольше timeout. По истечении
// возвращает CheckPending. Признак проверки — запись в watch_state, выключение
// или удаление пользователя recorder'ом. Если у recorder нет живого heartbeat
// (ServiceAlive), ждать нечего: сразу CheckNoRecorder.
func (s *Store) WaitFirstCheck(ctx context.Context, user string, timeout time.Duration) (CheckResult, error) {
	alive, err := s.ServiceAlive(ctx, ServiceRecorder)
	if err != nil {
		return CheckResult{}, err
	}
	if !alive {
		return CheckResult{Outcome: CheckNoRecorder}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		res, err := s.firstCheck(ctx, user)
		if err != nil && ctx.Err() != nil {
			// Запрос прервало истечение timeout — проверка ещё не прошла.
			return CheckResult{Outcome: CheckPending}, nil //nolint:nilerr // таймаут — не ошибка
		}
		if err != nil || res.Outcome != CheckPending {
			return res, err
		}
		if ctxutil.Sleep(ctx, firstCheckPoll) {
			return CheckResult{Outcome: CheckPending}, nil
		}
	}
}

func (s *Store) firstCheck(ctx context.Context, user string) (CheckResult, error) {
	var (
		enabled    bool
		reason     string
		roomStatus sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT w.enabled, w.disabled_reason, st.room_status
		FROM watchlist w LEFT JOIN watch_state st ON st.user = w.user
		WHERE w.user = ?`, user).Scan(&enabled, &reason, &roomStatus)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return CheckResult{Outcome: CheckRejected}, nil
	case err != nil:
		return CheckResult{}, fmt.Errorf("first check: %w", err)
	case !enabled:
		return CheckResult{Outcome: CheckDisabled, Reason: reason}, nil
	case !roomStatus.Valid:
		return CheckResult{Outcome: CheckPending}, nil
	case roomStatus.String == RoomBlocked:
		return CheckResult{Outcome: CheckBlocked}, nil
	case roomStatus.String == RoomNotFound:
		return CheckResult{Outcome: CheckNotFound}, nil
	default:
		return CheckResult{Outcome: CheckWatching, RoomStatus: roomStatus.String}, nil
	}
}
