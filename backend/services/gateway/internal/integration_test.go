//go:build integration

// Integration tests exercise the gateway against real backend services.
// They are skipped unless all backend services are reachable. Run with:
//
//	go test -tags=integration ./services/gateway/...
package gateway

import (
	"encoding/json"
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
	libmw "github.com/bdsplatform/platform/backend/libs/middleware"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/auth"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/proxy"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/router"
)

const testSigningKey = "integration-test-key-12345"

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

func setupGateway(t *testing.T, backends map[string]*httptest.Server) *fiber.App {
	t.Helper()

	validator := auth.NewValidator(testAuthConfig())

	cfg := router.Config{}
	if s, ok := backends["auth"]; ok {
		cfg.AuthService = proxy.ServiceConfig{Name: "auth", BaseURL: s.URL}
	}
	if s, ok := backends["tenant"]; ok {
		cfg.TenantService = proxy.ServiceConfig{Name: "tenant", BaseURL: s.URL}
	}
	if s, ok := backends["project"]; ok {
		cfg.ProjectService = proxy.ServiceConfig{Name: "project", BaseURL: s.URL}
	}
	if s, ok := backends["audit"]; ok {
		cfg.AuditService = proxy.ServiceConfig{Name: "audit", BaseURL: s.URL}
	}

	r, err := router.New(validator, nil, nil, cfg, slog.Default())
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}

	app := fiber.New(fiber.Config{
		ErrorHandler: libmw.ErrorHandler(),
	})
	r.Register(app)

	return app
}

func TestIntegration_PublicAuthEndpoints(t *testing.T) {
	authBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/auth/signup":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"status": "created"})
		case "/v1/auth/login":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{
				"accessToken":  "test-access-token",
				"refreshToken": "test-refresh-token",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer authBackend.Close()

	app := setupGateway(t, map[string]*httptest.Server{"auth": authBackend})

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{
			name:       "signup",
			method:     http.MethodPost,
			path:       "/v1/auth/signup",
			body:       `{"email":"test@example.com","password":"password123"}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "login",
			method:     http.MethodPost,
			path:       "/v1/auth/login",
			body:       `{"email":"test@example.com","password":"password123"}`,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("expected %d, got %d: %s", tt.wantStatus, resp.StatusCode, body)
			}

			// Verify rate limit headers are present.
			if resp.Header.Get("X-RateLimit-Limit") == "" {
				t.Error("expected X-RateLimit-Limit header")
			}
		})
	}
}

func TestIntegration_ProtectedEndpoints(t *testing.T) {
	tenantBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer tenantBackend.Close()

	app := setupGateway(t, map[string]*httptest.Server{"tenant": tenantBackend})

	t.Run("without token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/organizations", strings.NewReader(`{"name":"Test","slug":"test"}`))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("with valid token succeeds", func(t *testing.T) {
		token := signUserToken("user-123", "test@example.com")
		req := httptest.NewRequest(http.MethodPost, "/v1/organizations", strings.NewReader(`{"name":"Test","slug":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 200, got %d: %s", resp.StatusCode, body)
		}
	})

	t.Run("with invalid token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/organizations", strings.NewReader(`{"name":"Test","slug":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer invalid-token")

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

func TestIntegration_HeaderPropagation(t *testing.T) {
	var receivedHeaders http.Header
	var receivedPath string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	app := setupGateway(t, map[string]*httptest.Server{"project": backend})

	token := signUserToken("user-123", "test@example.com")
	req := httptest.NewRequest(http.MethodGet, "/v1/organizations/org-456/projects", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Custom-Header", "custom-value")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Verify identity headers were added.
	if receivedHeaders.Get("X-User-ID") != "user-123" {
		t.Errorf("expected X-User-ID user-123, got %s", receivedHeaders.Get("X-User-ID"))
	}
	if receivedHeaders.Get("X-User-Email") != "test@example.com" {
		t.Errorf("expected X-User-Email test@example.com, got %s", receivedHeaders.Get("X-User-Email"))
	}
	if receivedHeaders.Get("X-Org-ID") != "org-456" {
		t.Errorf("expected X-Org-ID org-456, got %s", receivedHeaders.Get("X-Org-ID"))
	}

	// Verify custom headers were forwarded.
	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("expected X-Custom-Header to be forwarded")
	}

	// Verify path was preserved.
	if receivedPath != "/v1/organizations/org-456/projects" {
		t.Errorf("expected path /v1/organizations/org-456/projects, got %s", receivedPath)
	}
}

func TestIntegration_ErrorResponses(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	defer backend.Close()

	app := setupGateway(t, map[string]*httptest.Server{"tenant": backend})

	token := signUserToken("user-123", "test@example.com")
	req := httptest.NewRequest(http.MethodGet, "/v1/organizations/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Backend error should be passed through.
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}

	// Request ID should still be present.
	if resp.Header.Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID header")
	}
}

func TestIntegration_AuditLogEndpoints(t *testing.T) {
	auditBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"items":      []any{},
			"nextCursor": "",
		})
	}))
	defer auditBackend.Close()

	app := setupGateway(t, map[string]*httptest.Server{"audit": auditBackend})

	token := signUserToken("user-123", "test@example.com")
	req := httptest.NewRequest(http.MethodGet, "/v1/organizations/org-123/audit-logs?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, body)
	}
}
