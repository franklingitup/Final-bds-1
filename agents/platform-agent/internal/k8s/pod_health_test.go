package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func newPod(name, appSlug string, ready bool, waitingReason string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"app": appSlug},
		},
	}
	readyStatus := corev1.ConditionFalse
	if ready {
		readyStatus = corev1.ConditionTrue
	}
	pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
		Type: corev1.PodReady, Status: readyStatus,
	})
	cs := corev1.ContainerStatus{Name: "app"}
	if waitingReason != "" {
		cs.State.Waiting = &corev1.ContainerStateWaiting{Reason: waitingReason, Message: waitingReason + " detail"}
	}
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, cs)
	return pod
}

func TestGetPodHealth_Empty(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")
	h, err := m.GetPodHealth(context.Background(), "my-app")
	require.NoError(t, err)
	assert.Equal(t, 0, h.Total)
	assert.Equal(t, "", h.Issue)
}

func TestGetPodHealth_AllReady(t *testing.T) {
	client := fake.NewSimpleClientset(
		newPod("p1", "my-app", true, ""),
		newPod("p2", "my-app", true, ""),
	)
	m := NewManager(client, "default")
	h, err := m.GetPodHealth(context.Background(), "my-app")
	require.NoError(t, err)
	assert.Equal(t, 2, h.Total)
	assert.Equal(t, 2, h.Ready)
	assert.Equal(t, "", h.Issue)
}

func TestGetPodHealth_ImagePullBackOff(t *testing.T) {
	client := fake.NewSimpleClientset(newPod("p1", "my-app", false, "ImagePullBackOff"))
	m := NewManager(client, "default")
	h, err := m.GetPodHealth(context.Background(), "my-app")
	require.NoError(t, err)
	assert.Equal(t, "ImagePullBackOff", h.Issue)
	assert.Contains(t, h.Message, "p1")
}

func TestGetPodHealth_CrashLoopBackOff(t *testing.T) {
	client := fake.NewSimpleClientset(newPod("p1", "my-app", false, "CrashLoopBackOff"))
	m := NewManager(client, "default")
	h, err := m.GetPodHealth(context.Background(), "my-app")
	require.NoError(t, err)
	assert.Equal(t, "CrashLoopBackOff", h.Issue)
}

func TestGetPodHealth_Unschedulable(t *testing.T) {
	pod := newPod("p1", "my-app", false, "")
	pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
		Type: corev1.PodScheduled, Status: corev1.ConditionFalse,
		Reason: "Unschedulable", Message: "0/3 nodes are available",
	})
	client := fake.NewSimpleClientset(pod)
	m := NewManager(client, "default")
	h, err := m.GetPodHealth(context.Background(), "my-app")
	require.NoError(t, err)
	assert.Equal(t, "Unschedulable", h.Issue)
	assert.Contains(t, h.Message, "unschedulable")
}

func TestGetPodHealth_NonFatalWaitingIsHealthy(t *testing.T) {
	// ContainerCreating is a transient, non-fatal reason and must not be flagged.
	client := fake.NewSimpleClientset(newPod("p1", "my-app", false, "ContainerCreating"))
	m := NewManager(client, "default")
	h, err := m.GetPodHealth(context.Background(), "my-app")
	require.NoError(t, err)
	assert.Equal(t, "", h.Issue)
}

func TestApplyDeployment_SetsProgressDeadline(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewManager(client, "default")
	_, err := m.ApplyDeployment(context.Background(), DeploymentSpec{
		ApplicationSlug: "my-app", ApplicationName: "My App", Image: "nginx:1", Replicas: 1, Revision: 1,
	})
	require.NoError(t, err)
	dep, err := client.AppsV1().Deployments("default").Get(context.Background(), "my-app", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, dep.Spec.ProgressDeadlineSeconds)
	assert.Equal(t, DefaultProgressDeadlineSeconds, *dep.Spec.ProgressDeadlineSeconds)
}

func TestGetDeploymentStatus_EnrichedFields(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-app",
			Namespace:   "default",
			Generation:  7,
			Annotations: map[string]string{AnnotationRevision: "3", AnnotationReleaseID: "rel-3"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:3"}}},
			},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration:  7,
			Replicas:            3,
			ReadyReplicas:       2,
			UpdatedReplicas:     3,
			AvailableReplicas:   2,
			UnavailableReplicas: 1,
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue, Reason: "NewReplicaSetAvailable"},
				{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			},
		},
	}
	client := fake.NewSimpleClientset(dep)
	m := NewManager(client, "default")

	st, err := m.GetDeploymentStatus(context.Background(), "my-app")
	require.NoError(t, err)
	require.NotNil(t, st)
	assert.Equal(t, 3, st.DesiredReplicas)
	assert.Equal(t, 2, st.ReadyReplicas)
	assert.Equal(t, 3, st.UpdatedReplicas)
	assert.Equal(t, 2, st.AvailableReplicas)
	assert.Equal(t, 1, st.UnavailableReplicas)
	assert.Equal(t, int64(7), st.Generation)
	assert.Equal(t, int64(7), st.ObservedGeneration)
	assert.Equal(t, "nginx:3", st.Image)
	assert.Len(t, st.Conditions, 2)
	assert.False(t, st.ProgressDeadlineExceeded)
	assert.False(t, st.Failed)
}

func TestGetDeploymentStatus_ProgressDeadlineExceeded(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "default", Generation: 1},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(2)},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded", Message: "exceeded"},
			},
		},
	}
	client := fake.NewSimpleClientset(dep)
	m := NewManager(client, "default")

	st, err := m.GetDeploymentStatus(context.Background(), "my-app")
	require.NoError(t, err)
	assert.True(t, st.ProgressDeadlineExceeded)
	assert.True(t, st.Failed)
}

func int32Ptr(i int32) *int32 { return &i }
