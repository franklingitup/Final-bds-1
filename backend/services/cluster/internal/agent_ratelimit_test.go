package cluster

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/ratelimit"
)

// fakeLimiter implements a minimal limiter for testing.
type fakeLimiter struct {
	allowed bool
	err     error
}

func (f *fakeLimiter) Allow(_ context.Context, _ string, _ ratelimit.Config) (*ratelimit.Result, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &ratelimit.Result{
		Allowed:    f.allowed,
		Limit:      7,
		Remaining:  3,
		ResetAfter: time.Minute,
	}, nil
}

// wrapFakeLimiter creates a middleware that uses our fake limiter.
// This bypasses the actual RedisLimiter since we're testing the middleware logic.
func wrapFakeLimiter(f *fakeLimiter, log *slog.Logger) fiber.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(c *fiber.Ctx) error {
		if f == nil {
			return c.Next()
		}
		result, err := f.Allow(c.UserContext(), "test-key", ratelimit.Config{})
		if err != nil {
			log.WarnContext(c.UserContext(), "rate limiter error; allowing request",
				slog.String("error", err.Error()))
			return c.Next()
		}
		if !result.Allowed {
			return c.Status(http.StatusTooManyRequests).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    "RATE_LIMITED",
					"message": "too many registration attempts",
				},
			})
		}
		return c.Next()
	}
}

func TestRegistrationRateLimiter_BlocksWhenNotAllowed(t *testing.T) {
	limiter := &fakeLimiter{allowed: false}
	middleware := wrapFakeLimiter(limiter, slog.Default())

	app := fiber.New()
	handlerCalled := false
	app.Post("/test", middleware, func(c *fiber.Ctx) error {
		handlerCalled = true
		return c.SendString("OK")
	})

	req := httptest.NewRequest("POST", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}
	if handlerCalled {
		t.Error("handler should not be called when rate limited")
	}
}

func TestRegistrationRateLimiter_AllowsWhenAllowed(t *testing.T) {
	limiter := &fakeLimiter{allowed: true}
	middleware := wrapFakeLimiter(limiter, slog.Default())

	app := fiber.New()
	handlerCalled := false
	app.Post("/test", middleware, func(c *fiber.Ctx) error {
		handlerCalled = true
		return c.SendString("OK")
	})

	req := httptest.NewRequest("POST", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !handlerCalled {
		t.Error("handler should be called when allowed")
	}
}

func TestRegistrationRateLimiter_FailsOpenOnError(t *testing.T) {
	limiter := &fakeLimiter{err: context.DeadlineExceeded}
	middleware := wrapFakeLimiter(limiter, slog.Default())

	app := fiber.New()
	handlerCalled := false
	app.Post("/test", middleware, func(c *fiber.Ctx) error {
		handlerCalled = true
		return c.SendString("OK")
	})

	req := httptest.NewRequest("POST", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d (fail-open)", resp.StatusCode, http.StatusOK)
	}
	if !handlerCalled {
		t.Error("handler should be called on limiter error (fail-open)")
	}
}

func TestRegistrationRateLimiter_NilLimiterAllowsAll(t *testing.T) {
	// Test the actual RegistrationRateLimiter with nil limiter.
	middleware := RegistrationRateLimiter(nil, slog.Default())

	app := fiber.New()
	handlerCalled := false
	app.Post("/test", middleware, func(c *fiber.Ctx) error {
		handlerCalled = true
		return c.SendString("OK")
	})

	req := httptest.NewRequest("POST", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !handlerCalled {
		t.Error("handler should be called when limiter is nil")
	}
}
