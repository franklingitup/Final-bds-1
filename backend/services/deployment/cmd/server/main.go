// Command server is the entrypoint for the deployment service.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/argocd"
	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	"github.com/bdsplatform/platform/backend/libs/logger"
	"github.com/bdsplatform/platform/backend/migrations"
	deployment "github.com/bdsplatform/platform/backend/services/deployment/internal"
	"github.com/bdsplatform/platform/backend/services/deployment/internal/pipeline"
)

func main() {
	cfg := config.MustLoad("deployment")
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

	publisher, subscriber, closeEvents, err := newEvents(ctx, cfg, log)
	if err != nil {
		log.Error("init events", "error", err)
		os.Exit(1)
	}
	defer closeEvents()

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := deployment.NewService(deployment.Deps{
		Applications: deployment.NewApplicationStore(db),
		Deployments:  deployment.NewDeploymentStore(db),
		Releases:     deployment.NewReleaseStore(db),
		OrgMembers:   authz.NewOrgMemberRepo(db), // For org membership authorization
		Outbox:       outbox,
		Tenant:       db,
		Logger:       log,
	})
	handler := deployment.NewHandler(svc, deployment.NewTokenVerifier(cfg.Auth))

	// Agent-specific stores and handlers.
	desiredStateStore := deployment.NewDesiredStateStore(db)
	agentHandler := deployment.NewAgentHandler(deployment.AgentHandlerDeps{
		DesiredState: desiredStateStore,
		Tenant:       db,
		Outbox:       outbox,
		Rollouts:     deployment.NewRolloutStatusStore(db),
		// Automatic rollback on rollout failure is opt-in to preserve existing
		// behaviour; enable with DEPLOYMENT_AUTO_ROLLBACK_ENABLED=true.
		AutoRollback: autoRollbackEnabled(),
		Logger:       log,
	})
	clusterValidator := deployment.NewClusterValidator(db.Pool)
	agentAuth := deployment.AgentAuthMiddleware(clusterValidator)

	// Pipeline service for deployment orchestration.
	pipelineAdapter := &pipelineServiceAdapter{
		apps:     deployment.NewApplicationStore(db),
		deps:     deployment.NewDeploymentStore(db),
		releases: deployment.NewReleaseStore(db),
	}
	pipelineSvc := pipeline.NewService(pipeline.Deps{
		DesiredStates:     pipeline.NewDesiredStateStore(db),
		PipelineRuns:      pipeline.NewPipelineRunStore(db),
		PipelineEvents:    pipeline.NewPipelineEventStore(db),
		DeploymentMetrics: pipeline.NewDeploymentMetricsStore(db),
		Deployments:       pipelineAdapter,
		Releases:          pipelineAdapter,
		OrgMembers:        authz.NewOrgMemberRepo(db),
		Outbox:            outbox,
		Tenant:            db,
		Logger:            log,
	})
	pipelineHandler := pipeline.NewHandler(pipelineSvc)

	// Drain the transactional outbox to the broker in the background.
	relay := events.NewRelay(db, outbox, publisher, log, events.RelayOptions{})
	relayCtx, cancelRelay := context.WithCancel(ctx)
	defer cancelRelay()
	go func() {
		if err := relay.Run(relayCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("outbox relay stopped", "error", err)
		}
	}()

	// Consume build.succeeded events and roll the freshly built image out to the
	// deployments that run it (Build -> Deployment integration).
	buildConsumer := deployment.NewBuildConsumer(deployment.BuildConsumerDeps{
		Deployments:   deployment.NewDeploymentStore(db),
		Releases:      deployment.NewReleaseStore(db),
		Processed:     deployment.NewProcessedEventStore(db),
		Outbox:        outbox,
		Tenant:        db,
		Subscriber:    subscriber,
		SubjectPrefix: cfg.NATS.SubjectPrefix,
		Logger:        log,
	})
	if err := buildConsumer.Start(ctx); err != nil {
		log.Error("start build consumer", "error", err)
		os.Exit(1)
	}
	defer buildConsumer.Stop()

	// GitOps (Argo CD) deployment engine. Additive and opt-in: enabled only when
	// ARGOCD_ENABLED=true and an Argo CD server URL is configured. When disabled
	// the deployment service behaves exactly as before.
	var argoHandler *deployment.ArgoHandler
	if argoCfg, ok := argoConfig(); ok {
		argoClient, err := argocd.New(argoCfg)
		if err != nil {
			log.Error("init argocd client", "error", err)
			os.Exit(1)
		}
		argoSvc := deployment.NewArgoService(deployment.ArgoServiceDeps{
			Client:       argoClient,
			ArgoApps:     deployment.NewArgoApplicationStore(db),
			Applications: deployment.NewApplicationStore(db),
			Deployments:  deployment.NewDeploymentStore(db),
			Releases:     deployment.NewReleaseStore(db),
			Rollouts:     deployment.NewRolloutStatusStore(db),
			OrgMembers:   authz.NewOrgMemberRepo(db),
			Outbox:       outbox,
			Tenant:       db,
			Logger:       log,
		})
		argoHandler = deployment.NewArgoHandler(argoSvc)
		// Enable inline GitOps registration from CreateDeployment (opt-in per
		// request via the request's gitops source).
		svc.SetArgoRegistrar(argoSvc)

		argoMonitor := deployment.NewArgoMonitor(deployment.ArgoMonitorDeps{
			Client:   argoClient,
			ArgoApps: deployment.NewArgoApplicationStore(db),
			Releases: deployment.NewReleaseStore(db),
			Rollouts: deployment.NewRolloutStatusStore(db),
			Outbox:   outbox,
			Tenant:   db,
			Logger:   log,
			Interval: argoMonitorInterval(),
		})
		argoMonitor.Start(ctx)
		defer argoMonitor.Stop()
		log.Info("gitops (argo cd) deployment engine enabled", "server", argoCfg.BaseURL)
	}

	if err := httpserver.Run(cfg, func(app *fiber.App) {
		deployment.RegisterRoutes(app, handler)
		deployment.RegisterAgentRoutes(app, agentHandler, agentAuth, deployment.NewReleaseStore(db), deployment.NewDeploymentStore(db))
		pipeline.RegisterRoutes(app, pipelineHandler, handler.RequireAuth())
		if argoHandler != nil {
			deployment.RegisterArgoRoutes(app, argoHandler, handler.RequireAuth())
		}
	}); err != nil {
		log.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// argoConfig resolves the Argo CD client configuration from the environment.
// It returns ok=false when GitOps is disabled or no server URL is set, so the
// deployment service starts unchanged in non-GitOps environments.
func argoConfig() (argocd.Config, bool) {
	switch os.Getenv("ARGOCD_ENABLED") {
	case "true", "1":
	default:
		return argocd.Config{}, false
	}
	serverURL := os.Getenv("ARGOCD_SERVER_URL")
	if serverURL == "" {
		return argocd.Config{}, false
	}
	insecure := false
	switch os.Getenv("ARGOCD_INSECURE") {
	case "true", "1":
		insecure = true
	}
	return argocd.Config{
		BaseURL:   serverURL,
		AuthToken: os.Getenv("ARGOCD_AUTH_TOKEN"),
		Insecure:  insecure,
	}, true
}

// argoMonitorInterval reads the GitOps monitor poll interval from the
// environment, defaulting to 15s.
func argoMonitorInterval() time.Duration {
	if v := os.Getenv("ARGOCD_MONITOR_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 15 * time.Second
}

// autoRollbackEnabled reports whether the deployment engine should trigger an
// automatic rollback when a rollout fails. Off by default for backward
// compatibility; enabled via DEPLOYMENT_AUTO_ROLLBACK_ENABLED=true|1.
func autoRollbackEnabled() bool {
	switch os.Getenv("DEPLOYMENT_AUTO_ROLLBACK_ENABLED") {
	case "true", "1":
		return true
	default:
		return false
	}
}

// runMigrations applies required schemas.
func runMigrations(ctx context.Context, db *database.DB) error {
	for _, m := range []struct {
		service string
		table   string
	}{
		{"tenant", "schema_migrations_tenant"},
		{"project", "schema_migrations_project"},
		{"cluster", "schema_migrations_cluster"},
		{"deployment", "schema_migrations_deployment"},
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

// newEvents returns an event publisher, a subscriber, and a cleanup function.
// When NATS is configured the same JetStream client backs both; otherwise an
// in-memory broker is used (which delivers only within this process).
func newEvents(ctx context.Context, cfg config.Config, log *slog.Logger) (events.Publisher, events.Subscriber, func(), error) {
	if cfg.NATS.URL == "" {
		log.Warn("NATS not configured; using in-memory event broker")
		broker := events.NewMemoryBroker(cfg.NATS.SubjectPrefix)
		return broker, broker, func() {}, nil
	}
	client, err := events.Connect(ctx, cfg.NATS, log)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := client.EnsureStreams(ctx); err != nil {
		client.Close()
		return nil, nil, nil, err
	}
	return client.NewPublisher(), client, client.Close, nil
}

// pipelineServiceAdapter adapts deployment stores for the pipeline service.
type pipelineServiceAdapter struct {
	apps     deployment.ApplicationStore
	deps     deployment.DeploymentStore
	releases deployment.ReleaseStore
}

func (a *pipelineServiceAdapter) GetDeployment(ctx context.Context, id string) (*pipeline.DeploymentInfo, error) {
	d, err := a.deps.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &pipeline.DeploymentInfo{
		ID:            d.ID,
		OrgID:         d.OrgID,
		ApplicationID: d.ApplicationID,
		ClusterID:     d.ClusterID,
		Image:         d.Image,
		Replicas:      d.Replicas,
		Port:          d.Port,
		CPURequest:    d.CPURequest,
		CPULimit:      d.CPULimit,
		MemoryRequest: d.MemoryRequest,
		MemoryLimit:   d.MemoryLimit,
		EnvVars:       d.EnvVars,
	}, nil
}

func (a *pipelineServiceAdapter) GetApplication(ctx context.Context, id string) (*pipeline.ApplicationInfo, error) {
	app, err := a.apps.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &pipeline.ApplicationInfo{
		ID:        app.ID,
		OrgID:     app.OrgID,
		ProjectID: app.ProjectID,
		Name:      app.Name,
		Slug:      app.Slug,
	}, nil
}

func (a *pipelineServiceAdapter) CreateRelease(ctx context.Context, deploymentID, image string, replicas int, config json.RawMessage, userID string) (string, int, error) {
	latest, _ := a.releases.GetLatest(ctx, deploymentID)
	nextRevision := 1
	if latest != nil {
		nextRevision = latest.Revision + 1
	}

	d, err := a.deps.GetByID(ctx, deploymentID)
	if err != nil {
		return "", 0, err
	}

	r := &deployment.Release{
		OrgID:        d.OrgID,
		DeploymentID: deploymentID,
		Revision:     nextRevision,
		Image:        image,
		Replicas:     replicas,
		ConfigHash:   hashConfig(config),
		Config:       config,
		Status:       "pending",
		CreatedBy:    &userID,
	}

	if err := a.releases.Create(ctx, r); err != nil {
		return "", 0, err
	}

	return r.ID, r.Revision, nil
}

func (a *pipelineServiceAdapter) MarkReleaseDeploying(ctx context.Context, releaseID string) error {
	return a.releases.MarkStarted(ctx, releaseID)
}

func (a *pipelineServiceAdapter) MarkReleaseSucceeded(ctx context.Context, releaseID string) error {
	return a.releases.MarkFinished(ctx, releaseID, "succeeded", nil)
}

func (a *pipelineServiceAdapter) MarkReleaseFailed(ctx context.Context, releaseID string, errMsg string) error {
	return a.releases.MarkFinished(ctx, releaseID, "failed", &errMsg)
}

func hashConfig(config json.RawMessage) string {
	if len(config) == 0 {
		return ""
	}
	h := uint64(0)
	for _, b := range config {
		h = h*31 + uint64(b)
	}
	return string(rune(h % 1000000))
}
