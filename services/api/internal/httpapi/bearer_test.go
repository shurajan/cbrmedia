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

func TestBearerAuthMissingHeader(t *testing.T) {
	svc, _ := newAuthWithToken(t)
	app := newProtectedApp(svc)

	resp := doGet(t, app, "")
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestBearerAuthWrongToken(t *testing.T) {
	svc, _ := newAuthWithToken(t)
	app := newProtectedApp(svc)

	resp := doGet(t, app, "Bearer wrong-token")
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestBearerAuthMalformedHeader(t *testing.T) {
	svc, token := newAuthWithToken(t)
	app := newProtectedApp(svc)

	// Missing the "Bearer " prefix.
	resp := doGet(t, app, token)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestBearerAuthEmptyTokenBeforeLogin(t *testing.T) {
	svc := auth.NewService(filepath.Join(t.TempDir(), "auth.json"))
	if err := svc.SetPassword("pw"); err != nil {
		t.Fatal(err)
	}
	app := newProtectedApp(svc)

	resp := doGet(t, app, "Bearer ")
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}
