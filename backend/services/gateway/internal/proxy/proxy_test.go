package proxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func TestNewService(t *testing.T) {
	log := slog.Default()

	tests := []struct {
		name    string
		cfg     ServiceConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: ServiceConfig{
				Name:    "test",
				BaseURL: "http://localhost:8080",
			},
			wantErr: false,
		},
		{
			name: "invalid URL",
			cfg: ServiceConfig{
				Name:    "test",
				BaseURL: "://invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewService(tt.cfg, log)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewService() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && svc == nil {
				t.Error("expected non-nil service")
			}
		})
	}
}

func TestService_Name(t *testing.T) {
	log := slog.Default()
	svc, _ := NewService(ServiceConfig{
		Name:    "test-service",
		BaseURL: "http://localhost:8080",
	}, log)

	if svc.Name() != "test-service" {
		t.Errorf("expected test-service, got %s", svc.Name())
	}
}

func TestService_Handler(t *testing.T) {
	// Create a test backend server.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend-Response", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend response"))
	}))
	defer backend.Close()

	log := slog.Default()
	svc, err := NewService(ServiceConfig{
		Name:    "test",
		BaseURL: backend.URL,
		Timeout: 5 * time.Second,
	}, log)
	if err != nil {
		t.Fatal(err)
	}

	// Create Fiber app with proxy handler.
	app := fiber.New()
	app.All("/*", svc.Handler())

	req := httptest.NewRequest(http.MethodGet, "/test/path", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "backend response" {
		t.Errorf("expected 'backend response', got %q", body)
	}

	if resp.Header.Get("X-Backend-Response") != "true" {
		t.Error("expected X-Backend-Response header")
	}
}

func TestService_Handler_ForwardsHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back some headers.
		w.Header().Set("X-Received-Auth", r.Header.Get("Authorization"))
		w.Header().Set("X-Received-Custom", r.Header.Get("X-Custom-Header"))
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	log := slog.Default()
	svc, _ := NewService(ServiceConfig{
		Name:    "test",
		BaseURL: backend.URL,
	}, log)

	app := fiber.New()
	app.All("/*", svc.Handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Custom-Header", "custom-value")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("X-Received-Auth") != "Bearer test-token" {
		t.Error("expected Authorization header to be forwarded")
	}
	if resp.Header.Get("X-Received-Custom") != "custom-value" {
		t.Error("expected custom header to be forwarded")
	}
}

func TestService_Handler_StripPrefix(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path))
	}))
	defer backend.Close()

	log := slog.Default()
	svc, _ := NewService(ServiceConfig{
		Name:        "test",
		BaseURL:     backend.URL,
		StripPrefix: "/api/v1",
	}, log)

	app := fiber.New()
	app.All("/api/v1/*", svc.Handler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/123", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "/users/123" {
		t.Errorf("expected /users/123, got %q", body)
	}
}

func TestService_Handler_PassesQueryString(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.RawQuery))
	}))
	defer backend.Close()

	log := slog.Default()
	svc, _ := NewService(ServiceConfig{
		Name:    "test",
		BaseURL: backend.URL,
	}, log)

	app := fiber.New()
	app.All("/*", svc.Handler())

	req := httptest.NewRequest(http.MethodGet, "/test?foo=bar&baz=qux", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "foo=bar&baz=qux" {
		t.Errorf("expected query string, got %q", body)
	}
}

func TestService_Handler_BackendUnavailable(t *testing.T) {
	log := slog.Default()
	svc, _ := NewService(ServiceConfig{
		Name:    "test",
		BaseURL: "http://localhost:59999", // Unlikely to be running.
		Timeout: 100 * time.Millisecond,
	}, log)

	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusBadGateway).SendString(err.Error())
		},
	})
	app.All("/*", svc.Handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", resp.StatusCode)
	}
}

func TestService_HealthCheck(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "healthy backend",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "unhealthy backend",
			statusCode: http.StatusServiceUnavailable,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("expected /healthz, got %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer backend.Close()

			log := slog.Default()
			svc, _ := NewService(ServiceConfig{
				Name:    "test",
				BaseURL: backend.URL,
			}, log)

			err := svc.HealthCheck(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("HealthCheck() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsHopByHop(t *testing.T) {
	hopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Transfer-Encoding",
		"Upgrade",
	}

	normalHeaders := []string{
		"Content-Type",
		"Authorization",
		"X-Custom-Header",
	}

	for _, h := range hopHeaders {
		if !isHopByHop(h) {
			t.Errorf("expected %q to be hop-by-hop", h)
		}
	}

	for _, h := range normalHeaders {
		if isHopByHop(h) {
			t.Errorf("expected %q to not be hop-by-hop", h)
		}
	}
}
