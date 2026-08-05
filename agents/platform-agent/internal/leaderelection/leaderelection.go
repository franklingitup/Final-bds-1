// Package leaderelection implements Kubernetes Lease-based leader election for
// the platform agent using the upstream client-go primitives
// (k8s.io/client-go/tools/leaderelection with a coordination.k8s.io/v1 Lease).
//
// It deliberately does NOT implement a custom election algorithm and does NOT
// contain any reconciliation logic. It exposes a single boolean predicate,
// IsLeader, that the existing reconciler and secrets syncer consult to decide
// whether to act. Only the elected leader reports IsLeader()==true, so at most
// one replica reconciles at any time. Followers keep IsLeader()==false, stay
// warm, and take over automatically when the current leader's lease expires or
// is released.
package leaderelection

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/metrics"
)

// Config holds the parameters required to run leader election.
type Config struct {
	// LeaseName / LeaseNamespace identify the shared coordination.k8s.io/v1
	// Lease object all replicas contend for.
	LeaseName      string
	LeaseNamespace string
	// Identity is this replica's unique holder identity (typically the pod name).
	Identity string
	// LeaseDuration, RenewDeadline and RetryPeriod are the standard client-go
	// leader-election timings. Kubernetes recommends 15s / 10s / 2s.
	LeaseDuration time.Duration
	RenewDeadline time.Duration
	RetryPeriod   time.Duration
}

// Elector runs leader election in a loop and tracks the current leadership
// state. It is safe for concurrent use.
type Elector struct {
	cfg    Config
	client kubernetes.Interface
	log    *slog.Logger
	tracer trace.Tracer

	isLeader atomic.Bool

	// electionStart marks when the current acquisition attempt began, used to
	// record agent_leader_election_duration_seconds on acquisition.
	electionStart atomic.Int64 // unix nanos

	// parentCtx is captured on Run so callbacks (which take no context) can
	// distinguish a graceful shutdown (ctx cancelled -> lease released) from an
	// involuntary loss (renew failed).
	parentCtx context.Context

	spanMu   sync.Mutex
	leadSpan trace.Span

	// newElector is overridable in tests to inject a fake election loop
	// without a Kubernetes API server. In production it builds a real
	// client-go LeaderElector backed by a Lease lock.
	newElector func(leaderelection.LeaderElectionConfig) (leaderElector, error)
}

// leaderElector is the subset of *leaderelection.LeaderElector the Elector
// depends on, so tests can substitute a fake.
type leaderElector interface {
	Run(ctx context.Context)
}

// New creates an Elector. The provided client must be able to read and write
// Lease objects in the configured namespace.
func New(cfg Config, client kubernetes.Interface, log *slog.Logger) *Elector {
	e := &Elector{
		cfg:    cfg,
		client: client,
		log:    log.With("component", "leader-election", "identity", cfg.Identity, "lease", cfg.LeaseName),
		tracer: otel.Tracer("platform-agent/leaderelection"),
	}
	e.newElector = func(lec leaderelection.LeaderElectionConfig) (leaderElector, error) {
		return leaderelection.NewLeaderElector(lec)
	}
	// Followers start not-leading; publish the initial gauge value.
	metrics.IsLeader.Set(0)
	return e
}

// IsLeader reports whether this replica currently holds leadership. It is the
// gate consulted by the reconciler and secrets syncer.
func (e *Elector) IsLeader() bool { return e.isLeader.Load() }

// Run participates in leader election until ctx is cancelled. It blocks, so
// callers typically run it in a goroutine. On leadership loss it automatically
// re-enters the election as a follower; on ctx cancellation the leader releases
// its lease (ReleaseOnCancel) before returning.
func (e *Elector) Run(ctx context.Context) {
	e.parentCtx = ctx
	e.log.Info("starting leader election",
		"lease_duration", e.cfg.LeaseDuration.String(),
		"renew_deadline", e.cfg.RenewDeadline.String(),
		"retry_period", e.cfg.RetryPeriod.String(),
	)

	for {
		if ctx.Err() != nil {
			return
		}

		e.electionStart.Store(time.Now().UnixNano())

		le, err := e.newElector(e.leaderElectionConfig())
		if err != nil {
			e.log.Error("failed to construct leader elector, retrying", "error", err)
			if !e.sleep(ctx, e.cfg.RetryPeriod) {
				return
			}
			continue
		}

		// Blocks until this replica acquires then loses leadership, or ctx is
		// cancelled. While blocked as a follower it keeps trying to acquire.
		le.Run(ctx)

		if ctx.Err() != nil {
			return
		}
		// Lost leadership involuntarily; pause briefly before re-contending so
		// we do not hot-loop against the API server.
		if !e.sleep(ctx, e.cfg.RetryPeriod) {
			return
		}
	}
}

// sleep waits for d or ctx cancellation. It returns false if ctx was cancelled.
func (e *Elector) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// leaderElectionConfig builds the client-go configuration for a single
// election attempt, wiring our instrumented callbacks.
func (e *Elector) leaderElectionConfig() leaderelection.LeaderElectionConfig {
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      e.cfg.LeaseName,
			Namespace: e.cfg.LeaseNamespace,
		},
		Client: e.client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: e.cfg.Identity,
		},
	}

	return leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   e.cfg.LeaseDuration,
		RenewDeadline:   e.cfg.RenewDeadline,
		RetryPeriod:     e.cfg.RetryPeriod,
		Name:            e.cfg.LeaseName,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: e.onStartedLeading,
			OnStoppedLeading: e.onStoppedLeading,
			OnNewLeader:      e.onNewLeader,
		},
	}
}

// onStartedLeading runs when this replica acquires leadership.
func (e *Elector) onStartedLeading(ctx context.Context) {
	e.isLeader.Store(true)
	metrics.IsLeader.Set(1)
	metrics.LeaderTransitions.Inc()

	if start := e.electionStart.Load(); start > 0 {
		metrics.LeaderElectionDuration.Set(time.Since(time.Unix(0, start)).Seconds())
	}

	e.startLeadSpan()

	e.log.Info("leader acquired: became leader",
		slog.String("event", "leader_acquired"),
	)
}

// onStoppedLeading runs when this replica loses leadership, whether because
// renewal failed (involuntary) or because ctx was cancelled during shutdown
// (graceful; the lease is released via ReleaseOnCancel).
func (e *Elector) onStoppedLeading() {
	e.isLeader.Store(false)
	metrics.IsLeader.Set(0)
	metrics.LeaderTransitions.Inc()

	graceful := e.parentCtx != nil && e.parentCtx.Err() != nil
	e.endLeadSpan(graceful)

	if graceful {
		e.log.Info("lease released: shutting down leader",
			slog.String("event", "lease_released"),
		)
		return
	}

	e.log.Warn("renew failed: leader lost, became follower",
		slog.String("event", "leader_lost"),
	)
}

// onNewLeader runs whenever a new leader is observed, including while this
// replica is a follower waiting to acquire.
func (e *Elector) onNewLeader(identity string) {
	if identity == e.cfg.Identity {
		// Our own acquisition is reported by onStartedLeading.
		return
	}
	if identity == "" {
		return
	}
	e.log.Info("became follower: observed new leader",
		slog.String("event", "new_leader"),
		slog.String("leader", identity),
	)
}

func (e *Elector) startLeadSpan() {
	e.spanMu.Lock()
	defer e.spanMu.Unlock()
	// A detached span spanning the leadership tenure. No-op unless a tracer
	// provider is installed, so this is free when tracing is not configured.
	_, span := e.tracer.Start(context.Background(), "leaderelection.lead",
		trace.WithAttributes(
			attribute.String("lease.name", e.cfg.LeaseName),
			attribute.String("lease.identity", e.cfg.Identity),
		),
	)
	span.AddEvent("leader_acquired")
	e.leadSpan = span
}

func (e *Elector) endLeadSpan(graceful bool) {
	e.spanMu.Lock()
	defer e.spanMu.Unlock()
	if e.leadSpan == nil {
		return
	}
	if graceful {
		e.leadSpan.AddEvent("lease_released")
	} else {
		e.leadSpan.AddEvent("leader_lost")
	}
	e.leadSpan.End()
	e.leadSpan = nil
}
