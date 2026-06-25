package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
)

func TestManager_ApplyDeployment_Create(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	spec := DeploymentSpec{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        3,
		Revision:        1,
	}

	ctx := context.Background()
	result, err := manager.ApplyDeployment(ctx, spec)

	require.NoError(t, err)
	assert.True(t, result.Created)
	assert.False(t, result.Updated)
	assert.False(t, result.NoOp)

	// Verify deployment was created.
	dep, err := client.AppsV1().Deployments("default").Get(ctx, "my-app", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "my-app", dep.Name)
	assert.Equal(t, int32(3), *dep.Spec.Replicas)
	assert.Equal(t, "nginx:1.25", dep.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, "dep-1", dep.Labels[LabelDeploymentID])
	assert.Equal(t, "1", dep.Annotations[AnnotationRevision])
	assert.Equal(t, "rel-1", dep.Annotations[AnnotationReleaseID])
}

func TestManager_ApplyDeployment_Update(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	// Create initial deployment.
	spec := DeploymentSpec{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        3,
		Revision:        1,
	}

	ctx := context.Background()
	_, err := manager.ApplyDeployment(ctx, spec)
	require.NoError(t, err)

	// Update deployment.
	spec.Image = "nginx:1.26"
	spec.Replicas = 5
	spec.Revision = 2
	spec.ReleaseID = "rel-2"

	result, err := manager.ApplyDeployment(ctx, spec)

	require.NoError(t, err)
	assert.False(t, result.Created)
	assert.True(t, result.Updated)
	assert.False(t, result.NoOp)

	// Verify deployment was updated.
	dep, err := client.AppsV1().Deployments("default").Get(ctx, "my-app", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(5), *dep.Spec.Replicas)
	assert.Equal(t, "nginx:1.26", dep.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, "2", dep.Annotations[AnnotationRevision])
	assert.Equal(t, "rel-2", dep.Annotations[AnnotationReleaseID])
}

func TestManager_ApplyDeployment_NoOp(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	spec := DeploymentSpec{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        3,
		Revision:        1,
	}

	ctx := context.Background()
	_, err := manager.ApplyDeployment(ctx, spec)
	require.NoError(t, err)

	// Apply same spec again.
	result, err := manager.ApplyDeployment(ctx, spec)

	require.NoError(t, err)
	assert.False(t, result.Created)
	assert.False(t, result.Updated)
	assert.True(t, result.NoOp)
}

func TestManager_ApplyDeployment_WithPort(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	port := int32(8080)
	spec := DeploymentSpec{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        1,
		Port:            &port,
		Revision:        1,
	}

	ctx := context.Background()
	_, err := manager.ApplyDeployment(ctx, spec)
	require.NoError(t, err)

	dep, err := client.AppsV1().Deployments("default").Get(ctx, "my-app", metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, dep.Spec.Template.Spec.Containers[0].Ports, 1)
	assert.Equal(t, int32(8080), dep.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort)
	assert.Equal(t, "http", dep.Spec.Template.Spec.Containers[0].Ports[0].Name)
}

func TestManager_ApplyDeployment_WithEnvVars(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	spec := DeploymentSpec{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        1,
		EnvVars: []corev1.EnvVar{
			{Name: "FOO", Value: "bar"},
			{Name: "BAZ", Value: "qux"},
		},
		Revision: 1,
	}

	ctx := context.Background()
	_, err := manager.ApplyDeployment(ctx, spec)
	require.NoError(t, err)

	dep, err := client.AppsV1().Deployments("default").Get(ctx, "my-app", metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, dep.Spec.Template.Spec.Containers[0].Env, 2)
	assert.Equal(t, "FOO", dep.Spec.Template.Spec.Containers[0].Env[0].Name)
	assert.Equal(t, "bar", dep.Spec.Template.Spec.Containers[0].Env[0].Value)
}

func TestManager_ApplyDeployment_WithResources(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	spec := DeploymentSpec{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        1,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		Revision: 1,
	}

	ctx := context.Background()
	_, err := manager.ApplyDeployment(ctx, spec)
	require.NoError(t, err)

	dep, err := client.AppsV1().Deployments("default").Get(ctx, "my-app", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "100m", dep.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().String())
	assert.Equal(t, "128Mi", dep.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().String())
	assert.Equal(t, "500m", dep.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().String())
	assert.Equal(t, "512Mi", dep.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String())
}

func TestManager_ApplyService_Create(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	port := int32(8080)
	spec := DeploymentSpec{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        1,
		Port:            &port,
		Revision:        1,
	}

	ctx := context.Background()
	result, err := manager.ApplyService(ctx, spec)

	require.NoError(t, err)
	assert.True(t, result.Created)

	// Verify service was created.
	svc, err := client.CoreV1().Services("default").Get(ctx, "my-app", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "my-app", svc.Name)
	assert.Equal(t, int32(8080), svc.Spec.Ports[0].Port)
	assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
}

func TestManager_ApplyService_NoPortNoOp(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	spec := DeploymentSpec{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        1,
		Port:            nil, // No port
		Revision:        1,
	}

	ctx := context.Background()
	result, err := manager.ApplyService(ctx, spec)

	require.NoError(t, err)
	assert.True(t, result.NoOp)
}

func TestManager_GetDeploymentStatus(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	// Create a deployment with status.
	ctx := context.Background()
	replicas := int32(3)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-app",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationRevision:  "2",
				AnnotationReleaseID: "rel-2",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			Replicas:          3,
			ReadyReplicas:     3,
			UpdatedReplicas:   3,
			AvailableReplicas: 3,
		},
	}
	_, err := client.AppsV1().Deployments("default").Create(ctx, dep, metav1.CreateOptions{})
	require.NoError(t, err)

	status, err := manager.GetDeploymentStatus(ctx, "my-app")

	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "my-app", status.Name)
	assert.Equal(t, 3, status.ReadyReplicas)
	assert.Equal(t, "2", status.Revision)
	assert.Equal(t, "rel-2", status.ReleaseID)
	assert.True(t, status.Ready)
	assert.False(t, status.Failed)
}

func TestManager_GetDeploymentStatus_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	ctx := context.Background()
	status, err := manager.GetDeploymentStatus(ctx, "nonexistent")

	require.NoError(t, err)
	assert.Nil(t, status)
}

func TestManager_DeleteDeployment(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	ctx := context.Background()

	// Create deployment and service.
	spec := DeploymentSpec{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        1,
		Revision:        1,
	}
	port := int32(8080)
	spec.Port = &port

	_, err := manager.ApplyDeployment(ctx, spec)
	require.NoError(t, err)
	_, err = manager.ApplyService(ctx, spec)
	require.NoError(t, err)

	// Delete.
	err = manager.DeleteDeployment(ctx, "my-app")
	require.NoError(t, err)

	// Verify deletion.
	_, err = client.AppsV1().Deployments("default").Get(ctx, "my-app", metav1.GetOptions{})
	assert.Error(t, err)
	_, err = client.CoreV1().Services("default").Get(ctx, "my-app", metav1.GetOptions{})
	assert.Error(t, err)
}

func TestManager_ListManagedDeployments(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewManager(client, "default")

	ctx := context.Background()

	// Create managed deployment.
	_, err := manager.ApplyDeployment(ctx, DeploymentSpec{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "App 1",
		ApplicationSlug: "app-1",
		Image:           "nginx:1.25",
		Replicas:        1,
		Revision:        1,
	})
	require.NoError(t, err)

	// Create unmanaged deployment.
	unmanaged := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unmanaged-app",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "unmanaged"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "unmanaged"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "nginx"}},
				},
			},
		},
	}
	_, err = client.AppsV1().Deployments("default").Create(ctx, unmanaged, metav1.CreateOptions{})
	require.NoError(t, err)

	// List managed deployments.
	names, err := manager.ListManagedDeployments(ctx)
	require.NoError(t, err)

	assert.Len(t, names, 1)
	assert.Contains(t, names, "app-1")
}

func TestFromDesiredDeployment(t *testing.T) {
	port := 8080
	desired := controlplane.DesiredDeployment{
		DeploymentID:    "dep-1",
		ReleaseID:       "rel-1",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		Image:           "nginx:1.25",
		Replicas:        3,
		Port:            &port,
		EnvVars: []controlplane.EnvVar{
			{Name: "FOO", Value: "bar"},
		},
		ResourceRequests: &controlplane.ResourceSpec{
			CPU:    "100m",
			Memory: "128Mi",
		},
		ResourceLimits: &controlplane.ResourceSpec{
			CPU:    "500m",
			Memory: "512Mi",
		},
		Revision: 5,
	}

	spec := FromDesiredDeployment(desired)

	assert.Equal(t, "dep-1", spec.DeploymentID)
	assert.Equal(t, "rel-1", spec.ReleaseID)
	assert.Equal(t, "My App", spec.ApplicationName)
	assert.Equal(t, "my-app", spec.ApplicationSlug)
	assert.Equal(t, "nginx:1.25", spec.Image)
	assert.Equal(t, int32(3), spec.Replicas)
	assert.Equal(t, int32(8080), *spec.Port)
	assert.Len(t, spec.EnvVars, 1)
	assert.Equal(t, "FOO", spec.EnvVars[0].Name)
	assert.Equal(t, 5, spec.Revision)
	assert.NotNil(t, spec.Resources.Requests)
	assert.NotNil(t, spec.Resources.Limits)
}

func TestDeploymentSpec_ResourceName(t *testing.T) {
	spec := DeploymentSpec{
		ApplicationSlug: "my-awesome-app",
	}

	assert.Equal(t, "my-awesome-app", spec.ResourceName())
}
