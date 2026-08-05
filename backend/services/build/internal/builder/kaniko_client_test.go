package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testJobSpec() JobSpec {
	return JobSpec{
		JobName:               "bdsplatform-build-b1",
		SecretName:            "bdsplatform-build-reg-b1",
		Namespace:             "builds",
		Image:                 "gcr.io/kaniko-project/executor:v1.23.2",
		Args:                  []string{"--context=git://x#refs/heads/main", "--destination=reg/app:1", "--digest-file=/dev/termination-log"},
		Env:                   map[string]string{"DOCKER_CONFIG": dockerConfigMount, "GIT_PASSWORD": "t"},
		DockerConfigJSON:      []byte(`{"auths":{}}`),
		Labels:                jobLabels("b1", "org1"),
		CPURequest:            "500m",
		MemoryRequest:         "1Gi",
		CPULimit:              "2",
		MemoryLimit:           "4Gi",
		ActiveDeadlineSeconds: 900,
		TTLSecondsAfterFinished: 3600,
	}
}

func TestBuildJobObject(t *testing.T) {
	k := &kubeClient{}
	job := k.buildJobObject(testJobSpec())

	assert.Equal(t, "bdsplatform-build-b1", job.Name)
	assert.Equal(t, "builds", job.Namespace)
	require.NotNil(t, job.Spec.BackoffLimit)
	assert.Equal(t, int32(0), *job.Spec.BackoffLimit, "build-service owns retries; Job must not retry")
	require.NotNil(t, job.Spec.ActiveDeadlineSeconds)
	assert.Equal(t, int64(900), *job.Spec.ActiveDeadlineSeconds)
	require.NotNil(t, job.Spec.TTLSecondsAfterFinished)

	c := job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, corev1.RestartPolicyNever, job.Spec.Template.Spec.RestartPolicy)
	assert.Equal(t, digestFile, c.TerminationMessagePath)
	assert.Equal(t, corev1.TerminationMessageReadFile, c.TerminationMessagePolicy)
	// Registry secret is mounted at the docker config path.
	require.Len(t, c.VolumeMounts, 1)
	assert.Equal(t, dockerConfigMount, c.VolumeMounts[0].MountPath)
	// Resources parsed.
	assert.False(t, c.Resources.Requests.Cpu().IsZero())
	assert.False(t, c.Resources.Limits.Memory().IsZero())
}

func TestBuildJobObject_NoSecret(t *testing.T) {
	k := &kubeClient{}
	spec := testJobSpec()
	spec.SecretName = ""
	job := k.buildJobObject(spec)
	assert.Empty(t, job.Spec.Template.Spec.Volumes)
	assert.Empty(t, job.Spec.Template.Spec.Containers[0].VolumeMounts)
}

func TestEnsureRegistrySecret_CreateThenUpdate(t *testing.T) {
	cs := fake.NewSimpleClientset()
	k := &kubeClient{cs: cs}
	spec := testJobSpec()

	require.NoError(t, k.ensureRegistrySecret(context.Background(), spec))
	sec, err := cs.CoreV1().Secrets("builds").Get(context.Background(), spec.SecretName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, corev1.SecretTypeDockerConfigJson, sec.Type)

	// Idempotent: second call updates rather than failing on AlreadyExists.
	spec.DockerConfigJSON = []byte(`{"auths":{"reg":{}}}`)
	require.NoError(t, k.ensureRegistrySecret(context.Background(), spec))
	sec, _ = cs.CoreV1().Secrets("builds").Get(context.Background(), spec.SecretName, metav1.GetOptions{})
	assert.Equal(t, `{"auths":{"reg":{}}}`, string(sec.Data[corev1.DockerConfigJsonKey]))
}

func TestCreateJob_Idempotent(t *testing.T) {
	cs := fake.NewSimpleClientset()
	k := &kubeClient{cs: cs}
	spec := testJobSpec()

	require.NoError(t, k.createJob(context.Background(), spec))
	// Second create (re-claim) must not error.
	require.NoError(t, k.createJob(context.Background(), spec))

	jobs, err := cs.BatchV1().Jobs("builds").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, jobs.Items, 1, "re-claim must adopt the existing Job, not duplicate it")
}

func TestWaitForCompletion_Succeeded(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "bdsplatform-build-b1", Namespace: "builds"},
			Status:     batchv1.JobStatus{Succeeded: 1},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bdsplatform-build-b1-xyz", Namespace: "builds",
				Labels: jobLabels("b1", "org1"),
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "kaniko",
					State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
						Message: "sha256:deadbeefcafe",
					}},
				}},
			},
		},
	)
	k := &kubeClient{cs: cs}
	res, err := k.waitForCompletion(context.Background(), testJobSpec())
	require.NoError(t, err)
	assert.True(t, res.Succeeded)
	assert.Equal(t, "sha256:deadbeefcafe", res.Digest)
}

func TestWaitForCompletion_Failed(t *testing.T) {
	cs := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "bdsplatform-build-b1", Namespace: "builds"},
		Status: batchv1.JobStatus{
			Failed: 1,
			Conditions: []batchv1.JobCondition{{
				Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: "BackoffLimitExceeded",
			}},
		},
	})
	k := &kubeClient{cs: cs}
	res, err := k.waitForCompletion(context.Background(), testJobSpec())
	require.NoError(t, err)
	assert.False(t, res.Succeeded)
	assert.Equal(t, "BackoffLimitExceeded", res.FailureReason)
}

func TestWaitForCompletion_ContextCancelled(t *testing.T) {
	cs := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "bdsplatform-build-b1", Namespace: "builds"},
		Status:     batchv1.JobStatus{}, // never completes
	})
	k := &kubeClient{cs: cs}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := k.waitForCompletion(ctx, testJobSpec())
	require.Error(t, err)
}

func TestCleanup_DeletesJobAndSecret(t *testing.T) {
	spec := testJobSpec()
	cs := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: spec.JobName, Namespace: "builds"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: spec.SecretName, Namespace: "builds"}},
	)
	k := &kubeClient{cs: cs}
	k.cleanup(context.Background(), spec)

	_, err := cs.BatchV1().Jobs("builds").Get(context.Background(), spec.JobName, metav1.GetOptions{})
	assert.Error(t, err)
	_, err = cs.CoreV1().Secrets("builds").Get(context.Background(), spec.SecretName, metav1.GetOptions{})
	assert.Error(t, err)
}
