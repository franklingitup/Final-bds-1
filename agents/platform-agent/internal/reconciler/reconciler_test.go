package reconciler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"log/slog"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/k8s"
)

// ----------------------------------------------------------------------------
// Fake Deployment Client
// ----------------------------------------------------------------------------

type fakeDeploymentClient struct {
	deployments      []controlplane.DesiredDeployment
	reportedStatuses map[string]controlplane.DeploymentStatusRequest
	getError         error
	reportError      error
}

func newFakeDeploymentClient() *fakeDeploymentClient {
	return &fakeDeploymentClient{
		reportedStatuses: make(map[string]controlplane.DeploymentStatusRequest),
	}
}

func (c *fakeDeploymentClient) GetDesiredState(ctx context.Context, creds controlplane.AgentCredentials) ([]controlplane.DesiredDeployment, error) {
	if c.getError != nil {
		return nil, c.getError
	}
	return c.deployments, nil
}

func (c *fakeDeploymentClient) ReportDeploymentStatusWithCreds(ctx context.Context, creds controlplane.AgentCredentials, deploymentID, releaseID string, req controlplane.DeploymentStatusRequest) error {
	if c.reportError != nil {
		return c.reportError
	}
	c.reportedStatuses[releaseID] = req
	return nil
}

// ----------------------------------------------------------------------------
// Fake Resource Manager
// ----------------------------------------------------------------------------

type fakeResourceManager struct {
	deployments      map[string]*k8s.DeploymentStatus
	appliedSpecs     []k8s.DeploymentSpec
	applyError       error
	deleteError      error
}

func newFakeResourceManager() *fakeResourceManager {
	return &fakeResourceManager{
		deployments: make(map[string]*k8s.DeploymentStatus),
	}
}

func (m *fakeResourceManager) ApplyDeployment(ctx context.Context, spec k8s.DeploymentSpec) (*k8s.ApplyResult, error) {
	if m.applyError != nil {
		return nil, m.applyError
	}
	m.appliedSpecs = append(m.appliedSpecs, spec)

	name := spec.ResourceName()
	existing, found := m.deployments[name]
	if !found {
		m.deployments[name] = &k8s.DeploymentStatus{
			Name:      name,
			Revision:  spec.Revision,
			ReleaseID: spec.ReleaseID,
		}
		return &k8s.ApplyResult{Created: true}, nil
	}

	if existing.Revision != spec.Revision {
		existing.Revision = spec.Revision
		existing.ReleaseID = spec.ReleaseID
		return &k8s.ApplyResult{Updated: true}, nil
	}

	return &k8s.ApplyResult{NoOp: true}, nil
}

func (m *fakeResourceManager) ApplyService(ctx context.Context, spec k8s.DeploymentSpec) (*k8s.ApplyResult, error) {
	return &k8s.ApplyResult{NoOp: true}, nil
}

func (m *fakeResourceManager) GetDeploymentStatus(ctx context.Context, name string) (*k8s.DeploymentStatus, error) {
	status, ok := m.deployments[name]
	if !ok {
		return nil, nil
	}
	return status, nil
}

func (m *fakeResourceManager) DeleteDeployment(ctx context.Context, name string) error {
	if m.deleteError != nil {
		return m.deleteError
	}
	delete(m.deployments, name)
	return nil
}

func (m *fakeResourceManager) ListManagedDeployments(ctx context.Context) ([]string, error) {
	var names []string
	for name := range m.deployments {
		names = append(names, name)
	}
	return names, nil
}

// MarkReady marks a deployment as ready.
func (m *fakeResourceManager) MarkReady(name string, replicas int) {
	if status, ok := m.deployments[name]; ok {
		status.Ready = true
		status.ReadyReplicas = replicas
		status.UpdatedReplicas = replicas
		status.AvailableReplicas = replicas
	}
}

// MarkFailed marks a deployment as failed.
func (m *fakeResourceManager) MarkFailed(name string, reason, message string) {
	if status, ok := m.deployments[name]; ok {
		status.Failed = true
		status.FailureReason = reason
		status.FailureMessage = message
	}
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func testConfig(t *testing.T) Config {
	return Config{
		Interval:  100 * time.Millisecond,
		StateFile: filepath.Join(t.TempDir(), "state.json"),
		AgentCredentials: controlplane.AgentCredentials{
			ClusterID: "cluster-1",
			AgentID:   "agent-1",
		},
	}
}

func TestReconciler_AppliesNewDeployment(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	client.deployments = []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "My App",
			ApplicationSlug: "my-app",
			Image:           "nginx:1.25",
			Replicas:        3,
			Revision:        1,
			Status:          "pending",
		},
	}

	cfg := testConfig(t)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(client, manager, cfg, log)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Run reconciler (will timeout after 500ms).
	_ = rec.Run(ctx)

	// Verify deployment was applied.
	assert.Len(t, manager.appliedSpecs, 1)
	assert.Equal(t, "my-app", manager.appliedSpecs[0].ResourceName())
	assert.Equal(t, "nginx:1.25", manager.appliedSpecs[0].Image)
	assert.Equal(t, int32(3), manager.appliedSpecs[0].Replicas)
}

func TestReconciler_UpdatesExistingDeployment(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	// Pre-populate existing deployment.
	manager.deployments["my-app"] = &k8s.DeploymentStatus{
		Name:     "my-app",
		Revision: "1",
	}

	client.deployments = []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-2",
			ApplicationName: "My App",
			ApplicationSlug: "my-app",
			Image:           "nginx:1.26", // Updated image
			Replicas:        5,            // Updated replicas
			Revision:        2,            // New revision
			Status:          "pending",
		},
	}

	cfg := testConfig(t)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(client, manager, cfg, log)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = rec.Run(ctx)

	// Verify deployment was updated.
	assert.Len(t, manager.appliedSpecs, 1)
	assert.Equal(t, "nginx:1.26", manager.appliedSpecs[0].Image)
	assert.Equal(t, int32(5), manager.appliedSpecs[0].Replicas)
	assert.Equal(t, 2, manager.appliedSpecs[0].Revision)
}

func TestReconciler_ReportsSuccessStatus(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	client.deployments = []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "My App",
			ApplicationSlug: "my-app",
			Image:           "nginx:1.25",
			Replicas:        3,
			Revision:        1,
			Status:          "pending",
		},
	}

	cfg := testConfig(t)
	cfg.Interval = 50 * time.Millisecond
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(client, manager, cfg, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run initial reconciliation.
	go rec.Run(ctx)

	// Wait for initial apply.
	time.Sleep(100 * time.Millisecond)

	// Mark deployment as ready.
	manager.MarkReady("my-app", 3)

	// Wait for next reconciliation cycle.
	time.Sleep(100 * time.Millisecond)

	cancel()

	// Verify success was reported.
	require.Contains(t, client.reportedStatuses, "rel-1")
	assert.Equal(t, "succeeded", client.reportedStatuses["rel-1"].Status)
}

func TestReconciler_ReportsFailureStatus(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	client.deployments = []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "My App",
			ApplicationSlug: "my-app",
			Image:           "nginx:1.25",
			Replicas:        3,
			Revision:        1,
			Status:          "pending",
		},
	}

	cfg := testConfig(t)
	cfg.Interval = 50 * time.Millisecond
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(client, manager, cfg, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go rec.Run(ctx)

	time.Sleep(100 * time.Millisecond)

	// Mark deployment as failed.
	manager.MarkFailed("my-app", "ImagePullBackOff", "Failed to pull image")

	time.Sleep(100 * time.Millisecond)

	cancel()

	// Verify failure was reported.
	require.Contains(t, client.reportedStatuses, "rel-1")
	assert.Equal(t, "failed", client.reportedStatuses["rel-1"].Status)
}

func TestReconciler_DeletesOrphanedDeployments(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	// Pre-populate an orphaned deployment.
	manager.deployments["orphaned-app"] = &k8s.DeploymentStatus{
		Name: "orphaned-app",
	}

	// No desired deployments.
	client.deployments = []controlplane.DesiredDeployment{}

	cfg := testConfig(t)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(client, manager, cfg, log)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = rec.Run(ctx)

	// Verify orphaned deployment was deleted.
	assert.Empty(t, manager.deployments)
}

func TestReconciler_SkipsDeletedDeployments(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	client.deployments = []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "My App",
			ApplicationSlug: "my-app",
			Image:           "nginx:1.25",
			Replicas:        3,
			Revision:        1,
			Status:          "deleted", // Marked as deleted
		},
	}

	cfg := testConfig(t)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(client, manager, cfg, log)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = rec.Run(ctx)

	// Verify no deployments were created.
	assert.Empty(t, manager.appliedSpecs)
}

func TestReconciler_Idempotent(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	client.deployments = []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "My App",
			ApplicationSlug: "my-app",
			Image:           "nginx:1.25",
			Replicas:        3,
			Revision:        1,
			Status:          "pending",
		},
	}

	cfg := testConfig(t)
	cfg.Interval = 50 * time.Millisecond
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(client, manager, cfg, log)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_ = rec.Run(ctx)

	// Multiple reconciliation cycles should not duplicate resources.
	// The first apply creates, subsequent ones should be no-ops.
	assert.Len(t, manager.deployments, 1)
}

func TestReconciler_PersistsState(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	client.deployments = []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "My App",
			ApplicationSlug: "my-app",
			Image:           "nginx:1.25",
			Replicas:        3,
			Revision:        1,
			Status:          "pending",
		},
	}

	stateFile := filepath.Join(t.TempDir(), "state.json")
	cfg := testConfig(t)
	cfg.StateFile = stateFile

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(client, manager, cfg, log)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Mark ready so state gets updated.
	manager.MarkReady("my-app", 3)

	_ = rec.Run(ctx)

	// Verify state file was created.
	_, err := os.Stat(stateFile)
	assert.NoError(t, err)

	// Verify state was saved.
	state := rec.State()
	assert.Equal(t, 1, state.AppliedRevisions["dep-1"])
}

func TestReconciler_HandlesAPIError(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	client.getError = &controlplane.APIError{StatusCode: 500, Message: "internal error"}

	cfg := testConfig(t)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(client, manager, cfg, log)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Should not panic on API error.
	_ = rec.Run(ctx)

	// No deployments should be applied.
	assert.Empty(t, manager.appliedSpecs)
}

func TestReconciler_ResourceSpecConversion(t *testing.T) {
	desired := controlplane.DesiredDeployment{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        3,
		Port:            intPtr(8080),
		EnvVars: []controlplane.EnvVar{
			{Name: "FOO", Value: "bar"},
			{Name: "BAZ", Value: "qux"},
		},
		ResourceRequests: &controlplane.ResourceSpec{
			CPU:    "100m",
			Memory: "128Mi",
		},
		ResourceLimits: &controlplane.ResourceSpec{
			CPU:    "500m",
			Memory: "512Mi",
		},
		Revision: 1,
		Status:   "pending",
	}

	spec := k8s.FromDesiredDeployment(desired)

	assert.Equal(t, "dep-1", spec.DeploymentID)
	assert.Equal(t, "rel-1", spec.ReleaseID)
	assert.Equal(t, "My App", spec.ApplicationName)
	assert.Equal(t, "my-app", spec.ApplicationSlug)
	assert.Equal(t, "nginx:1.25", spec.Image)
	assert.Equal(t, int32(3), spec.Replicas)
	assert.Equal(t, int32(8080), *spec.Port)
	assert.Len(t, spec.EnvVars, 2)
	assert.Equal(t, "FOO", spec.EnvVars[0].Name)
	assert.Equal(t, "bar", spec.EnvVars[0].Value)
	assert.NotNil(t, spec.Resources.Requests)
	assert.NotNil(t, spec.Resources.Limits)
}

func intPtr(i int) *int {
	return &i
}
