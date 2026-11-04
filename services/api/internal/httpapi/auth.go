package httpapi

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"cbr/api/internal/auth"
)

// authHandler обменивает пароль на токен: POST /auth {"password": "..."}.
func authHandler(svc *auth.Service, log *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			Password string `json:"password"`
		}
		if err := c.Bind().Body(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid json")
		}

		if !svc.CheckPassword(req.Password) {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid password")
		}

		token, err := svc.IssueToken()
		if err != nil {
			log.Error("issue token", "err", err)
			return fiber.NewError(fiber.StatusInternalServerError, "failed to issue token")
		}
		return c.JSON(fiber.Map{"token": token})
	}
}

// bearerAuth пропускает только запросы с заголовком "Authorization: Bearer <token>".
func bearerAuth(svc *auth.Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		token, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
		if !ok || !svc.ValidToken(token) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		return c.Next()
	}
}
