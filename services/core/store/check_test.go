package store

import (
	"context"
	"testing"
	"time"
)

func TestWaitFirstCheck(t *testing.T) {
	ctx := context.Background()
	s, _ := openTest(t)
	for _, u := range []string{"pending", "watching", "blocked", "notfound", "disabled", "rejected"} {
		if err := s.AddUser(ctx, u, "api"); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.SetState(ctx, "blocked", RoomBlocked, false)
	_ = s.SetState(ctx, "notfound", RoomNotFound, false)
	_ = s.DisableUser(ctx, "disabled", "status API returned HTTP 400")
	_ = s.RemoveUser(ctx, "rejected")

	// recorder проверяет «watching» уже во время ожидания.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = s.SetState(ctx, "watching", "offline", false)
	}()

	cases := map[string]CheckResult{
		"watching": {Outcome: CheckWatching, RoomStatus: "offline"},
		"blocked":  {Outcome: CheckBlocked},
		"notfound": {Outcome: CheckNotFound},
		"disabled": {Outcome: CheckDisabled, Reason: "status API returned HTTP 400"},
		"rejected": {Outcome: CheckRejected},
		"pending":  {Outcome: CheckPending},
	}
	for user, want := range cases {
		got, err := s.WaitFirstCheck(ctx, user, 2*time.Second)
		if err != nil || got != want {
			t.Errorf("%s: got %+v, %v; want %+v", user, got, err, want)
		}
	}

	// Снова включённый пользователь ждёт новой проверки: прежнего состояния у него нет.
	if err := s.AddUser(ctx, "disabled", "api"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.WaitFirstCheck(ctx, "disabled", 50*time.Millisecond); err != nil || got.Outcome != CheckPending {
		t.Errorf("re-enabled: got %+v, %v; want pending", got, err)
	}
}
