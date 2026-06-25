//go:build integration

package reconciler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"log/slog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/k8s"
)

// These tests require a running Kubernetes cluster (kind).
// Run with: go test -tags=integration -v ./...
//
// Setup kind cluster:
//   kind create cluster --name platform-agent-test
//   export KUBECONFIG=$(kind get kubeconfig-path --name platform-agent-test)

func getKubeClient(t *testing.T) kubernetes.Interface {
	t.Helper()

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("KUBECONFIG not set and cannot determine home directory")
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Skipf("failed to load kubeconfig: %v", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Skipf("failed to create kubernetes client: %v", err)
	}

	// Verify cluster is accessible.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		t.Skipf("kubernetes cluster not accessible: %v", err)
	}

	return client
}

func TestIntegration_ReconcileDeployment(t *testing.T) {
	client := getKubeClient(t)
	ctx := context.Background()

	// Create test namespace.
	namespace := fmt.Sprintf("test-reconciler-%d", time.Now().UnixNano())
	_, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	defer func() {
		_ = client.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	}()

	// Setup mock deployment service.
	desiredDeployments := []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "Test App",
			ApplicationSlug: "test-app",
			Image:           "nginx:1.25-alpine",
			Replicas:        1,
			Revision:        1,
			Status:          "pending",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/organizations/org-1/clusters/cluster-1/deployments" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"items":%s}`, mustJSON(desiredDeployments))
			return
		}
		if r.Method == "POST" && contains(r.URL.Path, "/status") {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create reconciler.
	cpClient := controlplane.NewClient(server.URL, 10*time.Second)
	manager := k8s.NewManager(client, namespace)

	cfg := Config{
		Interval:    100 * time.Millisecond,
		StateFile:   filepath.Join(t.TempDir(), "state.json"),
		AccessToken: "test-token",
		Namespace:   namespace,
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(cpClient, manager, cfg, "org-1", "cluster-1", log)

	// Run reconciler for a short time.
	recCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = rec.Run(recCtx)

	// Verify deployment was created.
	dep, err := client.AppsV1().Deployments(namespace).Get(ctx, "test-app", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "test-app", dep.Name)
	assert.Equal(t, int32(1), *dep.Spec.Replicas)
	assert.Equal(t, "nginx:1.25-alpine", dep.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, "dep-1", dep.Labels[k8s.LabelDeploymentID])
	assert.Equal(t, "bdsplatform-agent", dep.Labels[k8s.LabelManagedBy])
}

func TestIntegration_ReconcileMultipleDeployments(t *testing.T) {
	client := getKubeClient(t)
	ctx := context.Background()

	namespace := fmt.Sprintf("test-reconciler-%d", time.Now().UnixNano())
	_, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	defer func() {
		_ = client.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	}()

	desiredDeployments := []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "App One",
			ApplicationSlug: "app-one",
			Image:           "nginx:1.25-alpine",
			Replicas:        1,
			Revision:        1,
			Status:          "pending",
		},
		{
			DeploymentID:    "dep-2",
			ReleaseID:       "rel-2",
			ApplicationName: "App Two",
			ApplicationSlug: "app-two",
			Image:           "redis:7-alpine",
			Replicas:        1,
			Revision:        1,
			Status:          "pending",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/organizations/org-1/clusters/cluster-1/deployments" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"items":%s}`, mustJSON(desiredDeployments))
			return
		}
		if r.Method == "POST" && contains(r.URL.Path, "/status") {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cpClient := controlplane.NewClient(server.URL, 10*time.Second)
	manager := k8s.NewManager(client, namespace)

	cfg := Config{
		Interval:    100 * time.Millisecond,
		StateFile:   filepath.Join(t.TempDir(), "state.json"),
		AccessToken: "test-token",
		Namespace:   namespace,
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(cpClient, manager, cfg, "org-1", "cluster-1", log)

	recCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = rec.Run(recCtx)

	// Verify both deployments were created.
	deps, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: k8s.LabelManagedBy + "=" + k8s.LabelManagedByValue,
	})
	require.NoError(t, err)
	assert.Len(t, deps.Items, 2)
}

func TestIntegration_UpdateDeployment(t *testing.T) {
	client := getKubeClient(t)
	ctx := context.Background()

	namespace := fmt.Sprintf("test-reconciler-%d", time.Now().UnixNano())
	_, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	defer func() {
		_ = client.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	}()

	// Start with revision 1.
	desiredDeployments := []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "Test App",
			ApplicationSlug: "test-app",
			Image:           "nginx:1.25-alpine",
			Replicas:        1,
			Revision:        1,
			Status:          "pending",
		},
	}

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/organizations/org-1/clusters/cluster-1/deployments" {
			w.Header().Set("Content-Type", "application/json")
			// After first request, return updated deployment.
			if requestCount > 0 {
				desiredDeployments[0].Image = "nginx:1.26-alpine"
				desiredDeployments[0].Replicas = 2
				desiredDeployments[0].Revision = 2
				desiredDeployments[0].ReleaseID = "rel-2"
			}
			fmt.Fprintf(w, `{"items":%s}`, mustJSON(desiredDeployments))
			requestCount++
			return
		}
		if r.Method == "POST" && contains(r.URL.Path, "/status") {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cpClient := controlplane.NewClient(server.URL, 10*time.Second)
	manager := k8s.NewManager(client, namespace)

	cfg := Config{
		Interval:    100 * time.Millisecond,
		StateFile:   filepath.Join(t.TempDir(), "state.json"),
		AccessToken: "test-token",
		Namespace:   namespace,
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(cpClient, manager, cfg, "org-1", "cluster-1", log)

	recCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_ = rec.Run(recCtx)

	// Verify deployment was updated.
	dep, err := client.AppsV1().Deployments(namespace).Get(ctx, "test-app", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nginx:1.26-alpine", dep.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, int32(2), *dep.Spec.Replicas)
	assert.Equal(t, "2", dep.Annotations[k8s.AnnotationRevision])
}

func TestIntegration_DeleteOrphanedDeployment(t *testing.T) {
	client := getKubeClient(t)
	ctx := context.Background()

	namespace := fmt.Sprintf("test-reconciler-%d", time.Now().UnixNano())
	_, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	defer func() {
		_ = client.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	}()

	// Create an orphaned deployment first.
	manager := k8s.NewManager(client, namespace)
	_, err = manager.ApplyDeployment(ctx, k8s.DeploymentSpec{
		DeploymentID:    "orphan-dep",
		ReleaseID:       "orphan-rel",
		ApplicationName: "Orphan App",
		ApplicationSlug: "orphan-app",
		Image:           "nginx:1.25-alpine",
		Replicas:        1,
		Revision:        1,
	})
	require.NoError(t, err)

	// Return empty desired deployments.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/organizations/org-1/clusters/cluster-1/deployments" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"items":[]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cpClient := controlplane.NewClient(server.URL, 10*time.Second)

	cfg := Config{
		Interval:    100 * time.Millisecond,
		StateFile:   filepath.Join(t.TempDir(), "state.json"),
		AccessToken: "test-token",
		Namespace:   namespace,
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(cpClient, manager, cfg, "org-1", "cluster-1", log)

	recCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = rec.Run(recCtx)

	// Verify orphaned deployment was deleted.
	deps, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: k8s.LabelManagedBy + "=" + k8s.LabelManagedByValue,
	})
	require.NoError(t, err)
	assert.Len(t, deps.Items, 0)
}

func TestIntegration_CreateService(t *testing.T) {
	client := getKubeClient(t)
	ctx := context.Background()

	namespace := fmt.Sprintf("test-reconciler-%d", time.Now().UnixNano())
	_, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	defer func() {
		_ = client.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	}()

	port := 8080
	desiredDeployments := []controlplane.DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "Test App",
			ApplicationSlug: "test-app",
			Image:           "nginx:1.25-alpine",
			Replicas:        1,
			Port:            &port,
			Revision:        1,
			Status:          "pending",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/organizations/org-1/clusters/cluster-1/deployments" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"items":%s}`, mustJSON(desiredDeployments))
			return
		}
		if r.Method == "POST" && contains(r.URL.Path, "/status") {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cpClient := controlplane.NewClient(server.URL, 10*time.Second)
	manager := k8s.NewManager(client, namespace)

	cfg := Config{
		Interval:    100 * time.Millisecond,
		StateFile:   filepath.Join(t.TempDir(), "state.json"),
		AccessToken: "test-token",
		Namespace:   namespace,
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := New(cpClient, manager, cfg, "org-1", "cluster-1", log)

	recCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = rec.Run(recCtx)

	// Verify service was created.
	svc, err := client.CoreV1().Services(namespace).Get(ctx, "test-app", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(8080), svc.Spec.Ports[0].Port)
}

// Helpers

import (
	"encoding/json"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
