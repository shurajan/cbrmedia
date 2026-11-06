package store

import (
	"errors"
	"testing"
)

func TestUserFromInput(t *testing.T) {
	ok := map[string]string{
		"alice":                                "alice",
		"  alice \n":                           "alice",
		"https://someadress.com/alice":         "alice",
		"https://someadress.com/alice/":        "alice",
		"https://someadress.com/female/alice/": "alice",
		"http://someadress.com/a/b/alice":      "alice",
		"https://someadress.com/alice/?tab=1":  "alice",
		"https://someadress.com/alice#bio":     "alice",
		"someadress.com/alice/":                "alice",
		"https://someadress.com//alice//":      "alice",
		"https://someadress.com/al_i.ce-1":     "al_i.ce-1",
		"Alice":                                "alice",
		"https://SomeAdress.com/Female/BOB/":   "bob",
	}
	for in, want := range ok {
		got, err := UserFromInput(in)
		if err != nil || got != want {
			t.Errorf("UserFromInput(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	bad := []string{
		"",
		"   ",
		"https://someadress.com",
		"https://someadress.com/",
		"https://someadress.com/al%20ice",
		"https://someadress.com/..",
		"al ice",
		"://",
	}
	for _, in := range bad {
		if got, err := UserFromInput(in); !errors.Is(err, ErrInvalidUser) {
			t.Errorf("UserFromInput(%q) = %q, %v; want ErrInvalidUser", in, got, err)
		}
	}
}
