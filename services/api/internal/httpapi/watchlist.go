package httpapi

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"cbr/core/store"
)

// AddedBy — отметка источника в watchlist.added_by.
const AddedBy = "api"

// Пределы GET /events.
const (
	defaultEventsLimit = 100
	maxEventsLimit     = 500
)

// WatchEntry — элемент ответа GET /watchlist.
type WatchEntry struct {
	User           string `json:"user"`
	AddedAt        string `json:"addedAt"`
	AddedBy        string `json:"addedBy"`
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabledReason,omitempty"`
	RoomStatus     string `json:"roomStatus,omitempty"`
	Recording      bool   `json:"recording"`
	UpdatedAt      string `json:"updatedAt,omitempty"` // пусто, если recorder за ним сейчас не следит
}

// EventItem — элемент ответа GET /events.
type EventItem struct {
	ID      int64  `json:"id"`
	At      string `json:"at"`
	Kind    string `json:"kind"`
	User    string `json:"user,omitempty"`
	Message string `json:"message"`
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// listWatchlist — GET /watchlist.
func listWatchlist(db *store.Store) fiber.Handler {
	return func(c fiber.Ctx) error {
		entries, err := db.Users(c.Context())
		if err != nil {
			return jsonError(c, fiber.StatusInternalServerError, err.Error())
		}
		items := make([]WatchEntry, 0, len(entries))
		for _, e := range entries {
			items = append(items, WatchEntry{
				User:           e.User,
				AddedAt:        rfc3339(e.AddedAt),
				AddedBy:        e.AddedBy,
				Enabled:        e.Enabled,
				DisabledReason: e.DisabledReason,
				RoomStatus:     e.RoomStatus,
				Recording:      e.Recording,
				UpdatedAt:      rfc3339(e.StateAt),
			})
		}
		return c.JSON(items)
	}
}

// addToWatchlist — POST /watchlist {"user": "..."}: добавить или снова включить.
// В user можно передать ссылку на страницу пользователя — имя берётся из неё
// (store.UserFromInput) и возвращается в ответе.
//
// Ответ приходит после первой проверки пользователя recorder'ом (обычно 5–8 с,
// не дольше checkTimeout): 201 с итогом в "check" или 404, если пользователь не
// найден и recorder его удалил. Без живого recorder (heartbeat) ответ сразу:
// check = recorder_offline.
func addToWatchlist(db *store.Store, checkTimeout time.Duration) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			User string `json:"user"`
		}
		if err := c.Bind().Body(&req); err != nil {
			return jsonError(c, fiber.StatusBadRequest, "invalid json")
		}

		user, err := store.UserFromInput(req.User)
		if err == nil {
			err = db.AddUser(c.Context(), user, AddedBy)
		}
		switch {
		case errors.Is(err, store.ErrInvalidUser):
			return jsonError(c, fiber.StatusBadRequest, err.Error())
		case errors.Is(err, store.ErrUserExists):
			return jsonError(c, fiber.StatusConflict, err.Error())
		case err != nil:
			return jsonError(c, fiber.StatusInternalServerError, err.Error())
		}

		res, err := db.WaitFirstCheck(c.Context(), user, checkTimeout)
		if c.Context().Err() != nil {
			// Служба останавливается: пользователь уже добавлен, итог проверки покажет список.
			return jsonError(c, fiber.StatusServiceUnavailable, "server is shutting down")
		}
		if err != nil {
			return jsonError(c, fiber.StatusInternalServerError, err.Error())
		}
		if res.Outcome == store.CheckRejected {
			return jsonError(c, fiber.StatusNotFound, fmt.Sprintf("User %s not found, not added", user))
		}
		resp := fiber.Map{"message": "added", "user": user, "check": checkNames[res.Outcome]}
		if res.RoomStatus != "" {
			resp["roomStatus"] = res.RoomStatus
		}
		if res.Reason != "" {
			resp["reason"] = res.Reason
		}
		return c.Status(fiber.StatusCreated).JSON(resp)
	}
}

// checkNames — значения поля "check" в ответе POST /watchlist.
var checkNames = map[store.CheckOutcome]string{
	store.CheckPending:  "pending",
	store.CheckWatching: "watching",
	store.CheckBlocked:  "blocked",
	store.CheckNotFound: "not_found",
	store.CheckDisabled: "disabled",
	// recorder не запущен (нет heartbeat): пользователь добавлен, проверит при запуске.
	store.CheckNoRecorder: "recorder_offline",
}

// removeFromWatchlist — DELETE /watchlist/:user. Имя приводится к нижнему
// регистру, как при добавлении (store.UserFromInput).
func removeFromWatchlist(db *store.Store) fiber.Handler {
	return func(c fiber.Ctx) error {
		raw, ok := param(c, "user")
		if !ok {
			return jsonError(c, fiber.StatusBadRequest, "invalid user")
		}
		user, err := store.UserFromInput(raw)
		if err == nil {
			err = db.RemoveUser(c.Context(), user)
		}
		switch {
		case errors.Is(err, store.ErrInvalidUser):
			return jsonError(c, fiber.StatusBadRequest, err.Error())
		case errors.Is(err, store.ErrUserNotFound):
			return jsonError(c, fiber.StatusNotFound, err.Error())
		case err != nil:
			return jsonError(c, fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(fiber.Map{"message": "deleted"})
	}
}

// listEvents — GET /events?after=ID&limit=N: события с ID больше after.
func listEvents(db *store.Store) fiber.Handler {
	return func(c fiber.Ctx) error {
		after, err := queryInt(c, "after", 0)
		if err != nil || after < 0 {
			return jsonError(c, fiber.StatusBadRequest, "invalid after")
		}
		limit, err := queryInt(c, "limit", defaultEventsLimit)
		if err != nil || limit <= 0 {
			return jsonError(c, fiber.StatusBadRequest, "invalid limit")
		}
		limit = min(limit, maxEventsLimit)

		events, err := db.EventsAfter(c.Context(), after, int(limit))
		if err != nil {
			return jsonError(c, fiber.StatusInternalServerError, err.Error())
		}
		items := make([]EventItem, 0, len(events))
		for _, e := range events {
			items = append(items, EventItem{ID: e.ID, At: rfc3339(e.At), Kind: e.Kind, User: e.User, Message: e.Message})
		}
		return c.JSON(items)
	}
}

func queryInt(c fiber.Ctx, key string, def int64) (int64, error) {
	raw := c.Query(key)
	if raw == "" {
		return def, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}
