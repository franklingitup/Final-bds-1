// Command server is the entrypoint for the auth service.
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
	"github.com/bdsplatform/platform/backend/libs/ratelimit"
	"github.com/bdsplatform/platform/backend/libs/security"
	"github.com/bdsplatform/platform/backend/migrations"
	auth "github.com/bdsplatform/platform/backend/services/auth/internal"
)

func main() {
	cfg := config.MustLoad("auth")
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

	// Auth publishes domain events through the transactional outbox so a
	// committed state change always has its event durably recorded; the relay
	// drains the outbox to the broker.
	outbox := events.NewPostgresOutbox(db, "outbox")

	// Create org member repo for authorization checks on service account endpoints
	orgMemberRepo := authz.NewOrgMemberRepo(db)

	// Token revocation store. When REDIS_URL is set, logout and refresh rotation
	// record the affected session in Redis so the gateway rejects its access
	// token before expiry. Reuses the shared Redis client factory (no bespoke
	// client) and the "revoked:" key convention from libs/security. Without
	// Redis the service still revokes sessions in the database.
	revoker, closeRevoker := newRevoker(ctx, cfg, log)
	defer closeRevoker()

	svc := auth.NewService(auth.Deps{
		Users:           auth.NewUserStore(db),
		Sessions:        auth.NewSessionStore(db),
		OneTimeTokens:   auth.NewOneTimeTokenStore(db),
		ServiceAccounts: auth.NewServiceAccountStore(db),
		APITokens:       auth.NewAPITokenStore(db),
		OrgMembers:      orgMemberRepo,
		Tx:              db,
		Tenant:          db,
		JWT:             auth.NewJWTIssuer(cfg.Auth),
		Outbox:          outbox,
		Revoker:         revoker,
		Auth:            cfg.Auth,
		Logger:          log,
	})
	handler := auth.NewHandler(svc)

	relay := events.NewRelay(db, outbox, publisher, log, events.RelayOptions{})
	relayCtx, cancelRelay := context.WithCancel(ctx)
	defer cancelRelay()
	go func() {
		if err := relay.Run(relayCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("outbox relay stopped", "error", err)
		}
	}()

	if err := httpserver.Run(cfg, func(app *fiber.App) {
		auth.RegisterRoutes(app, handler)
	}); err != nil {
		log.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// runMigrations applies the auth schema and the shared outbox schema, each
// tracked in its own migrations table so they can share one database.
func runMigrations(ctx context.Context, db *database.DB) error {
	for _, m := range []struct {
		service string
		table   string
	}{
		{"auth", "schema_migrations_auth"},
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

// newRevoker builds the Redis-backed token revocation store and a cleanup
// function. When REDIS_URL is unset it returns a nil revoker (revocation of
// access tokens before expiry is disabled; database session state still
// applies) and a no-op cleanup. A Redis that is configured but unreachable is
// fatal: revocation is a security control and silently degrading it at startup
// would be worse than failing loudly.
func newRevoker(ctx context.Context, cfg config.Config, log *slog.Logger) (auth.TokenRevoker, func()) {
	if cfg.Redis.URL == "" {
		log.Warn("REDIS_URL not set; access-token revocation disabled (database session revocation still applies)")
		return nil, func() {}
	}
	client, err := ratelimit.ParseRedisURL(cfg.Redis.URL)
	if err != nil {
		log.Error("parse REDIS_URL", "error", err)
		os.Exit(1)
	}
	if err := client.Ping(ctx).Err(); err != nil {
		log.Error("connect to revocation Redis", "error", err)
		os.Exit(1)
	}
	log.Info("token revocation enabled", "backend", "redis")
	return security.NewTokenRevocationList(client, "revoked:"), func() { _ = client.Close() }
}

// newPublisher returns an event publisher and a cleanup function. When NATS is
// configured it uses JetStream; otherwise it falls back to an in-memory broker
// so the service runs in environments without a broker.
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
