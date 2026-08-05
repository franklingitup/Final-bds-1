// Command server is the entrypoint for the API Gateway.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/cors"

	"github.com/redis/go-redis/v9"

	"github.com/bdsplatform/platform/backend/libs/config"
	libmw "github.com/bdsplatform/platform/backend/libs/middleware"
	"github.com/bdsplatform/platform/backend/libs/logger"
	"github.com/bdsplatform/platform/backend/libs/ratelimit"
	"github.com/bdsplatform/platform/backend/libs/security"
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
	ctx := context.Background()
	shutdownTelemetry, err := telemetry.Init(ctx, cfg)
	if err != nil {
		log.Warn("telemetry initialization failed", "error", err)
	}
	defer func() {
		if shutdownTelemetry != nil {
			_ = shutdownTelemetry(context.Background())
		}
	}()

	// Create token validator.
	validator := auth.NewValidator(cfg.Auth)

	// One Redis client, shared by rate limiting and token revocation (no
	// duplicate clients). When a Redis URL is configured both features use the
	// distributed backend across gateway replicas; otherwise rate limiting falls
	// back to in-process and revocation is disabled.
	var redisClient redis.UniversalClient
	if gwCfg.RateLimitRedisURL != "" {
		client, rlErr := ratelimit.ParseRedisURL(gwCfg.RateLimitRedisURL)
		if rlErr != nil {
			log.Error("failed to parse Redis URL", "error", rlErr)
			os.Exit(1)
		}
		if pingErr := client.Ping(ctx).Err(); pingErr != nil {
			log.Error("failed to connect to Redis", "error", pingErr)
			os.Exit(1)
		}
		defer func() { _ = client.Close() }()
		redisClient = client
	}

	// Create the rate limiter using the shared client when present.
	var rateLimiter middleware.Limiter
	if redisClient != nil {
		rateLimiter = middleware.NewRedisRateLimiter(
			ratelimit.NewRedisLimiter(redisClient, "gateway:"),
			gwCfg.RateLimitRequestsPerMinute,
			gwCfg.RateLimitBurstSize,
			log,
		)
		log.Info("rate limiting enabled", "backend", "redis")
	} else {
		rlConfig := middleware.DefaultRateLimiterConfig()
		rlConfig.RequestsPerMinute = gwCfg.RateLimitRequestsPerMinute
		rlConfig.BurstSize = gwCfg.RateLimitBurstSize
		memLimiter := middleware.NewRateLimiter(rlConfig)

		// Start cleanup goroutine for the in-memory limiter.
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				memLimiter.Cleanup(10 * time.Minute)
			}
		}()

		rateLimiter = memLimiter
		log.Info("rate limiting enabled", "backend", "memory")
	}

	// Token revocation checker over the same Redis client, reusing the shared
	// "revoked:" convention that the auth service writes to. Nil when Redis is
	// not configured (signature-only auth).
	var revoker *middleware.RevocationChecker
	if redisClient != nil {
		revoker = middleware.NewRevocationChecker(security.NewTokenRevocationList(redisClient, "revoked:"), log)
		log.Info("token revocation enabled", "backend", "redis")
	} else {
		log.Warn("REDIS_URL not set; token revocation checks disabled (signature-only auth)")
	}

	// Create router with the shared rate limiter and revocation checker.
	r, err := router.New(validator, revoker, rateLimiter, gwCfg.Services, log)
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

	// CORS middleware - must be first to handle preflight OPTIONS requests.
	app.Use(cors.New(cors.Config{
		AllowOrigins:     gwCfg.CORSAllowedOrigins,
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization,X-Request-ID,X-Correlation-ID",
		AllowCredentials: true,
		MaxAge:           86400, // 24 hours
	}))

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

	app.Get("/metrics", adaptor.HTTPHandler(telemetry.MetricsHandler()))
}
