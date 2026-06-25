// Command agent is the platform agent that runs inside customer Kubernetes clusters.
//
// The agent is responsible for:
// - Registering with the control plane using a registration token
// - Sending periodic heartbeats with cluster inventory
// - Reporting cluster health status
// - Reconciling desired deployment state into Kubernetes resources
//
// Configuration is via environment variables:
//   - AGENT_TOKEN: Registration token from the control plane (required)
//   - CONTROL_PLANE_URL: Base URL of the control plane API (required)
//   - AGENT_ID: Override the generated agent ID (optional)
//   - HEARTBEAT_INTERVAL: How often to send heartbeats (default: 30s)
//   - RECONCILE_INTERVAL: How often to reconcile deployments (default: 30s)
//   - STATE_FILE: Path to persist agent state (default: /var/lib/platform-agent/state.json)
//   - RECONCILER_STATE_FILE: Path to persist reconciler state
//   - NAMESPACE: Kubernetes namespace for workloads (default: default)
//   - RECONCILER_ENABLED: Enable/disable reconciler (true/false)
//
// When reconciler is enabled, the agent uses its registered cluster credentials
// (cluster ID and agent ID from state) to authenticate with the dedicated agent
// API endpoints for fetching desired state and reporting deployment status.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/agent"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/config"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/inventory"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/k8s"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/reconciler"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/secrets"
)

func main() {
	// Setup structured logging.
	logLevel := slog.LevelInfo
	if os.Getenv("DEBUG") == "true" {
		logLevel = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(log)

	log.Info("platform agent starting")

	// Load configuration.
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Create control plane client.
	client := controlplane.NewClient(cfg.ControlPlaneURL, cfg.RequestTimeout)

	// Create inventory collector.
	collector, err := inventory.NewCollector()
	if err != nil {
		log.Error("failed to create inventory collector", "error", err)
		os.Exit(1)
	}

	// Create agent.
	a := agent.New(cfg, client, collector, log)

	// Setup worker factories. These are called AFTER registration completes
	// to ensure valid credentials are available. This fixes the first-boot
	// credential race condition where workers would get empty credentials.
	workerFactory := &agent.WorkerFactory{}

	if cfg.ReconcilerEnabled {
		log.Info("deployment reconciler enabled",
			"interval", cfg.ReconcileInterval,
			"namespace", cfg.Namespace)
		workerFactory.ReconcilerFactory = makeReconcilerFactory(cfg, client, log)
	} else {
		log.Info("deployment reconciler disabled")
	}

	if cfg.SecretsSyncerEnabled {
		log.Info("secrets syncer enabled",
			"interval", cfg.SecretsSyncInterval,
			"namespace", cfg.Namespace)
		workerFactory.SecretsSyncerFactory = makeSecretsSyncerFactory(cfg, client, log)
	} else {
		log.Info("secrets syncer disabled")
	}

	a.SetWorkerFactory(workerFactory)

	// Setup graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Info("received shutdown signal", "signal", sig.String())
		cancel()
	}()

	// Run the agent.
	if err := a.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			log.Info("agent shutdown complete")
			return
		}
		log.Error("agent exited with error", "error", err)
		os.Exit(1)
	}
}

// makeReconcilerFactory returns a factory function that creates the reconciler
// with the provided credentials. This is called AFTER registration to ensure
// valid credentials are available.
func makeReconcilerFactory(cfg config.Config, client *controlplane.Client, log *slog.Logger) func(creds controlplane.AgentCredentials) (*reconciler.Reconciler, error) {
	return func(creds controlplane.AgentCredentials) (*reconciler.Reconciler, error) {
		// Create Kubernetes client.
		k8sConfig, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}

		k8sClient, err := kubernetes.NewForConfig(k8sConfig)
		if err != nil {
			return nil, err
		}

		// Create Kubernetes manager.
		manager := k8s.NewManager(k8sClient, cfg.Namespace)

		// Create reconciler with the provided credentials.
		recCfg := reconciler.Config{
			Interval:         cfg.ReconcileInterval,
			StateFile:        cfg.ReconcilerStateFile,
			Namespace:        cfg.Namespace,
			AgentCredentials: creds,
		}

		rec := reconciler.New(
			client,
			manager,
			recCfg,
			log.With("component", "reconciler"),
		)

		return rec, nil
	}
}

// makeSecretsSyncerFactory returns a factory function that creates the secrets
// syncer with the provided credentials. This is called AFTER registration to
// ensure valid credentials are available.
func makeSecretsSyncerFactory(cfg config.Config, client *controlplane.Client, log *slog.Logger) func(creds controlplane.AgentCredentials) (*secrets.Syncer, error) {
	return func(creds controlplane.AgentCredentials) (*secrets.Syncer, error) {
		// Create Kubernetes client.
		k8sConfig, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}

		k8sClient, err := kubernetes.NewForConfig(k8sConfig)
		if err != nil {
			return nil, err
		}

		// Create Kubernetes secret manager.
		manager := secrets.NewK8sSecretManager(k8sClient, cfg.Namespace)

		// Create secrets syncer with the provided credentials.
		syncerCfg := secrets.Config{
			Interval:         cfg.SecretsSyncInterval,
			StateFile:        cfg.SecretsSyncerStateFile,
			Namespace:        cfg.Namespace,
			AgentCredentials: creds,
		}

		syncer := secrets.New(
			client,
			manager,
			syncerCfg,
			log.With("component", "secrets-syncer"),
		)

		return syncer, nil
	}
}
