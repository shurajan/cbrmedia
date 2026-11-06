package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

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

	if err := s.DisableUser(ctx, "bob", "HTTP 404"); err != nil {
		t.Fatal(err)
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

	if err := s.RemoveUser(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveUser(ctx, "bob"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("second remove: err = %v", err)
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
	_ = s.DisableUser(ctx, "alice", "HTTP 400")
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
