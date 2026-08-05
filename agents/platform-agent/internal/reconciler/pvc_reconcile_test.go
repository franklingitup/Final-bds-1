package reconciler

import (
	"context"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/k8s"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/metrics"
)

func mustQty(s string) resource.Quantity { return resource.MustParse(s) }

func deploymentWithPVCs(pvcs ...controlplane.DesiredPVC) controlplane.DesiredDeployment {
	return controlplane.DesiredDeployment{
		DeploymentID:           "dep-1",
		ReleaseID:              "rel-1",
		ApplicationName:        "My App",
		ApplicationSlug:        "my-app",
		Image:                  "nginx:1.25",
		Replicas:               1,
		Revision:               1,
		Status:                 "pending",
		PersistentVolumeClaims: pvcs,
	}
}

func basicPVC(name, storage string) controlplane.DesiredPVC {
	return controlplane.DesiredPVC{
		Name:           name,
		AccessModes:    []string{"ReadWriteOnce"},
		StorageRequest: storage,
	}
}

// Integration: missing PVC is created.
func TestReconcile_CreatesMissingPVC(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithPVCs(basicPVC("data", "10Gi")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())

	rec.reconcile(context.Background())

	require.Contains(t, manager.pvcs, "data")
}

// Integration: existing, unchanged PVC is a no-op (apply counter unchanged).
func TestReconcile_ExistingPVCNoOp(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithPVCs(basicPVC("data", "10Gi")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())

	rec.reconcile(context.Background())
	applyAfterCreate := testutil.ToFloat64(metrics.PVCApply)

	rec.reconcile(context.Background())
	assert.Equal(t, applyAfterCreate, testutil.ToFloat64(metrics.PVCApply),
		"unchanged pvc must not count as an apply")
}

// Integration: expanded PVC triggers a drift-correcting update.
func TestReconcile_ExpandedPVCDrift(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithPVCs(basicPVC("data", "10Gi")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())
	rec.reconcile(context.Background())

	driftBefore := testutil.ToFloat64(metrics.PVCDrift)
	client.deployments[0].PersistentVolumeClaims[0].StorageRequest = "20Gi"
	rec.reconcile(context.Background())

	gotQty := manager.pvcs["data"].StorageRequest
	assert.Equal(t, 0, gotQty.Cmp(mustQty("20Gi")))
	assert.Equal(t, driftBefore+1, testutil.ToFloat64(metrics.PVCDrift), "expansion must record drift")
}

// Integration: immutable field change is skipped and metered, not applied.
func TestReconcile_ImmutableChangeSkipped(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	manager.pvcImmutable["data"] = true // force the manager to report immutable-skip
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithPVCs(basicPVC("data", "10Gi")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())

	immutableBefore := testutil.ToFloat64(metrics.PVCImmutableChange)
	applyBefore := testutil.ToFloat64(metrics.PVCApply)

	rec.reconcile(context.Background())

	assert.Equal(t, immutableBefore+1, testutil.ToFloat64(metrics.PVCImmutableChange),
		"immutable change must be metered")
	assert.Equal(t, applyBefore, testutil.ToFloat64(metrics.PVCApply),
		"immutable change must not count as an apply")
}

// Integration: PVC removed from desired state is garbage collected.
func TestReconcile_GarbageCollectsOrphanPVC(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	manager.managedPVCs["orphan"] = true
	manager.pvcs["orphan"] = k8s.PVCSpec{Name: "orphan"}

	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithPVCs(basicPVC("keep", "10Gi")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())

	rec.reconcile(context.Background())

	assert.Contains(t, manager.pvcs, "keep")
	assert.NotContains(t, manager.pvcs, "orphan")
	assert.Contains(t, manager.deletedPVCs, "orphan")
}

// Integration: multiple PVCs for one deployment are all reconciled.
func TestReconcile_MultiplePVCs(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	var pvcs []controlplane.DesiredPVC
	for i := 0; i < 4; i++ {
		pvcs = append(pvcs, basicPVC(fmt.Sprintf("vol-%d", i), "5Gi"))
	}
	client.deployments = []controlplane.DesiredDeployment{deploymentWithPVCs(pvcs...)}
	rec := New(client, manager, testConfig(t), quietLogger())

	rec.reconcile(context.Background())

	assert.Len(t, manager.pvcs, 4)
}

// Concurrency: rapid re-reconciliation of identical desired state applies
// exactly once (create), then no duplicate applies.
func TestReconcile_NoDuplicatePVCApplyOnRapidCycles(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithPVCs(basicPVC("data", "10Gi")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())

	applyBefore := testutil.ToFloat64(metrics.PVCApply)
	for i := 0; i < 50; i++ {
		rec.reconcile(context.Background())
	}
	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.PVCApply)-applyBefore,
		"identical desired state must apply exactly once across many cycles")
}

// Backward compatibility: a deployment with no PVCs touches no PVC machinery.
func TestReconcile_NoPVCsIsNoOp(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{{
		DeploymentID: "dep-1", ReleaseID: "rel-1", ApplicationName: "My App",
		ApplicationSlug: "my-app", Image: "nginx:1.25", Replicas: 1, Revision: 1, Status: "pending",
	}}
	rec := New(client, manager, testConfig(t), quietLogger())

	rec.reconcile(context.Background())
	assert.Empty(t, manager.pvcs)
}
