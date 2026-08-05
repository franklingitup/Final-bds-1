package reconciler

import (
	"context"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/k8s"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/metrics"
)

func deploymentWithConfigMaps(cms ...controlplane.DesiredConfigMap) controlplane.DesiredDeployment {
	return controlplane.DesiredDeployment{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        1,
		Revision:        1,
		Status:          "pending",
		ConfigMaps:      cms,
	}
}

func newCMReconciler(t *testing.T, client *fakeDeploymentClient, manager *fakeResourceManager) *Reconciler {
	t.Helper()
	return New(client, manager, testConfig(t), quietLogger())
}

// Integration: missing ConfigMap is created.
func TestReconcile_CreatesMissingConfigMap(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithConfigMaps(controlplane.DesiredConfigMap{
			Name: "app-config", Data: map[string]string{"K": "v"},
		}),
	}
	rec := newCMReconciler(t, client, manager)

	rec.reconcile(context.Background())

	require.Contains(t, manager.configMaps, "app-config")
	assert.Equal(t, "v", manager.configMaps["app-config"].Data["K"])
}

// Integration: existing, unchanged ConfigMap is a no-op (apply_total unchanged
// on the second cycle).
func TestReconcile_ExistingConfigMapNoOp(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithConfigMaps(controlplane.DesiredConfigMap{
			Name: "app-config", Data: map[string]string{"K": "v"},
		}),
	}
	rec := newCMReconciler(t, client, manager)

	rec.reconcile(context.Background()) // create
	applyAfterCreate := testutil.ToFloat64(metrics.ConfigMapApply)

	rec.reconcile(context.Background()) // no-op
	applyAfterNoop := testutil.ToFloat64(metrics.ConfigMapApply)

	assert.Equal(t, applyAfterCreate, applyAfterNoop, "unchanged configmap must not count as an apply")
}

// Integration: modified ConfigMap triggers a drift-correcting update.
func TestReconcile_ModifiedConfigMapDrift(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithConfigMaps(controlplane.DesiredConfigMap{
			Name: "app-config", Data: map[string]string{"K": "v1"},
		}),
	}
	rec := newCMReconciler(t, client, manager)
	rec.reconcile(context.Background()) // create

	driftBefore := testutil.ToFloat64(metrics.ConfigMapDrift)

	client.deployments[0].ConfigMaps[0].Data = map[string]string{"K": "v2"}
	rec.reconcile(context.Background()) // update

	assert.Equal(t, "v2", manager.configMaps["app-config"].Data["K"])
	assert.Equal(t, driftBefore+1, testutil.ToFloat64(metrics.ConfigMapDrift), "drift must be recorded")
}

// Integration: ConfigMap removed from desired state is garbage collected.
func TestReconcile_GarbageCollectsOrphanConfigMap(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	// A managed configmap exists in the cluster but is no longer desired.
	manager.managedConfigMaps["orphan-config"] = true
	manager.configMaps["orphan-config"] = k8s.ConfigMapSpec{Name: "orphan-config"}

	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithConfigMaps(controlplane.DesiredConfigMap{
			Name: "keep-config", Data: map[string]string{"K": "v"},
		}),
	}
	rec := newCMReconciler(t, client, manager)

	rec.reconcile(context.Background())

	assert.Contains(t, manager.configMaps, "keep-config", "desired configmap kept")
	assert.NotContains(t, manager.configMaps, "orphan-config", "orphan configmap deleted")
	assert.Contains(t, manager.deletedConfigMaps, "orphan-config")
}

// Integration: multiple ConfigMaps for one deployment are all reconciled.
func TestReconcile_MultipleConfigMaps(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	var cms []controlplane.DesiredConfigMap
	for i := 0; i < 5; i++ {
		cms = append(cms, controlplane.DesiredConfigMap{
			Name: fmt.Sprintf("cfg-%d", i),
			Data: map[string]string{"i": fmt.Sprintf("%d", i)},
		})
	}
	client.deployments = []controlplane.DesiredDeployment{deploymentWithConfigMaps(cms...)}
	rec := newCMReconciler(t, client, manager)

	rec.reconcile(context.Background())

	assert.Len(t, manager.configMaps, 5)
	for i := 0; i < 5; i++ {
		assert.Contains(t, manager.configMaps, fmt.Sprintf("cfg-%d", i))
	}
}

// Concurrency: rapid re-reconciliation of identical desired state performs
// exactly one apply (create), then no duplicate applies.
func TestReconcile_NoDuplicateApplyOnRapidCycles(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithConfigMaps(controlplane.DesiredConfigMap{
			Name: "app-config", Data: map[string]string{"K": "v"},
		}),
	}
	rec := newCMReconciler(t, client, manager)

	applyBefore := testutil.ToFloat64(metrics.ConfigMapApply)
	for i := 0; i < 50; i++ {
		rec.reconcile(context.Background())
	}
	applyDelta := testutil.ToFloat64(metrics.ConfigMapApply) - applyBefore

	assert.Equal(t, float64(1), applyDelta, "identical desired state must apply exactly once across many cycles")
}

// Backward compatibility: a deployment with no ConfigMaps reconciles exactly as
// before and touches no configmap machinery.
func TestReconcile_NoConfigMapsIsNoOp(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        1,
		Revision:        1,
		Status:          "pending",
	}}
	rec := newCMReconciler(t, client, manager)

	rec.reconcile(context.Background())
	assert.Empty(t, manager.configMaps, "no configmaps should be applied when none are desired")
}
