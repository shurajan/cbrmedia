package httpapi

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"cbr/api/internal/auth"
)

// authHandler обменивает пароль на токен: POST /auth {"password": "..."}.
// Ошибки — JSON {"error": "..."}, как у остальных маршрутов: iOS показывает текст из него.
func authHandler(svc *auth.Service, log *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			Password string `json:"password"`
		}
		if err := c.Bind().Body(&req); err != nil {
			return jsonError(c, fiber.StatusBadRequest, "invalid json")
		}

		if !svc.CheckPassword(req.Password) {
			return jsonError(c, fiber.StatusUnauthorized, "invalid password")
		}

		token, err := svc.IssueToken()
		if err != nil {
			log.Error("issue token", "err", err)
			return jsonError(c, fiber.StatusInternalServerError, "failed to issue token")
		}
		return c.JSON(fiber.Map{"token": token})
	}
}

// bearerAuth пропускает только запросы с заголовком "Authorization: Bearer <token>".
func bearerAuth(svc *auth.Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		token, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
		if !ok || !svc.ValidToken(token) {
			return jsonError(c, fiber.StatusUnauthorized, "unauthorized")
		}
		return c.Next()
	}
}
