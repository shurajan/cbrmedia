// Package svc — общий запуск служб: логгер и контекст, отменяемый по SIGINT/SIGTERM.
package svc

import (
	"context"
	"io"
	"log/slog"
	"os/signal"
	"syscall"
)

// Logger создаёт текстовый slog-логгер в w и делает его логгером по умолчанию.
func Logger(w io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)
	return log
}

// SignalContext возвращает контекст, который отменяется по SIGINT или SIGTERM.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}
