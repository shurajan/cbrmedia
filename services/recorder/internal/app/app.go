// Package app собирает recorder: читает список слежения из общей базы,
// сверяет с ним запущенные слежения и пишет записи в ~/.cbr/recordings.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cbr/core/home"
	"cbr/core/statusapi"
	"cbr/core/store"
	"cbr/recorder/internal/config"
	"cbr/recorder/internal/hls"
	"cbr/recorder/internal/recorder"
)

// Значения по умолчанию.
const (
	DefaultSyncInterval = 3 * time.Second
	eventRetention      = 30 * 24 * time.Hour
	pruneInterval       = time.Hour
)

// Options — параметры запуска.
type Options struct {
	// SyncInterval — как часто сверяться со списком слежения; 0 — DefaultSyncInterval.
	SyncInterval time.Duration
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

	// Состояние принадлежит recorder: после прошлого запуска оно устарело.
	if err := db.ClearStates(ctx); err != nil {
		return fmt.Errorf("clear states: %w", err)
	}
	hls.RecoverPartial(layout.Recordings, log.With("component", "hls"))

	httpClient, err := statusapi.NewHTTPClient(cfg.Proxy, log)
	if err != nil {
		return err
	}
	manager := recorder.NewManager(
		statusapi.NewClient(httpClient, cfg.BaseURL),
		&hls.Recorder{Client: httpClient, Dir: layout.Recordings, Log: log.With("component", "hls")},
		db,
		log,
	)

	interval := opts.SyncInterval
	if interval <= 0 {
		interval = DefaultSyncInterval
	}
	log.Info("recorder started", "home", layout.Root, "sync", interval)

	syncTicker := time.NewTicker(interval)
	defer syncTicker.Stop()
	pruneTicker := time.NewTicker(pruneInterval)
	defer pruneTicker.Stop()

	sync := func() {
		users, err := db.EnabledUsers(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Warn("read watchlist failed", "err", err)
			}
			return
		}
		manager.Sync(ctx, users)
	}
	sync()

	for {
		select {
		case <-ctx.Done():
			log.Info("recorder shutting down")
			manager.Wait()
			if err := db.ClearStates(context.WithoutCancel(ctx)); err != nil {
				log.Warn("clear states", "err", err)
			}
			log.Info("recorder stopped")
			return nil
		case <-syncTicker.C:
			sync()
		case <-pruneTicker.C:
			if n, err := db.PruneEvents(ctx, time.Now().Add(-eventRetention)); err != nil {
				log.Warn("prune events failed", "err", err)
			} else if n > 0 {
				log.Info("old events pruned", "count", n)
			}
		}
	}
}
