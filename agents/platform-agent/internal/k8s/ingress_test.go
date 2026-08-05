package k8s

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
)

func ingSpec() IngressSpec {
	cls := "nginx"
	return IngressSpec{
		Name:             "web",
		Namespace:        "default",
		DeploymentID:     "dep-1",
		ApplicationName:  "My App",
		ApplicationSlug:  "my-app",
		IngressClassName: &cls,
		Rules: []controlplane.DesiredIngressRule{{
			Host: "app.example.com",
			Paths: []controlplane.DesiredIngressPath{{
				Path: "/", PathType: "Prefix", ServiceName: "web-svc", ServicePort: 80,
			}},
		}},
		Annotations: map[string]string{"nginx.ingress.kubernetes.io/rewrite-target": "/"},
	}
}

func TestApplyIngress_Create(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	res, err := m.ApplyIngress(context.Background(), ingSpec())
	require.NoError(t, err)
	assert.True(t, res.Created)

	ing, err := client.NetworkingV1().Ingresses("default").Get(context.Background(), "web", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, LabelManagedByValue, ing.Labels[LabelManagedBy])
	assert.Equal(t, "my-app", ing.Labels["app"])
	require.Len(t, ing.Spec.Rules, 1)
	assert.Equal(t, "app.example.com", ing.Spec.Rules[0].Host)
	assert.Equal(t, "nginx", *ing.Spec.IngressClassName)
	require.Len(t, ing.Spec.Rules[0].HTTP.Paths, 1)
	assert.Equal(t, "web-svc", ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name)
	assert.Equal(t, int32(80), ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Number)
}

func TestApplyIngress_DefaultsPathType(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	spec := ingSpec()
	spec.Rules[0].Paths[0].PathType = "" // should default to Prefix
	_, err := m.ApplyIngress(context.Background(), spec)
	require.NoError(t, err)

	ing, _ := client.NetworkingV1().Ingresses("default").Get(context.Background(), "web", metav1.GetOptions{})
	assert.Equal(t, networkingv1.PathTypePrefix, *ing.Spec.Rules[0].HTTP.Paths[0].PathType)
}

func TestApplyIngress_NoOpWhenUnchanged(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	_, err := m.ApplyIngress(context.Background(), ingSpec())
	require.NoError(t, err)

	res, err := m.ApplyIngress(context.Background(), ingSpec())
	require.NoError(t, err)
	assert.True(t, res.NoOp, "identical spec must be a no-op")
	assert.False(t, res.Updated)
}

func TestApplyIngress_RuleDrift(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")
	_, err := m.ApplyIngress(context.Background(), ingSpec())
	require.NoError(t, err)

	spec := ingSpec()
	spec.Rules[0].Paths[0].ServicePort = 8080 // changed backend port
	res, err := m.ApplyIngress(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.Updated, "backend port change is drift")

	ing, _ := client.NetworkingV1().Ingresses("default").Get(context.Background(), "web", metav1.GetOptions{})
	assert.Equal(t, int32(8080), ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Number)
}

func TestApplyIngress_AnnotationDrift(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")
	_, err := m.ApplyIngress(context.Background(), ingSpec())
	require.NoError(t, err)

	spec := ingSpec()
	spec.Annotations = map[string]string{"new": "annotation"}
	res, err := m.ApplyIngress(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.Updated, "annotation change is drift")
}

func TestApplyIngress_TLS(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	spec := ingSpec()
	spec.TLS = []controlplane.DesiredIngressTLS{{
		Hosts: []string{"app.example.com"}, SecretName: "web-tls",
	}}
	res, err := m.ApplyIngress(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.Created)

	ing, _ := client.NetworkingV1().Ingresses("default").Get(context.Background(), "web", metav1.GetOptions{})
	require.Len(t, ing.Spec.TLS, 1)
	assert.Equal(t, "web-tls", ing.Spec.TLS[0].SecretName)

	// Re-applying identical TLS is a no-op.
	res, err = m.ApplyIngress(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.NoOp)

	// Changing the TLS secret is drift.
	spec.TLS[0].SecretName = "web-tls-2"
	res, err = m.ApplyIngress(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.Updated)
}

func TestApplyIngress_MultipleRules(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	spec := ingSpec()
	spec.Rules = append(spec.Rules, controlplane.DesiredIngressRule{
		Host: "api.example.com",
		Paths: []controlplane.DesiredIngressPath{{
			Path: "/v1", PathType: "Prefix", ServiceName: "api-svc", ServicePort: 8080,
		}},
	})
	res, err := m.ApplyIngress(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.Created)

	ing, _ := client.NetworkingV1().Ingresses("default").Get(context.Background(), "web", metav1.GetOptions{})
	require.Len(t, ing.Spec.Rules, 2)

	// Idempotent across cycles.
	res, err = m.ApplyIngress(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.NoOp)
}

func TestApplyIngress_RefusesUnmanaged(t *testing.T) {
	client := fake.NewSimpleClientset(&networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"}, // no managed-by label
	})
	m := NewManager(client, "default")

	_, err := m.ApplyIngress(context.Background(), ingSpec())
	require.Error(t, err, "must refuse to update a user-owned ingress")
}

func TestDeleteIngress_OnlyManaged(t *testing.T) {
	managed := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "managed-ing", Namespace: "default",
			Labels: map[string]string{LabelManagedBy: LabelManagedByValue},
		},
	}
	userOwned := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "user-ing", Namespace: "default"},
	}
	client := fake.NewSimpleClientset(managed, userOwned)
	m := NewManager(client, "default")

	require.NoError(t, m.DeleteIngress(context.Background(), "managed-ing"))
	_, err := client.NetworkingV1().Ingresses("default").Get(context.Background(), "managed-ing", metav1.GetOptions{})
	assert.Error(t, err, "managed ingress should be deleted")

	require.Error(t, m.DeleteIngress(context.Background(), "user-ing"))
	_, err = client.NetworkingV1().Ingresses("default").Get(context.Background(), "user-ing", metav1.GetOptions{})
	assert.NoError(t, err, "user-owned ingress must not be deleted")
}

func TestDeleteIngress_AbsentIsSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")
	assert.NoError(t, m.DeleteIngress(context.Background(), "missing"))
}

func TestListManagedIngresses(t *testing.T) {
	client := fake.NewSimpleClientset(
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
			Name: "ing-a", Namespace: "default",
			Labels: map[string]string{LabelManagedBy: LabelManagedByValue},
		}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "user-ing", Namespace: "default"}},
	)
	m := NewManager(client, "default")

	names, err := m.ListManagedIngresses(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ing-a"}, names)
}

func TestFromDesiredIngress_NamespaceFallback(t *testing.T) {
	d := controlplane.DesiredDeployment{
		DeploymentID: "dep-9", ApplicationName: "App", ApplicationSlug: "app", Namespace: "ns-1",
	}
	spec := FromDesiredIngress(d, controlplane.DesiredIngress{Name: "web"})
	assert.Equal(t, "ns-1", spec.Namespace, "empty namespace should fall back to deployment namespace")
}

// TestApplyIngress_ConcurrentDistinct exercises concurrent applies to distinct
// Ingresses. Run under `go test -race` to detect data races.
func TestApplyIngress_ConcurrentDistinct(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	const n = 25
	errCh := make(chan error, n)
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			spec := ingSpec()
			spec.Name = fmt.Sprintf("ing-%d", i)
			_, err := m.ApplyIngress(context.Background(), spec)
			if err != nil {
				errCh <- err
			}
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < n; i++ {
		<-done
	}
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent apply failed: %v", err)
	}

	list, err := m.ListManagedIngresses(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, n)
}
