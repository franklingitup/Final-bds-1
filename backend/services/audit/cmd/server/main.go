// Command server is the entrypoint for the audit service. It consumes platform
// domain events into an immutable audit log and serves tenant-scoped queries.
package main

import (
	"context"
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
	audit "github.com/bdsplatform/platform/backend/services/audit/internal"
)

func main() {
	cfg := config.MustLoad("audit")
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

	svc := audit.NewService(audit.Deps{
		Store:      audit.NewAuditLogStore(db),
		OrgMembers: authz.NewOrgMemberRepo(db), // For org membership authorization
		Tenant:     db,
		Logger:     log,
	})

	// Subscribe to all platform events and record the supported domains. The
	// subscriber is the NATS client when configured, otherwise an in-memory
	// broker (which receives nothing in this process, so audit is idle in local
	// dev without a broker).
	subscriber, closeEvents, err := newSubscriber(ctx, cfg, log)
	if err != nil {
		log.Error("init events", "error", err)
		os.Exit(1)
	}
	defer closeEvents()

	consumer := audit.NewConsumer(svc, subscriber, cfg.NATS.SubjectPrefix, log)
	if err := consumer.Start(ctx); err != nil {
		log.Error("start audit consumer", "error", err)
		os.Exit(1)
	}
	defer consumer.Stop()

	handler := audit.NewHandler(svc, audit.NewTokenVerifier(cfg.Auth))
	if err := httpserver.Run(cfg, func(app *fiber.App) {
		audit.RegisterRoutes(app, handler)
	}); err != nil {
		log.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// runMigrations applies the audit schema, tracked in its own migrations table.
func runMigrations(ctx context.Context, db *database.DB) error {
	fsys, err := migrations.Service("audit")
	if err != nil {
		return err
	}
	migs, err := database.LoadMigrations(fsys)
	if err != nil {
		return err
	}
	migrator, err := database.NewMigrator(db, "schema_migrations_audit", migs)
	if err != nil {
		return err
	}
	_, err = migrator.Up(ctx)
	return err
}

// newSubscriber returns an event subscriber and a cleanup function. When NATS is
// configured it uses JetStream (ensuring streams exist); otherwise it falls back
// to an in-memory broker so the service runs without a broker.
func newSubscriber(ctx context.Context, cfg config.Config, log *slog.Logger) (events.Subscriber, func(), error) {
	if cfg.NATS.URL == "" {
		log.Warn("NATS not configured; using in-memory event broker (audit will record no events)")
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
	return client, client.Close, nil
}
