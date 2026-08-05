// Command server is the entrypoint for the provisioning service.
package main

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	"github.com/bdsplatform/platform/backend/migrations"
	provisioning "github.com/bdsplatform/platform/backend/services/provisioning/internal"
)

func main() {
	cfg := config.MustLoad("provisioning")

	// Initialize database
	db, err := database.Connect(context.Background(), cfg)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Run migrations
	migFS, err := migrations.Service("provisioning")
	if err != nil {
		slog.Error("failed to load migrations", "error", err)
		os.Exit(1)
	}

	migs, err := database.LoadMigrations(migFS)
	if err != nil {
		slog.Error("failed to parse migrations", "error", err)
		os.Exit(1)
	}

	migrator, err := database.NewMigrator(db, "schema_migrations_provisioning", migs)
	if err != nil {
		slog.Error("failed to create migrator", "error", err)
		os.Exit(1)
	}

	if _, err := migrator.Up(context.Background()); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Initialize stores
	credentials := provisioning.NewCredentialStore(db)
	templates := provisioning.NewTemplateStore(db)
	requests := provisioning.NewRequestStore(db)
	sessions := provisioning.NewSessionStore(db)
	steps := provisioning.NewStepStore(db)
	bootstrapTokens := provisioning.NewBootstrapTokenStore(db)
	events := provisioning.NewEventStore(db)

	// Create org member store adapter
	orgMembers := &orgMemberStoreAdapter{db: db}

	// Create encryptor
	var encryptor provisioning.CredentialEncryptor
	if key := os.Getenv("CREDENTIAL_ENCRYPTION_KEY"); key != "" {
		keyBytes, err := base64.StdEncoding.DecodeString(key)
		if err == nil && len(keyBytes) == 32 {
			encryptor, _ = provisioning.NewAESEncryptor(keyBytes)
		}
	}
	if encryptor == nil {
		encryptor = &provisioning.NoopEncryptor{}
	}

	// Create service
	svc := provisioning.NewService(provisioning.Deps{
		Credentials:     credentials,
		Templates:       templates,
		Requests:        requests,
		Sessions:        sessions,
		Steps:           steps,
		BootstrapTokens: bootstrapTokens,
		Events:          events,
		OrgMembers:      orgMembers,
		Tenant:          db,
		Encryptor:       encryptor,
		Logger:          slog.Default(),
	})

	// Create handler
	handler := provisioning.NewHandler(svc)

	// Create JWT verifier
	verifier := provisioning.NewJWTVerifier(cfg.Auth.JWTSigningKey)

	// Run HTTP server
	if err := httpserver.Run(cfg, func(app *fiber.App) {
		provisioning.RegisterRoutesWithDeps(app, handler, verifier)
	}); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// orgMemberStoreAdapter adapts database queries to authz.OrgMemberStore.
type orgMemberStoreAdapter struct {
	db *database.DB
}

func (a *orgMemberStoreAdapter) GetOrgMember(ctx context.Context, userID string) (*authz.OrgMember, error) {
	const sql = `
		SELECT organization_id, user_id, role, status
		FROM organization_members
		WHERE user_id = $1`

	row := a.db.Conn(ctx).QueryRow(ctx, sql, userID)

	var m authz.OrgMember
	var orgID, uID, role, status string
	if err := row.Scan(&orgID, &uID, &role, &status); err != nil {
		return nil, database.MapError(err)
	}
	m.OrgID = orgID
	m.UserID = uID
	m.Role = authz.OrgRole(role)
	m.Status = status
	return &m, nil
}
