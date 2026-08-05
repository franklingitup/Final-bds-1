package reconciler

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/k8s"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/rollout"
)

func progressLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func desired(revision int) controlplane.DesiredDeployment {
	return controlplane.DesiredDeployment{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:2",
		Replicas:        3,
		Revision:        revision,
		Status:          "pending",
	}
}

func TestReportProgress_RollingOut(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{desired(1)}
	manager.deployments["my-app"] = &k8s.DeploymentStatus{
		Name: "my-app", Revision: "1", DesiredReplicas: 3,
		ReadyReplicas: 1, UpdatedReplicas: 3, AvailableReplicas: 1, UnavailableReplicas: 2,
		Generation: 1, ObservedGeneration: 1,
	}

	rec := New(client, manager, testConfig(t), progressLogger())
	rec.reconcile(context.Background())

	last, ok := client.lastProgress("rel-1")
	require.True(t, ok, "expected a progress report")
	assert.Equal(t, string(rollout.PhaseRollingOut), last.Phase)
	assert.Equal(t, 33, last.RolloutPercentage)
	assert.Equal(t, 1, last.ReadyReplicas)
	assert.Equal(t, 3, last.DesiredReplicas)
	assert.False(t, last.Timeout)
}

func TestReportProgress_Healthy(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{desired(1)}
	manager.deployments["my-app"] = &k8s.DeploymentStatus{
		Name: "my-app", Revision: "1", DesiredReplicas: 3,
		ReadyReplicas: 3, UpdatedReplicas: 3, AvailableReplicas: 3,
		Generation: 1, ObservedGeneration: 1, Ready: true,
	}

	rec := New(client, manager, testConfig(t), progressLogger())
	rec.reconcile(context.Background())

	last, ok := client.lastProgress("rel-1")
	require.True(t, ok)
	assert.Equal(t, string(rollout.PhaseHealthy), last.Phase)
	assert.Equal(t, 100, last.RolloutPercentage)
}

func TestReportProgress_Timeout(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{desired(1)}
	manager.deployments["my-app"] = &k8s.DeploymentStatus{
		Name: "my-app", Revision: "1", DesiredReplicas: 3,
		ReadyReplicas: 1, UpdatedReplicas: 3, AvailableReplicas: 1,
		Generation: 1, ObservedGeneration: 1,
		Failed: true, ProgressDeadlineExceeded: true, FailureMessage: "progress deadline exceeded",
	}

	rec := New(client, manager, testConfig(t), progressLogger())
	rec.reconcile(context.Background())

	last, ok := client.lastProgress("rel-1")
	require.True(t, ok)
	assert.Equal(t, string(rollout.PhaseFailed), last.Phase)
	assert.True(t, last.Timeout)
	assert.Equal(t, "progress deadline exceeded", last.ErrorMessage)
}

func TestReportProgress_PodImagePullBackOff(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{desired(1)}
	manager.deployments["my-app"] = &k8s.DeploymentStatus{
		Name: "my-app", Revision: "1", DesiredReplicas: 3,
		Generation: 1, ObservedGeneration: 1,
	}
	manager.podHealth = map[string]*k8s.PodHealth{
		"my-app": {Total: 3, Issue: "ImagePullBackOff", Message: "container app in pod p1: back-off pulling image"},
	}

	rec := New(client, manager, testConfig(t), progressLogger())
	rec.reconcile(context.Background())

	last, ok := client.lastProgress("rel-1")
	require.True(t, ok)
	assert.Equal(t, string(rollout.PhaseFailed), last.Phase)
	assert.False(t, last.Timeout)
	assert.Contains(t, last.ErrorMessage, "back-off pulling image")
}

func TestReportProgress_Throttled(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{desired(1)}
	manager.deployments["my-app"] = &k8s.DeploymentStatus{
		Name: "my-app", Revision: "1", DesiredReplicas: 3,
		ReadyReplicas: 1, UpdatedReplicas: 3, AvailableReplicas: 1,
		Generation: 1, ObservedGeneration: 1,
	}

	rec := New(client, manager, testConfig(t), progressLogger())
	rec.reconcile(context.Background())
	rec.reconcile(context.Background()) // identical state -> no new report

	assert.Len(t, client.reportedProgress["rel-1"], 1, "unchanged state must not re-report progress")
}

func TestReportProgress_ConcurrentDeployments(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	client.deployments = []controlplane.DesiredDeployment{
		{DeploymentID: "dep-1", ReleaseID: "rel-1", ApplicationName: "A", ApplicationSlug: "app-a", Image: "a:1", Replicas: 2, Revision: 1, Status: "pending"},
		{DeploymentID: "dep-2", ReleaseID: "rel-2", ApplicationName: "B", ApplicationSlug: "app-b", Image: "b:1", Replicas: 2, Revision: 1, Status: "pending"},
	}
	manager.deployments["app-a"] = &k8s.DeploymentStatus{Name: "app-a", Revision: "1", DesiredReplicas: 2, ReadyReplicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2, Generation: 1, ObservedGeneration: 1, Ready: true}
	manager.deployments["app-b"] = &k8s.DeploymentStatus{Name: "app-b", Revision: "1", DesiredReplicas: 2, ReadyReplicas: 0, UpdatedReplicas: 0, AvailableReplicas: 0, Generation: 1, ObservedGeneration: 1}

	rec := New(client, manager, testConfig(t), progressLogger())
	rec.reconcile(context.Background())

	a, ok := client.lastProgress("rel-1")
	require.True(t, ok)
	assert.Equal(t, string(rollout.PhaseHealthy), a.Phase)

	b, ok := client.lastProgress("rel-2")
	require.True(t, ok)
	assert.Equal(t, string(rollout.PhaseReconciling), b.Phase)
}
