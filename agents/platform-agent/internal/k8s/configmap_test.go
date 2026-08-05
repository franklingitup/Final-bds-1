package k8s

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
)

func cmSpec() ConfigMapSpec {
	return ConfigMapSpec{
		Name:            "app-config",
		Namespace:       "default",
		DeploymentID:    "dep-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Data:            map[string]string{"KEY": "value"},
		BinaryData:      map[string][]byte{"blob": []byte{0x01, 0x02}},
		Annotations:     map[string]string{"bdsplatform.io/release-id": "rel-1"},
	}
}

func TestApplyConfigMap_Create(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	res, err := m.ApplyConfigMap(context.Background(), cmSpec())
	require.NoError(t, err)
	assert.True(t, res.Created)

	cm, err := client.CoreV1().ConfigMaps("default").Get(context.Background(), "app-config", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "value", cm.Data["KEY"])
	assert.Equal(t, []byte{0x01, 0x02}, cm.BinaryData["blob"])
	// Platform ownership labels applied.
	assert.Equal(t, LabelManagedByValue, cm.Labels[LabelManagedBy])
	assert.Equal(t, "dep-1", cm.Labels[LabelDeploymentID])
	assert.Equal(t, "my-app", cm.Labels["app"])
	assert.Equal(t, "rel-1", cm.Annotations["bdsplatform.io/release-id"])
}

func TestApplyConfigMap_NoOpWhenUnchanged(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	_, err := m.ApplyConfigMap(context.Background(), cmSpec())
	require.NoError(t, err)

	res, err := m.ApplyConfigMap(context.Background(), cmSpec())
	require.NoError(t, err)
	assert.True(t, res.NoOp, "re-applying identical spec must be a no-op (no drift)")
	assert.False(t, res.Updated)
}

func TestApplyConfigMap_UpdateOnDataDrift(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	_, err := m.ApplyConfigMap(context.Background(), cmSpec())
	require.NoError(t, err)

	spec := cmSpec()
	spec.Data = map[string]string{"KEY": "changed"}
	res, err := m.ApplyConfigMap(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.Updated, "data change must trigger update")

	cm, _ := client.CoreV1().ConfigMaps("default").Get(context.Background(), "app-config", metav1.GetOptions{})
	assert.Equal(t, "changed", cm.Data["KEY"])
}

func TestApplyConfigMap_DriftDetectionMatrix(t *testing.T) {
	cases := map[string]func(s *ConfigMapSpec){
		"data":        func(s *ConfigMapSpec) { s.Data = map[string]string{"KEY": "other"} },
		"binaryData":  func(s *ConfigMapSpec) { s.BinaryData = map[string][]byte{"blob": {0x09}} },
		"labels":      func(s *ConfigMapSpec) { s.Labels = map[string]string{"extra": "x"} },
		"annotations": func(s *ConfigMapSpec) { s.Annotations = map[string]string{"a": "b"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			m := NewManager(client, "default")
			_, err := m.ApplyConfigMap(context.Background(), cmSpec())
			require.NoError(t, err)

			spec := cmSpec()
			mutate(&spec)
			res, err := m.ApplyConfigMap(context.Background(), spec)
			require.NoError(t, err)
			assert.True(t, res.Updated, "drift in %s must trigger update", name)
		})
	}
}

// TestApplyConfigMap_IgnoresServerMetadata verifies that server-managed metadata
// (resourceVersion, UID, creationTimestamp) does not cause spurious drift and is
// preserved across updates.
func TestApplyConfigMap_IgnoresServerMetadata(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	_, err := m.ApplyConfigMap(context.Background(), cmSpec())
	require.NoError(t, err)

	created, _ := client.CoreV1().ConfigMaps("default").Get(context.Background(), "app-config", metav1.GetOptions{})
	originalUID := created.UID
	originalCreation := created.CreationTimestamp

	// Re-apply identical spec -> no-op, metadata untouched.
	res, err := m.ApplyConfigMap(context.Background(), cmSpec())
	require.NoError(t, err)
	assert.True(t, res.NoOp)

	// Apply a drifting spec -> update; UID/creationTimestamp must be preserved.
	spec := cmSpec()
	spec.Data = map[string]string{"KEY": "v2"}
	_, err = m.ApplyConfigMap(context.Background(), spec)
	require.NoError(t, err)

	after, _ := client.CoreV1().ConfigMaps("default").Get(context.Background(), "app-config", metav1.GetOptions{})
	assert.Equal(t, originalUID, after.UID, "UID must be preserved")
	assert.Equal(t, originalCreation, after.CreationTimestamp, "creationTimestamp must be preserved")
}

// TestApplyConfigMap_RefusesUnmanaged verifies the ownership guard: a ConfigMap
// that is not platform-owned is never mutated.
func TestApplyConfigMap_RefusesUnmanaged(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-config",
			Namespace: "default",
			// No managed-by label => user-owned.
		},
		Data: map[string]string{"USER": "data"},
	})
	m := NewManager(client, "default")

	_, err := m.ApplyConfigMap(context.Background(), cmSpec())
	require.Error(t, err, "must refuse to update a user-owned configmap")

	cm, _ := client.CoreV1().ConfigMaps("default").Get(context.Background(), "app-config", metav1.GetOptions{})
	assert.Equal(t, "data", cm.Data["USER"], "user data must be untouched")
}

func TestDeleteConfigMap_OnlyManaged(t *testing.T) {
	managed := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managed-cm",
			Namespace: "default",
			Labels:    map[string]string{LabelManagedBy: LabelManagedByValue},
		},
	}
	userOwned := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "user-cm", Namespace: "default"},
	}
	client := fake.NewSimpleClientset(managed, userOwned)
	m := NewManager(client, "default")

	require.NoError(t, m.DeleteConfigMap(context.Background(), "managed-cm"))
	_, err := client.CoreV1().ConfigMaps("default").Get(context.Background(), "managed-cm", metav1.GetOptions{})
	assert.Error(t, err, "managed configmap should be deleted")

	// Refuse to delete user-owned.
	require.Error(t, m.DeleteConfigMap(context.Background(), "user-cm"))
	_, err = client.CoreV1().ConfigMaps("default").Get(context.Background(), "user-cm", metav1.GetOptions{})
	assert.NoError(t, err, "user-owned configmap must not be deleted")
}

func TestDeleteConfigMap_AbsentIsSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")
	assert.NoError(t, m.DeleteConfigMap(context.Background(), "missing"))
}

func TestListManagedConfigMaps(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: "cm-a", Namespace: "default",
			Labels: map[string]string{LabelManagedBy: LabelManagedByValue},
		}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: "cm-b", Namespace: "default",
			Labels: map[string]string{LabelManagedBy: LabelManagedByValue},
		}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "user-cm", Namespace: "default"}},
	)
	m := NewManager(client, "default")

	names, err := m.ListManagedConfigMaps(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"cm-a", "cm-b"}, names, "only managed configmaps are listed")
}

// TestApplyConfigMap_ConcurrentDistinct exercises concurrent applies to
// distinct ConfigMaps (as would happen across many deployments) and asserts all
// are created without error or corruption. Run under `go test -race` to detect
// data races.
func TestApplyConfigMap_ConcurrentDistinct(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	const n = 25
	errCh := make(chan error, n)
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			spec := cmSpec()
			spec.Name = fmt.Sprintf("cfg-%d", i)
			spec.Data = map[string]string{"i": fmt.Sprintf("%d", i)}
			_, err := m.ApplyConfigMap(context.Background(), spec)
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

	list, err := m.ListManagedConfigMaps(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, n, "all concurrently-applied configmaps should exist")
}

func TestFromDesiredConfigMap(t *testing.T) {
	d := controlplane.DesiredDeployment{
		DeploymentID:    "dep-9",
		ApplicationName: "App",
		ApplicationSlug: "app",
		Namespace:       "ns-1",
	}
	cm := controlplane.DesiredConfigMap{
		Name: "cfg",
		Data: map[string]string{"a": "b"},
	}
	spec := FromDesiredConfigMap(d, cm)
	assert.Equal(t, "cfg", spec.Name)
	assert.Equal(t, "ns-1", spec.Namespace)
	assert.Equal(t, "dep-9", spec.DeploymentID)
	assert.Equal(t, "app", spec.ApplicationSlug)
	assert.Equal(t, "b", spec.Data["a"])
}
