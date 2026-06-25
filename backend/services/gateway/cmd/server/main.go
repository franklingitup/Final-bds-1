// Command server is the entrypoint for the API Gateway.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/config"
	libmw "github.com/bdsplatform/platform/backend/libs/middleware"
	"github.com/bdsplatform/platform/backend/libs/logger"
	"github.com/bdsplatform/platform/backend/libs/telemetry"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/auth"
	gwconfig "github.com/bdsplatform/platform/backend/services/gateway/internal/config"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/middleware"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/router"
)

func main() {
	cfg := config.MustLoad("gateway")
	gwCfg := gwconfig.Load()
	log := logger.New(cfg)

	// Initialize telemetry.
	shutdownTelemetry, err := telemetry.Init(cfg.OTEL, cfg.ServiceName)
	if err != nil {
		log.Warn("telemetry initialization failed", "error", err)
	}
	defer shutdownTelemetry()

	// Create token validator.
	validator := auth.NewValidator(cfg.Auth)

	// Create rate limiter with custom config.
	rateLimiter := middleware.NewRateLimiter(middleware.RateLimiterConfig{
		RequestsPerMinute: gwCfg.RateLimitRequestsPerMinute,
		BurstSize:         gwCfg.RateLimitBurstSize,
	})

	// Start cleanup goroutine for rate limiter.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rateLimiter.Cleanup(10 * time.Minute)
		}
	}()

	// Create router with the shared rate limiter.
	r, err := router.New(validator, rateLimiter, gwCfg.Services, log)
	if err != nil {
		log.Error("failed to create router", "error", err)
		os.Exit(1)
	}

	// Create Fiber app with standard middleware.
	app := fiber.New(fiber.Config{
		AppName:               cfg.ServiceName,
		DisableStartupMessage: true,
		ErrorHandler:          libmw.ErrorHandler(),
	})

	// Standard middleware chain.
	app.Use(libmw.Recover(log))
	app.Use(libmw.CorrelationID())
	app.Use(libmw.Tracing(cfg.ServiceName))
	app.Use(libmw.RequestLogger(log))

	// Register operational endpoints.
	registerOps(app, cfg.ServiceName, r)

	// Register gateway routes.
	r.Register(app)

	// Start server.
	go func() {
		log.Info("starting gateway", "addr", cfg.HTTPAddr)
		if err := app.Listen(cfg.HTTPAddr); err != nil {
			log.Error("server error", "error", err)
		}
	}()

	// Wait for shutdown signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down gateway")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := app.ShutdownWithContext(ctx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("gateway stopped")
}

func registerOps(app *fiber.App, serviceName string, r *router.Router) {
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": serviceName})
	})

	app.Get("/readyz", func(c *fiber.Ctx) error {
		ctx := c.UserContext()
		services := r.Services()
		status := "ready"
		details := make(map[string]string)

		for name, svc := range services {
			if err := svc.HealthCheck(ctx); err != nil {
				details[name] = "unhealthy: " + err.Error()
				status = "degraded"
			} else {
				details[name] = "healthy"
			}
		}

		return c.JSON(fiber.Map{
			"status":   status,
			"services": details,
		})
	})

	app.Get("/version", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"service": serviceName, "version": "dev"})
	})

	app.Get("/metrics", fiber.Handler(func(c *fiber.Ctx) error {
		// Use the standard telemetry metrics handler.
		return c.SendString("# Gateway metrics endpoint - use Prometheus scrape")
	}))
}
