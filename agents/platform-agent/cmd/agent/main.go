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
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/agent"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/config"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/inventory"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/k8s"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/leaderelection"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/metrics"
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

	// Set up leader election if enabled. The elector exposes an IsLeader
	// predicate that gates the reconciler and secrets syncer so only one
	// replica acts at a time. When disabled, isLeaderFn stays nil and the agent
	// behaves exactly as before (every replica reconciles).
	var isLeaderFn func() bool
	var elector *leaderelection.Elector
	if cfg.LeaderElectionEnabled {
		clientset, err := newInClusterClientset()
		if err != nil {
			log.Error("failed to create Kubernetes client for leader election", "error", err)
			os.Exit(1)
		}
		identity := leaderIdentity(cfg, log)
		elector = leaderelection.New(leaderelection.Config{
			LeaseName:      cfg.LeaseName,
			LeaseNamespace: cfg.LeaseNamespace,
			Identity:       identity,
			LeaseDuration:  cfg.LeaseDuration,
			RenewDeadline:  cfg.RenewDeadline,
			RetryPeriod:    cfg.RetryPeriod,
		}, clientset, log)
		isLeaderFn = elector.IsLeader
		a.SetLeaderElector(elector)
		log.Info("leader election enabled",
			"lease", cfg.LeaseName,
			"namespace", cfg.LeaseNamespace,
			"identity", identity,
			"lease_duration", cfg.LeaseDuration.String(),
			"renew_deadline", cfg.RenewDeadline.String(),
			"retry_period", cfg.RetryPeriod.String())
	} else {
		log.Info("leader election disabled")
	}

	// Setup worker factories. These are called AFTER registration completes
	// to ensure valid credentials are available. This fixes the first-boot
	// credential race condition where workers would get empty credentials.
	workerFactory := &agent.WorkerFactory{}

	if cfg.ReconcilerEnabled {
		log.Info("deployment reconciler enabled",
			"interval", cfg.ReconcileInterval,
			"namespace", cfg.Namespace)
		workerFactory.ReconcilerFactory = makeReconcilerFactory(cfg, client, log, isLeaderFn)
	} else {
		log.Info("deployment reconciler disabled")
	}

	if cfg.SecretsSyncerEnabled {
		log.Info("secrets syncer enabled",
			"interval", cfg.SecretsSyncInterval,
			"namespace", cfg.Namespace)
		workerFactory.SecretsSyncerFactory = makeSecretsSyncerFactory(cfg, client, log, isLeaderFn)
	} else {
		log.Info("secrets syncer disabled")
	}

	a.SetWorkerFactory(workerFactory)

	// Setup graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the always-on health server. It serves /healthz (liveness),
	// /readyz (readiness — Ready only after registration succeeds) and /metrics
	// on the port the Kubernetes probes target (default :8080). Without this the
	// probes have nothing to hit and the pod CrashLoopBackOffs.
	if cfg.HealthAddr != "" {
		startHealthServer(ctx, cfg.HealthAddr, a.Ready, log)
	}

	// Optionally start a DEDICATED metrics server on a separate port. This is
	// only needed when METRICS_ADDR points somewhere other than the health
	// server (e.g. a scrape port isolated from probes). Metrics are already
	// served on HealthAddr, so skip when they coincide to avoid a redundant
	// listener.
	if cfg.MetricsAddr != "" && cfg.MetricsAddr != cfg.HealthAddr {
		startMetricsServer(ctx, cfg.MetricsAddr, log)
	}

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
func makeReconcilerFactory(cfg config.Config, client *controlplane.Client, log *slog.Logger, isLeader func() bool) func(creds controlplane.AgentCredentials) (*reconciler.Reconciler, error) {
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
			IsLeader:         isLeader,
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
func makeSecretsSyncerFactory(cfg config.Config, client *controlplane.Client, log *slog.Logger, isLeader func() bool) func(creds controlplane.AgentCredentials) (*secrets.Syncer, error) {
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
			IsLeader:         isLeader,
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

// newInClusterClientset builds a Kubernetes clientset from the in-cluster
// service-account config. Used for leader election (Lease access).
func newInClusterClientset() (kubernetes.Interface, error) {
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(k8sConfig)
}

// leaderIdentity resolves this replica's Lease holder identity. It prefers the
// value already resolved by config (LEASE_IDENTITY / POD_NAME / HOSTNAME) and
// falls back to the OS hostname, then a random ID, so every replica is unique.
func leaderIdentity(cfg config.Config, log *slog.Logger) string {
	if cfg.LeaseIdentity != "" {
		return cfg.LeaseIdentity
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	id := "platform-agent-" + uuid.NewString()
	log.Warn("no stable leader identity (POD_NAME/HOSTNAME) found, using random identity", "identity", id)
	return id
}

// startHealthServer serves liveness (/healthz), readiness (/readyz) and
// Prometheus metrics (/metrics) on addr until ctx is cancelled. Liveness is
// always healthy while the process runs; readiness reflects whether the agent
// has completed registration (via ready()). It runs in the background and
// logs, but never aborts, the agent on failure.
func startHealthServer(ctx context.Context, addr string, ready func() bool, log *slog.Logger) {
	serveHTTP(ctx, addr, "health", healthHandler(ready), log)
}

// healthHandler builds the liveness/readiness/metrics mux. /healthz is always
// 200 while the process runs; /readyz is 200 only once ready() reports true
// (registration established); /metrics serves the Prometheus registry.
func healthHandler(ready func() bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ready != nil && ready() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("registering"))
	})
	mux.Handle("/metrics", metrics.Handler())
	return mux
}

// startMetricsServer serves the Prometheus metrics registry on addr until ctx
// is cancelled. It runs in the background and logs, but never aborts, the agent
// on failure so metrics can never take down reconciliation.
func startMetricsServer(ctx context.Context, addr string, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	serveHTTP(ctx, addr, "metrics", mux, log)
}

// serveHTTP runs an HTTP server on addr until ctx is cancelled, shutting it down
// gracefully. Failures are logged but never abort the agent.
func serveHTTP(ctx context.Context, addr, name string, handler http.Handler, log *slog.Logger) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("starting "+name+" server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error(name+" server stopped with error", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Warn(name+" server shutdown error", "error", err)
		}
	}()
}
