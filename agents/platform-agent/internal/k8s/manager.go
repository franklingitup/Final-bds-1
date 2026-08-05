// Package k8s provides Kubernetes resource management for the platform agent.
package k8s

import (
	"context"
	"fmt"
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
)

const (
	// LabelManagedBy identifies resources managed by the platform agent.
	LabelManagedBy = "app.kubernetes.io/managed-by"
	// LabelManagedByValue is the value for the managed-by label.
	LabelManagedByValue = "bdsplatform-agent"
	// LabelDeploymentID stores the platform deployment ID.
	LabelDeploymentID = "bdsplatform.io/deployment-id"
	// LabelApplicationName stores the application name.
	LabelApplicationName = "bdsplatform.io/application-name"
	// AnnotationRevision stores the current revision.
	AnnotationRevision = "bdsplatform.io/revision"
	// AnnotationReleaseID stores the release ID.
	AnnotationReleaseID = "bdsplatform.io/release-id"
)

// Manager manages Kubernetes resources for platform deployments.
type Manager struct {
	client    kubernetes.Interface
	namespace string
}

// NewManager creates a new Kubernetes resource manager.
func NewManager(client kubernetes.Interface, namespace string) *Manager {
	if namespace == "" {
		namespace = "default"
	}
	return &Manager{
		client:    client,
		namespace: namespace,
	}
}

// DeploymentSpec represents the desired state for a Kubernetes Deployment.
type DeploymentSpec struct {
	DeploymentID    string
	ReleaseID       string
	ApplicationName string
	ApplicationSlug string
	Image           string
	Replicas        int32
	Port            *int32
	EnvVars         []corev1.EnvVar
	Resources       corev1.ResourceRequirements
	Revision        int
	// ProgressDeadlineSeconds bounds how long a rollout may make no progress
	// before Kubernetes marks the Progressing condition False with reason
	// ProgressDeadlineExceeded. This is the native rollout timeout the engine
	// surfaces as a Failed/timeout phase. Nil defaults to
	// DefaultProgressDeadlineSeconds.
	ProgressDeadlineSeconds *int32
}

// DefaultProgressDeadlineSeconds is the rollout progress deadline applied when
// a DeploymentSpec does not specify one. It matches the Kubernetes default.
const DefaultProgressDeadlineSeconds int32 = 600

// FromDesiredDeployment converts a control plane deployment to a DeploymentSpec.
func FromDesiredDeployment(d controlplane.DesiredDeployment) DeploymentSpec {
	spec := DeploymentSpec{
		DeploymentID:    d.DeploymentID,
		ReleaseID:       d.ReleaseID,
		ApplicationName: d.ApplicationName,
		ApplicationSlug: d.ApplicationSlug,
		Image:           d.Image,
		Replicas:        int32(d.Replicas),
		Revision:        d.Revision,
	}

	if d.Port != nil {
		port := int32(*d.Port)
		spec.Port = &port
	}

	if d.ProgressDeadlineSeconds != nil {
		pds := int32(*d.ProgressDeadlineSeconds)
		spec.ProgressDeadlineSeconds = &pds
	}

	for _, ev := range d.EnvVars {
		spec.EnvVars = append(spec.EnvVars, corev1.EnvVar{
			Name:  ev.Name,
			Value: ev.Value,
		})
	}

	if d.ResourceRequests != nil {
		spec.Resources.Requests = corev1.ResourceList{}
		if d.ResourceRequests.CPU != "" {
			spec.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(d.ResourceRequests.CPU)
		}
		if d.ResourceRequests.Memory != "" {
			spec.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(d.ResourceRequests.Memory)
		}
	}

	if d.ResourceLimits != nil {
		spec.Resources.Limits = corev1.ResourceList{}
		if d.ResourceLimits.CPU != "" {
			spec.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(d.ResourceLimits.CPU)
		}
		if d.ResourceLimits.Memory != "" {
			spec.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(d.ResourceLimits.Memory)
		}
	}

	return spec
}

// ResourceName returns the Kubernetes resource name for a deployment.
func (s DeploymentSpec) ResourceName() string {
	return s.ApplicationSlug
}

// ApplyResult represents the result of applying a resource.
type ApplyResult struct {
	Created bool
	Updated bool
	NoOp    bool
	// ImmutableSkipped is set when an update was skipped because the desired
	// state changed an immutable field (e.g. a PVC's storageClassName). The
	// caller records a metric and logs; it never triggers delete-and-recreate.
	ImmutableSkipped bool
}

// ApplyDeployment creates or updates a Kubernetes Deployment.
func (m *Manager) ApplyDeployment(ctx context.Context, spec DeploymentSpec) (*ApplyResult, error) {
	name := spec.ResourceName()
	labels := map[string]string{
		LabelManagedBy:       LabelManagedByValue,
		LabelDeploymentID:    spec.DeploymentID,
		LabelApplicationName: spec.ApplicationName,
		"app":                spec.ApplicationSlug,
	}
	annotations := map[string]string{
		AnnotationRevision:  fmt.Sprintf("%d", spec.Revision),
		AnnotationReleaseID: spec.ReleaseID,
	}

	// Bound rollout progress with a deadline so a stuck rollout surfaces the
	// native Progressing=False/ProgressDeadlineExceeded signal the engine maps
	// to a Failed/timeout phase.
	progressDeadline := DefaultProgressDeadlineSeconds
	if spec.ProgressDeadlineSeconds != nil {
		progressDeadline = *spec.ProgressDeadlineSeconds
	}

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   m.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:                &spec.Replicas,
			ProgressDeadlineSeconds: &progressDeadline,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": spec.ApplicationSlug,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:      "app",
							Image:     spec.Image,
							Env:       spec.EnvVars,
							Resources: spec.Resources,
						},
					},
				},
			},
		},
	}

	// Add port if specified.
	if spec.Port != nil {
		desired.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: *spec.Port,
				Protocol:      corev1.ProtocolTCP,
			},
		}
	}

	// Try to get existing deployment.
	existing, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// Create new deployment.
		_, err := m.client.AppsV1().Deployments(m.namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("create deployment: %w", err)
		}
		return &ApplyResult{Created: true}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}

	// Check if update is needed.
	if !m.needsUpdate(existing, desired, spec.Revision) {
		return &ApplyResult{NoOp: true}, nil
	}

	// Update existing deployment.
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	existing.Spec.Template.Labels = desired.Spec.Template.Labels
	existing.Spec.Template.Annotations = desired.Spec.Template.Annotations

	_, err = m.client.AppsV1().Deployments(m.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update deployment: %w", err)
	}
	return &ApplyResult{Updated: true}, nil
}

// needsUpdate checks if the deployment needs to be updated.
func (m *Manager) needsUpdate(existing, desired *appsv1.Deployment, revision int) bool {
	// Check revision annotation.
	existingRev := existing.Annotations[AnnotationRevision]
	desiredRev := fmt.Sprintf("%d", revision)
	if existingRev != desiredRev {
		return true
	}

	// Check image.
	if len(existing.Spec.Template.Spec.Containers) > 0 && len(desired.Spec.Template.Spec.Containers) > 0 {
		if existing.Spec.Template.Spec.Containers[0].Image != desired.Spec.Template.Spec.Containers[0].Image {
			return true
		}
	}

	// Check replicas.
	if existing.Spec.Replicas != nil && desired.Spec.Replicas != nil {
		if *existing.Spec.Replicas != *desired.Spec.Replicas {
			return true
		}
	}

	return false
}

// ApplyService creates or updates a Kubernetes Service for a deployment.
func (m *Manager) ApplyService(ctx context.Context, spec DeploymentSpec) (*ApplyResult, error) {
	if spec.Port == nil {
		return &ApplyResult{NoOp: true}, nil
	}

	name := spec.ResourceName()
	labels := map[string]string{
		LabelManagedBy:       LabelManagedByValue,
		LabelDeploymentID:    spec.DeploymentID,
		LabelApplicationName: spec.ApplicationName,
		"app":                spec.ApplicationSlug,
	}

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: m.namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": spec.ApplicationSlug,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       *spec.Port,
					TargetPort: intstr.FromInt(int(*spec.Port)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	// Try to get existing service.
	existing, err := m.client.CoreV1().Services(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// Create new service.
		_, err := m.client.CoreV1().Services(m.namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("create service: %w", err)
		}
		return &ApplyResult{Created: true}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get service: %w", err)
	}

	// Check if update is needed (port change).
	needsUpdate := false
	if len(existing.Spec.Ports) > 0 && len(desired.Spec.Ports) > 0 {
		if existing.Spec.Ports[0].Port != desired.Spec.Ports[0].Port {
			needsUpdate = true
		}
	}

	if !needsUpdate {
		return &ApplyResult{NoOp: true}, nil
	}

	// Update existing service.
	existing.Spec.Ports = desired.Spec.Ports
	existing.Labels = desired.Labels

	_, err = m.client.CoreV1().Services(m.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update service: %w", err)
	}
	return &ApplyResult{Updated: true}, nil
}

// GetDeploymentStatus returns the current status of a Kubernetes Deployment.
func (m *Manager) GetDeploymentStatus(ctx context.Context, name string) (*DeploymentStatus, error) {
	dep, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}

	status := &DeploymentStatus{
		Name:                dep.Name,
		Replicas:            int(dep.Status.Replicas),
		ReadyReplicas:       int(dep.Status.ReadyReplicas),
		UpdatedReplicas:     int(dep.Status.UpdatedReplicas),
		AvailableReplicas:   int(dep.Status.AvailableReplicas),
		UnavailableReplicas: int(dep.Status.UnavailableReplicas),
		Generation:          dep.Generation,
		ObservedGeneration:  dep.Status.ObservedGeneration,
		Revision:            dep.Annotations[AnnotationRevision],
		ReleaseID:           dep.Annotations[AnnotationReleaseID],
	}

	// Current image from the app container (index 0), used for progress reporting.
	if len(dep.Spec.Template.Spec.Containers) > 0 {
		status.Image = dep.Spec.Template.Spec.Containers[0].Image
	}

	// Determine desired replicas and overall readiness.
	if dep.Spec.Replicas != nil {
		status.DesiredReplicas = int(*dep.Spec.Replicas)
		if status.ReadyReplicas >= status.DesiredReplicas && status.UpdatedReplicas >= status.DesiredReplicas {
			status.Ready = true
		}
	}

	// Copy conditions verbatim and derive failure/timeout signals from them.
	for _, cond := range dep.Status.Conditions {
		status.Conditions = append(status.Conditions, DeploymentCondition{
			Type:    string(cond.Type),
			Status:  string(cond.Status),
			Reason:  cond.Reason,
			Message: cond.Message,
		})

		if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
			status.Failed = true
			status.FailureReason = cond.Reason
			status.FailureMessage = cond.Message
			// The Progressing=False/ProgressDeadlineExceeded pair is the native
			// rollout timeout signal.
			if cond.Reason == "ProgressDeadlineExceeded" {
				status.ProgressDeadlineExceeded = true
			}
		}
		if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
			status.Failed = true
			status.ReplicaFailure = true
			status.FailureReason = cond.Reason
			status.FailureMessage = cond.Message
		}
	}

	return status, nil
}

// DeploymentCondition mirrors a Kubernetes Deployment status condition
// (Progressing, Available, ReplicaFailure) in a client-agnostic form so it can
// be reported to the control plane without leaking apimachinery types.
type DeploymentCondition struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

// DeploymentStatus represents the current status of a Kubernetes Deployment.
type DeploymentStatus struct {
	Name                string
	Replicas            int
	ReadyReplicas       int
	UpdatedReplicas     int
	AvailableReplicas   int
	UnavailableReplicas int
	DesiredReplicas     int
	Generation          int64
	ObservedGeneration  int64
	Image               string
	Revision            string
	ReleaseID           string
	Ready               bool
	Failed              bool
	// ProgressDeadlineExceeded is true when Kubernetes reported the rollout
	// timed out (Progressing=False, reason=ProgressDeadlineExceeded).
	ProgressDeadlineExceeded bool
	// ReplicaFailure is true when the ReplicaFailure condition is True.
	ReplicaFailure bool
	FailureReason  string
	FailureMessage string
	Conditions     []DeploymentCondition
}

// PodHealth summarizes pod-level health for a deployment's pods. It surfaces
// fatal container/scheduling conditions the Deployment status alone does not
// expose early enough (ImagePullBackOff, CrashLoopBackOff, Unschedulable),
// letting the rollout state machine fail fast instead of waiting for the
// progress deadline.
type PodHealth struct {
	// Total is the number of pods matched by the app selector.
	Total int
	// Ready is the number of pods with the Ready condition True.
	Ready int
	// Issue is a non-empty fatal reason when at least one pod is stuck. Empty
	// means no fatal pod issue was observed.
	Issue string
	// Message is a human-readable description of Issue (e.g. the container name
	// and reason), used for status reporting/logging.
	Message string
}

// fatalWaitingReasons are container waiting reasons that indicate a pod will
// not become ready without intervention (a new image, config or code fix).
var fatalWaitingReasons = map[string]bool{
	"ImagePullBackOff":  true,
	"ErrImagePull":      true,
	"CrashLoopBackOff":  true,
	"CreateContainerConfigError": true,
	"InvalidImageName":  true,
}

// GetPodHealth lists the pods for a deployment (by the app=<slug> selector the
// agent applies) and reports the first fatal issue found, if any. It never
// returns an error for an empty pod list (a freshly-created deployment simply
// has no pods yet) and is safe to call every reconcile cycle.
func (m *Manager) GetPodHealth(ctx context.Context, appSlug string) (*PodHealth, error) {
	list, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appSlug),
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	health := &PodHealth{Total: len(list.Items)}
	for i := range list.Items {
		pod := &list.Items[i]

		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				health.Ready++
			}
			// A pod that cannot be scheduled (no node with capacity/affinity)
			// reports PodScheduled=False with reason Unschedulable.
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse &&
				cond.Reason == "Unschedulable" && health.Issue == "" {
				health.Issue = "Unschedulable"
				health.Message = fmt.Sprintf("pod %s unschedulable: %s", pod.Name, cond.Message)
			}
		}

		if health.Issue != "" {
			continue
		}

		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && fatalWaitingReasons[cs.State.Waiting.Reason] {
				health.Issue = cs.State.Waiting.Reason
				health.Message = fmt.Sprintf("container %s in pod %s: %s", cs.Name, pod.Name, cs.State.Waiting.Message)
				break
			}
		}
	}

	return health, nil
}

// DeleteDeployment deletes a Kubernetes Deployment and its Service.
func (m *Manager) DeleteDeployment(ctx context.Context, name string) error {
	// Delete deployment.
	err := m.client.AppsV1().Deployments(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete deployment: %w", err)
	}

	// Delete service.
	err = m.client.CoreV1().Services(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete service: %w", err)
	}

	return nil
}

// ListManagedDeployments returns all deployments managed by the platform agent.
func (m *Manager) ListManagedDeployments(ctx context.Context) ([]string, error) {
	selector := fmt.Sprintf("%s=%s", LabelManagedBy, LabelManagedByValue)
	list, err := m.client.AppsV1().Deployments(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}

	var names []string
	for _, dep := range list.Items {
		names = append(names, dep.Name)
	}
	return names, nil
}

// ---------------------------------------------------------------------------
// ConfigMap controller
//
// Mirrors the Deployment/Service controller pattern: create-if-missing,
// update-only-on-drift, ownership-guarded mutation, and label-selector based
// listing for garbage collection.
// ---------------------------------------------------------------------------

// ConfigMapSpec represents the desired state for a Kubernetes ConfigMap.
type ConfigMapSpec struct {
	Name            string
	Namespace       string
	DeploymentID    string
	ApplicationName string
	ApplicationSlug string
	Data            map[string]string
	BinaryData      map[string][]byte
	Labels          map[string]string
	Annotations     map[string]string
}

// FromDesiredConfigMap converts a control plane desired ConfigMap (owned by the
// given deployment) into a ConfigMapSpec, attaching platform ownership metadata.
func FromDesiredConfigMap(d controlplane.DesiredDeployment, cm controlplane.DesiredConfigMap) ConfigMapSpec {
	return ConfigMapSpec{
		Name:            cm.Name,
		Namespace:       d.Namespace,
		DeploymentID:    d.DeploymentID,
		ApplicationName: d.ApplicationName,
		ApplicationSlug: d.ApplicationSlug,
		Data:            cm.Data,
		BinaryData:      cm.BinaryData,
		Labels:          cm.Labels,
		Annotations:     cm.Annotations,
	}
}

// managedConfigMapLabels builds the label set for a managed ConfigMap. Platform
// ownership labels always win over caller-supplied labels so ownership can never
// be spoofed away by desired-state input.
func (s ConfigMapSpec) managedLabels() map[string]string {
	labels := map[string]string{}
	maps.Copy(labels, s.Labels)
	labels[LabelManagedBy] = LabelManagedByValue
	labels[LabelDeploymentID] = s.DeploymentID
	labels[LabelApplicationName] = s.ApplicationName
	if s.ApplicationSlug != "" {
		labels["app"] = s.ApplicationSlug
	}
	return labels
}

// ApplyConfigMap creates or updates a Kubernetes ConfigMap. It returns NoOp when
// the live object already matches desired state (no drift). It refuses to mutate
// a ConfigMap that is not owned by the platform agent.
func (m *Manager) ApplyConfigMap(ctx context.Context, spec ConfigMapSpec) (*ApplyResult, error) {
	namespace := spec.Namespace
	if namespace == "" {
		namespace = m.namespace
	}

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        spec.Name,
			Namespace:   namespace,
			Labels:      spec.managedLabels(),
			Annotations: spec.Annotations,
		},
		Data:       spec.Data,
		BinaryData: spec.BinaryData,
	}

	existing, err := m.client.CoreV1().ConfigMaps(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := m.client.CoreV1().ConfigMaps(namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("create configmap: %w", err)
		}
		return &ApplyResult{Created: true}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get configmap: %w", err)
	}

	// Ownership guard: never hijack a ConfigMap the platform does not own.
	if existing.Labels[LabelManagedBy] != LabelManagedByValue {
		return nil, fmt.Errorf("refusing to update configmap %q not managed by %s", spec.Name, LabelManagedByValue)
	}

	// Drift detection over the reconcilable surface only.
	if !configMapNeedsUpdate(existing, desired) {
		return &ApplyResult{NoOp: true}, nil
	}

	// Mutate the fetched object in place so server-owned immutable metadata
	// (UID, creationTimestamp, resourceVersion, managedFields) is preserved.
	existing.Data = desired.Data
	existing.BinaryData = desired.BinaryData
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations

	_, err = m.client.CoreV1().ConfigMaps(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update configmap: %w", err)
	}
	return &ApplyResult{Updated: true}, nil
}

// configMapNeedsUpdate reports whether the live ConfigMap has drifted from
// desired on any reconcilable field (data, binaryData, labels, annotations).
// Server-managed metadata (resourceVersion, UID, managedFields, creationTimestamp)
// is intentionally not compared.
func configMapNeedsUpdate(existing, desired *corev1.ConfigMap) bool {
	if !maps.Equal(existing.Data, desired.Data) {
		return true
	}
	if !binaryDataEqual(existing.BinaryData, desired.BinaryData) {
		return true
	}
	if !maps.Equal(existing.Labels, desired.Labels) {
		return true
	}
	if !maps.Equal(existing.Annotations, desired.Annotations) {
		return true
	}
	return false
}

// binaryDataEqual compares two binary-data maps by key and byte content.
func binaryDataEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		if string(av) != string(bv) {
			return false
		}
	}
	return true
}

// DeleteConfigMap deletes a ConfigMap owned by the platform agent. It never
// deletes a ConfigMap that is not platform-owned, and treats an already-absent
// ConfigMap as success.
func (m *Manager) DeleteConfigMap(ctx context.Context, name string) error {
	existing, err := m.client.CoreV1().ConfigMaps(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get configmap: %w", err)
	}

	if existing.Labels[LabelManagedBy] != LabelManagedByValue {
		return fmt.Errorf("refusing to delete configmap %q not managed by %s", name, LabelManagedByValue)
	}

	err = m.client.CoreV1().ConfigMaps(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete configmap: %w", err)
	}
	return nil
}

// ListManagedConfigMaps returns the names of all ConfigMaps managed by the
// platform agent, used for orphan garbage collection.
func (m *Manager) ListManagedConfigMaps(ctx context.Context) ([]string, error) {
	selector := fmt.Sprintf("%s=%s", LabelManagedBy, LabelManagedByValue)
	list, err := m.client.CoreV1().ConfigMaps(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("list configmaps: %w", err)
	}

	names := make([]string, 0, len(list.Items))
	for _, cm := range list.Items {
		names = append(names, cm.Name)
	}
	return names, nil
}

// ---------------------------------------------------------------------------
// PersistentVolumeClaim controller
//
// Mirrors the ConfigMap controller (create-if-missing, ownership-guarded,
// drift-detecting, label-selector GC) with two PVC-specific rules:
//   - Immutable spec fields (accessModes, storageClassName, volumeMode) and
//     storage shrink can never be reconciled in place; such changes are flagged
//     via ApplyResult.ImmutableSkipped and the update is skipped (never a
//     delete-and-recreate).
//   - Only labels, annotations, and storage *expansion* are mutable.
// ---------------------------------------------------------------------------

// PVCSpec represents the desired state for a Kubernetes PersistentVolumeClaim.
type PVCSpec struct {
	Name             string
	Namespace        string
	DeploymentID     string
	ApplicationName  string
	ApplicationSlug  string
	AccessModes      []corev1.PersistentVolumeAccessMode
	StorageClassName *string
	StorageRequest   resource.Quantity
	VolumeMode       *corev1.PersistentVolumeMode
	Labels           map[string]string
	Annotations      map[string]string
}

// FromDesiredPVC converts a control plane desired PVC (owned by the given
// deployment) into a PVCSpec, parsing the storage quantity and attaching
// platform ownership metadata. It returns an error if the storage request is
// malformed.
func FromDesiredPVC(d controlplane.DesiredDeployment, pvc controlplane.DesiredPVC) (PVCSpec, error) {
	spec := PVCSpec{
		Name:             pvc.Name,
		Namespace:        pvc.Namespace,
		DeploymentID:     d.DeploymentID,
		ApplicationName:  d.ApplicationName,
		ApplicationSlug:  d.ApplicationSlug,
		StorageClassName: pvc.StorageClassName,
		Labels:           pvc.Labels,
		Annotations:      pvc.Annotations,
	}
	if spec.Namespace == "" {
		spec.Namespace = d.Namespace
	}

	for _, am := range pvc.AccessModes {
		spec.AccessModes = append(spec.AccessModes, corev1.PersistentVolumeAccessMode(am))
	}

	if pvc.VolumeMode != nil {
		vm := corev1.PersistentVolumeMode(*pvc.VolumeMode)
		spec.VolumeMode = &vm
	}

	if pvc.StorageRequest != "" {
		q, err := resource.ParseQuantity(pvc.StorageRequest)
		if err != nil {
			return PVCSpec{}, fmt.Errorf("parse storage request %q: %w", pvc.StorageRequest, err)
		}
		spec.StorageRequest = q
	}

	return spec, nil
}

// managedLabels builds the label set for a managed PVC. Platform ownership
// labels always win over caller-supplied labels.
func (s PVCSpec) managedLabels() map[string]string {
	labels := map[string]string{}
	maps.Copy(labels, s.Labels)
	labels[LabelManagedBy] = LabelManagedByValue
	labels[LabelDeploymentID] = s.DeploymentID
	labels[LabelApplicationName] = s.ApplicationName
	if s.ApplicationSlug != "" {
		labels["app"] = s.ApplicationSlug
	}
	return labels
}

// ApplyPVC creates or updates a PersistentVolumeClaim. Returns Created on
// create, Updated on legal drift correction, NoOp when already in sync, and
// ImmutableSkipped when the desired state would require changing an immutable
// field (which is logged and skipped, never delete-and-recreated). Refuses to
// mutate a PVC not owned by the platform agent.
func (m *Manager) ApplyPVC(ctx context.Context, spec PVCSpec) (*ApplyResult, error) {
	namespace := spec.Namespace
	if namespace == "" {
		namespace = m.namespace
	}

	desired := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        spec.Name,
			Namespace:   namespace,
			Labels:      spec.managedLabels(),
			Annotations: spec.Annotations,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      spec.AccessModes,
			StorageClassName: spec.StorageClassName,
			VolumeMode:       spec.VolumeMode,
		},
	}
	if !spec.StorageRequest.IsZero() {
		desired.Spec.Resources = corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceStorage: spec.StorageRequest},
		}
	}

	existing, err := m.client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := m.client.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("create pvc: %w", err)
		}
		return &ApplyResult{Created: true}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pvc: %w", err)
	}

	// Ownership guard: never hijack a PVC the platform does not own.
	if existing.Labels[LabelManagedBy] != LabelManagedByValue {
		return nil, fmt.Errorf("refusing to update pvc %q not managed by %s", spec.Name, LabelManagedByValue)
	}

	// An attempt to change an immutable field (or shrink storage) cannot be
	// reconciled in place; flag and skip rather than delete-and-recreate.
	if pvcImmutableChanged(existing, spec) {
		return &ApplyResult{ImmutableSkipped: true}, nil
	}

	if !pvcLegalDrift(existing, spec) {
		return &ApplyResult{NoOp: true}, nil
	}

	// Apply only the mutable surface: metadata and storage expansion. All other
	// spec fields (accessModes, storageClassName, volumeMode, volumeName) and
	// server metadata (UID, resourceVersion, creationTimestamp, status) are left
	// untouched on the fetched object.
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	if !spec.StorageRequest.IsZero() {
		if existing.Spec.Resources.Requests == nil {
			existing.Spec.Resources.Requests = corev1.ResourceList{}
		}
		existing.Spec.Resources.Requests[corev1.ResourceStorage] = spec.StorageRequest
	}

	_, err = m.client.CoreV1().PersistentVolumeClaims(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update pvc: %w", err)
	}
	return &ApplyResult{Updated: true}, nil
}

// existingStorage returns the currently requested storage quantity of a PVC.
func existingStorage(pvc *corev1.PersistentVolumeClaim) resource.Quantity {
	if pvc.Spec.Resources.Requests == nil {
		return resource.Quantity{}
	}
	return pvc.Spec.Resources.Requests[corev1.ResourceStorage]
}

// pvcImmutableChanged reports whether the desired spec would change an immutable
// PVC field, or shrink storage (also forbidden). Optional desired fields that
// are unset are not enforced, so a control plane that omits a field never
// triggers a false immutable-change.
func pvcImmutableChanged(existing *corev1.PersistentVolumeClaim, spec PVCSpec) bool {
	if len(spec.AccessModes) > 0 && !accessModesEqual(existing.Spec.AccessModes, spec.AccessModes) {
		return true
	}
	if spec.StorageClassName != nil {
		existingSC := ""
		if existing.Spec.StorageClassName != nil {
			existingSC = *existing.Spec.StorageClassName
		}
		if existingSC != *spec.StorageClassName {
			return true
		}
	}
	if spec.VolumeMode != nil {
		existingVM := ""
		if existing.Spec.VolumeMode != nil {
			existingVM = string(*existing.Spec.VolumeMode)
		}
		if existingVM != string(*spec.VolumeMode) {
			return true
		}
	}
	// Storage shrink is forbidden by Kubernetes; treat it as an immutable change.
	if !spec.StorageRequest.IsZero() {
		cur := existingStorage(existing)
		if spec.StorageRequest.Cmp(cur) < 0 {
			return true
		}
	}
	return false
}

// pvcLegalDrift reports whether the desired spec differs from the live PVC on a
// legally-reconcilable field: labels, annotations, or storage expansion.
func pvcLegalDrift(existing *corev1.PersistentVolumeClaim, spec PVCSpec) bool {
	if !maps.Equal(existing.Labels, spec.managedLabels()) {
		return true
	}
	if !maps.Equal(existing.Annotations, spec.Annotations) {
		return true
	}
	if !spec.StorageRequest.IsZero() {
		if spec.StorageRequest.Cmp(existingStorage(existing)) > 0 {
			return true
		}
	}
	return false
}

// accessModesEqual compares two access-mode slices as sets.
func accessModesEqual(a, b []corev1.PersistentVolumeAccessMode) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[corev1.PersistentVolumeAccessMode]int, len(a))
	for _, m := range a {
		set[m]++
	}
	for _, m := range b {
		set[m]--
		if set[m] < 0 {
			return false
		}
	}
	return true
}

// DeletePVC deletes a PVC owned by the platform agent. It never deletes a PVC
// that is not platform-owned, and treats an already-absent PVC as success.
func (m *Manager) DeletePVC(ctx context.Context, name string) error {
	existing, err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get pvc: %w", err)
	}

	if existing.Labels[LabelManagedBy] != LabelManagedByValue {
		return fmt.Errorf("refusing to delete pvc %q not managed by %s", name, LabelManagedByValue)
	}

	err = m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete pvc: %w", err)
	}
	return nil
}

// ListManagedPVCs returns the names of all PVCs managed by the platform agent,
// used for orphan garbage collection.
func (m *Manager) ListManagedPVCs(ctx context.Context) ([]string, error) {
	selector := fmt.Sprintf("%s=%s", LabelManagedBy, LabelManagedByValue)
	list, err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("list pvcs: %w", err)
	}

	names := make([]string, 0, len(list.Items))
	for _, pvc := range list.Items {
		names = append(names, pvc.Name)
	}
	return names, nil
}

// ---------------------------------------------------------------------------
// Ingress controller
//
// Mirrors the ConfigMap/PVC controller (create-if-missing, ownership-guarded,
// drift-detecting, label-selector GC). Unlike PVCs the Ingress spec is fully
// mutable, so there is no immutable-skip path: any drift on the managed surface
// (rules, TLS, ingressClassName, labels, annotations) is corrected via update.
// ---------------------------------------------------------------------------

// IngressSpec represents the desired state for a Kubernetes Ingress.
type IngressSpec struct {
	Name             string
	Namespace        string
	DeploymentID     string
	ApplicationName  string
	ApplicationSlug  string
	IngressClassName *string
	Rules            []controlplane.DesiredIngressRule
	TLS              []controlplane.DesiredIngressTLS
	Labels           map[string]string
	Annotations      map[string]string
}

// FromDesiredIngress converts a control plane desired Ingress (owned by the
// given deployment) into an IngressSpec, attaching platform ownership metadata.
func FromDesiredIngress(d controlplane.DesiredDeployment, ing controlplane.DesiredIngress) IngressSpec {
	spec := IngressSpec{
		Name:             ing.Name,
		Namespace:        ing.Namespace,
		DeploymentID:     d.DeploymentID,
		ApplicationName:  d.ApplicationName,
		ApplicationSlug:  d.ApplicationSlug,
		IngressClassName: ing.IngressClassName,
		Rules:            ing.Rules,
		TLS:              ing.TLS,
		Labels:           ing.Labels,
		Annotations:      ing.Annotations,
	}
	if spec.Namespace == "" {
		spec.Namespace = d.Namespace
	}
	return spec
}

// managedLabels builds the label set for a managed Ingress. Platform ownership
// labels always win over caller-supplied labels.
func (s IngressSpec) managedLabels() map[string]string {
	labels := map[string]string{}
	maps.Copy(labels, s.Labels)
	labels[LabelManagedBy] = LabelManagedByValue
	labels[LabelDeploymentID] = s.DeploymentID
	labels[LabelApplicationName] = s.ApplicationName
	if s.ApplicationSlug != "" {
		labels["app"] = s.ApplicationSlug
	}
	return labels
}

// buildRules converts the desired routing rules into networkingv1 rules,
// defaulting an empty pathType to Prefix.
func (s IngressSpec) buildRules() []networkingv1.IngressRule {
	rules := make([]networkingv1.IngressRule, 0, len(s.Rules))
	for _, r := range s.Rules {
		paths := make([]networkingv1.HTTPIngressPath, 0, len(r.Paths))
		for _, p := range r.Paths {
			pathType := networkingv1.PathType(p.PathType)
			if p.PathType == "" {
				pathType = networkingv1.PathTypePrefix
			}
			paths = append(paths, networkingv1.HTTPIngressPath{
				Path:     p.Path,
				PathType: &pathType,
				Backend: networkingv1.IngressBackend{
					Service: &networkingv1.IngressServiceBackend{
						Name: p.ServiceName,
						Port: networkingv1.ServiceBackendPort{Number: p.ServicePort},
					},
				},
			})
		}
		rules = append(rules, networkingv1.IngressRule{
			Host: r.Host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{Paths: paths},
			},
		})
	}
	return rules
}

// buildTLS converts the desired TLS entries into networkingv1 TLS entries.
func (s IngressSpec) buildTLS() []networkingv1.IngressTLS {
	if len(s.TLS) == 0 {
		return nil
	}
	tls := make([]networkingv1.IngressTLS, 0, len(s.TLS))
	for _, t := range s.TLS {
		tls = append(tls, networkingv1.IngressTLS{
			Hosts:      t.Hosts,
			SecretName: t.SecretName,
		})
	}
	return tls
}

// ApplyIngress creates or updates a Kubernetes Ingress. Returns NoOp when the
// live object already matches desired state, Updated on drift correction, and
// Created on create. Refuses to mutate an Ingress not owned by the platform.
func (m *Manager) ApplyIngress(ctx context.Context, spec IngressSpec) (*ApplyResult, error) {
	namespace := spec.Namespace
	if namespace == "" {
		namespace = m.namespace
	}

	desired := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        spec.Name,
			Namespace:   namespace,
			Labels:      spec.managedLabels(),
			Annotations: spec.Annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: spec.IngressClassName,
			Rules:            spec.buildRules(),
			TLS:              spec.buildTLS(),
		},
	}

	existing, err := m.client.NetworkingV1().Ingresses(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := m.client.NetworkingV1().Ingresses(namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("create ingress: %w", err)
		}
		return &ApplyResult{Created: true}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ingress: %w", err)
	}

	// Ownership guard: never hijack an Ingress the platform does not own.
	if existing.Labels[LabelManagedBy] != LabelManagedByValue {
		return nil, fmt.Errorf("refusing to update ingress %q not managed by %s", spec.Name, LabelManagedByValue)
	}

	if !ingressNeedsUpdate(existing, desired) {
		return &ApplyResult{NoOp: true}, nil
	}

	// Mutate the fetched object in place so server-owned metadata (UID,
	// resourceVersion, creationTimestamp, managedFields, status/loadBalancer) is
	// preserved. ingressClassName is only overwritten when desired sets it, so a
	// cluster-default class is never clobbered.
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	existing.Spec.Rules = desired.Spec.Rules
	existing.Spec.TLS = desired.Spec.TLS
	if desired.Spec.IngressClassName != nil {
		existing.Spec.IngressClassName = desired.Spec.IngressClassName
	}

	_, err = m.client.NetworkingV1().Ingresses(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update ingress: %w", err)
	}
	return &ApplyResult{Updated: true}, nil
}

// ingressNeedsUpdate reports whether the live Ingress has drifted from desired
// on any managed field (labels, annotations, ingressClassName, rules, TLS).
// Server-managed fields (resourceVersion, UID, managedFields, creationTimestamp,
// status/loadBalancer) are intentionally not compared.
func ingressNeedsUpdate(existing, desired *networkingv1.Ingress) bool {
	if !maps.Equal(existing.Labels, desired.Labels) {
		return true
	}
	if !maps.Equal(existing.Annotations, desired.Annotations) {
		return true
	}
	// ingressClassName is only enforced when desired sets it, so a cluster
	// default is not treated as drift.
	if desired.Spec.IngressClassName != nil {
		if existing.Spec.IngressClassName == nil ||
			*existing.Spec.IngressClassName != *desired.Spec.IngressClassName {
			return true
		}
	}
	if !ingressRulesEqual(existing.Spec.Rules, desired.Spec.Rules) {
		return true
	}
	if !ingressTLSEqual(existing.Spec.TLS, desired.Spec.TLS) {
		return true
	}
	return false
}

// ingressRulesEqual compares routing rules in order (host + ordered paths).
func ingressRulesEqual(a, b []networkingv1.IngressRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Host != b[i].Host {
			return false
		}
		ah, bh := a[i].HTTP, b[i].HTTP
		if (ah == nil) != (bh == nil) {
			return false
		}
		if ah == nil {
			continue
		}
		if len(ah.Paths) != len(bh.Paths) {
			return false
		}
		for j := range ah.Paths {
			if !ingressPathEqual(ah.Paths[j], bh.Paths[j]) {
				return false
			}
		}
	}
	return true
}

// ingressPathEqual compares a single path route (path, pathType, backend).
func ingressPathEqual(a, b networkingv1.HTTPIngressPath) bool {
	if a.Path != b.Path {
		return false
	}
	if derefPathType(a.PathType) != derefPathType(b.PathType) {
		return false
	}
	as, bs := a.Backend.Service, b.Backend.Service
	if (as == nil) != (bs == nil) {
		return false
	}
	if as == nil {
		return true
	}
	return as.Name == bs.Name && as.Port.Number == bs.Port.Number && as.Port.Name == bs.Port.Name
}

func derefPathType(p *networkingv1.PathType) networkingv1.PathType {
	if p == nil {
		return ""
	}
	return *p
}

// ingressTLSEqual compares TLS entries in order (secretName + host set).
func ingressTLSEqual(a, b []networkingv1.IngressTLS) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].SecretName != b[i].SecretName {
			return false
		}
		if !stringSetEqual(a[i].Hosts, b[i].Hosts) {
			return false
		}
	}
	return true
}

// stringSetEqual compares two string slices as multisets (order-insensitive).
func stringSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]int, len(a))
	for _, s := range a {
		set[s]++
	}
	for _, s := range b {
		set[s]--
		if set[s] < 0 {
			return false
		}
	}
	return true
}

// DeleteIngress deletes an Ingress owned by the platform agent. It never deletes
// an Ingress that is not platform-owned, and treats an already-absent Ingress as
// success.
func (m *Manager) DeleteIngress(ctx context.Context, name string) error {
	existing, err := m.client.NetworkingV1().Ingresses(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get ingress: %w", err)
	}

	if existing.Labels[LabelManagedBy] != LabelManagedByValue {
		return fmt.Errorf("refusing to delete ingress %q not managed by %s", name, LabelManagedByValue)
	}

	err = m.client.NetworkingV1().Ingresses(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete ingress: %w", err)
	}
	return nil
}

// ListManagedIngresses returns the names of all Ingresses managed by the
// platform agent, used for orphan garbage collection.
func (m *Manager) ListManagedIngresses(ctx context.Context) ([]string, error) {
	selector := fmt.Sprintf("%s=%s", LabelManagedBy, LabelManagedByValue)
	list, err := m.client.NetworkingV1().Ingresses(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("list ingresses: %w", err)
	}

	names := make([]string, 0, len(list.Items))
	for _, ing := range list.Items {
		names = append(names, ing.Name)
	}
	return names, nil
}
