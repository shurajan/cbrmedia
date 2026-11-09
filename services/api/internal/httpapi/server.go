// Package httpapi — HTTP API для плеера: авторизация по паролю, раздача видео
// (HLS) из медиатеки и управление списком слежения через общую базу.
//
//	POST   /auth                     {"password": "..."} -> {"token": "..."}
//	GET    /videos                   список видео                (Bearer)
//	GET    /videos/:videoname/*      файл видео: m3u8, ts, mp4…  (Bearer)
//	DELETE /videos/:videoname        удалить видео               (Bearer)
//	GET    /watchlist                список слежения с состоянием (Bearer)
//	POST   /watchlist                {"user": "..."} добавить     (Bearer)
//	DELETE /watchlist/:user          убрать из слежения          (Bearer)
//	GET    /watchlist/:user/live     свежая ссылка на живой стрим (Bearer)
//	GET    /events?after=ID&limit=N  журнал событий recorder     (Bearer)
//	GET    /health                   версии и heartbeat служб    (Bearer)
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/gofiber/fiber/v3"

	"cbr/api/internal/auth"
	"cbr/core/store"
)

const shutdownTimeout = 10 * time.Second

// Config — настройки сервера.
type Config struct {
	Addr     string // адрес для прослушивания, например ":8000"
	MediaDir string // папка медиатеки
	Version  string // версия сборки для GET /health
}

// Server — HTTP-сервер плеера.
type Server struct {
	app *fiber.App
	cfg Config
	log *slog.Logger
	// baseCtx — контекст Serve: обработчики получают его через c.Context(), чтобы
	// долгие ожидания (POST /watchlist ждёт recorder до 30 с, запрос к status API)
	// прерывались остановкой службы. Пишется до запуска слушателя, дальше только читается.
	baseCtx context.Context
}

// New создаёт сервер и регистрирует маршруты.
// live — источник ссылок на живые стримы; nil — живой просмотр выключен (503).
func New(cfg Config, authSvc *auth.Service, db *store.Store, live LiveFetcher, log *slog.Logger) *Server {
	app := fiber.New()
	s := &Server{app: app, cfg: cfg, log: log}

	// В fiber v3 c.Context() по умолчанию — context.Background(): его не отменяет
	// ни отключение клиента, ни остановка сервера.
	app.Use(func(c fiber.Ctx) error {
		if s.baseCtx != nil {
			c.SetContext(s.baseCtx)
		}
		return c.Next()
	})

	// Запросы (в том числе каждый сегмент видео) логируются только с -debug.
	if debugEnabled(log) {
		app.Use(requestLogger(log))
	}

	app.Post("/auth", authHandler(authSvc, log))

	app.Use(bearerAuth(authSvc))
	app.Get("/videos", listVideos(cfg.MediaDir))
	app.Get("/videos/:videoname/*", streamFile(cfg.MediaDir))
	app.Delete("/videos/:videoname", deleteVideo(cfg.MediaDir, log))
	app.Get("/watchlist", listWatchlist(db))
	app.Post("/watchlist", addToWatchlist(db, store.FirstCheckTimeout))
	app.Delete("/watchlist/:user", removeFromWatchlist(db))
	app.Get("/watchlist/:user/live", liveStream(db, live, log))
	app.Get("/events", listEvents(db))
	app.Get("/health", health(db, cfg.Version))

	return s
}

// Run слушает cfg.Addr до отмены ctx, затем корректно останавливает сервер.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.Addr, err)
	}
	return s.Serve(ctx, ln)
}

// Serve обслуживает запросы на ln до отмены ctx.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.baseCtx = ctx
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true})
	}()
	s.log.Info("http api listening", "addr", ln.Addr().String(), "media", s.cfg.MediaDir)

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := s.app.ShutdownWithContext(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	s.log.Info("http api stopped")
	return nil
}
