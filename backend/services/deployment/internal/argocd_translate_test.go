package deployment

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdsplatform/platform/backend/libs/argocd"
)

func TestBuildArgoApplication_Directory(t *testing.T) {
	rec := &ArgoApplication{
		DeploymentID:   "dep-1",
		OrgID:          "org-1",
		AppName:        "my-app-abc123",
		Project:        "default",
		RepoURL:        "https://github.com/acme/config",
		Path:           "envs/prod",
		TargetRevision: "main",
		SourceType:     SourceTypeDirectory,
		DestServer:     "https://kubernetes.default.svc",
		DestNamespace:  "my-app",
		AutoSync:       true,
		SelfHeal:       true,
		Prune:          true,
	}

	app := buildArgoApplication(rec)

	assert.Equal(t, "my-app-abc123", app.Metadata.Name)
	assert.Equal(t, ManagedByValue, app.Metadata.Labels[LabelManagedBy])
	assert.Equal(t, "org-1", app.Metadata.Labels[LabelOrgID])
	assert.Equal(t, "dep-1", app.Metadata.Labels[LabelDeploymentID])
	assert.Contains(t, app.Metadata.Finalizers, argocd.ResourcesFinalizer)

	assert.Equal(t, "https://github.com/acme/config", app.Spec.Source.RepoURL)
	assert.Equal(t, "envs/prod", app.Spec.Source.Path)
	assert.Equal(t, "main", app.Spec.Source.TargetRevision)
	assert.NotNil(t, app.Spec.Source.Directory)
	assert.Nil(t, app.Spec.Source.Helm)

	assert.Equal(t, "my-app", app.Spec.Destination.Namespace)
	assert.Equal(t, "https://kubernetes.default.svc", app.Spec.Destination.Server)

	if assert.NotNil(t, app.Spec.SyncPolicy) && assert.NotNil(t, app.Spec.SyncPolicy.Automated) {
		assert.True(t, app.Spec.SyncPolicy.Automated.Prune)
		assert.True(t, app.Spec.SyncPolicy.Automated.SelfHeal)
	}
	// Replica drift is ignored so autoscaling does not read as config drift.
	assert.Len(t, app.Spec.IgnoreDifferences, 1)
}

func TestBuildArgoApplication_HelmAndNoAutoSync(t *testing.T) {
	rec := &ArgoApplication{
		AppName:    "svc-1",
		SourceType: SourceTypeHelm,
		AutoSync:   false,
	}
	app := buildArgoApplication(rec)
	assert.NotNil(t, app.Spec.Source.Helm)
	assert.Nil(t, app.Spec.Source.Directory)
	assert.Nil(t, app.Spec.SyncPolicy, "no automated sync policy when auto-sync is disabled")
}

func TestBuildArgoApplication_Kustomize(t *testing.T) {
	app := buildArgoApplication(&ArgoApplication{AppName: "k", SourceType: SourceTypeKustomize, AutoSync: true})
	assert.NotNil(t, app.Spec.Source.Kustomize)
}

func TestArgoStatusToRolloutPhase(t *testing.T) {
	cases := []struct {
		name         string
		sync, health string
		phase        string
		want         string
	}{
		{"synced+healthy", argocd.SyncStatusSynced, argocd.HealthStatusHealthy, argocd.OperationSucceeded, RolloutPhaseHealthy},
		{"operation running", argocd.SyncStatusOutOfSync, argocd.HealthStatusProgressing, argocd.OperationRunning, RolloutPhaseRollingOut},
		{"operation failed", argocd.SyncStatusOutOfSync, argocd.HealthStatusProgressing, argocd.OperationFailed, RolloutPhaseFailed},
		{"degraded", argocd.SyncStatusSynced, argocd.HealthStatusDegraded, "", RolloutPhaseFailed},
		{"progressing health", argocd.SyncStatusSynced, argocd.HealthStatusProgressing, "", RolloutPhaseRollingOut},
		{"missing outofsync", argocd.SyncStatusOutOfSync, argocd.HealthStatusMissing, "", RolloutPhasePending},
		{"missing but synced", argocd.SyncStatusSynced, argocd.HealthStatusMissing, "", RolloutPhaseReconciling},
		{"healthy but drifted", argocd.SyncStatusOutOfSync, argocd.HealthStatusHealthy, "", RolloutPhaseReconciling},
		{"suspended", argocd.SyncStatusSynced, argocd.HealthStatusSuspended, "", RolloutPhaseReconciling},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := argoStatusToRolloutPhase(tc.sync, tc.health, tc.phase)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestApplyObservedDetectsChange(t *testing.T) {
	rec := &ArgoApplication{SyncStatus: argocd.SyncStatusSynced, HealthStatus: argocd.HealthStatusHealthy}
	app := &argocd.Application{Status: argocd.ApplicationStatus{
		Sync:   argocd.SyncStatus{Status: argocd.SyncStatusSynced, Revision: "abc"},
		Health: argocd.HealthStatus{Status: argocd.HealthStatusHealthy},
	}}
	// Revision differs but tracked fields (status/health/phase/synced-rev/drift):
	// synced revision changed from "" to "abc" -> change.
	assert.True(t, rec.applyObserved(app))
	assert.Equal(t, "abc", rec.SyncedRevision)

	// Re-applying identical status is a no-op.
	assert.False(t, rec.applyObserved(app))
}

func TestApplyObservedDrift(t *testing.T) {
	rec := &ArgoApplication{}
	app := &argocd.Application{Status: argocd.ApplicationStatus{
		Sync:   argocd.SyncStatus{Status: argocd.SyncStatusOutOfSync},
		Health: argocd.HealthStatus{Status: argocd.HealthStatusHealthy},
	}}
	rec.applyObserved(app)
	assert.True(t, rec.Drift, "OutOfSync must set drift")
}

func TestArgoAppName(t *testing.T) {
	assert.Equal(t, "my-app-abcdef12", argoAppName("my-app", "abcdef12-3456-7890-abcd-ef1234567890"))
	assert.Equal(t, "app-", argoAppName("", ""))
	assert.Equal(t, "svc-1234", argoAppName("SVC", "1234"))
}
