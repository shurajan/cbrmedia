package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"cbrservice/internal/config"
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

type memUsers struct{ users []string }

func (m *memUsers) Add(u string) error {
	if slices.Contains(m.users, u) {
		return errors.New("user already exists")
	}
	m.users = append(m.users, u)
	return nil
}

func (m *memUsers) Remove(u string) error {
	i := slices.Index(m.users, u)
	if i < 0 {
		return errors.New("user not found")
	}
	m.users = slices.Delete(m.users, i, i+1)
	return nil
}

func (m *memUsers) List() ([]string, error) { return slices.Clone(m.users), nil }

type recWatcher struct{ watched, unwatched []string }

func (r *recWatcher) Watch(u string)   { r.watched = append(r.watched, u) }
func (r *recWatcher) Unwatch(u string) { r.unwatched = append(r.unwatched, u) }

func newTestBot(t *testing.T) (*Bot, *fakeAPI, *memUsers, *recWatcher) {
	t.Helper()
	api := &fakeAPI{}
	srv := httptest.NewServer(http.HandlerFunc(api.handler))
	t.Cleanup(srv.Close)

	users := &memUsers{}
	watcher := &recWatcher{}
	b, err := New(context.Background(),
		config.Telegram{BotToken: "123:test", AllowedUserID: ownerID},
		users, watcher, discard,
		bot.WithServerURL(srv.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	return b, api, users, watcher
}

func message(fromID int64, text string) *models.Update {
	return &models.Update{Message: &models.Message{
		Text: text,
		From: &models.User{ID: fromID},
		Chat: models.Chat{ID: fromID},
	}}
}

func TestStrangerIsDenied(t *testing.T) {
	b, api, users, watcher := newTestBot(t)

	b.handleAdd(context.Background(), nil, message(7, "/add eve"))

	if got := api.sent(); !slices.Equal(got, []string{"Access denied."}) {
		t.Errorf("sent = %v", got)
	}
	if len(users.users) != 0 || len(watcher.watched) != 0 {
		t.Error("stranger must not change state")
	}
}

func TestAddDelList(t *testing.T) {
	b, api, users, watcher := newTestBot(t)
	ctx := context.Background()

	b.handleAdd(ctx, nil, message(ownerID, "/add alice"))
	b.handleAdd(ctx, nil, message(ownerID, "/add alice"))
	b.handleAdd(ctx, nil, message(ownerID, "/add"))
	b.handleList(ctx, nil, message(ownerID, "/list"))
	b.handleDel(ctx, nil, message(ownerID, "/del alice"))
	b.handleList(ctx, nil, message(ownerID, "/list"))

	want := []string{
		"User added successfully.",
		"Error adding user: user already exists",
		"Usage: /add <user>",
		"alice",
		"User removed successfully.",
		"No users found.",
	}
	if got := api.sent(); !slices.Equal(got, want) {
		t.Errorf("sent =\n%q\nwant\n%q", got, want)
	}
	if !slices.Equal(watcher.watched, []string{"alice"}) || !slices.Equal(watcher.unwatched, []string{"alice"}) {
		t.Errorf("watcher = %+v", watcher)
	}
	if len(users.users) != 0 {
		t.Errorf("users = %v", users.users)
	}
}

func TestForwardNotifications(t *testing.T) {
	b, api, _, _ := newTestBot(t)

	ch := make(chan string, 1)
	ch <- "User: x will be deleted till restart. Code: 404"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.ForwardNotifications(ctx, ch); close(done) }()

	deadline := time.Now().Add(3 * time.Second)
	for len(api.sent()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	if got := api.sent(); !slices.Equal(got, []string{"⚠️ User: x will be deleted till restart. Code: 404"}) {
		t.Errorf("sent = %v", got)
	}
}
