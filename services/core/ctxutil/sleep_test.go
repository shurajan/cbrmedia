package ctxutil

import (
	"context"
	"testing"
	"time"
)

func TestSleepElapses(t *testing.T) {
	if Sleep(context.Background(), time.Millisecond) {
		t.Error("Sleep reported cancellation for a live context")
	}
}

func TestSleepCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if !Sleep(ctx, time.Hour) {
		t.Error("Sleep did not report cancellation")
	}
	if time.Since(start) > time.Second {
		t.Error("Sleep did not return promptly on cancelled context")
	}
}
