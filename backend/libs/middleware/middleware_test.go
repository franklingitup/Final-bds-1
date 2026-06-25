package middleware

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/logger"
)

func testApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler()})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	app.Use(Recover(log))
	app.Use(CorrelationID())
	app.Use(Tracing("test"))
	app.Use(Tenant())
	app.Use(RequestLogger(log))
	return app
}

func TestCorrelationID_GeneratedAndEchoed(t *testing.T) {
	app := testApp()
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if resp.Header.Get(HeaderCorrelationID) == "" {
		t.Error("expected correlation id header to be set")
	}
}

func TestCorrelationID_PreservesInbound(t *testing.T) {
	app := testApp()
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(HeaderCorrelationID, "abc-123")
	resp, _ := app.Test(req)
	if got := resp.Header.Get(HeaderCorrelationID); got != "abc-123" {
		t.Errorf("correlation id = %q, want abc-123", got)
	}
}

func TestTenant_PopulatesContext(t *testing.T) {
	app := testApp()
	var seen string
	app.Get("/", func(c *fiber.Ctx) error {
		seen = authz.OrgFromContext(c.UserContext())
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(HeaderOrgID, "org-7")
	if _, err := app.Test(req); err != nil {
		t.Fatalf("test: %v", err)
	}
	if seen != "org-7" {
		t.Errorf("org in context = %q, want org-7", seen)
	}
}

func TestErrorHandler_RendersEnvelope(t *testing.T) {
	app := testApp()
	app.Get("/", func(c *fiber.Ctx) error { return errors.NotFound("no such thing") })

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRecover_TurnsPanicInto500(t *testing.T) {
	app := testApp()
	app.Get("/", func(c *fiber.Ctx) error { panic("boom") })

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// ensure logger context helper is wired (compile dependency)
var _ = logger.CorrelationID
