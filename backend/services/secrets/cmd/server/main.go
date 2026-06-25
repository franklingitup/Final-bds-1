// Command server is the entrypoint for the secrets service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	"github.com/bdsplatform/platform/backend/libs/logger"
	"github.com/bdsplatform/platform/backend/migrations"
	secrets "github.com/bdsplatform/platform/backend/services/secrets/internal"
)

const jwtIssuer = "bdsplatform-auth"

func main() {
	cfg := config.MustLoad("secrets")
	log := logger.New(cfg)
	ctx := context.Background()

	// Load master encryption key
	masterKey := os.Getenv("SECRETS_MASTER_KEY")
	if masterKey == "" {
		log.Error("SECRETS_MASTER_KEY environment variable is required")
		os.Exit(1)
	}

	encryptor, err := secrets.NewEncryptor(masterKey)
	if err != nil {
		log.Error("failed to initialize encryptor", "error", err)
		os.Exit(1)
	}

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
	memberLookup := newMemberLookup(db)

	svc := secrets.NewService(secrets.Deps{
		Secrets:    secrets.NewSecretRepository(db),
		AccessLogs: secrets.NewAccessLogRepository(db),
		Encryptor:  encryptor,
		Outbox:     outbox,
		Tenant:     db,
		Members:    memberLookup,
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     log,
	})

	// User-facing handler
	handler := secrets.NewHandler(svc, newTokenVerifier(cfg.Auth))

	// Agent handler (for Platform Agent to sync secrets)
	clusterValidator := secrets.NewClusterValidator(db.Pool)
	agentHandler := secrets.NewAgentHandler(svc, clusterValidator, log)

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
		secrets.RegisterRoutes(app, handler)
		secrets.RegisterAgentRoutes(app, agentHandler)
	}); err != nil {
		log.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// runMigrations applies required migrations.
func runMigrations(ctx context.Context, db *database.DB) error {
	for _, m := range []struct {
		service string
		table   string
	}{
		{"tenant", "schema_migrations_tenant"},
		{"project", "schema_migrations_project"},
		{"secrets", "schema_migrations_secrets"},
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

// tokenVerifier implements secrets.TokenVerifier.
type tokenVerifier struct {
	cfg config.AuthConfig
}

func newTokenVerifier(cfg config.AuthConfig) *tokenVerifier {
	return &tokenVerifier{cfg: cfg}
}

func (v *tokenVerifier) Verify(token string) (secrets.Identity, error) {
	claims, err := parseJWT(token, v.cfg.JWTSigningKey)
	if err != nil {
		return secrets.Identity{}, err
	}
	return secrets.Identity{
		UserID: claims.Subject,
		Email:  claims.Email,
	}, nil
}

// memberLookup implements secrets.MemberLookup.
type memberLookup struct {
	db *database.DB
}

func newMemberLookup(db *database.DB) *memberLookup {
	return &memberLookup{db: db}
}

func (m *memberLookup) GetByUser(ctx context.Context, projectID, userID string) (authz.ProjectRole, error) {
	const sql = `
		SELECT role FROM project_members
		WHERE project_id = $1 AND user_id = $2`

	var role string
	err := m.db.Conn(ctx).QueryRow(ctx, sql, projectID, userID).Scan(&role)
	if err != nil {
		return "", database.MapError(err)
	}
	return authz.ProjectRole(role), nil
}

// accessClaims mirrors the claims minted by the auth service.
type accessClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

var errInvalidToken = fmt.Errorf("invalid token")

// parseJWT parses and validates an access token.
func parseJWT(token, signingKey string) (*accessClaims, error) {
	claims := &accessClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return []byte(signingKey), nil
	}, jwt.WithIssuer(jwtIssuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return nil, errInvalidToken
	}
	return claims, nil
}
