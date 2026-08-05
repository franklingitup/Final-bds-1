package deployment

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/argocd"
)

type monitorTestEnv struct {
	monitor  *ArgoMonitor
	argoApps *fakeArgoAppStore
	rels     *fakeReleaseStore
	rollouts *fakeRolloutStore
	outbox   *fakeOutbox
	client   *fakeArgoClient
}

func newMonitorTestEnv() *monitorTestEnv {
	argoApps := newFakeArgoAppStore()
	rels := newFakeReleaseStore()
	rollouts := newFakeRolloutStore()
	outbox := &fakeOutbox{}
	client := &fakeArgoClient{}

	m := NewArgoMonitor(ArgoMonitorDeps{
		Client:   client,
		ArgoApps: argoApps,
		Releases: rels,
		Rollouts: rollouts,
		Outbox:   outbox,
		Tenant:   &fakeTenant{},
	})
	return &monitorTestEnv{monitor: m, argoApps: argoApps, rels: rels, rollouts: rollouts, outbox: outbox, client: client}
}

// seedBinding persists an argo_applications row and a release for a deployment.
func (e *monitorTestEnv) seedBinding(orgID, depID, appName string) string {
	_ = e.argoApps.Upsert(context.Background(), &ArgoApplication{
		DeploymentID:  depID,
		OrgID:         orgID,
		AppName:       appName,
		DestServer:    "https://kubernetes.default.svc",
		DestNamespace: "ns",
		SyncStatus:    argocd.SyncStatusUnknown,
		HealthStatus:  argocd.HealthStatusUnknown,
	})
	rel := &Release{OrgID: orgID, DeploymentID: depID, Revision: 1, Image: "nginx:1", Status: ReleaseStatusSucceeded}
	_ = e.rels.Create(context.Background(), rel)
	return rel.ID
}

// argoAppWithStatus builds a listed Application carrying identity labels.
func argoAppWithStatus(orgID, depID, appName, sync, health, phase string) argocd.Application {
	return argocd.Application{
		Metadata: argocd.ObjectMeta{
			Name: appName,
			Labels: map[string]string{
				LabelManagedBy:    ManagedByValue,
				LabelOrgID:        orgID,
				LabelDeploymentID: depID,
			},
		},
		Status: argocd.ApplicationStatus{
			Sync:           argocd.SyncStatus{Status: sync, Revision: "rev-1"},
			Health:         argocd.HealthStatus{Status: health},
			OperationState: &argocd.OperationState{Phase: phase},
		},
	}
}

func countEvents(outbox *fakeOutbox, eventType string) int {
	n := 0
	for _, ev := range outbox.events {
		if ev.Type == eventType {
			n++
		}
	}
	return n
}

func TestMonitor_SyncStartedAndCompleted(t *testing.T) {
	e := newMonitorTestEnv()
	orgID := uuid.NewString()
	depID := uuid.NewString()
	relID := e.seedBinding(orgID, depID, "app-1")

	// First observation: operation running -> sync.started.
	e.client.listApps = []argocd.Application{
		argoAppWithStatus(orgID, depID, "app-1", argocd.SyncStatusOutOfSync, argocd.HealthStatusProgressing, argocd.OperationRunning),
	}
	e.monitor.cycle(context.Background())
	assert.Equal(t, 1, countEvents(e.outbox, EventDeploymentSyncStarted))

	// Rollout persisted as RollingOut.
	rs, err := e.rollouts.Get(context.Background(), depID, relID)
	require.NoError(t, err)
	assert.Equal(t, RolloutPhaseRollingOut, rs.Phase)

	// Second observation: operation succeeded + healthy -> sync.completed + health.changed.
	e.client.listApps = []argocd.Application{
		argoAppWithStatus(orgID, depID, "app-1", argocd.SyncStatusSynced, argocd.HealthStatusHealthy, argocd.OperationSucceeded),
	}
	e.monitor.cycle(context.Background())
	assert.Equal(t, 1, countEvents(e.outbox, EventDeploymentSyncCompleted))
	assert.GreaterOrEqual(t, countEvents(e.outbox, EventDeploymentHealthChanged), 1)

	rs, err = e.rollouts.Get(context.Background(), depID, relID)
	require.NoError(t, err)
	assert.Equal(t, RolloutPhaseHealthy, rs.Phase)
}

func TestMonitor_SyncFailed(t *testing.T) {
	e := newMonitorTestEnv()
	orgID := uuid.NewString()
	depID := uuid.NewString()
	e.seedBinding(orgID, depID, "app-1")

	e.client.listApps = []argocd.Application{
		argoAppWithStatus(orgID, depID, "app-1", argocd.SyncStatusOutOfSync, argocd.HealthStatusDegraded, argocd.OperationFailed),
	}
	e.monitor.cycle(context.Background())
	assert.Equal(t, 1, countEvents(e.outbox, EventDeploymentSyncFailed))
}

func TestMonitor_DriftDetected(t *testing.T) {
	e := newMonitorTestEnv()
	orgID := uuid.NewString()
	depID := uuid.NewString()
	e.seedBinding(orgID, depID, "app-1")

	// Healthy but OutOfSync -> drift.
	e.client.listApps = []argocd.Application{
		argoAppWithStatus(orgID, depID, "app-1", argocd.SyncStatusOutOfSync, argocd.HealthStatusHealthy, ""),
	}
	e.monitor.cycle(context.Background())
	assert.Equal(t, 1, countEvents(e.outbox, EventDeploymentDriftDetected))

	// A second identical observation must not re-emit drift (edge-triggered).
	e.monitor.cycle(context.Background())
	assert.Equal(t, 1, countEvents(e.outbox, EventDeploymentDriftDetected))
}

func TestMonitor_IdempotentNoTransition(t *testing.T) {
	e := newMonitorTestEnv()
	orgID := uuid.NewString()
	depID := uuid.NewString()
	e.seedBinding(orgID, depID, "app-1")

	app := argoAppWithStatus(orgID, depID, "app-1", argocd.SyncStatusSynced, argocd.HealthStatusHealthy, argocd.OperationSucceeded)
	e.client.listApps = []argocd.Application{app}

	e.monitor.cycle(context.Background())
	completedAfterFirst := countEvents(e.outbox, EventDeploymentSyncCompleted)

	// Same status again: no new sync.completed event.
	e.monitor.cycle(context.Background())
	assert.Equal(t, completedAfterFirst, countEvents(e.outbox, EventDeploymentSyncCompleted))
}

func TestMonitor_SkipsUnmanagedAndUnknown(t *testing.T) {
	e := newMonitorTestEnv()
	orgID := uuid.NewString()
	depID := uuid.NewString()
	e.seedBinding(orgID, depID, "app-1")

	// One app is missing identity labels; another references an untracked deployment.
	noLabels := argocd.Application{Metadata: argocd.ObjectMeta{Name: "x"}}
	untracked := argoAppWithStatus(orgID, "unknown-dep", "app-x", argocd.SyncStatusSynced, argocd.HealthStatusHealthy, argocd.OperationSucceeded)
	e.client.listApps = []argocd.Application{noLabels, untracked}

	// Must not panic or emit spurious events for the tracked binding.
	e.monitor.cycle(context.Background())
	assert.Equal(t, 0, countEvents(e.outbox, EventDeploymentSyncCompleted))
}

func TestMonitor_MultipleApplications(t *testing.T) {
	e := newMonitorTestEnv()
	orgA := uuid.NewString()
	orgB := uuid.NewString()
	depA := uuid.NewString()
	depB := uuid.NewString()
	e.seedBinding(orgA, depA, "app-a")
	e.seedBinding(orgB, depB, "app-b")

	e.client.listApps = []argocd.Application{
		argoAppWithStatus(orgA, depA, "app-a", argocd.SyncStatusSynced, argocd.HealthStatusHealthy, argocd.OperationSucceeded),
		argoAppWithStatus(orgB, depB, "app-b", argocd.SyncStatusOutOfSync, argocd.HealthStatusDegraded, argocd.OperationFailed),
	}
	e.monitor.cycle(context.Background())
	assert.Equal(t, 1, countEvents(e.outbox, EventDeploymentSyncCompleted))
	assert.Equal(t, 1, countEvents(e.outbox, EventDeploymentSyncFailed))
}

func TestMonitor_ListErrorIsNonFatal(t *testing.T) {
	e := newMonitorTestEnv()
	e.client.listErr = &argocd.APIError{StatusCode: 503, Message: "unavailable"}
	// Should return without panicking and emit nothing.
	e.monitor.cycle(context.Background())
	assert.Empty(t, e.outbox.events)
}

func TestMonitor_StartStop(t *testing.T) {
	e := newMonitorTestEnv()
	e.monitor.interval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	e.monitor.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()
	e.monitor.Stop() // must return promptly
}
