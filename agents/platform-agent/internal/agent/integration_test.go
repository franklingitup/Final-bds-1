//go:build integration

package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/config"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/inventory"
)

// Integration tests require:
// - CONTROL_PLANE_URL set to a running cluster service
// - AGENT_TOKEN set to a valid registration token
// - Running inside a Kubernetes cluster OR fake collector

func TestIntegration_FullRegistrationFlow(t *testing.T) {
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")
	token := os.Getenv("AGENT_TOKEN")

	if controlPlaneURL == "" || token == "" {
		t.Skip("CONTROL_PLANE_URL and AGENT_TOKEN required for integration test")
	}

	tmpDir := t.TempDir()
	cfg := config.Config{
		Token:                     token,
		ControlPlaneURL:           controlPlaneURL,
		HeartbeatInterval:         5 * time.Second,
		RegistrationRetryInterval: 2 * time.Second,
		RequestTimeout:            30 * time.Second,
		StateFile:                 filepath.Join(tmpDir, "state.json"),
	}

	client := controlplane.NewClient(cfg.ControlPlaneURL, cfg.RequestTimeout)

	// Use fake collector for integration test if not in cluster.
	collector := &fakeCollector{
		info: &inventory.Info{
			KubernetesVersion: "v1.28.5-integration-test",
			NodeCount:         3,
			ClusterUID:        "integration-test-uid",
			CloudProvider:     "integration-test",
			Region:            "test-region",
			APIServerHealthy:  true,
		},
		healthy: true,
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	agent := New(cfg, client, collector, log)

	// Run for a short time to complete registration and a few heartbeats.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- agent.Run(ctx)
	}()

	// Wait for registration.
	time.Sleep(5 * time.Second)

	// Verify registration.
	state := agent.State()
	if !state.Registered {
		t.Error("agent not registered after 5 seconds")
	}
	if state.ClusterID == "" {
		t.Error("ClusterID not set after registration")
	}
	if state.OrganizationID == "" {
		t.Error("OrganizationID not set after registration")
	}

	t.Logf("Registration successful: ClusterID=%s, OrgID=%s", state.ClusterID, state.OrganizationID)

	// Let a few heartbeats run.
	time.Sleep(15 * time.Second)

	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Errorf("agent error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("agent did not stop in time")
	}
}

func TestIntegration_ReconnectAfterRestart(t *testing.T) {
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")
	token := os.Getenv("AGENT_TOKEN")

	if controlPlaneURL == "" || token == "" {
		t.Skip("CONTROL_PLANE_URL and AGENT_TOKEN required for integration test")
	}

	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	cfg := config.Config{
		Token:                     token,
		ControlPlaneURL:           controlPlaneURL,
		HeartbeatInterval:         2 * time.Second,
		RegistrationRetryInterval: 2 * time.Second,
		RequestTimeout:            30 * time.Second,
		StateFile:                 stateFile,
	}

	client := controlplane.NewClient(cfg.ControlPlaneURL, cfg.RequestTimeout)
	collector := &fakeCollector{
		info: &inventory.Info{
			KubernetesVersion: "v1.28.5-reconnect-test",
			NodeCount:         2,
			ClusterUID:        "reconnect-test-uid",
			CloudProvider:     "test",
			Region:            "test-region",
			APIServerHealthy:  true,
		},
		healthy: true,
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// First run: register.
	agent1 := New(cfg, client, collector, log)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 10*time.Second)

	errCh1 := make(chan error, 1)
	go func() {
		errCh1 <- agent1.Run(ctx1)
	}()

	time.Sleep(5 * time.Second)
	cancel1()
	<-errCh1

	state1 := agent1.State()
	if !state1.Registered {
		t.Fatal("first agent not registered")
	}
	t.Logf("First agent registered: AgentID=%s, ClusterID=%s", state1.AgentID, state1.ClusterID)

	// Second run: should skip registration and go straight to heartbeat.
	// Note: In a real scenario, the token would already be used.
	// This test verifies state persistence works.
	agent2 := New(cfg, client, collector, log)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)

	errCh2 := make(chan error, 1)
	go func() {
		errCh2 <- agent2.Run(ctx2)
	}()

	time.Sleep(5 * time.Second)
	cancel2()
	<-errCh2

	state2 := agent2.State()
	if state2.AgentID != state1.AgentID {
		t.Errorf("AgentID changed: %q -> %q", state1.AgentID, state2.AgentID)
	}
	if state2.ClusterID != state1.ClusterID {
		t.Errorf("ClusterID changed: %q -> %q", state1.ClusterID, state2.ClusterID)
	}

	t.Log("Second agent reused persisted state successfully")
}
