// Command server is the entrypoint for the provisioning service.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	"github.com/bdsplatform/platform/backend/libs/internalapi"
	"github.com/bdsplatform/platform/backend/migrations"
	provisioning "github.com/bdsplatform/platform/backend/services/provisioning/internal"
)

// encryptorResult holds the result of initEncryptor for testability.
type encryptorResult struct {
	encryptor provisioning.CredentialEncryptor
	warning   string // non-empty if a warning should be logged
}

// initEncryptor validates CREDENTIAL_ENCRYPTION_KEY and returns an encryptor.
// Returns an error if the service should refuse to start.
func initEncryptor(key string, env config.Environment) (*encryptorResult, error) {
	if key != "" {
		keyBytes, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY is not valid base64: %w", err)
		}
		if len(keyBytes) != 32 {
			return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be exactly 32 bytes (AES-256), got %d", len(keyBytes))
		}
		enc, err := provisioning.NewAESEncryptor(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize credential encryptor: %w", err)
		}
		return &encryptorResult{encryptor: enc}, nil
	}

	// Key is unset
	if env == config.EnvProduction || env == config.EnvStaging {
		return nil, errors.New("CREDENTIAL_ENCRYPTION_KEY environment variable is required outside local development")
	}

	// Development: warn and proceed with NoopEncryptor
	return &encryptorResult{
		encryptor: &provisioning.NoopEncryptor{},
		warning:   "CREDENTIAL_ENCRYPTION_KEY is not set; cloud credentials will be stored UNENCRYPTED — only acceptable for local development",
	}, nil
}

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

	// Initialize credential encryption. Staging and production must fail closed;
	// local development may run without a key for convenience.
	encResult, err := initEncryptor(os.Getenv("CREDENTIAL_ENCRYPTION_KEY"), cfg.Environment)
	if err != nil {
		slog.Error("credential encryption initialization failed", "error", err)
		os.Exit(1)
	}
	if encResult.warning != "" {
		slog.Warn(encResult.warning)
	}
	encryptor := encResult.encryptor

	// Create cluster client for internal API calls
	clusterURL := os.Getenv("CLUSTER_SERVICE_URL")
	if clusterURL == "" {
		clusterURL = "http://localhost:8085"
	}
	clusterClient := internalapi.NewClusterClient(clusterURL)
	clusterClient.InternalToken = os.Getenv("INTERNAL_API_TOKEN")
	slog.Info("cluster client configured", "url", clusterURL)

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
		ClusterClient:   &clusterClientAdapter{client: clusterClient},
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

// clusterClientAdapter adapts internalapi.ClusterClient to provisioning.ClusterCreator.
type clusterClientAdapter struct {
	client *internalapi.ClusterClient
}

func (a *clusterClientAdapter) CreateClusterWithToken(ctx context.Context, orgID string, req provisioning.ClusterCreateRequest) (*provisioning.ClusterCreateResponse, error) {
	if a.client == nil {
		return nil, fmt.Errorf("cluster client not configured")
	}

	apiReq := internalapi.CreateClusterRequest{
		Name:          req.Name,
		Slug:          req.Slug,
		Description:   req.Description,
		CloudProvider: req.CloudProvider,
		Region:        req.Region,
		Labels:        req.Labels,
		CreatedBy:     req.CreatedBy,
	}

	resp, err := a.client.CreateClusterWithToken(ctx, orgID, apiReq)
	if err != nil {
		return nil, err
	}

	return &provisioning.ClusterCreateResponse{
		ID:                resp.ID,
		Name:              resp.Name,
		Slug:              resp.Slug,
		Status:            resp.Status,
		RegistrationToken: resp.RegistrationToken,
		ManifestURL:       resp.ManifestURL,
		InstallCommand:    resp.InstallCommand,
		ExpiresAt:         resp.ExpiresAt,
	}, nil
}

func (a *clusterClientAdapter) GetCluster(ctx context.Context, orgID, clusterID string) (*provisioning.ClusterStatus, error) {
	if a.client == nil {
		return nil, fmt.Errorf("cluster client not configured")
	}
	cluster, err := a.client.GetCluster(ctx, orgID, clusterID)
	if err != nil {
		return nil, err
	}
	return &provisioning.ClusterStatus{
		ID:                cluster.ID,
		Status:            cluster.Status,
		AgentID:           cluster.AgentID,
		KubernetesVersion: cluster.KubernetesVersion,
	}, nil
}
