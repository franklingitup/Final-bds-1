package builder

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	build "github.com/bdsplatform/platform/backend/services/build/internal"
)

// kubeClient is the production KubeBackend backed by client-go. It creates the
// registry secret and Kaniko Job, streams the pod logs, waits for completion,
// extracts the pushed digest from the container termination message, and cleans
// up. All operations are idempotent so a re-claimed build adopts an existing Job
// rather than starting a duplicate.
type kubeClient struct {
	cs           kubernetes.Interface
	pollInterval time.Duration
}

// NewKubeBackend constructs a KubeBackend from a Kubernetes clientset.
func NewKubeBackend(cs kubernetes.Interface) KubeBackend {
	return &kubeClient{cs: cs, pollInterval: 2 * time.Second}
}

// NewKubeClientset builds a Kubernetes clientset using in-cluster config when
// available, falling back to KUBECONFIG / ~/.kube/config for local operation.
func NewKubeClientset() (kubernetes.Interface, error) {
	cfg, err := restConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

func restConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}
	if kubeconfig == "" {
		return nil, fmt.Errorf("no in-cluster config and no KUBECONFIG available")
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

// Run implements KubeBackend.
func (k *kubeClient) Run(ctx context.Context, spec JobSpec, logSink func(stream, line string)) (JobResult, error) {
	// Ensure the registry secret exists (idempotent) before the Job references it.
	if spec.SecretName != "" && len(spec.DockerConfigJSON) > 0 {
		if err := k.ensureRegistrySecret(ctx, spec); err != nil {
			return JobResult{}, fmt.Errorf("ensure registry secret: %w", err)
		}
	}

	// Always attempt cleanup of the Job and secret when we return. Uses a
	// background context so cleanup still runs if ctx was cancelled/timed out.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		k.cleanup(cleanupCtx, spec)
	}()

	if err := k.createJob(ctx, spec); err != nil {
		return JobResult{}, fmt.Errorf("create job: %w", err)
	}

	// Stream logs best-effort in the background; log failures must not fail the
	// build. The stream ends when the pod terminates or ctx is done.
	logDone := make(chan struct{})
	go func() {
		defer close(logDone)
		k.streamPodLogs(ctx, spec, logSink)
	}()

	result, err := k.waitForCompletion(ctx, spec)

	// Give the log streamer a brief moment to flush remaining lines.
	select {
	case <-logDone:
	case <-time.After(3 * time.Second):
	}

	return result, err
}

func (k *kubeClient) ensureRegistrySecret(ctx context.Context, spec JobSpec) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.SecretName,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: spec.DockerConfigJSON,
		},
	}
	_, err := k.cs.CoreV1().Secrets(spec.Namespace).Create(ctx, secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		// Update in place to keep credentials current across retries.
		_, uerr := k.cs.CoreV1().Secrets(spec.Namespace).Update(ctx, secret, metav1.UpdateOptions{})
		return uerr
	}
	return err
}

func (k *kubeClient) createJob(ctx context.Context, spec JobSpec) error {
	job := k.buildJobObject(spec)
	_, err := k.cs.BatchV1().Jobs(spec.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		// A Job already exists for this build (worker re-claim): adopt it.
		return nil
	}
	return err
}

func (k *kubeClient) buildJobObject(spec JobSpec) *batchv1.Job {
	var env []corev1.EnvVar
	for key, val := range spec.Env {
		env = append(env, corev1.EnvVar{Name: key, Value: val})
	}

	container := corev1.Container{
		Name:                     "kaniko",
		Image:                    spec.Image,
		Args:                     spec.Args,
		Env:                      env,
		TerminationMessagePath:   digestFile,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		Resources:                buildResources(spec),
	}

	var volumes []corev1.Volume
	if spec.SecretName != "" {
		container.VolumeMounts = []corev1.VolumeMount{{
			Name:      "docker-config",
			MountPath: dockerConfigMount,
		}}
		volumes = append(volumes, corev1.Volume{
			Name: "docker-config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: spec.SecretName,
					Items: []corev1.KeyToPath{{
						Key:  corev1.DockerConfigJsonKey,
						Path: "config.json",
					}},
				},
			},
		})
	}

	backoffLimit := int32(0) // build-service owns retries; do not retry the Job
	deadline := spec.ActiveDeadlineSeconds
	podSpec := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		Containers:    []corev1.Container{container},
		Volumes:       volumes,
	}
	if spec.ServiceAccount != "" {
		podSpec.ServiceAccountName = spec.ServiceAccount
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.JobName,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: spec.Labels},
				Spec:       podSpec,
			},
		},
	}
	if deadline > 0 {
		job.Spec.ActiveDeadlineSeconds = &deadline
	}
	if spec.TTLSecondsAfterFinished > 0 {
		ttl := spec.TTLSecondsAfterFinished
		job.Spec.TTLSecondsAfterFinished = &ttl
	}
	return job
}

func buildResources(spec JobSpec) corev1.ResourceRequirements {
	req := corev1.ResourceList{}
	lim := corev1.ResourceList{}
	if q, err := resource.ParseQuantity(spec.CPURequest); err == nil {
		req[corev1.ResourceCPU] = q
	}
	if q, err := resource.ParseQuantity(spec.MemoryRequest); err == nil {
		req[corev1.ResourceMemory] = q
	}
	if q, err := resource.ParseQuantity(spec.CPULimit); err == nil {
		lim[corev1.ResourceCPU] = q
	}
	if q, err := resource.ParseQuantity(spec.MemoryLimit); err == nil {
		lim[corev1.ResourceMemory] = q
	}
	res := corev1.ResourceRequirements{}
	if len(req) > 0 {
		res.Requests = req
	}
	if len(lim) > 0 {
		res.Limits = lim
	}
	return res
}

// waitForCompletion polls the Job until it succeeds, fails, or ctx is done. On
// ctx cancellation the deferred cleanup deletes the Job.
func (k *kubeClient) waitForCompletion(ctx context.Context, spec JobSpec) (JobResult, error) {
	interval := k.pollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		job, err := k.cs.BatchV1().Jobs(spec.Namespace).Get(ctx, spec.JobName, metav1.GetOptions{})
		if err != nil {
			if ctx.Err() != nil {
				return JobResult{}, ctx.Err()
			}
			// Transient get error; retry on next tick.
		} else {
			if job.Status.Succeeded > 0 {
				digest := k.extractDigest(ctx, spec)
				return JobResult{Succeeded: true, Digest: digest}, nil
			}
			if job.Status.Failed > 0 {
				return JobResult{Succeeded: false, FailureReason: jobFailureReason(job)}, nil
			}
		}

		select {
		case <-ctx.Done():
			return JobResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// extractDigest reads the pushed digest from the terminated container's
// termination message (Kaniko --digest-file=/dev/termination-log).
func (k *kubeClient) extractDigest(ctx context.Context, spec JobSpec) string {
	pod, err := k.jobPod(ctx, spec)
	if err != nil || pod == nil {
		return ""
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Terminated != nil {
			msg := strings.TrimSpace(cs.State.Terminated.Message)
			// The digest may be prefixed by the image ref; take the sha256 token.
			if idx := strings.Index(msg, "sha256:"); idx != -1 {
				return strings.Fields(msg[idx:])[0]
			}
		}
	}
	return ""
}

func jobFailureReason(job *batchv1.Job) string {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			if c.Message != "" {
				return c.Message
			}
			return c.Reason
		}
	}
	return "job failed"
}

// jobPod returns the (most recent) pod created by the Job.
func (k *kubeClient) jobPod(ctx context.Context, spec JobSpec) (*corev1.Pod, error) {
	pods, err := k.cs.CoreV1().Pods(spec.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelBuildID + "=" + spec.Labels[labelBuildID],
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, nil
	}
	newest := &pods.Items[0]
	for i := range pods.Items {
		if pods.Items[i].CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = &pods.Items[i]
		}
	}
	return newest, nil
}

// streamPodLogs follows the Kaniko pod logs into logSink. It waits for the pod
// to appear, then streams until EOF (pod termination) or ctx cancellation.
func (k *kubeClient) streamPodLogs(ctx context.Context, spec JobSpec, logSink func(stream, line string)) {
	var podName string
	waitTicker := time.NewTicker(time.Second)
	defer waitTicker.Stop()
	for podName == "" {
		if pod, err := k.jobPod(ctx, spec); err == nil && pod != nil && pod.Status.Phase != corev1.PodPending {
			podName = pod.Name
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-waitTicker.C:
		}
	}

	req := k.cs.CoreV1().Pods(spec.Namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: "kaniko",
		Follow:    true,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		logSink(build.StreamStdout, scanner.Text())
	}
}

// cleanup deletes the Job (foreground/background) and the registry secret. It is
// idempotent and best-effort.
func (k *kubeClient) cleanup(ctx context.Context, spec JobSpec) {
	policy := metav1.DeletePropagationBackground
	_ = k.cs.BatchV1().Jobs(spec.Namespace).Delete(ctx, spec.JobName, metav1.DeleteOptions{
		PropagationPolicy: &policy,
	})
	if spec.SecretName != "" {
		_ = k.cs.CoreV1().Secrets(spec.Namespace).Delete(ctx, spec.SecretName, metav1.DeleteOptions{})
	}
}
