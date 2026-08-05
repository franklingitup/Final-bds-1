package reconciler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/k8s"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func oneDeployment() []controlplane.DesiredDeployment {
	return []controlplane.DesiredDeployment{{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        1,
		Revision:        1,
		Status:          "pending",
	}}
}

// TestFollowerDoesNotReconcile verifies that when IsLeader returns false the
// reconciler performs no work at all: it does not fetch desired state or apply
// any resources.
func TestFollowerDoesNotReconcile(t *testing.T) {
	client := newFakeDeploymentClient()
	client.deployments = oneDeployment()
	manager := newFakeResourceManager()

	cfg := testConfig(t)
	cfg.IsLeader = func() bool { return false }

	rec := New(client, manager, cfg, quietLogger())
	rec.reconcile(context.Background())

	assert.Empty(t, manager.appliedSpecs, "follower must not apply any deployment")
	assert.Empty(t, client.reportedStatuses, "follower must not report status")
}

// TestLeaderReconciles verifies that when IsLeader returns true the reconciler
// proceeds normally.
func TestLeaderReconciles(t *testing.T) {
	client := newFakeDeploymentClient()
	client.deployments = oneDeployment()
	manager := newFakeResourceManager()

	cfg := testConfig(t)
	cfg.IsLeader = func() bool { return true }

	rec := New(client, manager, cfg, quietLogger())
	rec.reconcile(context.Background())

	assert.Len(t, manager.appliedSpecs, 1, "leader must apply the desired deployment")
}

// TestNilGateReconciles verifies backward compatibility: a nil IsLeader gate
// (leader election disabled) always reconciles, exactly as before this feature.
func TestNilGateReconciles(t *testing.T) {
	client := newFakeDeploymentClient()
	client.deployments = oneDeployment()
	manager := newFakeResourceManager()

	cfg := testConfig(t)
	cfg.IsLeader = nil

	rec := New(client, manager, cfg, quietLogger())
	rec.reconcile(context.Background())

	assert.Len(t, manager.appliedSpecs, 1, "nil gate must behave as leader (legacy behaviour)")
}

// countingManager counts applies per reconciler instance so we can assert the
// gate blocked the follower entirely.
type countingManager struct {
	*fakeResourceManager
	applies atomic.Int32
}

func (m *countingManager) ApplyDeployment(ctx context.Context, spec k8s.DeploymentSpec) (*k8s.ApplyResult, error) {
	m.applies.Add(1)
	return &k8s.ApplyResult{NoOp: true}, nil
}

// TestFollowerNeverAppliesEvenUnderLoad drives many reconcile cycles on a fixed
// follower and asserts it applies nothing, complementing the leader case.
func TestFollowerNeverAppliesEvenUnderLoad(t *testing.T) {
	client := newFakeDeploymentClient()
	client.deployments = oneDeployment()
	mgr := &countingManager{fakeResourceManager: newFakeResourceManager()}

	cfg := testConfig(t)
	cfg.IsLeader = func() bool { return false }
	rec := New(client, mgr, cfg, quietLogger())

	for i := 0; i < 1000; i++ {
		rec.reconcile(context.Background())
	}
	assert.Zero(t, mgr.applies.Load(), "follower must never apply, even over many cycles")
}

// TestGateRaceFreeUnderRapidTransitions runs two reconcilers (each with its own
// resource manager, as in production where one pod == one reconciler) while
// leadership flips rapidly between them. It verifies the leadership gate is
// race-free (under `go test -race`) and that reconciliation still makes
// progress. At most one instance is leader at any instant (the Lease guarantee,
// modelled by the single `leader` atomic), so only the current leader applies;
// applies are idempotent server-side upserts, so a demoted leader's in-flight
// cycle draining during handover is harmless.
func TestGateRaceFreeUnderRapidTransitions(t *testing.T) {
	var leader atomic.Int32 // 0 => A leads, 1 => B leads

	makeRec := func(id int32) (*Reconciler, *countingManager) {
		client := newFakeDeploymentClient()
		client.deployments = oneDeployment()
		mgr := &countingManager{fakeResourceManager: newFakeResourceManager()}
		cfg := testConfig(t)
		cfg.IsLeader = func() bool { return leader.Load() == id }
		return New(client, mgr, cfg, quietLogger()), mgr
	}
	recA, mgrA := makeRec(0)
	recB, mgrB := makeRec(1)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	for _, rec := range []*Reconciler{recA, recB} {
		wg.Add(1)
		go func(r *Reconciler) {
			defer wg.Done()
			for ctx.Err() == nil {
				r.reconcile(ctx)
			}
		}(rec)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000 && ctx.Err() == nil; i++ {
			leader.Store(int32(i % 2))
			time.Sleep(100 * time.Microsecond)
		}
		cancel()
	}()

	wg.Wait()

	require.Positive(t, mgrA.applies.Load()+mgrB.applies.Load(),
		"expected reconciliation to make progress")
}
