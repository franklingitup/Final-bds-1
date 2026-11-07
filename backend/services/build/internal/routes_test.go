package build

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/middleware"
	"github.com/bdsplatform/platform/backend/services/build/internal/github"
)

type rejectGitHubVerifier struct{}

func (rejectGitHubVerifier) Verify(string) (github.Identity, error) {
	return github.Identity{}, errInvalidToken
}

func TestGitHubPublicRoutes_UnauthenticatedByUserJWTReachHandlers(t *testing.T) {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler(),
	})

	buildHandler := NewHandler(nil, NewTokenVerifier(config.AuthConfig{JWTSigningKey: "test-signing-key"}))
	githubHandler := github.NewHandler(nil, rejectGitHubVerifier{})

	// Production order in cmd/server/main.go.
	RegisterRoutes(app, buildHandler)
	github.RegisterRoutes(app, githubHandler)

	callback := httptest.NewRequest(http.MethodGet, "/v1/github/oauth/callback", nil)
	callbackResp, err := app.Test(callback)
	if err != nil {
		t.Fatalf("oauth callback app.Test: %v", err)
	}
	defer callbackResp.Body.Close()
	callbackBody, _ := io.ReadAll(callbackResp.Body)
	if callbackResp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("oauth callback status = %d, body = %s; user JWT intercepted a public GitHub route", callbackResp.StatusCode, callbackBody)
	}
	if callbackResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("oauth callback status = %d, body = %s; want 422 from GitHub handler (missing code/state)", callbackResp.StatusCode, callbackBody)
	}

	webhook := httptest.NewRequest(http.MethodPost, "/v1/webhooks/github/repo-1", strings.NewReader(`{}`))
	webhook.Header.Set("Content-Type", "application/json")
	webhookResp, err := app.Test(webhook)
	if err != nil {
		t.Fatalf("webhook app.Test: %v", err)
	}
	defer webhookResp.Body.Close()
	webhookBody, _ := io.ReadAll(webhookResp.Body)
	if webhookResp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("webhook status = %d, body = %s; user JWT intercepted a public GitHub route", webhookResp.StatusCode, webhookBody)
	}
	if webhookResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("webhook status = %d, body = %s; want 422 from GitHub handler (missing X-GitHub-Event)", webhookResp.StatusCode, webhookBody)
	}

	orgReq := httptest.NewRequest(http.MethodGet, "/v1/organizations/org-123/builds", nil)
	orgResp, err := app.Test(orgReq)
	if err != nil {
		t.Fatalf("org builds app.Test: %v", err)
	}
	defer orgResp.Body.Close()
	if orgResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("org build list status = %d, want 401 (RequireAuth still applied)", orgResp.StatusCode)
	}
}
