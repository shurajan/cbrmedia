package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gofiber/fiber/v3"

	"cbr/core/statusapi"
	"cbr/core/store"
)

// roomPublic — room_status, при котором стрим можно смотреть.
const roomPublic = "public"

// LiveFetcher — источник текущего состояния комнаты (statusapi.Client).
type LiveFetcher interface {
	Fetch(ctx context.Context, user string) (statusapi.Room, int, error)
}

// LiveStream — ответ GET /watchlist/:user/live.
type LiveStream struct {
	User       string `json:"user"`
	RoomStatus string `json:"roomStatus"`
	HLSURL     string `json:"hlsURL"` // ссылка на CDN, клиент открывает её напрямую, без токена api
}

// liveStream — GET /watchlist/:user/live: свежая ссылка на живой HLS-стрим.
//
// Ссылка с CDN-токеном у status API каждый раз новая, поэтому берётся в момент
// запроса, а не из базы. Смотреть можно только пользователей из списка слежения
// и только в статусе public: иначе 409 с текущим roomStatus.
func liveStream(db *store.Store, live LiveFetcher, log *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		if live == nil {
			return jsonError(c, fiber.StatusServiceUnavailable, "live streams are not configured (status_base_url in api.json)")
		}
		raw, ok := param(c, "user")
		if !ok {
			return jsonError(c, fiber.StatusBadRequest, "invalid user")
		}
		user, err := store.UserFromInput(raw)
		if err != nil {
			return jsonError(c, fiber.StatusBadRequest, err.Error())
		}
		found, err := db.HasUser(c.Context(), user)
		if err != nil {
			return jsonError(c, fiber.StatusInternalServerError, err.Error())
		}
		if !found {
			return jsonError(c, fiber.StatusNotFound, fmt.Sprintf("User %s is not in the watchlist", user))
		}

		room, status, err := live.Fetch(c.Context(), user)
		switch {
		case status == http.StatusNotFound || status == http.StatusGone:
			return jsonError(c, fiber.StatusNotFound, fmt.Sprintf("User %s not found", user))
		case err != nil:
			log.Warn("live: status API failed", "user", user, "status", status, "err", err)
			return jsonError(c, fiber.StatusBadGateway, "status API is unavailable")
		}
		if room.Status != roomPublic || room.HLSURL == "" {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error":      fmt.Sprintf("%s is not live now", user),
				"roomStatus": room.Status,
			})
		}
		return c.JSON(LiveStream{User: user, RoomStatus: room.Status, HLSURL: room.HLSURL})
	}
}

var _ LiveFetcher = (*statusapi.Client)(nil)
