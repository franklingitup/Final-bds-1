// Command server is the entrypoint for the notification service.
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
	notification "github.com/bdsplatform/platform/backend/services/notification/internal"
)

func main() {
	cfg := config.MustLoad("notification")

	// Initialize database
	db, err := database.Connect(context.Background(), cfg)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Run migrations
	migFS, err := migrations.Service("notification")
	if err != nil {
		slog.Error("failed to load migrations", "error", err)
		os.Exit(1)
	}

	migs, err := database.LoadMigrations(migFS)
	if err != nil {
		slog.Error("failed to parse migrations", "error", err)
		os.Exit(1)
	}

	migrator, err := database.NewMigrator(db, "schema_migrations_notification", migs)
	if err != nil {
		slog.Error("failed to create migrator", "error", err)
		os.Exit(1)
	}

	if _, err := migrator.Up(context.Background()); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Initialize stores
	channels := notification.NewChannelStore(db)
	templates := notification.NewTemplateStore(db)
	preferences := notification.NewPreferenceStore(db)
	notifications := notification.NewNotificationStore(db)
	deliveries := notification.NewDeliveryStore(db)
	dlq := notification.NewDLQStore(db)
	webhooks := notification.NewWebhookStore(db)

	// Create org member store adapter
	orgMembers := &orgMemberStoreAdapter{db: db}

	// Create service
	svc := notification.NewService(notification.Deps{
		Channels:      channels,
		Templates:     templates,
		Preferences:   preferences,
		Notifications: notifications,
		Deliveries:    deliveries,
		DLQ:           dlq,
		Webhooks:      webhooks,
		OrgMembers:    orgMembers,
		Tenant:        db,
		Logger:        slog.Default(),
	})

	// Create handler
	handler := notification.NewHandler(svc)

	// Create JWT verifier
	verifier := notification.NewJWTVerifier(cfg.Auth.JWTSigningKey)

	// Start worker
	workerCfg := notification.DefaultWorkerConfig()
	if os.Getenv("NOTIFICATION_WORKER_ENABLED") == "false" {
		workerCfg.Enabled = false
	}
	worker := notification.NewWorker(svc, workerCfg, slog.Default())
	worker.Start(context.Background())
	defer worker.Stop()

	// Consume deployment lifecycle events and turn them into notifications
	// (Deployment -> Notification integration). The delivery worker above then
	// fans each notification out to the org's channels and webhooks.
	subscriber, closeSubscriber, err := newSubscriber(context.Background(), cfg, slog.Default())
	if err != nil {
		slog.Error("failed to init event subscriber", "error", err)
		os.Exit(1)
	}
	defer closeSubscriber()
	if subscriber != nil {
		deploymentConsumer := notification.NewDeploymentConsumer(notification.DeploymentConsumerDeps{
			Dispatcher:    svc,
			Processed:     notification.NewProcessedEventStore(db),
			Tenant:        db,
			Subscriber:    subscriber,
			SubjectPrefix: cfg.NATS.SubjectPrefix,
			Logger:        slog.Default(),
		})
		if err := deploymentConsumer.Start(context.Background()); err != nil {
			slog.Error("failed to start deployment consumer", "error", err)
			os.Exit(1)
		}
		defer deploymentConsumer.Stop()
	}

	// Run HTTP server
	if err := httpserver.Run(cfg, func(app *fiber.App) {
		notification.RegisterRoutesWithDeps(app, handler, verifier)
	}); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// newSubscriber returns an event subscriber and a cleanup function. Deployment
// events cross a service boundary, so they only arrive over NATS/JetStream;
// when NATS is not configured the subscriber is nil and deployment-driven
// notifications are disabled (an in-memory broker would only see events
// published within this process, of which there are none).
func newSubscriber(ctx context.Context, cfg config.Config, log *slog.Logger) (events.Subscriber, func(), error) {
	if cfg.NATS.URL == "" {
		log.Warn("NATS not configured; deployment-driven notifications disabled")
		return nil, func() {}, nil
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
