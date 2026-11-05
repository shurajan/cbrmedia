package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
)

// requestLogger пишет каждый запрос в лог на уровне Debug: метод, путь, статус,
// длительность. Подключается, только если Debug включён (флаг -debug), —
// иначе плеер, запрашивающий сегменты каждые несколько секунд, засоряет лог.
func requestLogger(log *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		status := c.Response().StatusCode()
		if err != nil {
			// Код ошибки проставит обработчик ошибок fiber уже после middleware.
			status = fiber.StatusInternalServerError
			var fe *fiber.Error
			if errors.As(err, &fe) {
				status = fe.Code
			}
		}
		log.Debug("http request",
			"method", c.Method(),
			"path", c.Path(),
			"status", status,
			"duration", time.Since(start).Round(time.Microsecond),
			"ip", c.IP(),
		)
		return err
	}
}

// debugEnabled — включён ли у логгера уровень Debug.
func debugEnabled(log *slog.Logger) bool {
	return log.Enabled(context.Background(), slog.LevelDebug)
}
