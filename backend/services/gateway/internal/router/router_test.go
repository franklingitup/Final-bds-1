package router

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/auth"
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

	router, err := New(validator, cfg, log)
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

	router, err := New(validator, cfg, log)
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

	_, err := New(validator, cfg, log)
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

	router, _ := New(validator, cfg, log)
	app := fiber.New()
	router.Register(app)

	// Public auth endpoints should not require authentication.
	publicEndpoints := []string{
		"/v1/auth/signup",
		"/v1/auth/login",
		"/v1/auth/refresh",
	}

	for _, endpoint := range publicEndpoints {
		t.Run(endpoint, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, endpoint, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			// Should reach backend (200), not be rejected by auth (401).
			if resp.StatusCode == http.StatusUnauthorized {
				t.Errorf("endpoint %s should be public", endpoint)
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

	router, _ := New(validator, cfg, log)
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusUnauthorized).SendString(err.Error())
		},
	})
	router.Register(app)

	// Protected endpoints should require authentication.
	req := httptest.NewRequest(http.MethodPost, "/v1/organizations", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
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

	router, _ := New(validator, cfg, log)
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

	router, _ := New(validator, cfg, log)
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

	router, _ := New(validator, cfg, log)
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

	router, _ := New(validator, cfg, log)
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
