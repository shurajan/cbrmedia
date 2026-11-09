// Package app собирает tbot: Telegram-бот поверх общей базы.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-telegram/bot"

	"cbr/core/home"
	"cbr/core/store"
	"cbr/tbot/internal/config"
	"cbr/tbot/internal/telegram"
)

// DefaultEventsInterval — как часто проверять журнал событий.
const DefaultEventsInterval = 3 * time.Second

// Options — параметры запуска.
type Options struct {
	EventsInterval time.Duration // 0 — DefaultEventsInterval
	BotOptions     []bot.Option  // дополнительные опции go-telegram/bot (тесты)
	Version        string        // версия сборки для heartbeat в базе
}

// Run работает до отмены ctx. Возвращает ошибку только если не смог стартовать.
func Run(ctx context.Context, layout home.Layout, opts Options, log *slog.Logger) error {
	cfg, err := config.Load(layout.ConfigFile(config.Name))
	if err != nil {
		return err
	}

	db, err := store.Open(layout.DBFile())
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("close db", "err", err)
		}
	}()

	b, err := telegram.New(ctx, cfg, db, log, opts.BotOptions...)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}

	interval := opts.EventsInterval
	if interval <= 0 {
		interval = DefaultEventsInterval
	}

	var wg sync.WaitGroup
	wg.Go(func() { b.Run(ctx) })
	wg.Go(func() { b.ForwardEvents(ctx, interval) })
	wg.Go(func() { db.RunHeartbeat(ctx, store.ServiceTbot, opts.Version, store.DefaultHeartbeatInterval, log) })
	log.Info("tbot started", "home", layout.Root)
	wg.Wait()
	log.Info("tbot stopped")
	return nil
}
