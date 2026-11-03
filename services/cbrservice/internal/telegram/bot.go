// Package telegram — бот для управления списком отслеживаемых пользователей.
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

	"cbrservice/internal/config"
	"cbrservice/internal/ctxutil"
)

const (
	connectAttempts = 5
	callbackPrefix  = "button:"
)

// UserStore — постоянный список пользователей.
type UserStore interface {
	Add(user string) error
	Remove(user string) error
	List() ([]string, error)
}

// Watcher запускает и останавливает слежение за пользователем.
type Watcher interface {
	Watch(user string)
	Unwatch(user string)
}

// Bot — Telegram-бот сервиса.
type Bot struct {
	api           *bot.Bot
	users         UserStore
	watcher       Watcher
	allowedUserID int64
	log           *slog.Logger
}

// New подключается к Telegram (до 5 попыток с нарастающей паузой) и
// регистрирует команды /add, /del, /list. Дополнительные опции передаются в
// go-telegram/bot (используется в тестах).
func New(ctx context.Context, cfg config.Telegram, users UserStore, watcher Watcher, log *slog.Logger, opts ...bot.Option) (*Bot, error) {
	b := &Bot{
		users:         users,
		watcher:       watcher,
		allowedUserID: cfg.AllowedUserID,
		log:           log,
	}

	httpClient, err := newHTTPClient(cfg.Proxy, log)
	if err != nil {
		return nil, err
	}

	options := append([]bot.Option{
		bot.WithHTTPClient(time.Minute, httpClient),
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

// ForwardNotifications пересылает сообщения из ch владельцу бота до отмены ctx.
func (b *Bot) ForwardNotifications(ctx context.Context, ch <-chan string) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			b.send(ctx, b.allowedUserID, "⚠️ "+msg)
		}
	}
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) {
	b.sendParams(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
}

func (b *Bot) sendParams(ctx context.Context, params *bot.SendMessageParams) {
	if _, err := b.api.SendMessage(ctx, params); err != nil {
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

	user := commandArgs(update.Message.Text, "/add")
	if user == "" {
		b.send(ctx, chatID, "Usage: /add <user>")
		return
	}

	if err := b.users.Add(user); err != nil {
		b.send(ctx, chatID, fmt.Sprintf("Error adding user: %v", err))
		return
	}
	b.watcher.Watch(user)
	b.send(ctx, chatID, "User added successfully.")
}

func (b *Bot) handleDel(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if !b.authorized(ctx, update) {
		return
	}
	chatID := update.Message.Chat.ID

	user := commandArgs(update.Message.Text, "/del")
	if user == "" {
		users, err := b.users.List()
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

	if err := b.users.Remove(user); err != nil {
		b.send(ctx, chatID, fmt.Sprintf("Error removing user: %v", err))
		return
	}
	b.watcher.Unwatch(user)
	b.send(ctx, chatID, "User removed successfully.")
}

func (b *Bot) handleList(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if !b.authorized(ctx, update) {
		return
	}
	chatID := update.Message.Chat.ID

	users, err := b.users.List()
	if err != nil {
		b.send(ctx, chatID, fmt.Sprintf("Error fetching users: %v", err))
		return
	}
	if len(users) == 0 {
		b.send(ctx, chatID, "No users found.")
		return
	}
	for _, user := range users {
		b.send(ctx, chatID, user)
		// Пауза, чтобы не упереться в лимиты Telegram.
		if ctxutil.Sleep(ctx, 100*time.Millisecond) {
			return
		}
	}
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
	if err := b.users.Remove(user); err != nil {
		b.answer(ctx, cb.ID, fmt.Sprintf("Error removing %s: %v", user, err))
		return
	}
	b.watcher.Unwatch(user)
	b.answer(ctx, cb.ID, fmt.Sprintf("User %s removed successfully.", user))

	users, err := b.users.List()
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
