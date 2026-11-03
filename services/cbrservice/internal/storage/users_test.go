package storage

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func openTestUsers(t *testing.T) *Users {
	t.Helper()
	u, err := OpenUsers(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = u.Close() })
	return u
}

func TestUsersAddListRemove(t *testing.T) {
	u := openTestUsers(t)

	for _, name := range []string{"bob", "alice"} {
		if err := u.Add(name); err != nil {
			t.Fatalf("Add(%s): %v", name, err)
		}
	}

	got, err := u.List()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"alice", "bob"}) {
		t.Errorf("List() = %v", got)
	}

	if err := u.Remove("alice"); err != nil {
		t.Fatal(err)
	}
	got, _ = u.List()
	if !slices.Equal(got, []string{"bob"}) {
		t.Errorf("List() after remove = %v", got)
	}
}

func TestUsersErrors(t *testing.T) {
	u := openTestUsers(t)

	if err := u.Add("bob"); err != nil {
		t.Fatal(err)
	}
	if err := u.Add("bob"); !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate Add: got %v, want ErrUserExists", err)
	}
	if err := u.Remove("nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Remove missing: got %v, want ErrUserNotFound", err)
	}
}

func TestUsersPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.db")
	u, err := OpenUsers(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Add("carol"); err != nil {
		t.Fatal(err)
	}
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}

	u, err = OpenUsers(path)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	got, _ := u.List()
	if !slices.Equal(got, []string{"carol"}) {
		t.Errorf("List() after reopen = %v", got)
	}
}

func TestUsersEmptyList(t *testing.T) {
	got, err := openTestUsers(t).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("List() = %v, want empty", got)
	}
}
