package leaderelection

import (
	"context"
	"log/slog"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/metrics"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestElector(t *testing.T) *Elector {
	t.Helper()
	return New(Config{
		LeaseName:      "test-lease",
		LeaseNamespace: "test-ns",
		Identity:       "instance-a",
		LeaseDuration:  15 * time.Second,
		RenewDeadline:  10 * time.Second,
		RetryPeriod:    2 * time.Second,
	}, fake.NewSimpleClientset(), testLogger())
}

// --- Callback unit tests -------------------------------------------------

func TestOnStartedLeadingSetsLeaderStateAndMetrics(t *testing.T) {
	e := newTestElector(t)
	e.electionStart.Store(time.Now().Add(-3 * time.Second).UnixNano())

	transitionsBefore := testutil.ToFloat64(metrics.LeaderTransitions)

	e.onStartedLeading(context.Background())

	if !e.IsLeader() {
		t.Fatal("IsLeader() should be true after acquiring leadership")
	}
	if got := testutil.ToFloat64(metrics.IsLeader); got != 1 {
		t.Errorf("agent_is_leader = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.LeaderTransitions); got != transitionsBefore+1 {
		t.Errorf("transitions delta = %v, want 1", got-transitionsBefore)
	}
	if got := testutil.ToFloat64(metrics.LeaderElectionDuration); got < 2 {
		t.Errorf("election duration = %v, want >= ~3s", got)
	}
}

func TestOnStoppedLeadingInvoluntary(t *testing.T) {
	e := newTestElector(t)
	e.parentCtx = context.Background() // not cancelled -> involuntary loss
	e.onStartedLeading(context.Background())

	transitionsBefore := testutil.ToFloat64(metrics.LeaderTransitions)
	e.onStoppedLeading()

	if e.IsLeader() {
		t.Fatal("IsLeader() should be false after losing leadership")
	}
	if got := testutil.ToFloat64(metrics.IsLeader); got != 0 {
		t.Errorf("agent_is_leader = %v, want 0", got)
	}
	if got := testutil.ToFloat64(metrics.LeaderTransitions); got != transitionsBefore+1 {
		t.Errorf("transitions delta = %v, want 1", got-transitionsBefore)
	}
}

func TestOnStoppedLeadingGraceful(t *testing.T) {
	e := newTestElector(t)
	ctx, cancel := context.WithCancel(context.Background())
	e.parentCtx = ctx
	e.onStartedLeading(context.Background())
	cancel() // simulate shutdown

	// Should not panic and should clear leadership; graceful path taken.
	e.onStoppedLeading()
	if e.IsLeader() {
		t.Fatal("IsLeader() should be false after graceful stop")
	}
}

func TestOnNewLeaderIgnoresSelf(t *testing.T) {
	e := newTestElector(t)
	// These should not panic; self and empty are ignored, others logged.
	e.onNewLeader("instance-a")
	e.onNewLeader("")
	e.onNewLeader("instance-b")
}

// --- Real client-go wiring ----------------------------------------------

func TestLeaderElectionConfigBuildsLeaseLock(t *testing.T) {
	e := newTestElector(t)
	lec := e.leaderElectionConfig()

	if lec.LeaseDuration != 15*time.Second || lec.RenewDeadline != 10*time.Second || lec.RetryPeriod != 2*time.Second {
		t.Errorf("timings mismatch: %v/%v/%v", lec.LeaseDuration, lec.RenewDeadline, lec.RetryPeriod)
	}
	if !lec.ReleaseOnCancel {
		t.Error("ReleaseOnCancel must be true so the lease is released on shutdown")
	}
	lock, ok := lec.Lock.(*resourcelock.LeaseLock)
	if !ok {
		t.Fatalf("lock type = %T, want *resourcelock.LeaseLock (coordination.k8s.io/v1)", lec.Lock)
	}
	if lock.LeaseMeta.Name != "test-lease" || lock.LeaseMeta.Namespace != "test-ns" {
		t.Errorf("lease meta = %s/%s", lock.LeaseMeta.Namespace, lock.LeaseMeta.Name)
	}
	if lock.LockConfig.Identity != "instance-a" {
		t.Errorf("identity = %q, want instance-a", lock.LockConfig.Identity)
	}
	if lec.Callbacks.OnStartedLeading == nil || lec.Callbacks.OnStoppedLeading == nil || lec.Callbacks.OnNewLeader == nil {
		t.Error("all leader callbacks must be wired")
	}
}

// TestRealElectorConstructs proves the production newElector builds a real
// client-go LeaderElector from our config without error (validates the timing
// invariants and lock wiring against the real library).
func TestRealElectorConstructs(t *testing.T) {
	e := newTestElector(t)
	le, err := e.newElector(e.leaderElectionConfig())
	if err != nil {
		t.Fatalf("real LeaderElector construction failed: %v", err)
	}
	if _, ok := le.(*leaderelection.LeaderElector); !ok {
		t.Fatalf("newElector returned %T, want *leaderelection.LeaderElector", le)
	}
}

// --- Integration: contention, single leader, failover -------------------

// coordinator hands leadership to exactly one identity at a time. It backs a
// fakeElector so the Elector.Run loop, callbacks and gate can be exercised
// deterministically without a real API server (whose optimistic concurrency the
// in-memory fake clientset does not emulate).
type coordinator struct {
	mu     sync.Mutex
	holder string
}

func (c *coordinator) acquire(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holder == "" {
		c.holder = id
		return true
	}
	return c.holder == id
}

func (c *coordinator) release(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holder == id {
		c.holder = ""
	}
}

func (c *coordinator) current() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holder
}

type fakeElector struct {
	identity string
	coord    *coordinator
	cb       leaderelection.LeaderCallbacks
}

func (f *fakeElector) Run(ctx context.Context) {
	// Block trying to acquire, like client-go's acquire loop.
	for {
		if ctx.Err() != nil {
			return
		}
		if f.coord.acquire(f.identity) {
			break
		}
		f.cb.OnNewLeader(f.coord.current())
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}

	leadCtx, cancel := context.WithCancel(ctx)
	f.cb.OnNewLeader(f.identity)
	go f.cb.OnStartedLeading(leadCtx)

	<-ctx.Done() // hold leadership until shutdown
	f.coord.release(f.identity)
	cancel()
	f.cb.OnStoppedLeading()
}

func newFakeCoordinatedElector(id string, coord *coordinator) *Elector {
	e := New(Config{
		LeaseName:     "lease",
		Identity:      id,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   10 * time.Millisecond,
	}, fake.NewSimpleClientset(), testLogger())
	e.newElector = func(lec leaderelection.LeaderElectionConfig) (leaderElector, error) {
		return &fakeElector{identity: id, coord: coord, cb: lec.Callbacks}, nil
	}
	return e
}

func TestTwoInstancesExactlyOneLeaderAndFailover(t *testing.T) {
	coord := &coordinator{}
	a := newFakeCoordinatedElector("instance-a", coord)
	b := newFakeCoordinatedElector("instance-b", coord)

	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.Run(ctxA) }()
	go func() { defer wg.Done(); b.Run(ctxB) }()

	// Wait until some instance has become leader.
	waitFor(t, 2*time.Second, func() bool { return a.IsLeader() || b.IsLeader() })

	// Invariant: never two leaders at once. Sample repeatedly.
	stop := make(chan struct{})
	var bothLeaders atomic.Bool
	go func() {
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if a.IsLeader() && b.IsLeader() {
					bothLeaders.Store(true)
				}
			}
		}
	}()

	// Determine the current leader and kill it.
	time.Sleep(100 * time.Millisecond)
	leader, follower := a, b
	cancelLeader := cancelA
	if b.IsLeader() {
		leader, follower = b, a
		cancelLeader = cancelB
	}
	if !leader.IsLeader() {
		t.Fatal("expected a leader before failover")
	}

	cancelLeader() // leader dies

	// Follower must be promoted within the failover window.
	waitFor(t, 3*time.Second, func() bool { return follower.IsLeader() })

	close(stop)
	if bothLeaders.Load() {
		t.Error("two instances were leader simultaneously")
	}

	cancelA()
	cancelB()
	wg.Wait()
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

// --- Concurrency: rapid transitions must not race ------------------------

func TestConcurrentTransitionsNoRace(t *testing.T) {
	e := newTestElector(t)
	e.parentCtx = context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				e.onStartedLeading(context.Background())
				_ = e.IsLeader()
				e.onStoppedLeading()
			}
		}()
	}
	wg.Wait()

	// After all toggling, leadership must be a consistent boolean (no panic /
	// corruption). Final value is not deterministic; just assert it is readable.
	_ = e.IsLeader()
}
