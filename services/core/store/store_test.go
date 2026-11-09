package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openTest(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cbr.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestWatchlist(t *testing.T) {
	ctx := context.Background()
	s, _ := openTest(t)

	if err := s.AddUser(ctx, "bob", "telegram"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUser(ctx, "alice", "api"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUser(ctx, "alice", "api"); !errors.Is(err, ErrUserExists) {
		t.Fatalf("duplicate add: err = %v", err)
	}
	if err := s.AddUser(ctx, "../etc", "api"); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("invalid add: err = %v", err)
	}

	users, err := s.EnabledUsers(ctx)
	if err != nil || len(users) != 2 || users[0] != "alice" || users[1] != "bob" {
		t.Fatalf("EnabledUsers = %v, %v", users, err)
	}

	if changed, err := s.DisableUser(ctx, "bob", "HTTP 404"); err != nil || !changed {
		t.Fatalf("DisableUser = %v, %v; want true, nil", changed, err)
	}
	// Повторное выключение ничего не меняет — recorder не повторит событие.
	if changed, err := s.DisableUser(ctx, "bob", "HTTP 404"); err != nil || changed {
		t.Fatalf("second DisableUser = %v, %v; want false, nil", changed, err)
	}
	if changed, err := s.DisableUser(ctx, "nobody", "HTTP 404"); err != nil || changed {
		t.Fatalf("DisableUser(unknown) = %v, %v; want false, nil", changed, err)
	}
	if users, _ := s.EnabledUsers(ctx); len(users) != 1 || users[0] != "alice" {
		t.Fatalf("after disable EnabledUsers = %v", users)
	}
	entries, err := s.Users(ctx)
	if err != nil || len(entries) != 2 {
		t.Fatalf("Users = %v, %v", entries, err)
	}
	if b := entries[1]; b.User != "bob" || b.Enabled || b.DisabledReason != "HTTP 404" || b.AddedBy != "telegram" {
		t.Fatalf("bob = %+v", b)
	}

	// Повторное добавление включает выключенного пользователя.
	if err := s.AddUser(ctx, "bob", "api"); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	entries, _ = s.Users(ctx)
	if b := entries[1]; !b.Enabled || b.DisabledReason != "" || b.AddedBy != "api" {
		t.Fatalf("bob after re-enable = %+v", b)
	}

	// Удаление уносит и состояние слежения: иначе при выключенном recorder повторное
	// добавление сразу выглядело бы проверенным.
	if err := s.SetState(ctx, "bob", "public", true); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveUser(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveUser(ctx, "bob"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("second remove: err = %v", err)
	}
	if err := s.AddUser(ctx, "bob", "api"); err != nil {
		t.Fatal(err)
	}
	// recorder жив, но bob ещё не проверен: состояния у него нет.
	if err := s.Heartbeat(ctx, ServiceRecorder, "test", time.Minute); err != nil {
		t.Fatal(err)
	}
	res, err := s.WaitFirstCheck(ctx, "bob", 10*time.Millisecond)
	if err != nil || res.Outcome != CheckPending {
		t.Fatalf("after remove and re-add: WaitFirstCheck = %+v, %v; want CheckPending", res, err)
	}
}

func TestState(t *testing.T) {
	ctx := context.Background()
	s, _ := openTest(t)
	_ = s.AddUser(ctx, "alice", "api")

	if err := s.SetState(ctx, "alice", "public", true); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Users(ctx)
	if a := entries[0]; a.RoomStatus != "public" || !a.Recording || a.StateAt.IsZero() {
		t.Fatalf("state = %+v", a)
	}

	if err := s.SetState(ctx, "alice", "offline", false); err != nil {
		t.Fatal(err)
	}
	entries, _ = s.Users(ctx)
	if a := entries[0]; a.RoomStatus != "offline" || a.Recording {
		t.Fatalf("updated state = %+v", a)
	}

	if err := s.ClearStates(ctx); err != nil {
		t.Fatal(err)
	}
	entries, _ = s.Users(ctx)
	if a := entries[0]; !a.StateAt.IsZero() || a.RoomStatus != "" {
		t.Fatalf("cleared state = %+v", a)
	}
}

func TestEventsAndCursors(t *testing.T) {
	ctx := context.Background()
	s, _ := openTest(t)

	if id, _ := s.LastEventID(ctx); id != 0 {
		t.Fatalf("empty LastEventID = %d", id)
	}
	if _, ok, err := s.Cursor(ctx, "tbot"); ok || err != nil {
		t.Fatalf("missing cursor: ok=%v err=%v", ok, err)
	}

	first, _ := s.AddEvent(ctx, KindRecordingStarted, "alice", "started")
	_, _ = s.AddEvent(ctx, KindRecordingFinished, "alice", "finished")
	_, _ = s.AddEvent(ctx, KindUserDisabled, "bob", "disabled")

	events, err := s.EventsAfter(ctx, first, 1)
	if err != nil || len(events) != 1 || events[0].Kind != KindRecordingFinished || events[0].User != "alice" {
		t.Fatalf("EventsAfter = %+v, %v", events, err)
	}

	if err := s.SetCursor(ctx, "tbot", events[0].ID); err != nil {
		t.Fatal(err)
	}
	if id, ok, _ := s.Cursor(ctx, "tbot"); !ok || id != events[0].ID {
		t.Fatalf("cursor = %d %v", id, ok)
	}

	if n, err := s.PruneEvents(ctx, time.Now().Add(time.Minute)); err != nil || n != 3 {
		t.Fatalf("PruneEvents = %d, %v", n, err)
	}
}

// TestConcurrentProcesses — два независимых подключения к одному файлу (как
// разные службы) пишут одновременно и видят изменения друг друга.
func TestConcurrentProcesses(t *testing.T) {
	ctx := context.Background()
	a, path := openTest(t)
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			if _, err := a.AddEvent(ctx, KindRecordingStarted, "a", ""); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			if err := b.SetState(ctx, "u", "public", i%2 == 0); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	if id, _ := b.LastEventID(ctx); id != 50 {
		t.Fatalf("LastEventID via second connection = %d, want 50", id)
	}
}

func TestHasUser(t *testing.T) {
	ctx := context.Background()
	s, _ := openTest(t)
	_ = s.AddUser(ctx, "alice", "api")
	_ = s.AddUser(ctx, "bob", "api")
	_, _ = s.DisableUser(ctx, "bob", "HTTP 400")

	for user, want := range map[string]bool{"alice": true, "bob": true, "nobody": false} {
		if got, err := s.HasUser(ctx, user); err != nil || got != want {
			t.Errorf("HasUser(%q) = %v, %v; want %v", user, got, err, want)
		}
	}
}

func TestVerified(t *testing.T) {
	ctx := context.Background()
	s, _ := openTest(t)
	_ = s.AddUser(ctx, "alice", "api")

	if ok, err := s.Verified(ctx, "alice"); err != nil || ok {
		t.Fatalf("new user verified = %v, %v", ok, err)
	}
	if err := s.MarkVerified(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Verified(ctx, "alice"); !ok {
		t.Fatal("verified not saved")
	}
	// Выключение и повторное включение подтверждение не сбрасывают.
	_, _ = s.DisableUser(ctx, "alice", "HTTP 400")
	_ = s.AddUser(ctx, "alice", "api")
	if ok, _ := s.Verified(ctx, "alice"); !ok {
		t.Fatal("re-enable reset verified")
	}
	if ok, err := s.Verified(ctx, "nobody"); err != nil || ok {
		t.Fatalf("missing user verified = %v, %v", ok, err)
	}
}

// Пользователи, добавленные до появления verified, считаются подтверждёнными.
func TestMigrationMarksExistingUsersVerified(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cbr.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		migrations[0],
		`PRAGMA user_version = 1`,
		`INSERT INTO watchlist (user, added_at) VALUES ('old', 0)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if ok, err := s.Verified(ctx, "old"); err != nil || !ok {
		t.Fatalf("old user verified = %v, %v", ok, err)
	}
	_ = s.AddUser(ctx, "new", "api")
	if ok, _ := s.Verified(ctx, "new"); ok {
		t.Fatal("new user must not be verified")
	}
}

func TestMigrationLowercasesUsers(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cbr.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		migrations[0],
		migrations[1],
		`PRAGMA user_version = 2`,
		`INSERT INTO watchlist (user, added_at) VALUES ('Alice', 0), ('alice', 0), ('BOB', 0), ('Bob', 0), ('carol', 0)`,
		`INSERT INTO watch_state (user, updated_at) VALUES ('BOB', 1), ('carol', 1)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entries, err := s.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var users []string
	for _, e := range entries {
		users = append(users, e.User)
	}
	if fmt.Sprint(users) != "[alice bob carol]" {
		t.Errorf("users = %v, want [alice bob carol]", users)
	}
	if entries[2].StateAt.IsZero() {
		t.Error("carol's state must be kept")
	}
}

func TestHeartbeat(t *testing.T) {
	ctx := context.Background()
	s, _ := openTest(t)

	if alive, err := s.ServiceAlive(ctx, ServiceRecorder); err != nil || alive {
		t.Fatalf("ServiceAlive(no row) = %v, %v; want false", alive, err)
	}
	if err := s.Heartbeat(ctx, ServiceRecorder, "1.3.0", 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.Heartbeat(ctx, ServiceConverter, "1.3.0", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if alive, err := s.ServiceAlive(ctx, ServiceRecorder); err != nil || !alive {
		t.Fatalf("ServiceAlive(fresh) = %v, %v; want true", alive, err)
	}

	states, err := s.Services(ctx)
	if err != nil || len(states) != 2 {
		t.Fatalf("Services = %+v, %v", states, err)
	}
	if states[0].Name != ServiceConverter || states[0].Version != "1.3.0" || states[0].Interval != 30*time.Second {
		t.Errorf("converter state = %+v", states[0])
	}
	now := time.Now()
	if !states[1].Alive(now) {
		t.Error("recorder must be alive right after heartbeat")
	}
	// Один пропущенный такт — ещё жив, три — нет.
	if !states[1].Alive(now.Add(2 * 3 * time.Second)) {
		t.Error("recorder must survive one missed beat")
	}
	if states[1].Alive(now.Add(4 * 3 * time.Second)) {
		t.Error("recorder must be dead after aliveFactor intervals")
	}

	if err := s.ClearHeartbeat(ctx, ServiceRecorder); err != nil {
		t.Fatal(err)
	}
	if alive, _ := s.ServiceAlive(ctx, ServiceRecorder); alive {
		t.Error("ServiceAlive after ClearHeartbeat = true")
	}
}

func TestRunHeartbeatClearsOnStop(t *testing.T) {
	s, _ := openTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.RunHeartbeat(ctx, ServiceAPI, "test", 20*time.Millisecond, discardLog())
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if alive, _ := s.ServiceAlive(context.Background(), ServiceAPI); alive {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("heartbeat never appeared")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if alive, _ := s.ServiceAlive(context.Background(), ServiceAPI); alive {
		t.Error("heartbeat must be cleared after stop")
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	s, path := openTest(t)
	if _, err := s.db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	_, err := Open(path)
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Open(newer schema) err = %v, want ErrSchemaTooNew", err)
	}
}
