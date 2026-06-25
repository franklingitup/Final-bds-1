// Package k8s provides Kubernetes resource management for the platform agent.
package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
}

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

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   m.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &spec.Replicas,
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
		Name:             dep.Name,
		Replicas:         int(dep.Status.Replicas),
		ReadyReplicas:    int(dep.Status.ReadyReplicas),
		UpdatedReplicas:  int(dep.Status.UpdatedReplicas),
		AvailableReplicas: int(dep.Status.AvailableReplicas),
		Revision:         dep.Annotations[AnnotationRevision],
		ReleaseID:        dep.Annotations[AnnotationReleaseID],
	}

	// Determine overall status.
	if dep.Spec.Replicas != nil {
		desired := int(*dep.Spec.Replicas)
		if status.ReadyReplicas >= desired && status.UpdatedReplicas >= desired {
			status.Ready = true
		}
	}

	// Check for failure conditions.
	for _, cond := range dep.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
			status.Failed = true
			status.FailureReason = cond.Reason
			status.FailureMessage = cond.Message
		}
		if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
			status.Failed = true
			status.FailureReason = cond.Reason
			status.FailureMessage = cond.Message
		}
	}

	return status, nil
}

// DeploymentStatus represents the current status of a Kubernetes Deployment.
type DeploymentStatus struct {
	Name              string
	Replicas          int
	ReadyReplicas     int
	UpdatedReplicas   int
	AvailableReplicas int
	Revision          string
	ReleaseID         string
	Ready             bool
	Failed            bool
	FailureReason     string
	FailureMessage    string
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
