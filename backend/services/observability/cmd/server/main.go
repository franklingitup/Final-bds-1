// Command server is the entrypoint for the observability service.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	"github.com/bdsplatform/platform/backend/migrations"
	observability "github.com/bdsplatform/platform/backend/services/observability/internal"
)

func main() {
	cfg := config.MustLoad("observability")
	log := slog.Default()

	// Connect to database
	db, err := database.Connect(context.Background(), cfg)
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Run migrations
	migrationsFS, err := migrations.Service("observability")
	if err != nil {
		log.Error("failed to load migrations", "error", err)
		os.Exit(1)
	}
	migs, err := database.LoadMigrations(migrationsFS)
	if err != nil {
		log.Error("failed to parse migrations", "error", err)
		os.Exit(1)
	}
	migrator, err := database.NewMigrator(db, "schema_migrations_observability", migs)
	if err != nil {
		log.Error("failed to create migrator", "error", err)
		os.Exit(1)
	}
	if _, err := migrator.Up(context.Background()); err != nil {
		log.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Initialize Prometheus client (optional)
	var prometheusClient *observability.PrometheusClient
	if promURL := os.Getenv("PROMETHEUS_URL"); promURL != "" {
		prometheusClient = observability.NewPrometheusClient(observability.PrometheusConfig{
			URL:     promURL,
			Timeout: 30 * time.Second,
		})
		log.Info("prometheus client configured", "url", promURL)
	}

	// Initialize Loki client (optional)
	var lokiClient *observability.LokiClient
	if lokiURL := os.Getenv("LOKI_URL"); lokiURL != "" {
		lokiClient = observability.NewLokiClient(observability.LokiConfig{
			URL:     lokiURL,
			Timeout: 30 * time.Second,
		})
		log.Info("loki client configured", "url", lokiURL)
	}

	// Initialize stores
	dashboardStore := observability.NewDashboardStore(db)
	healthCheckStore := observability.NewHealthCheckStore(db)
	eventStore := observability.NewEventStore(db)
	alertRuleStore := observability.NewAlertRuleStore(db)
	alertHistoryStore := observability.NewAlertHistoryStore(db)
	logStreamStore := observability.NewLogStreamStore(db)
	metricSampleStore := observability.NewMetricSampleStore(db)
	orgMemberStore := authz.NewOrgMemberRepo(db)

	// Create service
	svc := observability.NewService(observability.Deps{
		Dashboards:    dashboardStore,
		HealthChecks:  healthCheckStore,
		Events:        eventStore,
		AlertRules:    alertRuleStore,
		AlertHistory:  alertHistoryStore,
		LogStreams:    logStreamStore,
		MetricSamples: metricSampleStore,
		OrgMembers:    orgMemberStore,
		Tenant:        db,
		Prometheus:    prometheusClient,
		Loki:          lokiClient,
		Logger:        log,
	})

	// Create handler
	handler := observability.NewHandler(svc, log)

	// Create token verifier
	verifier := observability.NewTokenVerifier(cfg.Auth)

	// Register routes
	routeRegistrar := func(app *fiber.App) {
		observability.RegisterRoutes(app, handler, observability.RequireAuth(verifier))
	}

	if err := httpserver.Run(cfg, routeRegistrar); err != nil {
		log.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
