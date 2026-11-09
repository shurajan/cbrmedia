// Package app собирает api: HTTP API плеера над медиатекой ~/.cbr/media и общей базой.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"

	"cbr/api/internal/auth"
	"cbr/api/internal/config"
	"cbr/api/internal/httpapi"
	"cbr/core/home"
	"cbr/core/statusapi"
	"cbr/core/store"
)

// Run обслуживает HTTP API до отмены ctx. version — версия сборки (GET /health, heartbeat).
func Run(ctx context.Context, layout home.Layout, version string, log *slog.Logger) error {
	cfg, err := config.Load(layout.ConfigFile(config.Name))
	if err != nil {
		return err
	}

	authSvc := auth.NewService(layout.AuthFile())
	if err := authSvc.Load(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s not found: create it with `api hash-password --password <pwd>`", layout.AuthFile())
		}
		return fmt.Errorf("load auth: %w", err)
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

	// Живой просмотр включается, только если в api.json задан status_base_url.
	var live httpapi.LiveFetcher
	if cfg.StatusBaseURL != "" {
		httpClient, err := statusapi.NewHTTPClient(cfg.Proxy, log)
		if err != nil {
			return err
		}
		live = statusapi.NewClient(httpClient, cfg.StatusBaseURL)
	} else {
		log.Info("live streams are disabled: status_base_url is not set in api.json")
	}

	var wg sync.WaitGroup
	wg.Go(func() { db.RunHeartbeat(ctx, store.ServiceAPI, version, store.DefaultHeartbeatInterval, log) })
	defer wg.Wait()

	server := httpapi.New(httpapi.Config{
		Addr:     cfg.Addr,
		MediaDir: layout.Media,
		Version:  version,
	}, authSvc, db, live, log)
	return server.Run(ctx)
}
