package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/config"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/reconciler"
)

func silentLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestSetLeaderElectorStores verifies the setter wires the elector.
func TestSetLeaderElectorStores(t *testing.T) {
	a := &Agent{log: silentLogger()}
	if a.leaderElector != nil {
		t.Fatal("elector should be nil initially")
	}
	le := &stubElector{}
	a.SetLeaderElector(le)
	if a.leaderElector == nil {
		t.Fatal("SetLeaderElector did not store the elector")
	}
}

type stubElector struct{ ran chan struct{} }

func (s *stubElector) Run(ctx context.Context) {
	if s.ran != nil {
		close(s.ran)
	}
	<-ctx.Done()
}

// TestShutdownFlushesReconcilerState verifies graceful shutdown persists
// reconciler state and returns promptly once the elector has stopped.
func TestShutdownFlushesReconcilerState(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "reconciler-state.json")
	rec := reconciler.New(nil, nil, reconciler.Config{StateFile: stateFile}, silentLogger())

	a := &Agent{
		cfg:        config.Config{LeaseDuration: 15 * time.Second},
		reconciler: rec,
		log:        silentLogger(),
	}

	electorDone := make(chan struct{})
	close(electorDone) // elector already released the lease

	done := make(chan struct{})
	go func() {
		a.shutdown(electorDone)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not return promptly after elector stopped")
	}

	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("expected reconciler state flushed to disk: %v", err)
	}
}

// TestShutdownHandlesNilElectorAndReconciler ensures shutdown is safe when
// leader election is disabled (nil elector) and no reconciler is present.
func TestShutdownHandlesNilElectorAndReconciler(t *testing.T) {
	a := &Agent{cfg: config.Config{}, log: silentLogger()}
	a.shutdown(nil) // must not panic or block
}

// TestShutdownTimesOutWhenElectorHangs ensures shutdown cannot block forever if
// the elector fails to release the lease.
func TestShutdownTimesOutWhenElectorHangs(t *testing.T) {
	a := &Agent{
		cfg: config.Config{LeaseDuration: 10 * time.Millisecond},
		log: silentLogger(),
	}
	electorDone := make(chan struct{}) // never closed

	start := time.Now()
	a.shutdown(electorDone)
	elapsed := time.Since(start)

	// timeout = LeaseDuration + 2s; must return in a bounded time, not hang.
	if elapsed > 5*time.Second {
		t.Fatalf("shutdown blocked too long: %s", elapsed)
	}
	if elapsed < 10*time.Millisecond {
		t.Fatalf("shutdown returned too early: %s", elapsed)
	}
}
