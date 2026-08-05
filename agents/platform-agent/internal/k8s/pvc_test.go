package k8s

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
)

func strptr(s string) *string { return &s }

func pvcSpec() PVCSpec {
	sc := "standard"
	vm := corev1.PersistentVolumeFilesystem
	return PVCSpec{
		Name:             "data",
		Namespace:        "default",
		DeploymentID:     "dep-1",
		ApplicationName:  "My App",
		ApplicationSlug:  "my-app",
		AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		StorageClassName: &sc,
		StorageRequest:   resource.MustParse("10Gi"),
		VolumeMode:       &vm,
		Annotations:      map[string]string{"bdsplatform.io/release-id": "rel-1"},
	}
}

func TestApplyPVC_Create(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	res, err := m.ApplyPVC(context.Background(), pvcSpec())
	require.NoError(t, err)
	assert.True(t, res.Created)

	pvc, err := client.CoreV1().PersistentVolumeClaims("default").Get(context.Background(), "data", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, LabelManagedByValue, pvc.Labels[LabelManagedBy])
	assert.Equal(t, "my-app", pvc.Labels["app"])
	assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes)
	assert.Equal(t, "standard", *pvc.Spec.StorageClassName)
	q := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, 0, q.Cmp(resource.MustParse("10Gi")))
}

func TestApplyPVC_NoOpWhenUnchanged(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	_, err := m.ApplyPVC(context.Background(), pvcSpec())
	require.NoError(t, err)

	res, err := m.ApplyPVC(context.Background(), pvcSpec())
	require.NoError(t, err)
	assert.True(t, res.NoOp, "identical spec must be a no-op")
	assert.False(t, res.Updated)
	assert.False(t, res.ImmutableSkipped)
}

func TestApplyPVC_StorageExpansion(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	_, err := m.ApplyPVC(context.Background(), pvcSpec())
	require.NoError(t, err)

	spec := pvcSpec()
	spec.StorageRequest = resource.MustParse("20Gi")
	res, err := m.ApplyPVC(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.Updated, "storage expansion is a legal update")

	pvc, _ := client.CoreV1().PersistentVolumeClaims("default").Get(context.Background(), "data", metav1.GetOptions{})
	q := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, 0, q.Cmp(resource.MustParse("20Gi")))
}

func TestApplyPVC_MetadataDrift(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")
	_, err := m.ApplyPVC(context.Background(), pvcSpec())
	require.NoError(t, err)

	spec := pvcSpec()
	spec.Annotations = map[string]string{"new": "annotation"}
	res, err := m.ApplyPVC(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.Updated, "annotation change is legal drift")
}

func TestApplyPVC_ImmutableChangesSkipped(t *testing.T) {
	cases := map[string]func(s *PVCSpec){
		"accessModes": func(s *PVCSpec) {
			s.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
		},
		"storageClassName": func(s *PVCSpec) { s.StorageClassName = strptr("fast") },
		"volumeMode": func(s *PVCSpec) {
			b := corev1.PersistentVolumeBlock
			s.VolumeMode = &b
		},
		"storageShrink": func(s *PVCSpec) { s.StorageRequest = resource.MustParse("5Gi") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			m := NewManager(client, "default")
			_, err := m.ApplyPVC(context.Background(), pvcSpec())
			require.NoError(t, err)

			spec := pvcSpec()
			mutate(&spec)
			res, err := m.ApplyPVC(context.Background(), spec)
			require.NoError(t, err)
			assert.True(t, res.ImmutableSkipped, "%s change must be skipped, not applied", name)
			assert.False(t, res.Updated)

			// The live object must be untouched for immutable-field changes.
			pvc, _ := client.CoreV1().PersistentVolumeClaims("default").Get(context.Background(), "data", metav1.GetOptions{})
			q := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
			assert.Equal(t, 0, q.Cmp(resource.MustParse("10Gi")), "storage must remain 10Gi")
		})
	}
}

func TestApplyPVC_RefusesUnmanaged(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "default"}, // no managed-by label
	})
	m := NewManager(client, "default")

	_, err := m.ApplyPVC(context.Background(), pvcSpec())
	require.Error(t, err, "must refuse to update a user-owned PVC")
}

func TestDeletePVC_OnlyManaged(t *testing.T) {
	managed := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "managed-pvc", Namespace: "default",
			Labels: map[string]string{LabelManagedBy: LabelManagedByValue},
		},
	}
	userOwned := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "user-pvc", Namespace: "default"},
	}
	client := fake.NewSimpleClientset(managed, userOwned)
	m := NewManager(client, "default")

	require.NoError(t, m.DeletePVC(context.Background(), "managed-pvc"))
	_, err := client.CoreV1().PersistentVolumeClaims("default").Get(context.Background(), "managed-pvc", metav1.GetOptions{})
	assert.Error(t, err, "managed pvc should be deleted")

	require.Error(t, m.DeletePVC(context.Background(), "user-pvc"))
	_, err = client.CoreV1().PersistentVolumeClaims("default").Get(context.Background(), "user-pvc", metav1.GetOptions{})
	assert.NoError(t, err, "user-owned pvc must not be deleted")
}

func TestDeletePVC_AbsentIsSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")
	assert.NoError(t, m.DeletePVC(context.Background(), "missing"))
}

func TestListManagedPVCs(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
			Name: "pvc-a", Namespace: "default",
			Labels: map[string]string{LabelManagedBy: LabelManagedByValue},
		}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "user-pvc", Namespace: "default"}},
	)
	m := NewManager(client, "default")

	names, err := m.ListManagedPVCs(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"pvc-a"}, names)
}

func TestFromDesiredPVC(t *testing.T) {
	d := controlplane.DesiredDeployment{
		DeploymentID: "dep-9", ApplicationName: "App", ApplicationSlug: "app", Namespace: "ns-1",
	}
	pvc := controlplane.DesiredPVC{
		Name:             "vol",
		AccessModes:      []string{"ReadWriteOnce"},
		StorageClassName: strptr("gp3"),
		StorageRequest:   "15Gi",
		VolumeMode:       strptr("Filesystem"),
	}
	spec, err := FromDesiredPVC(d, pvc)
	require.NoError(t, err)
	assert.Equal(t, "vol", spec.Name)
	assert.Equal(t, "ns-1", spec.Namespace)
	assert.Equal(t, "gp3", *spec.StorageClassName)
	assert.Equal(t, 0, spec.StorageRequest.Cmp(resource.MustParse("15Gi")))
	assert.Equal(t, corev1.PersistentVolumeFilesystem, *spec.VolumeMode)
}

func TestFromDesiredPVC_InvalidStorage(t *testing.T) {
	_, err := FromDesiredPVC(controlplane.DesiredDeployment{}, controlplane.DesiredPVC{
		Name: "vol", StorageRequest: "not-a-size",
	})
	require.Error(t, err, "malformed storage request must error")
}

// TestApplyPVC_ConcurrentDistinct exercises concurrent applies to distinct PVCs
// and asserts all are created without error. Run under `go test -race` to
// detect data races.
func TestApplyPVC_ConcurrentDistinct(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")

	const n = 25
	errCh := make(chan error, n)
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			spec := pvcSpec()
			spec.Name = fmt.Sprintf("pvc-%d", i)
			_, err := m.ApplyPVC(context.Background(), spec)
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

	list, err := m.ListManagedPVCs(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, n)
}
