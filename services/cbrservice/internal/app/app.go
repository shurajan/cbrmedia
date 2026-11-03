// Package app собирает компоненты cbrservice и управляет их жизненным циклом:
// recorder (опрос и запись стримов), Telegram-бот и HTTP API для плеера.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"

	"github.com/go-telegram/bot"

	"cbrservice/internal/auth"
	"cbrservice/internal/config"
	"cbrservice/internal/hls"
	"cbrservice/internal/home"
	"cbrservice/internal/httpapi"
	"cbrservice/internal/recorder"
	"cbrservice/internal/storage"
	"cbrservice/internal/telegram"
)

// telegramOptions — дополнительные опции бота; подменяются в тестах.
var telegramOptions []bot.Option

// Options — параметры запуска из командной строки.
type Options struct {
	LogRequests bool // логировать HTTP-запросы
}

// Run запускает сервис и блокируется до отмены ctx или падения одного из
// компонентов. Возвращает первую ошибку; при штатной остановке — nil.
func Run(ctx context.Context, layout home.Layout, opts Options, log *slog.Logger) error {
	cfg, err := config.Load(layout.ConfigFile(), layout.Data)
	if err != nil {
		return err
	}

	authSvc := auth.NewService(layout.AuthFile())
	if err := authSvc.Load(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s not found: create it with `cbrservice hash-password --password <pwd>`", layout.AuthFile())
		}
		return fmt.Errorf("load auth: %w", err)
	}

	hls.RecoverPartial(layout.Recordings, log.With("component", "hls"))

	users, err := storage.OpenUsers(cfg.Storage.DB)
	if err != nil {
		return err
	}
	defer func() {
		if err := users.Close(); err != nil {
			log.Error("close users db", "err", err)
		}
	}()

	recLog := log.With("component", "recorder")
	httpClient, err := recorder.NewHTTPClient(cfg.Poller.Proxy, recLog)
	if err != nil {
		return err
	}
	manager := recorder.NewManager(
		recorder.NewStatusClient(httpClient, cfg.BaseURL),
		&hls.Recorder{Client: httpClient, Dir: layout.Recordings, Log: log.With("component", "hls")},
		recLog,
	)

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	tgBot, err := telegram.New(ctx, cfg.Telegram, users, manager, log.With("component", "telegram"), telegramOptions...)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}

	server := httpapi.New(httpapi.Config{
		Addr:        cfg.HTTP.Addr,
		MediaDir:    layout.Media,
		LogRequests: opts.LogRequests,
	}, authSvc, log.With("component", "http"))

	saved, err := users.List()
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}

	var wg sync.WaitGroup
	manager.Start(ctx, saved)
	wg.Go(func() {
		<-ctx.Done()
		manager.Wait()
	})
	wg.Go(func() { tgBot.Run(ctx) })
	wg.Go(func() { tgBot.ForwardNotifications(ctx, manager.Notifications()) })
	wg.Go(func() {
		if err := server.Run(ctx); err != nil {
			cancel(err)
		}
	})

	log.Info("cbrservice started", "users", len(saved), "home", layout.Root)
	<-ctx.Done()
	log.Info("cbrservice shutting down")
	wg.Wait()

	if err := context.Cause(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
