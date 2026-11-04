// Package app собирает api: HTTP API плеера над медиатекой ~/.cbr/media и общей базой.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"cbr/api/internal/auth"
	"cbr/api/internal/config"
	"cbr/api/internal/httpapi"
	"cbr/core/home"
	"cbr/core/store"
)

// Options — параметры запуска из командной строки.
type Options struct {
	LogRequests bool // логировать HTTP-запросы
}

// Run обслуживает HTTP API до отмены ctx.
func Run(ctx context.Context, layout home.Layout, opts Options, log *slog.Logger) error {
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

	server := httpapi.New(httpapi.Config{
		Addr:        cfg.Addr,
		MediaDir:    layout.Media,
		LogRequests: opts.LogRequests,
	}, authSvc, db, log)
	return server.Run(ctx)
}
