package router

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/auth"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/middleware"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/proxy"
)

const testSigningKey = "test-signing-key-12345"

func testAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		JWTSigningKey: testSigningKey,
	}
}

func signUserToken(userID, email string) string {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"iss":   "bdsplatform-auth",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(15 * time.Minute).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte(testSigningKey))
	return signed
}

func TestRouterCreation(t *testing.T) {
	validator := auth.NewValidator(testAuthConfig())
	log := slog.Default()

	// Create a test backend.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	cfg := Config{
		AuthService: proxy.ServiceConfig{
			Name:    "auth",
			BaseURL: backend.URL,
		},
		TenantService: proxy.ServiceConfig{
			Name:    "tenant",
			BaseURL: backend.URL,
		},
	}

	router, err := New(validator, nil, nil, cfg, log)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}

	if len(router.Services()) != 2 {
		t.Errorf("expected 2 services, got %d", len(router.Services()))
	}
}

func TestRouterCreation_DisabledServices(t *testing.T) {
	validator := auth.NewValidator(testAuthConfig())
	log := slog.Default()

	cfg := Config{
		AuthService: proxy.ServiceConfig{
			Name:    "auth",
			BaseURL: "", // Disabled.
		},
		TenantService: proxy.ServiceConfig{
			Name:    "tenant",
			BaseURL: "", // Disabled.
		},
	}

	router, err := New(validator, nil, nil, cfg, log)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}

	if len(router.Services()) != 0 {
		t.Errorf("expected 0 services, got %d", len(router.Services()))
	}
}

func TestRouterCreation_InvalidURL(t *testing.T) {
	validator := auth.NewValidator(testAuthConfig())
	log := slog.Default()

	cfg := Config{
		AuthService: proxy.ServiceConfig{
			Name:    "auth",
			BaseURL: "://invalid",
		},
	}

	_, err := New(validator, nil, nil, cfg, log)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestRouter_PublicAuthRoutes(t *testing.T) {
	validator := auth.NewValidator(testAuthConfig())
	log := slog.Default()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("from auth service"))
	}))
	defer backend.Close()

	cfg := Config{
		AuthService: proxy.ServiceConfig{
			Name:    "auth",
			BaseURL: backend.URL,
		},
	}

	router, _ := New(validator, nil, nil, cfg, log)
	app := fiber.New()
	router.Register(app)

	// Public auth endpoints should not require authentication.
	publicEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/auth/signup"},
		{http.MethodPost, "/v1/auth/login"},
		{http.MethodPost, "/v1/auth/refresh"},
		{http.MethodGet, "/v1/auth/sso/acme/login"},
		{http.MethodPost, "/v1/auth/sso/org-1/acs"},
		{http.MethodGet, "/v1/auth/sso/org-1/metadata"},
		{http.MethodPost, "/v1/auth/sso/exchange"},
	}

	for _, endpoint := range publicEndpoints {
		t.Run(endpoint.method+" "+endpoint.path, func(t *testing.T) {
			req := httptest.NewRequest(endpoint.method, endpoint.path, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			// Should reach backend (200), not be rejected by auth (401).
			if resp.StatusCode == http.StatusUnauthorized {
				t.Errorf("endpoint %s %s should be public", endpoint.method, endpoint.path)
			}
		})
	}
}

func TestRouter_ProtectedRoutes(t *testing.T) {
	validator := auth.NewValidator(testAuthConfig())
	log := slog.Default()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := Config{
		TenantService: proxy.ServiceConfig{
			Name:    "tenant",
			BaseURL: backend.URL,
		},
	}

	router, _ := New(validator, nil, nil, cfg, log)
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusUnauthorized).SendString(err.Error())
		},
	})
	router.Register(app)

	// Protected endpoints should require authentication.
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/organizations"},
		{http.MethodGet, "/v1/organizations/org-1/members"},
		{http.MethodGet, "/v1/organizations/org-1/invitations"},
		{http.MethodPost, "/v1/invitations/accept"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", resp.StatusCode)
			}
		})
	}
}

func TestRouter_AuthenticatedRequest(t *testing.T) {
	validator := auth.NewValidator(testAuthConfig())
	log := slog.Default()

	var receivedUserID string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserID = r.Header.Get("X-User-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := Config{
		TenantService: proxy.ServiceConfig{
			Name:    "tenant",
			BaseURL: backend.URL,
		},
	}

	router, _ := New(validator, nil, nil, cfg, log)
	app := fiber.New()
	router.Register(app)

	token := signUserToken("user-123", "test@example.com")
	req := httptest.NewRequest(http.MethodPost, "/v1/organizations", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	if receivedUserID != "user-123" {
		t.Errorf("expected X-User-ID user-123, got %s", receivedUserID)
	}
}

func TestRouter_OrgScopedRequest(t *testing.T) {
	validator := auth.NewValidator(testAuthConfig())
	log := slog.Default()

	var receivedOrgID string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedOrgID = r.Header.Get("X-Org-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := Config{
		ProjectService: proxy.ServiceConfig{
			Name:    "project",
			BaseURL: backend.URL,
		},
	}

	router, _ := New(validator, nil, nil, cfg, log)
	app := fiber.New()
	router.Register(app)

	token := signUserToken("user-123", "test@example.com")
	req := httptest.NewRequest(http.MethodGet, "/v1/organizations/org-456/projects", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if receivedOrgID != "org-456" {
		t.Errorf("expected X-Org-ID org-456, got %s", receivedOrgID)
	}
}

func TestRouter_RateLimitHeaders(t *testing.T) {
	validator := auth.NewValidator(testAuthConfig())
	log := slog.Default()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := Config{
		AuthService: proxy.ServiceConfig{
			Name:    "auth",
			BaseURL: backend.URL,
		},
	}

	rateLimiter := middleware.NewRateLimiter(middleware.DefaultRateLimiterConfig())
	router, _ := New(validator, nil, rateLimiter, cfg, log)
	app := fiber.New()
	router.Register(app)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Rate limit headers should be present.
	if resp.Header.Get("X-RateLimit-Limit") == "" {
		t.Error("expected X-RateLimit-Limit header")
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "" {
		t.Error("expected X-RateLimit-Remaining header")
	}
}

func TestRouter_RequestIDPropagation(t *testing.T) {
	validator := auth.NewValidator(testAuthConfig())
	log := slog.Default()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := Config{
		AuthService: proxy.ServiceConfig{
			Name:    "auth",
			BaseURL: backend.URL,
		},
	}

	router, _ := New(validator, nil, nil, cfg, log)
	app := fiber.New()
	router.Register(app)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get("X-Request-ID")
	if requestID == "" {
		t.Error("expected X-Request-ID header")
	}
	if len(requestID) != 36 { // UUID length.
		t.Errorf("expected UUID, got %q", requestID)
	}
}

func TestAgentInstallManifestDoesNotRequireBearerJWT(t *testing.T) {
	const token = "fresh-registration-token"

	clusterService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/agent-install/"+token+"/manifest.yaml" {
			t.Errorf("upstream path = %q", req.URL.Path)
			http.NotFound(w, req)
			return
		}
		if got := req.URL.Query().Get("controlPlaneEndpoint"); got != "http://localhost:8080" {
			t.Errorf("controlPlaneEndpoint = %q", got)
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = io.WriteString(w, "apiVersion: v1\nkind: Namespace\n")
	}))
	defer clusterService.Close()

	router, err := New(
		auth.NewValidator(testAuthConfig()),
		nil,
		nil,
		Config{
			ClusterService: proxy.ServiceConfig{
				Name:    "cluster",
				BaseURL: clusterService.URL,
			},
		},
		slog.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	router.Register(app)

	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/agent-install/"+token+"/manifest.yaml?controlPlaneEndpoint=http%3A%2F%2Flocalhost%3A8080",
		nil,
	)
	// Intentionally no Authorization header: the path token is the credential.
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/yaml" {
		t.Fatalf("Content-Type = %q, want application/yaml", got)
	}
}

// TestGitHubOAuthCallbackDoesNotRequireBearerJWT verifies that the GitHub OAuth
// callback route is reachable without a Bearer JWT. This is a regression test
// for the registration-order bug fixed earlier: GitHub redirects here with only
// query params (code, state) — no Authorization header.
func TestGitHubOAuthCallbackDoesNotRequireBearerJWT(t *testing.T) {
	buildService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/github/oauth/callback" {
			t.Errorf("upstream path = %q, want /v1/github/oauth/callback", req.URL.Path)
			http.NotFound(w, req)
			return
		}
		// Verify query params are forwarded.
		if code := req.URL.Query().Get("code"); code != "gh-auth-code" {
			t.Errorf("code = %q, want gh-auth-code", code)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer buildService.Close()

	router, err := New(
		auth.NewValidator(testAuthConfig()),
		nil,
		nil,
		Config{
			BuildService: proxy.ServiceConfig{
				Name:    "build",
				BaseURL: buildService.URL,
			},
		},
		slog.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	router.Register(app)

	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/github/oauth/callback?code=gh-auth-code&state=random-state",
		nil,
	)
	// Intentionally no Authorization header: GitHub redirects here with query params only.
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
}

// TestGitHubWebhookDoesNotRequireBearerJWT verifies that the GitHub webhook
// receiver route is reachable without a Bearer JWT. This is a regression test
// for the registration-order bug: GitHub calls this endpoint with HMAC signature
// verification, not a Bearer JWT.
func TestGitHubWebhookDoesNotRequireBearerJWT(t *testing.T) {
	const repoID = "repo-abc123"

	buildService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		expectedPath := "/v1/webhooks/github/" + repoID
		if req.URL.Path != expectedPath {
			t.Errorf("upstream path = %q, want %q", req.URL.Path, expectedPath)
			http.NotFound(w, req)
			return
		}
		if req.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", req.Method)
		}
		// Verify GitHub headers are forwarded.
		if got := req.Header.Get("X-GitHub-Event"); got != "push" {
			t.Errorf("X-GitHub-Event = %q, want push", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"received":true}`)
	}))
	defer buildService.Close()

	router, err := New(
		auth.NewValidator(testAuthConfig()),
		nil,
		nil,
		Config{
			BuildService: proxy.ServiceConfig{
				Name:    "build",
				BaseURL: buildService.URL,
			},
		},
		slog.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	router.Register(app)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/webhooks/github/"+repoID,
		strings.NewReader(`{"ref":"refs/heads/main"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-123")
	req.Header.Set("X-Hub-Signature-256", "sha256=abc123")
	// Intentionally no Authorization header: GitHub uses HMAC signature, not JWT.
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
}
