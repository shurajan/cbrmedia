package telegram

import (
	"context"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"cbr/core/store"
	"cbr/tbot/internal/config"
)

const ownerID = 42

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeAPI — фейковый Telegram Bot API, запоминает тексты отправленных сообщений.
type fakeAPI struct {
	mu    sync.Mutex
	texts []string
}

func (f *fakeAPI) handler(w http.ResponseWriter, r *http.Request) {
	if text := formValue(r, "text"); text != "" {
		f.mu.Lock()
		f.texts = append(f.texts, text)
		f.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, "/sendMessage") {
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1,"date":0,"chat":{"id":1,"type":"private"}}}`)
		return
	}
	_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
}

func formValue(r *http.Request, key string) string {
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return ""
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err != nil {
			return ""
		}
		if part.FormName() == key {
			b, _ := io.ReadAll(part)
			return string(b)
		}
	}
}

func (f *fakeAPI) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.texts)
}

func newTestBot(t *testing.T) (*Bot, *fakeAPI, *store.Store) {
	t.Helper()
	api := &fakeAPI{}
	srv := httptest.NewServer(http.HandlerFunc(api.handler))
	t.Cleanup(srv.Close)

	db, err := store.Open(filepath.Join(t.TempDir(), "cbr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	b, err := New(context.Background(),
		&config.Config{BotToken: "123:test", AllowedUserID: ownerID},
		db, discard,
		bot.WithServerURL(srv.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	b.checkTimeout = 10 * time.Millisecond // recorder в тестах не запущен
	return b, api, db
}

func message(fromID int64, text string) *models.Update {
	return &models.Update{Message: &models.Message{
		Text: text,
		From: &models.User{ID: fromID},
		Chat: models.Chat{ID: fromID},
	}}
}

func TestStrangerIsDenied(t *testing.T) {
	b, api, db := newTestBot(t)

	b.handleAdd(context.Background(), nil, message(7, "/add eve"))

	if got := api.sent(); !slices.Equal(got, []string{"Access denied."}) {
		t.Errorf("sent = %v", got)
	}
	if users, _ := db.Users(context.Background()); len(users) != 0 {
		t.Error("stranger must not change the watchlist")
	}
}

func TestAddDelList(t *testing.T) {
	b, api, db := newTestBot(t)
	ctx := context.Background()

	b.handleAdd(ctx, nil, message(ownerID, "/add alice"))
	b.handleAdd(ctx, nil, message(ownerID, "/add alice"))
	b.handleAdd(ctx, nil, message(ownerID, "/add"))
	b.handleAdd(ctx, nil, message(ownerID, "/add https://someadress.com/female/bob/"))
	_ = db.SetState(ctx, "bob", "public", true)
	b.handleList(ctx, nil, message(ownerID, "/list"))
	b.handleDel(ctx, nil, message(ownerID, "/del alice"))
	b.handleDel(ctx, nil, message(ownerID, "/del https://someadress.com/bob"))
	b.handleList(ctx, nil, message(ownerID, "/list"))

	want := []string{
		"User alice added, recorder has not checked it yet.",
		"Error adding user: user already exists",
		"Usage: /add <user or link>",
		"User bob added, recorder has not checked it yet.",
		"⏳ alice — waiting for recorder\n🔴 bob — recording (public)",
		"User alice removed.",
		"User bob removed.",
		"No users found.",
	}
	if got := api.sent(); !slices.Equal(got, want) {
		t.Errorf("sent =\n%q\nwant\n%q", got, want)
	}
}

func TestAddReply(t *testing.T) {
	cases := map[string]store.CheckResult{
		"User u not found, not added.":                   {Outcome: store.CheckRejected},
		"User u added (offline).":                        {Outcome: store.CheckWatching, RoomStatus: "offline"},
		"User u added: access denied, checking daily.":   {Outcome: store.CheckBlocked},
		"User u added: not found, checking daily.":       {Outcome: store.CheckNotFound},
		"User u added but disabled: HTTP 400":            {Outcome: store.CheckDisabled, Reason: "HTTP 400"},
		"User u added, recorder has not checked it yet.": {Outcome: store.CheckPending},
	}
	for want, res := range cases {
		if got := addReply("u", res); got != want {
			t.Errorf("addReply(%+v) = %q, want %q", res, got, want)
		}
	}
}

// /add отвечает после первой проверки recorder'ом; отклонённый не пересылается повторно.
func TestAddWaitsForFirstCheck(t *testing.T) {
	b, api, db := newTestBot(t)
	b.checkTimeout = 3 * time.Second
	ctx := context.Background()

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = db.RemoveUser(ctx, "ghost")
		_, _ = db.AddEvent(ctx, store.KindUserRejected, "ghost", "User ghost not found")
	}()
	b.handleAdd(ctx, nil, message(ownerID, "/add https://someadress.com/ghost"))

	if got := api.sent(); !slices.Equal(got, []string{"User ghost not found, not added."}) {
		t.Errorf("sent = %v", got)
	}
	if forwardKinds[store.KindUserRejected] {
		t.Error("user_rejected must not be forwarded: the reply already says it")
	}
}

func TestFormatEntry(t *testing.T) {
	now := time.Now()
	cases := map[string]store.Entry{
		"⏸ a — disabled: HTTP 404":            {User: "a", DisabledReason: "HTTP 404"},
		"👀 b — offline":                       {User: "b", Enabled: true, RoomStatus: "offline", StateAt: now},
		"👀 c — watching":                      {User: "c", Enabled: true, StateAt: now},
		"⏳ d — waiting for recorder":          {User: "d", Enabled: true},
		"🔴 e — recording (public)":            {User: "e", Enabled: true, Recording: true, RoomStatus: "public", StateAt: now},
		"🚫 f — access denied, checking daily": {User: "f", Enabled: true, RoomStatus: store.RoomBlocked, StateAt: now},
		"❓ g — not found, checking daily":     {User: "g", Enabled: true, RoomStatus: store.RoomNotFound, StateAt: now},
	}
	for want, e := range cases {
		if got := formatEntry(e); got != want {
			t.Errorf("formatEntry(%+v) = %q, want %q", e, got, want)
		}
	}
}

func TestChunk(t *testing.T) {
	got := chunk([]string{"aaa", "bbb", "ccc"}, 7)
	if want := []string{"aaa\nbbb", "ccc"}; !slices.Equal(got, want) {
		t.Errorf("chunk = %q, want %q", got, want)
	}
}

// TestForwardEvents: при первом запуске старые события пропускаются, из новых
// в Telegram уходят только важные, а позиция сохраняется в базе.
func TestForwardEvents(t *testing.T) {
	b, api, db := newTestBot(t)
	ctx := context.Background()

	_, _ = db.AddEvent(ctx, store.KindRecordingStarted, "old", "old event")

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { b.ForwardEvents(runCtx, 5*time.Millisecond); close(done) }()

	waitCursor := func(cond func(int64) bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			id, ok, _ := db.Cursor(ctx, Consumer)
			if ok && cond(id) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("cursor not updated")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitCursor(func(id int64) bool { return id == 1 })

	_, _ = db.AddEvent(ctx, store.KindRecordingStarted, "x", "Recording x started")
	_, _ = db.AddEvent(ctx, store.KindUserDisabled, "x", "User x disabled")
	_, _ = db.AddEvent(ctx, store.KindUserRemoved, "y", "User y removed")
	last, _ := db.AddEvent(ctx, store.KindRecordingFinished, "x", "Recording x finished")
	waitCursor(func(id int64) bool { return id == last })
	cancel()
	<-done

	if got := api.sent(); !slices.Equal(got, []string{"⚠️ User x disabled", "⚠️ User y removed"}) {
		t.Errorf("sent = %v", got)
	}
}

func callback(fromID int64, data string) *models.Update {
	return &models.Update{CallbackQuery: &models.CallbackQuery{
		ID:   "cb",
		From: models.User{ID: fromID},
		Data: data,
	}}
}

func TestDeleteMenuCallback(t *testing.T) {
	b, api, db := newTestBot(t)
	ctx := context.Background()
	for _, u := range []string{"alice", "bob"} {
		if err := db.AddUser(ctx, u, AddedBy); err != nil {
			t.Fatal(err)
		}
	}

	b.handleCallback(ctx, nil, callback(7, callbackPrefix+"alice"))
	b.handleCallback(ctx, nil, callback(ownerID, callbackPrefix+"alice"))
	b.handleCallback(ctx, nil, callback(ownerID, callbackPrefix+"alice"))
	b.handleCallback(ctx, nil, callback(ownerID, callbackPrefix+"bob"))

	want := []string{
		"Access denied.",
		"User alice removed successfully.",
		"Select user to delete:", // меню обновляется: остался bob
		"Error removing alice: user not found",
		"User bob removed successfully.",
		"All users have been removed.",
	}
	if got := api.sent(); !slices.Equal(got, want) {
		t.Errorf("sent =\n%q\nwant\n%q", got, want)
	}
}
