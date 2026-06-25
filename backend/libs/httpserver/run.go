package httpserver

import (
	"context"
	"errors"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/logger"
	"github.com/bdsplatform/platform/backend/libs/telemetry"
)

const shutdownTimeout = 15 * time.Second

// Run is the single entrypoint used by every service's main. It initializes the
// logger and telemetry, builds the standard app, lets the caller register
// routes, serves until an interrupt/term signal, then shuts down gracefully.
func Run(cfg config.Config, register func(app *fiber.App)) error {
	log := logger.New(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTel, err := telemetry.Init(ctx, cfg)
	if err != nil {
		log.Warn("telemetry init failed; continuing without tracing", "error", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if shutdownTel != nil {
			_ = shutdownTel(shutdownCtx)
		}
	}()

	app := New(cfg, log)
	if register != nil {
		register(app)
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("server listening", "addr", cfg.HTTPAddr, "service", cfg.ServiceName)
		if err := app.Listen(cfg.HTTPAddr); err != nil && !errors.Is(err, fiber.ErrGracefulTimeout) {
			serveErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case err := <-serveErr:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		return err
	}
	log.Info("server stopped cleanly")
	return nil
}
