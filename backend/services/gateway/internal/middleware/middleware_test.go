package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	libmw "github.com/bdsplatform/platform/backend/libs/middleware"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/auth"
)

// newTestApp creates a Fiber app with the standard error handler.
func newTestApp() *fiber.App {
	return fiber.New(fiber.Config{ErrorHandler: libmw.ErrorHandler()})
}

func TestRequestID_GeneratesID(t *testing.T) {
	app := fiber.New()
	app.Use(RequestID())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	requestID := resp.Header.Get(HeaderRequestID)
	if requestID == "" {
		t.Error("expected X-Request-ID header")
	}
	if len(requestID) != 36 { // UUID length.
		t.Errorf("expected UUID, got %q", requestID)
	}
}

func TestRequestID_PreservesExisting(t *testing.T) {
	app := fiber.New()
	app.Use(RequestID())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	existingID := "existing-request-id"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, existingID)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	requestID := resp.Header.Get(HeaderRequestID)
	if requestID != existingID {
		t.Errorf("expected %q, got %q", existingID, requestID)
	}
}

func TestRequestID_SetsCorrelationID(t *testing.T) {
	app := fiber.New()
	app.Use(RequestID())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	correlationID := resp.Header.Get(HeaderCorrelationID)
	requestID := resp.Header.Get(HeaderRequestID)
	if correlationID != requestID {
		t.Errorf("expected correlation ID %q to match request ID %q", correlationID, requestID)
	}
}

func TestRateLimiter_AllowsRequests(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerMinute: 10,
		BurstSize:         5,
		KeyFunc:           func(c *fiber.Ctx) string { return "test" },
	})

	app := fiber.New()
	app.Use(rl.Middleware())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// First 5 requests should succeed (burst).
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestRateLimiter_BlocksExcessRequests(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerMinute: 60, // 1 per second.
		BurstSize:         2,
		KeyFunc:           func(c *fiber.Ctx) string { return "test" },
	})

	app := newTestApp()
	app.Use(rl.Middleware())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// Exhaust burst.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		resp, _ := app.Test(req)
		resp.Body.Close()
	}

	// Next request should be rate limited.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, _ := app.Test(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 429, got %d: %s", resp.StatusCode, body)
	}

	// Check rate limit headers.
	if resp.Header.Get(HeaderRateLimitLimit) == "" {
		t.Error("expected X-RateLimit-Limit header")
	}
}

func TestRateLimiter_SeparatesKeys(t *testing.T) {
	keyCounter := 0
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerMinute: 60,
		BurstSize:         1,
		KeyFunc: func(c *fiber.Ctx) string {
			keyCounter++
			return "key" + string(rune('0'+keyCounter%10))
		},
	})

	app := fiber.New()
	app.Use(rl.Middleware())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// Different keys should each get their own bucket.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerMinute: 60,
		BurstSize:         5,
		KeyFunc:           func(c *fiber.Ctx) string { return "test" },
	})

	// Add an entry.
	rl.mu.Lock()
	rl.entries["old"] = &rateLimitEntry{
		tokens:    5,
		lastCheck: time.Now().Add(-20 * time.Minute),
	}
	rl.entries["recent"] = &rateLimitEntry{
		tokens:    5,
		lastCheck: time.Now().Add(-1 * time.Minute),
	}
	rl.mu.Unlock()

	// Cleanup old entries.
	rl.Cleanup(10 * time.Minute)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if _, exists := rl.entries["old"]; exists {
		t.Error("expected old entry to be cleaned up")
	}
	if _, exists := rl.entries["recent"]; !exists {
		t.Error("expected recent entry to remain")
	}
}

func TestOrgScope_SetsHeader(t *testing.T) {
	app := fiber.New()

	// Mock authentication.
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(LocalIdentity, auth.Identity{
			Type:   auth.TokenTypeUser,
			UserID: "user-123",
		})
		return c.Next()
	})

	var receivedOrgID string
	// OrgScope must be added to a route group (not globally) so c.Params() works.
	orgs := app.Group("/orgs/:orgId", OrgScope())
	orgs.Get("/test", func(c *fiber.Ctx) error {
		// Read the org ID from the request header that OrgScope set.
		receivedOrgID = c.Get(HeaderOrgID)
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/orgs/org-456/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// OrgScope sets the org ID as a request header for downstream services.
	if receivedOrgID != "org-456" {
		t.Errorf("expected org-456, got %s", receivedOrgID)
	}
}

func TestOrgScope_ValidatesServiceAccountOrg(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusForbidden).SendString(err.Error())
		},
	})

	// Mock service account authentication with different org.
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(LocalIdentity, auth.Identity{
			Type:   auth.TokenTypeServiceAccount,
			UserID: "sa-123",
			OrgID:  "org-999", // Different org.
		})
		return c.Next()
	})

	// OrgScope must be added to a route group (not globally) so c.Params() works.
	orgs := app.Group("/orgs/:orgId", OrgScope())
	orgs.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/orgs/org-456/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestRequireScope_AllowsUserTokens(t *testing.T) {
	app := fiber.New()

	app.Use(func(c *fiber.Ctx) error {
		c.Locals(LocalIdentity, auth.Identity{
			Type:   auth.TokenTypeUser,
			UserID: "user-123",
		})
		return c.Next()
	})
	app.Use(RequireScope("admin"))
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRequireScope_ChecksServiceAccountScope(t *testing.T) {
	tests := []struct {
		name          string
		scopes        []string
		requiredScope string
		expectStatus  int
	}{
		{
			name:          "has scope",
			scopes:        []string{"read", "write"},
			requiredScope: "write",
			expectStatus:  http.StatusOK,
		},
		{
			name:          "missing scope",
			scopes:        []string{"read"},
			requiredScope: "write",
			expectStatus:  http.StatusForbidden,
		},
		{
			name:          "wildcard scope",
			scopes:        []string{"*"},
			requiredScope: "anything",
			expectStatus:  http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New(fiber.Config{
				ErrorHandler: func(c *fiber.Ctx, err error) error {
					return c.Status(fiber.StatusForbidden).SendString(err.Error())
				},
			})

			app.Use(func(c *fiber.Ctx) error {
				c.Locals(LocalIdentity, auth.Identity{
					Type:   auth.TokenTypeServiceAccount,
					UserID: "sa-123",
					Scopes: tt.scopes,
				})
				return c.Next()
			})
			app.Use(RequireScope(tt.requiredScope))
			app.Get("/", func(c *fiber.Ctx) error {
				return c.SendString("ok")
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectStatus {
				t.Errorf("expected %d, got %d", tt.expectStatus, resp.StatusCode)
			}
		})
	}
}

func TestGetIdentity(t *testing.T) {
	app := fiber.New()

	app.Use(func(c *fiber.Ctx) error {
		c.Locals(LocalIdentity, auth.Identity{
			Type:   auth.TokenTypeUser,
			UserID: "user-123",
		})
		return c.Next()
	})
	app.Get("/", func(c *fiber.Ctx) error {
		id, ok := GetIdentity(c)
		if !ok {
			return c.SendString("no identity")
		}
		return c.SendString(id.UserID)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "user-123" {
		t.Errorf("expected user-123, got %s", body)
	}
}

func TestGetRequestID(t *testing.T) {
	app := fiber.New()

	app.Use(RequestID())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(GetRequestID(c))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if len(body) != 36 { // UUID length.
		t.Errorf("expected UUID, got %q", body)
	}
}
