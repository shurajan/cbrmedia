package auth

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	return NewService(filepath.Join(t.TempDir(), "auth.json"))
}

func TestSetAndCheckPassword(t *testing.T) {
	a := newTestService(t)
	if err := a.SetPassword("s3cret"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if !a.CheckPassword("s3cret") {
		t.Error("expected correct password to validate")
	}
	if a.CheckPassword("wrong") {
		t.Error("expected wrong password to be rejected")
	}
}

func TestSetPasswordPersists(t *testing.T) {
	a := newTestService(t)
	if err := a.SetPassword("hunter2"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	reloaded := NewService(a.Path())
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reloaded.CheckPassword("hunter2") {
		t.Error("password was not persisted across reload")
	}
}

func TestLoadMissingFile(t *testing.T) {
	a := NewService(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err := a.Load(); err == nil {
		t.Error("expected error loading a missing file")
	}
}

func TestGenerateTokenPersists(t *testing.T) {
	a := newTestService(t)
	if err := a.SetPassword("pw"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	token := mustIssue(t, a)

	reloaded := NewService(a.Path())
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reloaded.ValidToken(token) {
		t.Error("token was not persisted across reload")
	}
}

func TestSetPasswordResetsToken(t *testing.T) {
	a := newTestService(t)
	if err := a.SetPassword("pw"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	token := mustIssue(t, a)

	// Changing the password must invalidate the previously issued token.
	if err := a.SetPassword("new-pw"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if a.ValidToken(token) {
		t.Error("expected old token to be invalidated after password change")
	}
}

func mustIssue(t *testing.T, a *Service) string {
	t.Helper()
	token, err := a.IssueToken()
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	return token
}

func TestEmptyTokenNeverValid(t *testing.T) {
	a := newTestService(t)
	if err := a.SetPassword("pw"); err != nil {
		t.Fatal(err)
	}
	// Сразу после установки пароля токена ещё нет: пустой Bearer не должен проходить.
	if a.ValidToken("") {
		t.Error("empty token accepted before first login")
	}
	mustIssue(t, a)
	if a.ValidToken("") {
		t.Error("empty token accepted after login")
	}
}

func TestIssueTokenReplacesPrevious(t *testing.T) {
	a := newTestService(t)
	if err := a.SetPassword("pw"); err != nil {
		t.Fatal(err)
	}
	first := mustIssue(t, a)
	second := mustIssue(t, a)
	if a.ValidToken(first) || !a.ValidToken(second) {
		t.Error("only the latest token must be valid")
	}
	if a.ValidToken("not-the-token") {
		t.Error("expected a wrong token to be rejected")
	}
}

func TestIssueTokenKeepsPreviousWhenSaveFails(t *testing.T) {
	// Диск недоступен для записи: прежний токен продолжает действовать, иначе
	// плеер остался бы вообще без рабочего токена.
	dir := t.TempDir()
	a := NewService(filepath.Join(dir, "auth.json"))
	if err := a.SetPassword("pw"); err != nil {
		t.Fatal(err)
	}
	first := mustIssue(t, a)

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if tok, err := a.IssueToken(); err == nil {
		t.Skipf("write to a read-only dir succeeded (running as root?), token %q", tok)
	}
	if !a.ValidToken(first) {
		t.Error("previous token must stay valid when the new one could not be saved")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
}

func TestConcurrentAccess(t *testing.T) {
	a := newTestService(t)
	if err := a.SetPassword("pw"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				tok, err := a.IssueToken()
				if err != nil {
					t.Error(err)
					return
				}
				_ = a.ValidToken(tok)
			}
		})
	}
	wg.Wait()
}
