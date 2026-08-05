package agent

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/config"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/inventory"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/reconciler"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/secrets"
)

// TestWorkerFactoryReceivesCredentialsAfterRegistration verifies that
// worker factories are called with valid credentials AFTER registration completes.
// This is the fix for the first-boot credential race condition (BLOCKER-3).
func TestWorkerFactoryReceivesCredentialsAfterRegistration(t *testing.T) {
	// Track credentials received by factories.
	var reconcilerCreds, syncerCreds controlplane.AgentCredentials

	workerFactory := &WorkerFactory{
		ReconcilerFactory: func(creds controlplane.AgentCredentials) (*reconciler.Reconciler, error) {
			reconcilerCreds = creds
			// Return nil to avoid actual reconciler creation in tests.
			return nil, nil
		},
		SecretsSyncerFactory: func(creds controlplane.AgentCredentials) (*secrets.Syncer, error) {
			syncerCreds = creds
			// Return nil to avoid actual syncer creation in tests.
			return nil, nil
		},
	}

	cfg := config.Config{
		ReconcilerEnabled:    true,
		SecretsSyncerEnabled: true,
		HeartbeatInterval:    time.Hour, // Long interval to prevent heartbeat during test
	}

	// Simulate a registered agent state.
	state := &State{
		AgentID:        "test-agent-123",
		ClusterID:      "test-cluster-456",
		OrganizationID: "test-org-789",
		Registered:     true,
	}

	agent := &Agent{
		cfg:           cfg,
		state:         state,
		workerFactory: workerFactory,
		log:           slog.Default(),
	}

	// Initialize workers (called after registration in real flow).
	err := agent.initializeWorkers()
	if err != nil {
		t.Fatalf("initializeWorkers failed: %v", err)
	}

	// Verify reconciler received correct credentials.
	if reconcilerCreds.ClusterID != state.ClusterID {
		t.Errorf("reconciler clusterID = %q, want %q", reconcilerCreds.ClusterID, state.ClusterID)
	}
	if reconcilerCreds.AgentID != state.AgentID {
		t.Errorf("reconciler agentID = %q, want %q", reconcilerCreds.AgentID, state.AgentID)
	}

	// Verify syncer received correct credentials.
	if syncerCreds.ClusterID != state.ClusterID {
		t.Errorf("syncer clusterID = %q, want %q", syncerCreds.ClusterID, state.ClusterID)
	}
	if syncerCreds.AgentID != state.AgentID {
		t.Errorf("syncer agentID = %q, want %q", syncerCreds.AgentID, state.AgentID)
	}
}

// TestInitializeWorkersOnlyWhenEnabled verifies that worker factories
// are only called when the corresponding feature is enabled.
func TestInitializeWorkersOnlyWhenEnabled(t *testing.T) {
	tests := []struct {
		name                 string
		reconcilerEnabled    bool
		syncerEnabled        bool
		expectReconcilerCall bool
		expectSyncerCall     bool
	}{
		{
			name:                 "both enabled",
			reconcilerEnabled:    true,
			syncerEnabled:        true,
			expectReconcilerCall: true,
			expectSyncerCall:     true,
		},
		{
			name:                 "only reconciler enabled",
			reconcilerEnabled:    true,
			syncerEnabled:        false,
			expectReconcilerCall: true,
			expectSyncerCall:     false,
		},
		{
			name:                 "only syncer enabled",
			reconcilerEnabled:    false,
			syncerEnabled:        true,
			expectReconcilerCall: false,
			expectSyncerCall:     true,
		},
		{
			name:                 "neither enabled",
			reconcilerEnabled:    false,
			syncerEnabled:        false,
			expectReconcilerCall: false,
			expectSyncerCall:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconcilerCalled := false
			syncerCalled := false

			workerFactory := &WorkerFactory{
				ReconcilerFactory: func(creds controlplane.AgentCredentials) (*reconciler.Reconciler, error) {
					reconcilerCalled = true
					return nil, nil
				},
				SecretsSyncerFactory: func(creds controlplane.AgentCredentials) (*secrets.Syncer, error) {
					syncerCalled = true
					return nil, nil
				},
			}

			cfg := config.Config{
				ReconcilerEnabled:    tt.reconcilerEnabled,
				SecretsSyncerEnabled: tt.syncerEnabled,
			}

			state := &State{
				AgentID:   "test-agent",
				ClusterID: "test-cluster",
			}

			agent := &Agent{
				cfg:           cfg,
				state:         state,
				workerFactory: workerFactory,
				log:           slog.Default(),
			}

			_ = agent.initializeWorkers()

			if reconcilerCalled != tt.expectReconcilerCall {
				t.Errorf("reconciler factory called = %v, want %v", reconcilerCalled, tt.expectReconcilerCall)
			}
			if syncerCalled != tt.expectSyncerCall {
				t.Errorf("syncer factory called = %v, want %v", syncerCalled, tt.expectSyncerCall)
			}
		})
	}
}

// TestInitializeWorkersWithNilFactory verifies that initializeWorkers
// handles nil factory gracefully (backward compatibility).
func TestInitializeWorkersWithNilFactory(t *testing.T) {
	agent := &Agent{
		cfg: config.Config{
			ReconcilerEnabled:    true,
			SecretsSyncerEnabled: true,
		},
		state:         &State{AgentID: "test", ClusterID: "test"},
		workerFactory: nil, // No factory
	}

	err := agent.initializeWorkers()
	if err != nil {
		t.Errorf("initializeWorkers with nil factory should not error, got: %v", err)
	}
}

// fakeInventoryCollector implements InventoryCollector for tests.
type fakeInventoryCollector struct {
	info *inventory.Info
	err  error
}

func (f *fakeInventoryCollector) Collect(ctx context.Context) (*inventory.Info, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.info != nil {
		return f.info, nil
	}
	return &inventory.Info{
		KubernetesVersion: "v1.28.0",
		NodeCount:         3,
		APIServerHealthy:  true,
	}, nil
}

func (f *fakeInventoryCollector) CheckHealth(ctx context.Context) bool {
	return true
}
