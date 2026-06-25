// Package agent implements the platform agent runtime.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/config"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/inventory"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/reconciler"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/secrets"
)

// WorkerFactory creates workers that need agent credentials.
// These factories are called AFTER registration completes to ensure
// the credentials are available.
type WorkerFactory struct {
	// ReconcilerFactory creates the deployment reconciler.
	// Called with agent credentials after registration.
	ReconcilerFactory func(creds controlplane.AgentCredentials) (*reconciler.Reconciler, error)
	// SecretsSyncerFactory creates the secrets syncer.
	// Called with agent credentials after registration.
	SecretsSyncerFactory func(creds controlplane.AgentCredentials) (*secrets.Syncer, error)
}

// Agent is the platform agent that runs inside customer clusters.
type Agent struct {
	cfg           config.Config
	client        *controlplane.Client
	collector     InventoryCollector
	workerFactory *WorkerFactory
	reconciler    *reconciler.Reconciler
	secretsSyncer *secrets.Syncer
	state         *State
	log           *slog.Logger
}

// InventoryCollector collects cluster inventory information.
type InventoryCollector interface {
	Collect(ctx context.Context) (*inventory.Info, error)
	CheckHealth(ctx context.Context) bool
}

// New creates a new agent instance.
func New(cfg config.Config, client *controlplane.Client, collector InventoryCollector, log *slog.Logger) *Agent {
	return &Agent{
		cfg:       cfg,
		client:    client,
		collector: collector,
		state:     &State{},
		log:       log,
	}
}

// SetWorkerFactory sets the factory for creating workers after registration.
// This ensures workers are created with valid credentials.
func (a *Agent) SetWorkerFactory(f *WorkerFactory) {
	a.workerFactory = f
}

// SetReconciler sets the deployment reconciler (deprecated, use SetWorkerFactory).
// This is kept for backward compatibility but may use stale credentials on first boot.
func (a *Agent) SetReconciler(r *reconciler.Reconciler) {
	a.reconciler = r
}

// SetSecretsSyncer sets the secrets syncer (deprecated, use SetWorkerFactory).
// This is kept for backward compatibility but may use stale credentials on first boot.
func (a *Agent) SetSecretsSyncer(s *secrets.Syncer) {
	a.secretsSyncer = s
}

// Run starts the agent and blocks until the context is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	// Load persisted state.
	state, err := LoadState(a.cfg.StateFile)
	if err != nil {
		a.log.Warn("failed to load state, starting fresh", "error", err)
		state = &State{}
	}
	a.state = state

	// Generate agent ID if not set.
	if a.cfg.AgentID != "" {
		a.state.AgentID = a.cfg.AgentID
	} else if a.state.AgentID == "" {
		a.state.AgentID = "agent-" + uuid.NewString()[:8]
		a.log.Info("generated agent ID", "agent_id", a.state.AgentID)
	}

	// Register if not already registered.
	if !a.state.Registered {
		if err := a.register(ctx); err != nil {
			return fmt.Errorf("registration failed: %w", err)
		}
	} else {
		a.log.Info("already registered",
			"agent_id", a.state.AgentID,
			"cluster_id", a.state.ClusterID,
			"org_id", a.state.OrganizationID)
	}

	// IMPORTANT: Create workers AFTER registration to ensure valid credentials.
	// This fixes the first-boot credential race condition.
	if err := a.initializeWorkers(); err != nil {
		return fmt.Errorf("failed to initialize workers: %w", err)
	}

	// Start reconciler in background if enabled.
	if a.reconciler != nil && a.cfg.ReconcilerEnabled {
		go func() {
			a.log.Info("starting deployment reconciler")
			if err := a.reconciler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				a.log.Error("reconciler stopped with error", "error", err)
			}
		}()
	}

	// Start secrets syncer in background if enabled.
	if a.secretsSyncer != nil && a.cfg.SecretsSyncerEnabled {
		go func() {
			a.log.Info("starting secrets syncer")
			if err := a.secretsSyncer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				a.log.Error("secrets syncer stopped with error", "error", err)
			}
		}()
	}

	// Start heartbeat loop.
	return a.heartbeatLoop(ctx)
}

// initializeWorkers creates workers using the factory with valid credentials.
// This should be called AFTER registration completes.
func (a *Agent) initializeWorkers() error {
	if a.workerFactory == nil {
		// No factory provided, workers were set via deprecated SetReconciler/SetSecretsSyncer
		return nil
	}

	creds := controlplane.AgentCredentials{
		ClusterID: a.state.ClusterID,
		AgentID:   a.state.AgentID,
	}

	// Create reconciler if factory provided and enabled.
	if a.workerFactory.ReconcilerFactory != nil && a.cfg.ReconcilerEnabled {
		rec, err := a.workerFactory.ReconcilerFactory(creds)
		if err != nil {
			return fmt.Errorf("create reconciler: %w", err)
		}
		a.reconciler = rec
		a.log.Info("created reconciler with credentials",
			"cluster_id", creds.ClusterID,
			"agent_id", creds.AgentID)
	}

	// Create secrets syncer if factory provided and enabled.
	if a.workerFactory.SecretsSyncerFactory != nil && a.cfg.SecretsSyncerEnabled {
		syncer, err := a.workerFactory.SecretsSyncerFactory(creds)
		if err != nil {
			return fmt.Errorf("create secrets syncer: %w", err)
		}
		a.secretsSyncer = syncer
		a.log.Info("created secrets syncer with credentials",
			"cluster_id", creds.ClusterID,
			"agent_id", creds.AgentID)
	}

	return nil
}

// register registers the agent with the control plane.
func (a *Agent) register(ctx context.Context) error {
	a.log.Info("starting registration", "agent_id", a.state.AgentID)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Collect inventory for registration.
		info, err := a.collector.Collect(ctx)
		if err != nil {
			a.log.Warn("failed to collect inventory for registration", "error", err)
			time.Sleep(a.cfg.RegistrationRetryInterval)
			continue
		}

		// Attempt registration.
		resp, err := a.client.Register(ctx, controlplane.RegisterRequest{
			Token:             a.cfg.Token,
			AgentID:           a.state.AgentID,
			KubernetesVersion: info.KubernetesVersion,
			NodeCount:         info.NodeCount,
			CloudProvider:     info.CloudProvider,
			Region:            info.Region,
		})

		if err != nil {
			var apiErr *controlplane.APIError
			if errors.As(err, &apiErr) {
				if apiErr.IsUnauthorized() {
					return fmt.Errorf("registration token is invalid or expired")
				}
				if apiErr.IsConflict() {
					return fmt.Errorf("registration token has already been used")
				}
			}
			a.log.Warn("registration failed, retrying", "error", err)
			time.Sleep(a.cfg.RegistrationRetryInterval)
			continue
		}

		// Registration successful.
		a.state.ClusterID = resp.ID
		a.state.OrganizationID = resp.OrganizationID
		a.state.Registered = true

		// Persist state.
		if err := SaveState(a.cfg.StateFile, a.state); err != nil {
			a.log.Warn("failed to save state", "error", err)
		}

		a.log.Info("registration successful",
			"cluster_id", resp.ID,
			"cluster_name", resp.Name,
			"org_id", resp.OrganizationID,
			"status", resp.Status)

		return nil
	}
}

// heartbeatLoop sends periodic heartbeats to the control plane.
func (a *Agent) heartbeatLoop(ctx context.Context) error {
	ticker := time.NewTicker(a.cfg.HeartbeatInterval)
	defer ticker.Stop()

	// Send initial heartbeat immediately.
	a.sendHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			a.log.Info("heartbeat loop stopped")
			return ctx.Err()
		case <-ticker.C:
			a.sendHeartbeat(ctx)
		}
	}
}

// sendHeartbeat sends a single heartbeat to the control plane.
func (a *Agent) sendHeartbeat(ctx context.Context) {
	// Collect current inventory.
	info, err := a.collector.Collect(ctx)
	if err != nil {
		a.log.Warn("failed to collect inventory for heartbeat", "error", err)
		// Still send heartbeat with reduced info.
		info = &inventory.Info{
			APIServerHealthy: a.collector.CheckHealth(ctx),
		}
	}

	// Use credential-based authentication for heartbeats.
	creds := controlplane.AgentCredentials{
		ClusterID: a.state.ClusterID,
		AgentID:   a.state.AgentID,
	}

	err = a.client.HeartbeatWithCreds(ctx, creds, controlplane.HeartbeatRequest{
		AgentID:           a.state.AgentID,
		KubernetesVersion: info.KubernetesVersion,
		NodeCount:         info.NodeCount,
		APIServerHealthy:  info.APIServerHealthy,
	})

	if err != nil {
		var apiErr *controlplane.APIError
		if errors.As(err, &apiErr) {
			if apiErr.IsForbidden() {
				a.log.Error("heartbeat rejected: agent ID mismatch", "error", err)
				return
			}
		}
		a.log.Warn("heartbeat failed", "error", err)
		return
	}

	a.log.Debug("heartbeat sent",
		"k8s_version", info.KubernetesVersion,
		"node_count", info.NodeCount,
		"api_server_healthy", info.APIServerHealthy)
}

// State returns the current agent state.
func (a *Agent) State() *State {
	return a.state
}
