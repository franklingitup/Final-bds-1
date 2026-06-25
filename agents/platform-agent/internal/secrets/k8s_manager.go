package secrets

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ManagedByLabel is the label used to identify secrets managed by the agent.
const ManagedByLabel = "bdsplatform-agent"

// K8sSecretManager implements SecretManager using Kubernetes client-go.
type K8sSecretManager struct {
	clientset *kubernetes.Clientset
	namespace string
}

// NewK8sSecretManager creates a new Kubernetes secret manager.
func NewK8sSecretManager(clientset *kubernetes.Clientset, namespace string) *K8sSecretManager {
	return &K8sSecretManager{
		clientset: clientset,
		namespace: namespace,
	}
}

// ApplySecret creates or updates a Kubernetes Secret.
func (m *K8sSecretManager) ApplySecret(ctx context.Context, spec SecretSpec) (*ApplyResult, error) {
	namespace := spec.Namespace
	if namespace == "" {
		namespace = m.namespace
	}

	// Build the Secret object.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        spec.Name,
			Namespace:   namespace,
			Labels:      spec.Labels,
			Annotations: spec.Annotations,
		},
		Type: corev1.SecretTypeOpaque,
		Data: spec.Data,
	}

	// Try to get existing secret.
	existing, err := m.clientset.CoreV1().Secrets(namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Create new secret.
			_, err := m.clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("create secret: %w", err)
			}
			return &ApplyResult{Created: true}, nil
		}
		return nil, fmt.Errorf("get secret: %w", err)
	}

	// Verify this is a managed secret.
	if existing.Labels["app.kubernetes.io/managed-by"] != ManagedByLabel {
		return nil, fmt.Errorf("refusing to update secret not managed by %s", ManagedByLabel)
	}

	// Update existing secret.
	secret.ResourceVersion = existing.ResourceVersion
	_, err = m.clientset.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update secret: %w", err)
	}

	return &ApplyResult{Updated: true}, nil
}

// DeleteSecret deletes a Kubernetes Secret.
func (m *K8sSecretManager) DeleteSecret(ctx context.Context, name string) error {
	// First verify this is a managed secret.
	existing, err := m.clientset.CoreV1().Secrets(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("get secret: %w", err)
	}

	if existing.Labels["app.kubernetes.io/managed-by"] != ManagedByLabel {
		return fmt.Errorf("refusing to delete secret not managed by %s", ManagedByLabel)
	}

	err = m.clientset.CoreV1().Secrets(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete secret: %w", err)
	}

	return nil
}

// ListManagedSecrets returns the names of all secrets managed by the agent.
func (m *K8sSecretManager) ListManagedSecrets(ctx context.Context) ([]string, error) {
	labelSelector := fmt.Sprintf("app.kubernetes.io/managed-by=%s", ManagedByLabel)
	list, err := m.clientset.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
	}

	names := make([]string, 0, len(list.Items))
	for _, secret := range list.Items {
		names = append(names, secret.Name)
	}

	return names, nil
}
