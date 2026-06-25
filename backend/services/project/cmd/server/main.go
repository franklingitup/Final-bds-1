// Command server is the entrypoint for the project service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	"github.com/bdsplatform/platform/backend/libs/logger"
	"github.com/bdsplatform/platform/backend/migrations"
	project "github.com/bdsplatform/platform/backend/services/project/internal"
)

func main() {
	cfg := config.MustLoad("project")
	log := logger.New(cfg)
	ctx := context.Background()

	db, err := database.Connect(ctx, cfg)
	if err != nil {
		log.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := runMigrations(ctx, db); err != nil {
		log.Error("apply migrations", "error", err)
		os.Exit(1)
	}

	publisher, closeEvents, err := newPublisher(ctx, cfg, log)
	if err != nil {
		log.Error("init events", "error", err)
		os.Exit(1)
	}
	defer closeEvents()

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := project.NewService(project.Deps{
		Projects:   project.NewProjectStore(db),
		Members:    project.NewMemberStore(db),
		OrgMembers: authz.NewOrgMemberRepo(db), // For org membership authorization
		Outbox:     outbox,
		Tenant:     db,
		Logger:     log,
	})
	handler := project.NewHandler(svc, project.NewTokenVerifier(cfg.Auth))

	// Drain the transactional outbox to the broker in the background.
	relay := events.NewRelay(db, outbox, publisher, log, events.RelayOptions{})
	relayCtx, cancelRelay := context.WithCancel(ctx)
	defer cancelRelay()
	go func() {
		if err := relay.Run(relayCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("outbox relay stopped", "error", err)
		}
	}()

	if err := httpserver.Run(cfg, func(app *fiber.App) {
		project.RegisterRoutes(app, handler)
	}); err != nil {
		log.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// runMigrations applies the tenant schema (which owns projects table), the
// project-specific migrations (memberships), and the shared outbox schema.
func runMigrations(ctx context.Context, db *database.DB) error {
	for _, m := range []struct {
		service string
		table   string
	}{
		{"tenant", "schema_migrations_tenant"},
		{"project", "schema_migrations_project"},
		{"outbox", "schema_migrations_outbox"},
	} {
		fsys, err := migrations.Service(m.service)
		if err != nil {
			return err
		}
		migs, err := database.LoadMigrations(fsys)
		if err != nil {
			return err
		}
		migrator, err := database.NewMigrator(db, m.table, migs)
		if err != nil {
			return err
		}
		if _, err := migrator.Up(ctx); err != nil {
			return err
		}
	}
	return nil
}

// newPublisher returns an event publisher and a cleanup function. With NATS
// configured it uses JetStream; otherwise it falls back to an in-memory broker.
func newPublisher(ctx context.Context, cfg config.Config, log *slog.Logger) (events.Publisher, func(), error) {
	if cfg.NATS.URL == "" {
		log.Warn("NATS not configured; using in-memory event broker")
		return events.NewMemoryBroker(cfg.NATS.SubjectPrefix), func() {}, nil
	}
	client, err := events.Connect(ctx, cfg.NATS, log)
	if err != nil {
		return nil, nil, err
	}
	if err := client.EnsureStreams(ctx); err != nil {
		client.Close()
		return nil, nil, err
	}
	return client.NewPublisher(), client.Close, nil
}
