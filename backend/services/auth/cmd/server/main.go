// Command server is the entrypoint for the auth service.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

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
	ssoConfigs := auth.NewSSOConfigStore(db)

	ssoProviders, ssoKeyEncryptor, err := newSSOProviderManager(cfg, ssoConfigs, log)
	if err != nil {
		log.Error("init sso provider manager", "error", err)
		os.Exit(1)
	}
	exchangeEncryptor := ssoKeyEncryptor
	if exchangeEncryptor == nil {
		ephemeralKey := make([]byte, 32)
		if _, err := rand.Read(ephemeralKey); err != nil {
			log.Error("generate local SSO handoff encryption key", "error", err)
			os.Exit(1)
		}
		exchangeEncryptor, err = auth.NewSSOKeyEncryptorFromBytes(ephemeralKey)
		if err != nil {
			log.Error("init local SSO handoff encryption", "error", err)
			os.Exit(1)
		}
		log.Warn("using an ephemeral SSO token-handoff encryption key; configure CERTIFICATE_ENCRYPTION_KEY for multi-replica operation")
	}
	ssoRedirectURL, err := ssoBrowserRedirectURL(log)
	if err != nil {
		log.Error("init SSO browser redirect", "error", err)
		os.Exit(1)
	}

	// Token revocation store. When REDIS_URL is set, logout and refresh rotation
	// record the affected session in Redis so the gateway rejects its access
	// token before expiry. Reuses the shared Redis client factory (no bespoke
	// client) and the "revoked:" key convention from libs/security. Without
	// Redis the service still revokes sessions in the database.
	revoker, closeRevoker := newRevoker(ctx, cfg, log)
	defer closeRevoker()

	svc := auth.NewService(auth.Deps{
		Users:            auth.NewUserStore(db),
		Sessions:         auth.NewSessionStore(db),
		OneTimeTokens:    auth.NewOneTimeTokenStore(db),
		ServiceAccounts:  auth.NewServiceAccountStore(db),
		APITokens:        auth.NewAPITokenStore(db),
		SSOConfigs:       ssoConfigs,
		SSOProviders:     ssoProviders,
		SSOOrganizations: auth.NewSSOOrganizationStore(db),
		SSOMembers:       auth.NewSSOMemberStore(db),
		SSOHandoffs:      auth.NewSSOHandoffStore(db),
		SSOEncryptor:     exchangeEncryptor,
		SSORedirectURL:   ssoRedirectURL,
		OrgMembers:       orgMemberRepo,
		Tx:               db,
		Tenant:           db,
		JWT:              auth.NewJWTIssuer(cfg.Auth),
		Outbox:           outbox,
		Revoker:          revoker,
		Auth:             cfg.Auth,
		Logger:           log,
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

// newSSOProviderManager wires the per-org SAML ServiceProvider cache.
// PUBLIC_API_BASE_URL is the externally reachable API origin used for ACS and
// SP metadata URLs. CERTIFICATE_ENCRYPTION_KEY (same as the domain service)
// encrypts SP private keys at rest when set.
func newSSOProviderManager(cfg config.Config, keys auth.SSOConfigStore, log *slog.Logger) (*auth.SSOProviderManager, *auth.SSOKeyEncryptor, error) {
	publicBase := os.Getenv("PUBLIC_API_BASE_URL")
	if publicBase == "" {
		publicBase = "http://localhost:8080"
		log.Warn("PUBLIC_API_BASE_URL not set; defaulting for SAML ACS/metadata URLs", "url", publicBase)
	}

	var encryptor *auth.SSOKeyEncryptor
	if key := os.Getenv("CERTIFICATE_ENCRYPTION_KEY"); key != "" {
		var err error
		encryptor, err = auth.NewSSOKeyEncryptor(key)
		if err != nil {
			return nil, nil, err
		}
	} else if cfg.Environment == config.EnvProduction || cfg.Environment == config.EnvStaging {
		return nil, nil, errors.New("CERTIFICATE_ENCRYPTION_KEY is required outside local development for SAML SP keys")
	} else {
		log.Warn("CERTIFICATE_ENCRYPTION_KEY is not set; SAML SP private keys will be stored unencrypted")
	}

	manager, err := auth.NewSSOProviderManager(auth.SSOProviderManagerOpts{
		PublicBaseURL: publicBase,
		Encryptor:     encryptor,
		Keys:          keys,
	})
	if err != nil {
		return nil, nil, err
	}
	return manager, encryptor, nil
}

func ssoBrowserRedirectURL(log *slog.Logger) (string, error) {
	publicWebBase := strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_WEB_BASE_URL")), "/")
	if publicWebBase == "" {
		publicWebBase = "http://localhost:3000"
		log.Warn("PUBLIC_WEB_BASE_URL not set; defaulting SAML browser callback URL", "url", publicWebBase)
	}
	u, err := url.Parse(publicWebBase)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid PUBLIC_WEB_BASE_URL %q", publicWebBase)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/sso/callback"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
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
