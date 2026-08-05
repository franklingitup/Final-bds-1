// Command server is the entrypoint for the build service.
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
	build "github.com/bdsplatform/platform/backend/services/build/internal"
	"github.com/bdsplatform/platform/backend/services/build/internal/builder"
	"github.com/bdsplatform/platform/backend/services/build/internal/github"
)

func main() {
	cfg := config.MustLoad("build")
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
	svc := build.NewService(build.Deps{
		Repositories:   build.NewRepositoryStore(db),
		Builds:         build.NewBuildStore(db),
		BuildLogs:      build.NewBuildLogStore(db),
		BuildArtifacts: build.NewBuildArtifactStore(db),
		BuildQueue:     build.NewBuildQueueStore(db),
		OrgMembers:     authz.NewOrgMemberRepo(db),
		Outbox:         outbox,
		Tenant:         db,
		Logger:         log,
	})
	handler := build.NewHandler(svc, build.NewTokenVerifier(cfg.Auth))

	// Initialize GitHub integration with build trigger
	githubSvc, err := github.NewService(github.ServiceConfig{
		ClientID:        os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret:    os.Getenv("GITHUB_CLIENT_SECRET"),
		WebhookBaseURL:  os.Getenv("GITHUB_WEBHOOK_BASE_URL"),
		EncryptionKey:   os.Getenv("GITHUB_ENCRYPTION_KEY"),
		DefaultRegistry: os.Getenv("DEFAULT_REGISTRY"),
	}, github.Deps{
		Connections:       github.NewConnectionStore(db),
		Repositories:      github.NewRepositoryStore(db),
		Webhooks:          github.NewWebhookStore(db),
		WebhookDeliveries: github.NewWebhookDeliveryStore(db),
		OAuthStates:       github.NewOAuthStateStore(db),
		OrgMembers:        authz.NewOrgMemberRepo(db),
		Outbox:            outbox,
		Tenant:            db,
		BuildTrigger:      &buildTriggerAdapter{svc: svc},
		Logger:            log,
	})
	if err != nil {
		log.Error("init github service", "error", err)
		os.Exit(1)
	}
	githubHandler := github.NewHandler(githubSvc, &githubTokenVerifier{inner: build.NewTokenVerifier(cfg.Auth)})

	// Drain the transactional outbox to the broker in the background.
	relay := events.NewRelay(db, outbox, publisher, log, events.RelayOptions{})
	relayCtx, cancelRelay := context.WithCancel(ctx)
	defer cancelRelay()
	go func() {
		if err := relay.Run(relayCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("outbox relay stopped", "error", err)
		}
	}()

	// Start build worker if enabled
	if os.Getenv("BUILD_WORKER_ENABLED") == "true" || os.Getenv("BUILD_WORKER_ENABLED") == "1" {
		workerCfg := build.DefaultWorkerConfig()
		builderCfg := builder.DefaultConfig()

		// Configure registry credentials from environment
		builderCfg.RegistryAuth = loadRegistryCredentials()

		bldr := builder.NewMultiBuilder(builderCfg, svc).
			WithCredentialProvider(githubSvc)

		// Enable the Kubernetes-native Kaniko build engine when configured.
		// Otherwise the builder falls back to the local Docker daemon (dev).
		if os.Getenv("BUILD_KANIKO_ENABLED") == "true" || os.Getenv("BUILD_KANIKO_ENABLED") == "1" {
			cs, kerr := builder.NewKubeClientset()
			if kerr != nil {
				log.Error("init kaniko kubernetes client", "error", kerr)
				os.Exit(1)
			}
			builderCfg.UseDockerDaemon = false
			bldr = builder.NewMultiBuilder(builderCfg, svc).
				WithCredentialProvider(githubSvc).
				WithKubeBackend(builder.NewKubeBackend(cs), loadKanikoConfig())
			log.Info("kaniko build engine enabled")
		}

		worker := build.NewWorker(svc, bldr, workerCfg, log)

		workerCtx, cancelWorker := context.WithCancel(ctx)
		defer cancelWorker()
		go func() {
			if err := worker.Run(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Error("build worker stopped", "error", err)
			}
		}()
	}

	if err := httpserver.Run(cfg, func(app *fiber.App) {
		build.RegisterRoutes(app, handler)
		github.RegisterRoutes(app, githubHandler)
	}); err != nil {
		log.Error("server exited with error", "error", err)
		os.Exit(1)
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
		{"build", "schema_migrations_build"},
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

// githubTokenVerifier adapts build.TokenVerifier to github.TokenVerifier interface.
type githubTokenVerifier struct {
	inner *build.TokenVerifier
}

func (v *githubTokenVerifier) Verify(token string) (github.Identity, error) {
	id, err := v.inner.Verify(token)
	if err != nil {
		return github.Identity{}, err
	}
	return github.Identity{UserID: id.UserID, Email: id.Email}, nil
}

// buildTriggerAdapter adapts build.Service to github.BuildTrigger interface.
type buildTriggerAdapter struct {
	svc *build.Service
}

func (a *buildTriggerAdapter) TriggerBuildFromWebhook(ctx context.Context, orgID string, req github.WebhookBuildRequest) (string, error) {
	return a.svc.TriggerBuildFromWebhook(ctx, orgID, build.WebhookBuildRequest{
		GitURL:         req.GitURL,
		GitRef:         req.GitRef,
		GitCommit:      req.GitCommit,
		TargetImage:    req.TargetImage,
		TargetRegistry: req.TargetRegistry,
		RepositoryName: req.RepositoryName,
	})
}

// loadKanikoConfig builds the Kaniko engine configuration from environment,
// falling back to production-sensible defaults for any unset value.
func loadKanikoConfig() builder.KanikoConfig {
	cfg := builder.DefaultKanikoConfig()
	if v := os.Getenv("BUILD_KANIKO_NAMESPACE"); v != "" {
		cfg.Namespace = v
	}
	if v := os.Getenv("BUILD_KANIKO_IMAGE"); v != "" {
		cfg.Image = v
	}
	if v := os.Getenv("BUILD_KANIKO_SERVICE_ACCOUNT"); v != "" {
		cfg.ServiceAccount = v
	}
	if v := os.Getenv("BUILD_KANIKO_CPU_REQUEST"); v != "" {
		cfg.CPURequest = v
	}
	if v := os.Getenv("BUILD_KANIKO_MEMORY_REQUEST"); v != "" {
		cfg.MemoryRequest = v
	}
	if v := os.Getenv("BUILD_KANIKO_CPU_LIMIT"); v != "" {
		cfg.CPULimit = v
	}
	if v := os.Getenv("BUILD_KANIKO_MEMORY_LIMIT"); v != "" {
		cfg.MemoryLimit = v
	}
	return cfg
}

// loadRegistryCredentials loads container registry credentials from environment.
// Supports multiple registries via REGISTRY_AUTH_<NAME>_USERNAME and REGISTRY_AUTH_<NAME>_PASSWORD.
// Also supports DEFAULT_REGISTRY_USERNAME and DEFAULT_REGISTRY_PASSWORD for the default registry.
func loadRegistryCredentials() map[string]builder.RegistryCredentials {
	creds := make(map[string]builder.RegistryCredentials)

	// Load default registry credentials
	defaultUser := os.Getenv("DEFAULT_REGISTRY_USERNAME")
	defaultPass := os.Getenv("DEFAULT_REGISTRY_PASSWORD")
	defaultReg := os.Getenv("DEFAULT_REGISTRY")

	if defaultUser != "" && defaultPass != "" {
		registry := "docker.io"
		if defaultReg != "" {
			registry = defaultReg
		}
		creds[registry] = builder.RegistryCredentials{
			Username: defaultUser,
			Password: defaultPass,
		}
	}

	// Load additional registries from REGISTRY_AUTH_* environment variables
	// Format: REGISTRY_AUTH_DOCKERHUB_USERNAME, REGISTRY_AUTH_DOCKERHUB_PASSWORD, REGISTRY_AUTH_DOCKERHUB_HOST
	for _, name := range []string{"DOCKERHUB", "GCR", "ECR", "ACR", "GHCR"} {
		user := os.Getenv("REGISTRY_AUTH_" + name + "_USERNAME")
		pass := os.Getenv("REGISTRY_AUTH_" + name + "_PASSWORD")
		host := os.Getenv("REGISTRY_AUTH_" + name + "_HOST")

		if user != "" && pass != "" && host != "" {
			creds[host] = builder.RegistryCredentials{
				Username: user,
				Password: pass,
			}
		}
	}

	return creds
}
