package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cbr/core/ctxutil"
)

// Имена служб в таблице services.
const (
	ServiceRecorder  = "recorder"
	ServiceTbot      = "tbot"
	ServiceAPI       = "api"
	ServiceConverter = "converter"
)

// DefaultHeartbeatInterval — период heartbeat служб, которым не важна быстрая
// реакция на их остановку (api, tbot, converter). recorder обновляет heartbeat
// каждый такт Sync: по нему WaitFirstCheck решает, есть ли кого ждать.
const DefaultHeartbeatInterval = 10 * time.Second

// aliveFactor — служба считается живой, пока с последнего heartbeat прошло не
// больше aliveFactor × interval: один пропущенный такт (занятая база,
// busy_timeout) ещё не означает остановку.
const aliveFactor = 3

// heartbeatTimeout — сколько ждать записи heartbeat, когда контекст службы уже отменён.
const heartbeatTimeout = 5 * time.Second

// ServiceState — строка таблицы services: что служба сообщила о себе в последний раз.
type ServiceState struct {
	Name     string
	Version  string
	Interval time.Duration // обещанный период heartbeat
	SeenAt   time.Time
}

// Alive сообщает, жива ли служба на момент now (см. aliveFactor).
func (s ServiceState) Alive(now time.Time) bool {
	return now.Sub(s.SeenAt) <= aliveFactor*s.Interval
}

// Heartbeat записывает, что служба name версии version жива и обещает
// обновлять запись каждые interval.
func (s *Store) Heartbeat(ctx context.Context, name, version string, interval time.Duration) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO services (name, version, interval, seen_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			version = excluded.version, interval = excluded.interval, seen_at = excluded.seen_at`,
		name, version, interval.Milliseconds(), millis(time.Now()))
	if err != nil {
		return fmt.Errorf("heartbeat %s: %w", name, err)
	}
	return nil
}

// ClearHeartbeat удаляет запись службы (корректная остановка): остальные
// узнают о ней сразу, не дожидаясь, пока heartbeat устареет.
func (s *Store) ClearHeartbeat(ctx context.Context, name string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM services WHERE name = ?`, name); err != nil {
		return fmt.Errorf("clear heartbeat %s: %w", name, err)
	}
	return nil
}

// Services возвращает heartbeat всех служб по имени.
func (s *Store) Services(ctx context.Context) ([]ServiceState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, version, interval, seen_at FROM services ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	defer rows.Close()

	var states []ServiceState
	for rows.Next() {
		var (
			st               ServiceState
			interval, seenAt int64
		)
		if err := rows.Scan(&st.Name, &st.Version, &interval, &seenAt); err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		st.Interval, st.SeenAt = time.Duration(interval)*time.Millisecond, fromMillis(seenAt)
		states = append(states, st)
	}
	return states, rows.Err()
}

// ServiceAlive сообщает, жива ли служба name: есть запись и она не устарела.
func (s *Store) ServiceAlive(ctx context.Context, name string) (bool, error) {
	var interval, seenAt int64
	err := s.db.QueryRowContext(ctx, `SELECT interval, seen_at FROM services WHERE name = ?`, name).
		Scan(&interval, &seenAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("service alive %s: %w", name, err)
	}
	st := ServiceState{Interval: time.Duration(interval) * time.Millisecond, SeenAt: fromMillis(seenAt)}
	return st.Alive(time.Now()), nil
}

// RunHeartbeat обновляет heartbeat службы сразу и затем каждые interval до
// отмены ctx, после чего удаляет запись. Запускать в отдельной горутине.
func (s *Store) RunHeartbeat(ctx context.Context, name, version string, interval time.Duration, log *slog.Logger) {
	for {
		if err := s.Heartbeat(ctx, name, version, interval); err != nil && ctx.Err() == nil {
			log.Warn("heartbeat failed", "service", name, "err", err)
		}
		if ctxutil.Sleep(ctx, interval) {
			break
		}
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), heartbeatTimeout)
	defer cancel()
	if err := s.ClearHeartbeat(cctx, name); err != nil {
		log.Warn("clear heartbeat failed", "service", name, "err", err)
	}
}
