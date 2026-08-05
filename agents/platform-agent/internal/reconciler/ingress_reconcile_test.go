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

func deploymentWithIngresses(ings ...controlplane.DesiredIngress) controlplane.DesiredDeployment {
	return controlplane.DesiredDeployment{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        1,
		Revision:        1,
		Status:          "pending",
		Ingresses:       ings,
	}
}

func basicIngress(name, host string) controlplane.DesiredIngress {
	return controlplane.DesiredIngress{
		Name: name,
		Rules: []controlplane.DesiredIngressRule{{
			Host: host,
			Paths: []controlplane.DesiredIngressPath{{
				Path: "/", PathType: "Prefix", ServiceName: "web-svc", ServicePort: 80,
			}},
		}},
	}
}

// Integration: missing Ingress is created.
func TestReconcile_CreatesMissingIngress(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithIngresses(basicIngress("web", "app.example.com")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())

	rec.reconcile(context.Background())

	require.Contains(t, manager.ingresses, "web")
	assert.Equal(t, "app.example.com", manager.ingresses["web"].Rules[0].Host)
}

// Integration: existing, unchanged Ingress is a no-op (apply counter unchanged).
func TestReconcile_ExistingIngressNoOp(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithIngresses(basicIngress("web", "app.example.com")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())

	rec.reconcile(context.Background())
	applyAfterCreate := testutil.ToFloat64(metrics.IngressApply)

	rec.reconcile(context.Background())
	assert.Equal(t, applyAfterCreate, testutil.ToFloat64(metrics.IngressApply),
		"unchanged ingress must not count as an apply")
}

// Integration: updated Ingress triggers a drift-correcting update.
func TestReconcile_UpdatedIngressDrift(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithIngresses(basicIngress("web", "app.example.com")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())
	rec.reconcile(context.Background())

	driftBefore := testutil.ToFloat64(metrics.IngressDrift)
	// Supply fresh desired state (as the control plane would each poll) with a
	// changed host, rather than mutating the previously-applied slice in place.
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithIngresses(basicIngress("web", "new.example.com")),
	}
	rec.reconcile(context.Background())

	assert.Equal(t, "new.example.com", manager.ingresses["web"].Rules[0].Host)
	assert.Equal(t, driftBefore+1, testutil.ToFloat64(metrics.IngressDrift), "host change must record drift")
}

// Integration: Ingress removed from desired state is garbage collected.
func TestReconcile_GarbageCollectsOrphanIngress(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	manager.managedIngresses["orphan"] = true
	manager.ingresses["orphan"] = k8s.IngressSpec{Name: "orphan"}

	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithIngresses(basicIngress("keep", "app.example.com")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())

	rec.reconcile(context.Background())

	assert.Contains(t, manager.ingresses, "keep")
	assert.NotContains(t, manager.ingresses, "orphan")
	assert.Contains(t, manager.deletedIngresses, "orphan")
}

// Integration: multiple Ingresses for one deployment are all reconciled.
func TestReconcile_MultipleIngresses(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()

	var ings []controlplane.DesiredIngress
	for i := 0; i < 4; i++ {
		ings = append(ings, basicIngress(fmt.Sprintf("ing-%d", i), fmt.Sprintf("h%d.example.com", i)))
	}
	client.deployments = []controlplane.DesiredDeployment{deploymentWithIngresses(ings...)}
	rec := New(client, manager, testConfig(t), quietLogger())

	rec.reconcile(context.Background())

	assert.Len(t, manager.ingresses, 4)
}

// Concurrency: rapid re-reconciliation of identical desired state applies
// exactly once (create), then no duplicate applies.
func TestReconcile_NoDuplicateIngressApplyOnRapidCycles(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{
		deploymentWithIngresses(basicIngress("web", "app.example.com")),
	}
	rec := New(client, manager, testConfig(t), quietLogger())

	applyBefore := testutil.ToFloat64(metrics.IngressApply)
	for i := 0; i < 50; i++ {
		rec.reconcile(context.Background())
	}
	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.IngressApply)-applyBefore,
		"identical desired state must apply exactly once across many cycles")
}

// Backward compatibility: a deployment with no Ingresses touches no Ingress machinery.
func TestReconcile_NoIngressesIsNoOp(t *testing.T) {
	client := newFakeDeploymentClient()
	manager := newFakeResourceManager()
	client.deployments = []controlplane.DesiredDeployment{{
		DeploymentID: "dep-1", ReleaseID: "rel-1", ApplicationName: "My App",
		ApplicationSlug: "my-app", Image: "nginx:1.25", Replicas: 1, Revision: 1, Status: "pending",
	}}
	rec := New(client, manager, testConfig(t), quietLogger())

	rec.reconcile(context.Background())
	assert.Empty(t, manager.ingresses)
}
