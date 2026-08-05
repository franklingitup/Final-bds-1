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
	"github.com/bdsplatform/platform/agents/platform-agent/internal/metrics"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/reconciler"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/secrets"
)

// fatalConfigError marks an unrecoverable configuration problem (for example an
// installation token that is invalid and cannot recover an existing cluster).
// It is the ONLY error the agent lets terminate the process: every other
// registration failure is retried with exponential backoff so the pod never
// enters CrashLoopBackOff.
type fatalConfigError struct{ msg string }

func (e *fatalConfigError) Error() string { return e.msg }

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

// LeaderElector participates in leader election and blocks until its context is
// cancelled. It is satisfied by *leaderelection.Elector. The agent depends only
// on this narrow interface so it can be tested without a Kubernetes API server.
type LeaderElector interface {
	Run(ctx context.Context)
}

// Agent is the platform agent that runs inside customer clusters.
type Agent struct {
	cfg           config.Config
	client        *controlplane.Client
	collector     InventoryCollector
	workerFactory *WorkerFactory
	reconciler    *reconciler.Reconciler
	secretsSyncer *secrets.Syncer
	leaderElector LeaderElector
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

// SetLeaderElector installs the leader elector. When set, the agent runs it
// alongside the heartbeat loop; the elector controls whether the reconciler and
// secrets syncer actually act (via their IsLeader gate). When nil (leader
// election disabled) the agent behaves exactly as before.
func (a *Agent) SetLeaderElector(le LeaderElector) {
	a.leaderElector = le
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
	// Load persisted state. A missing file is normal (first boot); a corrupt
	// file is tolerated by starting fresh and rebuilding from the control plane.
	metrics.StateLoad.Inc()
	state, err := LoadState(a.cfg.StateFile)
	if err != nil {
		metrics.StateLoadFailure.Inc()
		a.log.Warn("failed to load state, starting fresh", "error", err)
		state = &State{}
	}
	a.state = state

	// Resolve a stable AgentID before touching the control plane.
	a.resolveAgentID()

	// Ensure the agent is registered. This never returns a transient error: it
	// retries registration/recovery with exponential backoff until it succeeds
	// or the context is cancelled. It returns only on cancellation or an
	// unrecoverable configuration error (*fatalConfigError), so a duplicate
	// registration or a temporarily unavailable control plane can never crash
	// the pod.
	if err := a.ensureRegistered(ctx); err != nil {
		return err
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

	// Start leader election in the background if configured. The elector only
	// flips the leadership flag consulted by the reconciler/syncer gate; the
	// heartbeat loop below always runs so followers stay warm. On shutdown the
	// elector releases the lease (ReleaseOnCancel) before Run returns.
	var electorDone chan struct{}
	if a.leaderElector != nil {
		electorDone = make(chan struct{})
		go func() {
			defer close(electorDone)
			a.leaderElector.Run(ctx)
		}()
	}

	// Start heartbeat loop. Blocks until the context is cancelled (shutdown).
	err = a.heartbeatLoop(ctx)

	// Graceful shutdown: allow the leader to release its lease so a follower can
	// take over promptly, then flush reconciler status to disk.
	a.shutdown(electorDone)

	return err
}

// shutdown performs best-effort cleanup after the heartbeat loop exits: it waits
// for the leader elector to release the lease and flushes reconciler state.
func (a *Agent) shutdown(electorDone <-chan struct{}) {
	if electorDone != nil {
		// Wait for the elector to finish releasing the lease, bounded by the
		// lease duration so shutdown can never hang.
		timeout := a.cfg.LeaseDuration + 2*time.Second
		if timeout <= 0 {
			timeout = 17 * time.Second
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-electorDone:
			a.log.Info("leader election stopped")
		case <-timer.C:
			a.log.Warn("timed out waiting for leader election to release lease")
		}
	}

	if a.reconciler != nil {
		if ferr := a.reconciler.Flush(); ferr != nil {
			a.log.Warn("failed to flush reconciler state on shutdown", "error", ferr)
		}
	}
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

// resolveAgentID establishes a stable AgentID using the following precedence,
// so the identity remains constant across restarts and local-state loss:
//
//	1. Existing persisted AgentID   (kept as-is; highest priority for stability)
//	2. Explicitly configured AgentID (AGENT_ID env)
//	3. Pod UID                       (downward API metadata.uid; stable per pod)
//	4. Generated UUID                (first boot only, last resort)
//
// On recovery the agent additionally adopts the control-plane's authoritative
// AgentID (see adoptRegistration), which supersedes any locally-derived value.
func (a *Agent) resolveAgentID() {
	switch {
	case a.state.AgentID != "":
		// Persisted identity wins so restarts never change the AgentID.
	case a.cfg.AgentID != "":
		a.state.AgentID = a.cfg.AgentID
		a.log.Info("using configured agent ID", "agent_id", a.state.AgentID)
	case a.cfg.PodUID != "":
		a.state.AgentID = "agent-" + a.cfg.PodUID
		a.log.Info("derived agent ID from pod UID", "agent_id", a.state.AgentID)
	default:
		a.state.AgentID = "agent-" + uuid.NewString()[:8]
		a.log.Info("generated agent ID", "agent_id", a.state.AgentID)
	}
}

// ensureRegistered guarantees the agent has a usable cluster registration before
// the heartbeat/reconcile loops start. It is the fault-tolerant replacement for
// the old register(): it never exits on a transient failure or on a
// duplicate-registration conflict. It returns nil once registered/recovered,
// ctx.Err() on cancellation, or *fatalConfigError for an unrecoverable token.
func (a *Agent) ensureRegistered(ctx context.Context) error {
	if a.state.Registered && a.state.ClusterID != "" {
		a.log.Info("already registered (from persisted state)",
			"agent_id", a.state.AgentID,
			"cluster_id", a.state.ClusterID,
			"org_id", a.state.OrganizationID)
		// The heartbeat loop continuously validates the registration and
		// triggers recovery if the control plane no longer knows the cluster.
		return nil
	}

	a.log.Info("establishing registration", "agent_id", a.state.AgentID)

	// Record how long it takes to establish registration (fresh or recovered).
	start := time.Now()
	defer func() {
		if a.state.Registered {
			metrics.RegistrationDuration.Observe(time.Since(start).Seconds())
		}
	}()

	backoff := a.cfg.RegistrationRetryInterval
	if backoff <= 0 {
		backoff = 10 * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		metrics.RegistrationAttempts.Inc()
		resp, err := a.tryRegister(ctx)
		if err == nil {
			a.adoptRegistration(resp, false)
			metrics.RegistrationSuccess.Inc()
			return nil
		}

		var apiErr *controlplane.APIError
		if errors.As(err, &apiErr) {
			switch {
			case apiErr.IsConflict():
				// The token was already consumed on a previous boot. Idempotent
				// control planes return the cluster directly (handled above);
				// this covers older ones that still return 409. Recover instead
				// of failing.
				if rec, rerr := a.recoverRegistration(ctx); rerr == nil {
					a.adoptRegistration(rec, true)
					metrics.RegistrationRecovered.Inc()
					return nil
				} else {
					a.log.Warn("recovery after registration conflict failed, backing off", "error", rerr)
				}
			case apiErr.IsUnauthorized():
				// The bootstrap token is invalid/expired/revoked. Try recovery
				// once (a used token can still recover its cluster); if that also
				// fails the token is genuinely unusable — an unrecoverable
				// configuration error that should terminate with a clear message
				// rather than loop forever.
				if rec, rerr := a.recoverRegistration(ctx); rerr == nil {
					a.adoptRegistration(rec, true)
					metrics.RegistrationRecovered.Inc()
					return nil
				}
				metrics.RegistrationFailure.Inc()
				return &fatalConfigError{msg: "installation token is invalid or expired and no existing cluster could be recovered; a new registration token is required"}
			}
		}

		metrics.RegistrationFailure.Inc()
		a.log.Warn("registration attempt failed, backing off",
			"error", err, "backoff", backoff.String())
		if !sleepCtx(ctx, backoff) {
			return ctx.Err()
		}
		backoff = nextBackoff(backoff, a.cfg.RegistrationMaxRetryInterval)
	}
}

// tryRegister performs a single registration attempt: collect inventory then
// POST /v1/agent/register. Idempotent control planes return the existing cluster
// (HTTP 200) when the token was already used, so a restarted agent that lost its
// state simply re-registers and adopts the returned identity.
func (a *Agent) tryRegister(ctx context.Context) (*controlplane.RegisterResponse, error) {
	info, err := a.collector.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect inventory: %w", err)
	}
	return a.client.Register(ctx, controlplane.RegisterRequest{
		Token:             a.cfg.Token,
		AgentID:           a.state.AgentID,
		KubernetesVersion: info.KubernetesVersion,
		NodeCount:         info.NodeCount,
		CloudProvider:     info.CloudProvider,
		Region:            info.Region,
	})
}

// recoverRegistration rebuilds registration from the control plane using the
// installation token via GET /v1/agent/recover.
func (a *Agent) recoverRegistration(ctx context.Context) (*controlplane.RegisterResponse, error) {
	metrics.RecoverRequests.Inc()
	return a.client.Recover(ctx, a.cfg.Token, a.state.AgentID)
}

// adoptRegistration applies a register/recover response to local state and
// persists it. It adopts the control-plane's authoritative AgentID so the
// identity stays stable even when the agent generated a temporary one after
// losing state.
func (a *Agent) adoptRegistration(resp *controlplane.RegisterResponse, recovered bool) {
	a.state.ClusterID = resp.ID
	a.state.OrganizationID = resp.OrganizationID
	if resp.AgentID != "" {
		a.state.AgentID = resp.AgentID
	}
	a.state.Registered = true

	if err := a.saveState(); err != nil {
		a.log.Warn("failed to save state", "error", err)
	}

	a.log.Info("registration established",
		"cluster_id", a.state.ClusterID,
		"cluster_name", resp.Name,
		"agent_id", a.state.AgentID,
		"org_id", a.state.OrganizationID,
		"status", resp.Status,
		"recovered", recovered)
}

// saveState persists agent state and records the state-save metrics.
func (a *Agent) saveState() error {
	metrics.StateSave.Inc()
	if err := SaveState(a.cfg.StateFile, a.state); err != nil {
		metrics.StateSaveFailure.Inc()
		return err
	}
	return nil
}

// sleepCtx sleeps for d or until ctx is cancelled. It returns true if the full
// duration elapsed and false if the context was cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// nextBackoff doubles cur, capped at max (when max > 0).
func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next <= 0 {
		next = cur
	}
	if max > 0 && next > max {
		return max
	}
	return next
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
		metrics.HeartbeatFailure.Inc()
		var apiErr *controlplane.APIError
		if errors.As(err, &apiErr) {
			switch {
			case apiErr.IsUnauthorized(), apiErr.IsNotFound():
				// The control plane no longer recognizes this cluster/agent
				// (e.g. the cluster record was lost). Attempt to recover the
				// registration so heartbeats can resume. Never crash.
				a.log.Warn("heartbeat rejected, attempting registration recovery",
					"status", apiErr.StatusCode, "error", err)
				if rerr := a.recoverAfterHeartbeatLoss(ctx); rerr != nil {
					a.log.Warn("registration recovery failed, will retry on next heartbeat", "error", rerr)
				}
				return
			case apiErr.IsForbidden():
				a.log.Error("heartbeat rejected: agent ID mismatch", "error", err)
				return
			}
		}
		a.log.Warn("heartbeat failed", "error", err)
		return
	}

	metrics.HeartbeatSuccess.Inc()
	a.log.Debug("heartbeat sent",
		"k8s_version", info.KubernetesVersion,
		"node_count", info.NodeCount,
		"api_server_healthy", info.APIServerHealthy)
}

// recoverAfterHeartbeatLoss rebuilds the registration when the control plane
// stops recognizing the cluster mid-flight. It first tries the recovery
// endpoint (returns the existing cluster) and falls back to an idempotent
// registration attempt. It never terminates the process; on failure the next
// heartbeat simply retries.
func (a *Agent) recoverAfterHeartbeatLoss(ctx context.Context) error {
	if rec, err := a.recoverRegistration(ctx); err == nil {
		a.adoptRegistration(rec, true)
		metrics.RegistrationRecovered.Inc()
		return nil
	}
	resp, err := a.tryRegister(ctx)
	if err != nil {
		return err
	}
	a.adoptRegistration(resp, true)
	metrics.RegistrationRecovered.Inc()
	return nil
}

// State returns the current agent state.
func (a *Agent) State() *State {
	return a.state
}
