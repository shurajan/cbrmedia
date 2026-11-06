// Package store — общая база служб (~/.cbr/data/cbr.db, SQLite в режиме WAL).
// Через неё службы общаются, ничего не зная друг о друге:
//
//   - watchlist — кого записывать (желаемое состояние). Пишут интерфейсы
//     управления (tbot, api), читает recorder; recorder сам выключает
//     пользователя (enabled = 0), если status API ответил 4xx.
//   - watch_state — что сейчас делает recorder по каждому пользователю.
//   - events — журнал событий (outbox): recorder пишет, интерфейсы читают,
//     каждый со своей позиции в cursors.
//
// Базу одновременно открывают несколько процессов: WAL и busy_timeout
// позволяют им читать параллельно и по очереди писать.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	_ "modernc.org/sqlite" // драйвер "sqlite"
)

// Ошибки хранилища. Тексты показываются пользователю (Telegram, API).
var (
	ErrUserExists   = errors.New("user already exists")
	ErrUserNotFound = errors.New("user not found")
	ErrInvalidUser  = errors.New("invalid user name: use letters, digits, '_', '-', '.'")
)

// Виды событий.
const (
	KindRecordingStarted  = "recording_started"
	KindRecordingFinished = "recording_finished"
	KindRecordingFailed   = "recording_failed"
	KindUserDisabled      = "user_disabled"
	KindUserRemoved       = "user_removed"
	// KindUserRejected — пользователь не найден при первой проверке и удалён.
	// Добавивший узнаёт об этом сразу из ответа (WaitFirstCheck), поэтому tbot его не пересылает.
	KindUserRejected = "user_rejected"
)

// room_status в watch_state, которые ставит сам recorder при ошибках status API.
// Таких пользователей он проверяет раз в сутки.
const (
	RoomBlocked  = "blocked"   // доступ запрещён (401/403/451), обычно временная блокировка
	RoomNotFound = "not found" // пользователь не найден (404/410); после 3 раз подряд удаляется
)

// userPattern — допустимое имя пользователя: оно попадает в URL status API и в имена файлов.
var userPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// ValidUser проверяет имя пользователя.
func ValidUser(user string) bool {
	return userPattern.MatchString(user) && user != "." && user != ".."
}

// migrations — схема по версиям (PRAGMA user_version). Только добавлять в конец.
var migrations = []string{
	`CREATE TABLE IF NOT EXISTS watchlist (
		user            TEXT PRIMARY KEY,
		added_at        INTEGER NOT NULL,
		added_by        TEXT NOT NULL DEFAULT '',
		enabled         INTEGER NOT NULL DEFAULT 1,
		disabled_reason TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS watch_state (
		user        TEXT PRIMARY KEY,
		room_status TEXT NOT NULL DEFAULT '',
		recording   INTEGER NOT NULL DEFAULT 0,
		updated_at  INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS events (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		at      INTEGER NOT NULL,
		kind    TEXT NOT NULL,
		user    TEXT NOT NULL DEFAULT '',
		message TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS events_at ON events(at);
	CREATE TABLE IF NOT EXISTS cursors (
		consumer TEXT PRIMARY KEY,
		event_id INTEGER NOT NULL
	);`,
	// verified — status API хоть раз ответил успешно, то есть пользователь существует.
	// Уже добавленные к моменту миграции считаются подтверждёнными.
	`ALTER TABLE watchlist ADD COLUMN verified INTEGER NOT NULL DEFAULT 0;
	UPDATE watchlist SET verified = 1;`,
}

// Store — подключение к общей базе. Безопасен для конкурентного использования.
type Store struct {
	db *sql.DB
}

// Open открывает (или создаёт) базу по пути path и применяет миграции.
func Open(path string) (*Store, error) {
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db %s: %w", path, err)
	}
	db.SetMaxOpenConns(4)

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate db %s: %w", path, err)
	}
	return s, nil
}

// Close закрывает базу.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	// _txlock=immediate: одновременно стартующие службы мигрируют по очереди.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	if version < len(migrations) {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func millis(t time.Time) int64 { return t.UnixMilli() }

func fromMillis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// --- watchlist ---------------------------------------------------------------

// Entry — пользователь из списка слежения вместе с состоянием от recorder.
type Entry struct {
	User           string
	AddedAt        time.Time
	AddedBy        string // кто добавил: "telegram", "api", …
	Enabled        bool
	DisabledReason string

	// Состояние от recorder; StateAt нулевое, если recorder за ним сейчас не следит.
	RoomStatus string
	Recording  bool
	StateAt    time.Time
}

// AddUser добавляет пользователя или снова включает выключенного.
// Возвращает ErrUserExists, если он уже есть и включён.
func (s *Store) AddUser(ctx context.Context, user, addedBy string) error {
	if !ValidUser(user) {
		return ErrInvalidUser
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO watchlist (user, added_at, added_by) VALUES (?, ?, ?)
		ON CONFLICT(user) DO UPDATE SET enabled = 1, disabled_reason = '', added_by = excluded.added_by
		WHERE watchlist.enabled = 0`,
		user, millis(time.Now()), addedBy)
	if err != nil {
		return fmt.Errorf("add user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserExists
	}
	return nil
}

// RemoveUser удаляет пользователя. Возвращает ErrUserNotFound, если его нет.
func (s *Store) RemoveUser(ctx context.Context, user string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM watchlist WHERE user = ?`, user)
	if err != nil {
		return fmt.Errorf("remove user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// Verified сообщает, подтверждён ли пользователь: status API хоть раз ответил
// о нём успешно. Для отсутствующего в списке — false.
func (s *Store) Verified(ctx context.Context, user string) (bool, error) {
	var verified bool
	err := s.db.QueryRowContext(ctx, `SELECT verified FROM watchlist WHERE user = ?`, user).Scan(&verified)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("verified: %w", err)
	}
	return verified, nil
}

// MarkVerified отмечает пользователя подтверждённым (см. Verified).
func (s *Store) MarkVerified(ctx context.Context, user string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE watchlist SET verified = 1 WHERE user = ? AND verified = 0`, user); err != nil {
		return fmt.Errorf("mark verified: %w", err)
	}
	return nil
}

// DisableUser выключает слежение за пользователем с причиной (пользователь
// остаётся в списке; AddUser включает его снова).
func (s *Store) DisableUser(ctx context.Context, user, reason string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE watchlist SET enabled = 0, disabled_reason = ? WHERE user = ?`, reason, user)
	if err != nil {
		return fmt.Errorf("disable user: %w", err)
	}
	return nil
}

// Users возвращает весь список слежения с состоянием, по алфавиту.
func (s *Store) Users(ctx context.Context) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.user, w.added_at, w.added_by, w.enabled, w.disabled_reason,
		       COALESCE(st.room_status, ''), COALESCE(st.recording, 0), COALESCE(st.updated_at, 0)
		FROM watchlist w LEFT JOIN watch_state st ON st.user = w.user
		ORDER BY w.user`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var (
			e                 Entry
			addedAt, stateAt  int64
			enabled, recorded int
		)
		if err := rows.Scan(&e.User, &addedAt, &e.AddedBy, &enabled, &e.DisabledReason,
			&e.RoomStatus, &recorded, &stateAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		e.AddedAt, e.StateAt = fromMillis(addedAt), fromMillis(stateAt)
		e.Enabled, e.Recording = enabled != 0, recorded != 0
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// EnabledUsers возвращает имена включённых пользователей по алфавиту.
func (s *Store) EnabledUsers(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user FROM watchlist WHERE enabled = 1 ORDER BY user`)
	if err != nil {
		return nil, fmt.Errorf("list enabled users: %w", err)
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// --- watch_state (пишет только recorder) -------------------------------------

// SetState сохраняет текущее состояние слежения за пользователем.
func (s *Store) SetState(ctx context.Context, user, roomStatus string, recording bool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO watch_state (user, room_status, recording, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(user) DO UPDATE SET
			room_status = excluded.room_status, recording = excluded.recording, updated_at = excluded.updated_at`,
		user, roomStatus, recording, millis(time.Now()))
	if err != nil {
		return fmt.Errorf("set state: %w", err)
	}
	return nil
}

// ClearState удаляет состояние пользователя (слежение остановлено).
func (s *Store) ClearState(ctx context.Context, user string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM watch_state WHERE user = ?`, user)
	return err
}

// ClearStates удаляет всё состояние (recorder при старте и остановке).
func (s *Store) ClearStates(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM watch_state`)
	return err
}

// --- events ------------------------------------------------------------------

// Event — запись журнала событий.
type Event struct {
	ID      int64
	At      time.Time
	Kind    string
	User    string
	Message string
}

// AddEvent добавляет событие и возвращает его ID.
func (s *Store) AddEvent(ctx context.Context, kind, user, message string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO events (at, kind, user, message) VALUES (?, ?, ?, ?)`,
		millis(time.Now()), kind, user, message)
	if err != nil {
		return 0, fmt.Errorf("add event: %w", err)
	}
	return res.LastInsertId()
}

// EventsAfter возвращает до limit событий с ID больше afterID, по возрастанию ID.
func (s *Store) EventsAfter(ctx context.Context, afterID int64, limit int) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, kind, user, message FROM events WHERE id > ? ORDER BY id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var (
			e  Event
			at int64
		)
		if err := rows.Scan(&e.ID, &at, &e.Kind, &e.User, &e.Message); err != nil {
			return nil, err
		}
		e.At = fromMillis(at)
		events = append(events, e)
	}
	return events, rows.Err()
}

// LastEventID — ID последнего события или 0, если журнал пуст.
func (s *Store) LastEventID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM events`).Scan(&id)
	return id, err
}

// PruneEvents удаляет события старше before и возвращает их число.
func (s *Store) PruneEvents(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE at < ?`, millis(before))
	if err != nil {
		return 0, fmt.Errorf("prune events: %w", err)
	}
	return res.RowsAffected()
}

// Cursor возвращает позицию читателя событий; ok = false, если её ещё нет.
func (s *Store) Cursor(ctx context.Context, consumer string) (id int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT event_id FROM cursors WHERE consumer = ?`, consumer).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// SetCursor сохраняет позицию читателя событий.
func (s *Store) SetCursor(ctx context.Context, consumer string, id int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cursors (consumer, event_id) VALUES (?, ?)
		ON CONFLICT(consumer) DO UPDATE SET event_id = excluded.event_id`, consumer, id)
	return err
}
