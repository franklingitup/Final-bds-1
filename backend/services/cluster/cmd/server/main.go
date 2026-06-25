// Command server is the entrypoint for the cluster service.
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
	cluster "github.com/bdsplatform/platform/backend/services/cluster/internal"
)

func main() {
	cfg := config.MustLoad("cluster")
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
	clusterStore := cluster.NewClusterStore(db)
	svc := cluster.NewService(cluster.Deps{
		Clusters:   clusterStore,
		Tokens:     cluster.NewTokenStore(db),
		Heartbeats: cluster.NewHeartbeatStore(db),
		OrgMembers: authz.NewOrgMemberRepo(db), // For org membership authorization
		Outbox:     outbox,
		Tenant:     db,
		Logger:     log,
	})
	handler := cluster.NewHandler(svc, cluster.NewTokenVerifier(cfg.Auth))

	// Create agent handler for credential-based agent endpoints.
	clusterValidator := cluster.NewClusterValidator(clusterStore)
	agentHandler := cluster.NewAgentHandler(svc, clusterValidator, log)

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
		cluster.RegisterRoutes(app, handler)
		cluster.RegisterAgentRoutes(app, agentHandler)
	}); err != nil {
		log.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// runMigrations applies the tenant schema (organizations), cluster schema,
// and the shared outbox schema.
func runMigrations(ctx context.Context, db *database.DB) error {
	for _, m := range []struct {
		service string
		table   string
	}{
		{"tenant", "schema_migrations_tenant"},
		{"cluster", "schema_migrations_cluster"},
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

// newPublisher returns an event publisher and a cleanup function.
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
