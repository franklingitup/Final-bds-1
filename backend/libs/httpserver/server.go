// Package httpserver builds a standardized Fiber application with all platform
// middleware and operational endpoints, and runs it with graceful shutdown.
package httpserver

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/middleware"
	"github.com/bdsplatform/platform/backend/libs/telemetry"
)

// Version is overridden at build time via -ldflags.
var Version = "dev"

// New builds a Fiber app preconfigured with the standard middleware chain and
// operational endpoints. Services register business routes on the returned app.
func New(cfg config.Config, log *slog.Logger) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               cfg.ServiceName,
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler(),
	})

	// Recover is outermost so it captures panics in all later middleware/handlers.
	app.Use(middleware.Recover(log))
	app.Use(middleware.CorrelationID())
	app.Use(middleware.Tracing(cfg.ServiceName))
	app.Use(middleware.Tenant())
	app.Use(middleware.RequestLogger(log))

	registerOps(app, cfg.ServiceName)
	return app
}

func registerOps(app *fiber.App, serviceName string) {
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": serviceName})
	})
	app.Get("/readyz", func(c *fiber.Ctx) error {
		// Services may override with dependency checks (db/redis/broker).
		return c.JSON(fiber.Map{"status": "ready"})
	})
	app.Get("/version", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"service": serviceName, "version": Version})
	})
	app.Get("/metrics", adaptor.HTTPHandler(telemetry.MetricsHandler()))
}
