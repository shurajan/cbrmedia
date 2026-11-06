package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v3"

	"cbr/api/internal/auth"
)

func newAuthWithToken(t *testing.T) (*auth.Service, string) {
	t.Helper()
	a := auth.NewService(filepath.Join(t.TempDir(), "svc.json"))
	if err := a.SetPassword("pw"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	token, err := a.IssueToken()
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	return a, token
}

func newProtectedApp(svc *auth.Service) *fiber.App {
	app := fiber.New()
	app.Use(bearerAuth(svc))
	app.Get("/protected", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})
	return app
}

func doGet(t *testing.T, app *fiber.App, header string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func TestBearerAuthValidToken(t *testing.T) {
	svc, token := newAuthWithToken(t)
	app := newProtectedApp(svc)

	resp := doGet(t, app, "Bearer "+token)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
}

func TestBearerAuthRejects(t *testing.T) {
	svc, token := newAuthWithToken(t)
	beforeLogin := auth.NewService(filepath.Join(t.TempDir(), "auth.json"))
	if err := beforeLogin.SetPassword("pw"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		svc    *auth.Service
		header string
	}{
		{"missing header", svc, ""},
		{"wrong token", svc, "Bearer wrong-token"},
		{"no Bearer prefix", svc, token},
		{"empty token before login", beforeLogin, "Bearer "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doGet(t, newProtectedApp(tt.svc), tt.header)
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
			}
		})
	}
}
