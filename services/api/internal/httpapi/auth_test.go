package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"cbr/api/internal/auth"
)

func newAuthService(t *testing.T, password string) *auth.Service {
	t.Helper()
	a := auth.NewService(filepath.Join(t.TempDir(), "svc.json"))
	if err := a.SetPassword(password); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	return a
}

func postAuth(t *testing.T, svc *auth.Service, body string) *http.Response {
	t.Helper()
	app := fiber.New()
	app.Post("/svc", authHandler(svc, discard))

	req := httptest.NewRequest(http.MethodPost, "/svc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func TestAuthHandlerCorrectPassword(t *testing.T) {
	svc := newAuthService(t, "letmein")
	resp := postAuth(t, svc, `{"password":"letmein"}`)
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Token == "" {
		t.Error("expected a token in the response")
	}
	if !svc.ValidToken(body.Token) {
		t.Error("returned token should validate against the service")
	}
}

func TestAuthHandlerWrongPassword(t *testing.T) {
	svc := newAuthService(t, "letmein")
	resp := postAuth(t, svc, `{"password":"nope"}`)
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestAuthHandlerInvalidJSON(t *testing.T) {
	svc := newAuthService(t, "letmein")
	resp := postAuth(t, svc, `{not-json`)
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}
