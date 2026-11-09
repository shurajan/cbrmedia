package httpapi

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"cbr/core/store"
)

// Health — ответ GET /health: версия api и heartbeat остальных служб из общей базы.
type Health struct {
	Version  string          `json:"version"`
	Services []ServiceHealth `json:"services"`
}

// ServiceHealth — одна служба по данным таблицы services (store.ServiceState).
// Служба, которая корректно остановилась, удаляет свою запись и в списке отсутствует.
type ServiceHealth struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Alive    bool   `json:"alive"`    // heartbeat не старше 3 × interval
	SeenAt   string `json:"seenAt"`   // последний heartbeat, RFC 3339 UTC
	Interval int    `json:"interval"` // обещанный период heartbeat, секунды
}

// health — GET /health. api здесь тоже есть: его heartbeat пишет app.Run.
func health(db *store.Store, version string) fiber.Handler {
	return func(c fiber.Ctx) error {
		states, err := db.Services(c.Context())
		if err != nil {
			return jsonError(c, fiber.StatusInternalServerError, err.Error())
		}
		now := time.Now()
		items := make([]ServiceHealth, 0, len(states))
		for _, st := range states {
			items = append(items, ServiceHealth{
				Name:     st.Name,
				Version:  st.Version,
				Alive:    st.Alive(now),
				SeenAt:   rfc3339(st.SeenAt),
				Interval: int(st.Interval / time.Second),
			})
		}
		return c.JSON(Health{Version: version, Services: items})
	}
}
