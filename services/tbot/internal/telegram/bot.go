// Package telegram — Telegram-интерфейс управления: команды /add, /del, /list
// меняют список слежения в общей базе, а события из журнала пересылаются
// владельцу. С recorder бот напрямую не общается.
// Команды принимаются только от одного разрешённого пользователя Telegram.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"cbr/core/ctxutil"
	"cbr/core/store"
	"cbr/tbot/internal/config"
)

const (
	connectAttempts = 5
	callbackPrefix  = "button:"

	// Consumer — имя читателя журнала событий в store.cursors.
	Consumer = "tbot"
	// AddedBy — отметка источника в watchlist.added_by.
	AddedBy = "telegram"

	eventBatch = 50
	// maxMessage — предел длины сообщения Telegram (4096) с запасом.
	maxMessage = 4000
)

// Bot — Telegram-бот.
type Bot struct {
	api           *bot.Bot
	db            *store.Store
	allowedUserID int64
	log           *slog.Logger
	// checkTimeout — сколько /add ждёт первую проверку пользователя recorder'ом.
	checkTimeout time.Duration
}

// workers — обработчики обновлений: /add ждёт проверку до checkTimeout,
// остальные команды в это время не должны стоять в очереди.
const workers = 4

// New подключается к Telegram (до 5 попыток с нарастающей паузой) и
// регистрирует команды /add, /del, /list. Дополнительные опции передаются в
// go-telegram/bot (используется в тестах).
func New(ctx context.Context, cfg *config.Config, db *store.Store, log *slog.Logger, opts ...bot.Option) (*Bot, error) {
	b := &Bot{
		db:            db,
		allowedUserID: cfg.AllowedUserID,
		log:           log,
		checkTimeout:  store.FirstCheckTimeout,
	}

	httpClient, err := newHTTPClient(cfg.Proxy, log)
	if err != nil {
		return nil, err
	}

	options := append([]bot.Option{
		bot.WithHTTPClient(time.Minute, httpClient),
		bot.WithWorkers(workers),
		bot.WithDefaultHandler(b.handleDefault),
		bot.WithCallbackQueryDataHandler(callbackPrefix, bot.MatchTypePrefix, b.handleCallback),
	}, opts...)

	for attempt := 1; attempt <= connectAttempts; attempt++ {
		b.api, err = bot.New(cfg.BotToken, options...)
		if err == nil {
			break
		}
		log.Warn("telegram connect failed", "attempt", attempt, "err", err)
		if ctxutil.Sleep(ctx, time.Duration(attempt*2)*time.Second) {
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create bot after %d attempts: %w", connectAttempts, err)
	}

	b.api.RegisterHandlerRegexp(bot.HandlerTypeMessageText, regexp.MustCompile(`^/add\b`), b.handleAdd)
	b.api.RegisterHandlerRegexp(bot.HandlerTypeMessageText, regexp.MustCompile(`^/del\b`), b.handleDel)
	b.api.RegisterHandlerRegexp(bot.HandlerTypeMessageText, regexp.MustCompile(`^/list\b`), b.handleList)

	return b, nil
}

func newHTTPClient(proxy string, log *slog.Logger) (*http.Client, error) {
	transport := &http.Transport{}
	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram proxy %q: %w", proxy, err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		log.Info("telegram proxy enabled", "proxy", proxy)
	} else {
		log.Info("telegram proxy not configured, using direct connection")
	}
	return &http.Client{Timeout: time.Minute, Transport: transport}, nil
}

// Run обрабатывает обновления до отмены ctx.
func (b *Bot) Run(ctx context.Context) {
	b.log.Info("telegram bot started")
	b.api.Start(ctx)
	b.log.Info("telegram bot stopped")
}

// ForwardEvents каждые interval пересылает владельцу важные события журнала (forwardKinds) до
// отмены ctx. Позиция хранится в базе, поэтому после перезапуска бот дошлёт
// пропущенное; при первом запуске старые события пропускаются.
func (b *Bot) ForwardEvents(ctx context.Context, interval time.Duration) {
	cursor, err := b.initCursor(ctx)
	if err != nil {
		if ctx.Err() == nil {
			b.log.Error("read events cursor", "err", err)
		}
		return
	}

	for {
		cursor = b.forwardBatch(ctx, cursor)
		if ctxutil.Sleep(ctx, interval) {
			return
		}
	}
}

// initCursor возвращает сохранённую позицию в журнале, а при первом запуске
// ставит её на последнее событие.
func (b *Bot) initCursor(ctx context.Context) (int64, error) {
	cursor, ok, err := b.db.Cursor(ctx, Consumer)
	if err != nil || ok {
		return cursor, err
	}
	if cursor, err = b.db.LastEventID(ctx); err != nil {
		return 0, err
	}
	return cursor, b.db.SetCursor(ctx, Consumer, cursor)
}

// forwardBatch отправляет события после cursor и возвращает новую позицию.
// Если отправка не удалась, позиция не сдвигается: событие уйдёт в следующий раз.
func (b *Bot) forwardBatch(ctx context.Context, cursor int64) int64 {
	events, err := b.db.EventsAfter(ctx, cursor, eventBatch)
	if err != nil {
		if ctx.Err() == nil {
			b.log.Warn("read events failed", "err", err)
		}
		return cursor
	}
	for _, e := range events {
		if !forwardKinds[e.Kind] {
			cursor = e.ID // не для Telegram — просто пропускаем
			continue
		}
		if err := b.sendErr(ctx, &bot.SendMessageParams{ChatID: b.allowedUserID, Text: formatEvent(e)}); err != nil {
			b.log.Warn("forward event failed, will retry", "event", e.ID, "err", err)
			return cursor
		}
		cursor = e.ID
		if err := b.db.SetCursor(ctx, Consumer, cursor); err != nil {
			b.log.Warn("save events cursor failed", "err", err)
		}
	}
	if err := b.db.SetCursor(ctx, Consumer, cursor); err != nil {
		b.log.Warn("save events cursor failed", "err", err)
	}
	return cursor
}

// forwardKinds — события, которые уходят в Telegram. О каждой записи бот не
// пишет, чтобы не засорять чат: они видны в /list и в GET /events у api.
var forwardKinds = map[string]bool{
	store.KindUserDisabled: true,
	store.KindUserRemoved:  true,
}

func formatEvent(e store.Event) string {
	return "⚠️ " + e.Message
}

func (b *Bot) sendErr(ctx context.Context, params *bot.SendMessageParams) error {
	_, err := b.api.SendMessage(ctx, params)
	return err
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) {
	b.sendParams(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
}

func (b *Bot) sendParams(ctx context.Context, params *bot.SendMessageParams) {
	if err := b.sendErr(ctx, params); err != nil {
		b.log.Warn("telegram send failed", "chat", params.ChatID, "err", err)
	}
}

func (b *Bot) answer(ctx context.Context, callbackID, text string) {
	_, err := b.api.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
		Text:            text,
		ShowAlert:       true,
	})
	if err != nil {
		b.log.Warn("telegram answer callback failed", "err", err)
	}
}

func (b *Bot) handleDefault(ctx context.Context, _ *bot.Bot, update *models.Update) {
	b.log.Debug("unhandled update", "update", fmt.Sprintf("%+v", update))
	if update.Message != nil {
		b.send(ctx, update.Message.Chat.ID, "Unknown command. Use /add, /del, /list")
	}
}

// authorized проверяет отправителя сообщения и отвечает "Access denied." чужим.
func (b *Bot) authorized(ctx context.Context, update *models.Update) bool {
	if update.Message == nil {
		return false
	}
	if update.Message.From != nil && update.Message.From.ID == b.allowedUserID {
		return true
	}
	b.send(ctx, update.Message.Chat.ID, "Access denied.")
	return false
}

func commandArgs(text, command string) string {
	return strings.TrimSpace(strings.TrimPrefix(text, command))
}

func (b *Bot) handleAdd(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if !b.authorized(ctx, update) {
		return
	}
	chatID := update.Message.Chat.ID

	input := commandArgs(update.Message.Text, "/add")
	if input == "" {
		b.send(ctx, chatID, "Usage: /add <user or link>")
		return
	}

	// Вместо имени можно прислать ссылку на страницу пользователя.
	user, err := store.UserFromInput(input)
	if err == nil {
		err = b.db.AddUser(ctx, user, AddedBy)
	}
	if err != nil {
		b.send(ctx, chatID, fmt.Sprintf("Error adding user: %v", err))
		return
	}
	// Отвечаем по итогу первой проверки recorder'ом (обычно 5–8 с).
	res, err := b.db.WaitFirstCheck(ctx, user, b.checkTimeout)
	if err != nil {
		b.log.Warn("wait first check failed", "user", user, "err", err)
	}
	b.send(ctx, chatID, addReply(user, res))
}

// addReply — ответ на /add по итогу первой проверки.
func addReply(user string, res store.CheckResult) string {
	switch res.Outcome {
	case store.CheckRejected:
		return fmt.Sprintf("User %s not found, not added.", user)
	case store.CheckWatching:
		return fmt.Sprintf("User %s added (%s).", user, res.RoomStatus)
	case store.CheckBlocked:
		return fmt.Sprintf("User %s added: access denied, checking daily.", user)
	case store.CheckNotFound:
		return fmt.Sprintf("User %s added: not found, checking daily.", user)
	case store.CheckDisabled:
		return fmt.Sprintf("User %s added but disabled: %s", user, res.Reason)
	case store.CheckNoRecorder:
		return fmt.Sprintf("User %s added, but recorder is not running: it will be checked when recorder starts.", user)
	default:
		return fmt.Sprintf("User %s added, recorder has not checked it yet.", user)
	}
}

func (b *Bot) handleDel(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if !b.authorized(ctx, update) {
		return
	}
	chatID := update.Message.Chat.ID

	input := commandArgs(update.Message.Text, "/del")
	if input == "" {
		users, err := b.userNames(ctx)
		if err != nil {
			b.send(ctx, chatID, fmt.Sprintf("Error: %v", err))
			return
		}
		b.sendParams(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "Select user to delete:",
			ReplyMarkup: deleteMenu(users),
		})
		return
	}

	user, err := store.UserFromInput(input)
	if err == nil {
		err = b.db.RemoveUser(ctx, user)
	}
	if err != nil {
		b.send(ctx, chatID, fmt.Sprintf("Error removing user: %v", err))
		return
	}
	b.send(ctx, chatID, fmt.Sprintf("User %s removed.", user))
}

func (b *Bot) handleList(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if !b.authorized(ctx, update) {
		return
	}
	chatID := update.Message.Chat.ID

	entries, err := b.db.Users(ctx)
	if err != nil {
		b.send(ctx, chatID, fmt.Sprintf("Error fetching users: %v", err))
		return
	}
	if len(entries) == 0 {
		b.send(ctx, chatID, "No users found.")
		return
	}

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, formatEntry(e))
	}
	for _, msg := range chunk(lines, maxMessage) {
		b.send(ctx, chatID, msg)
	}
}

// formatEntry — строка /list: имя и что с ним сейчас происходит.
func formatEntry(e store.Entry) string {
	switch {
	case !e.Enabled:
		return fmt.Sprintf("⏸ %s — disabled: %s", e.User, e.DisabledReason)
	case e.Recording:
		return fmt.Sprintf("🔴 %s — recording (%s)", e.User, e.RoomStatus)
	case e.StateAt.IsZero():
		return fmt.Sprintf("⏳ %s — waiting for recorder", e.User)
	case e.RoomStatus == store.RoomBlocked:
		return fmt.Sprintf("🚫 %s — access denied, checking daily", e.User)
	case e.RoomStatus == store.RoomNotFound:
		return fmt.Sprintf("❓ %s — not found, checking daily", e.User)
	case e.RoomStatus == "":
		return fmt.Sprintf("👀 %s — watching", e.User)
	default:
		return fmt.Sprintf("👀 %s — %s", e.User, e.RoomStatus)
	}
}

// chunk склеивает строки в сообщения не длиннее limit байт.
func chunk(lines []string, limit int) []string {
	var (
		msgs []string
		cur  strings.Builder
	)
	for _, line := range lines {
		if cur.Len() > 0 && cur.Len()+1+len(line) > limit {
			msgs = append(msgs, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		msgs = append(msgs, cur.String())
	}
	return msgs
}

func (b *Bot) userNames(ctx context.Context) ([]string, error) {
	entries, err := b.db.Users(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.User)
	}
	return names, nil
}

func (b *Bot) handleCallback(ctx context.Context, _ *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery
	if cb == nil {
		return
	}
	if cb.From.ID != b.allowedUserID {
		b.answer(ctx, cb.ID, "Access denied.")
		return
	}

	user := strings.TrimPrefix(cb.Data, callbackPrefix)
	if err := b.db.RemoveUser(ctx, user); err != nil {
		b.answer(ctx, cb.ID, fmt.Sprintf("Error removing %s: %v", user, err))
		return
	}
	b.answer(ctx, cb.ID, fmt.Sprintf("User %s removed successfully.", user))

	users, err := b.userNames(ctx)
	switch {
	case err != nil:
		b.log.Error("list users after deletion", "err", err)
	case len(users) == 0:
		b.send(ctx, b.allowedUserID, "All users have been removed.")
	default:
		b.sendParams(ctx, &bot.SendMessageParams{
			ChatID:      b.allowedUserID,
			Text:        "Select user to delete:",
			ReplyMarkup: deleteMenu(users),
		})
	}
}

func deleteMenu(users []string) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(users))
	for _, user := range users {
		rows = append(rows, []models.InlineKeyboardButton{{
			Text:         user,
			CallbackData: callbackPrefix + user,
		}})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}
