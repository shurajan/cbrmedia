// Package ctxutil — вспомогательные функции для работы с context.
package ctxutil

import (
	"context"
	"time"
)

// Sleep ждёт d или отмены ctx. Возвращает true, если ctx отменён.
func Sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
	}
}
