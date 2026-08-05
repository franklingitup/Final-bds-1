// Command server is the entrypoint for the domain service.
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
	"github.com/bdsplatform/platform/backend/migrations"
	domain "github.com/bdsplatform/platform/backend/services/domain/internal"
)

func main() {
	cfg := config.MustLoad("domain")
	log := slog.Default()

	// Connect to database
	db, err := database.Connect(context.Background(), cfg)
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Run migrations
	migrationsFS, err := migrations.Service("domain")
	if err != nil {
		log.Error("failed to load migrations", "error", err)
		os.Exit(1)
	}
	migs, err := database.LoadMigrations(migrationsFS)
	if err != nil {
		log.Error("failed to parse migrations", "error", err)
		os.Exit(1)
	}
	migrator, err := database.NewMigrator(db, "schema_migrations_domain", migs)
	if err != nil {
		log.Error("failed to create migrator", "error", err)
		os.Exit(1)
	}
	if _, err := migrator.Up(context.Background()); err != nil {
		log.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Initialize event outbox
	outbox := events.NewPostgresOutbox(db, "")

	// Initialize stores
	domainStore := domain.NewDomainStore(db)
	certStore := domain.NewCertificateStore(db)
	challengeStore := domain.NewACMEChallengeStore(db)
	ingressStore := domain.NewIngressStore(db)
	eventStore := domain.NewDomainEventStore(db)
	orgMemberStore := authz.NewOrgMemberRepo(db)

	// Initialize encryptor (use empty key for dev - in production, use a proper key)
	encryptor := domain.NewCertificateEncryptor(nil)

	// Create deployment reader adapter
	deploymentReader := &deploymentReaderAdapter{db: db}

	// Create service
	svc := domain.NewService(domain.Deps{
		Domains:      domainStore,
		Certificates: certStore,
		Challenges:   challengeStore,
		Ingresses:    ingressStore,
		Events:       eventStore,
		Deployments:  deploymentReader,
		OrgMembers:   orgMemberStore,
		Outbox:       outbox,
		Tenant:       db,
		Encryptor:    encryptor,
		Logger:       log,
	})

	// Create handler
	handler := domain.NewHandler(svc, log)

	// Create token verifier
	verifier := domain.NewTokenVerifier(cfg.Auth)

	// Register routes
	routeRegistrar := func(app *fiber.App) {
		domain.RegisterRoutes(app, handler, domain.RequireAuth(verifier))
	}

	if err := httpserver.Run(cfg, routeRegistrar); err != nil {
		log.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// deploymentReaderAdapter adapts database queries to DeploymentReader interface.
type deploymentReaderAdapter struct {
	db *database.DB
}

func (a *deploymentReaderAdapter) GetDeployment(ctx context.Context, id string) (*domain.DeploymentInfo, error) {
	const sql = `
		SELECT d.id, d.org_id, d.cluster_id, a.name, 
		       COALESCE((d.config->>'port')::int, 80) as port,
		       COALESCE(d.config->>'namespace', 'default') as namespace
		FROM deployments d
		JOIN applications a ON d.application_id = a.id
		WHERE d.id = $1`

	row := a.db.Conn(ctx).QueryRow(ctx, sql, id)

	var info domain.DeploymentInfo
	err := row.Scan(&info.ID, &info.OrgID, &info.ClusterID, &info.ServiceName, &info.ServicePort, &info.Namespace)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &info, nil
}
