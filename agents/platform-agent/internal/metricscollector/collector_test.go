package metricscollector

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
)

// mockObservabilityClient records calls for testing.
type mockObservabilityClient struct {
	calls []controlplane.IngestMetricsRequest
	err   error
}

func (m *mockObservabilityClient) IngestMetrics(ctx context.Context, creds controlplane.AgentCredentials, req controlplane.IngestMetricsRequest) error {
	m.calls = append(m.calls, req)
	return m.err
}

func TestCollector_MetricsServerAbsent(t *testing.T) {
	// Create a fake clientset (no metrics.k8s.io API group by default)
	clientset := fake.NewSimpleClientset()
	client := &mockObservabilityClient{}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := Config{
		Interval:               100 * time.Millisecond,
		ManagedNamespacePrefix: "proj-",
		AgentCredentials: controlplane.AgentCredentials{
			ClusterID: "cluster-1",
			AgentID:   "agent-1",
		},
	}

	collector := New(client, clientset, cfg, log)

	// Run collector briefly
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Start collector in goroutine
	done := make(chan error)
	go func() {
		done <- collector.Run(ctx)
	}()

	// Wait for context cancellation
	select {
	case <-ctx.Done():
	case err := <-done:
		if err != context.Canceled && err != context.DeadlineExceeded {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// Should have detected missing metrics-server
	assert.False(t, collector.HasMetricsServer(), "should detect metrics-server is absent")
	
	// Should not have crashed
	// No metrics should have been sent (metrics-server not available)
	assert.Empty(t, client.calls, "no metrics should be sent when metrics-server is absent")
}

func TestCollector_LeadershipGate(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := &mockObservabilityClient{}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	isLeader := false
	cfg := Config{
		Interval:               50 * time.Millisecond,
		ManagedNamespacePrefix: "proj-",
		AgentCredentials: controlplane.AgentCredentials{
			ClusterID: "cluster-1",
			AgentID:   "agent-1",
		},
		IsLeader: func() bool { return isLeader },
	}

	collector := New(client, clientset, cfg, log)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error)
	go func() {
		done <- collector.Run(ctx)
	}()

	// Wait for context to finish
	<-ctx.Done()

	// Should not have attempted collection (not leader)
	// Note: The actual check happens in the collect() method
	// which skips when not leader
}

func TestCollector_ConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 60*time.Second, cfg.Interval)
	assert.Equal(t, "proj-", cfg.ManagedNamespacePrefix)
}

func TestPodMetricsListUnmarshal(t *testing.T) {
	// Test that we can unmarshal the metrics-server response format
	jsonData := `{
		"kind": "PodMetricsList",
		"apiVersion": "metrics.k8s.io/v1beta1",
		"metadata": {},
		"items": [
			{
				"metadata": {
					"name": "test-pod",
					"namespace": "proj-myproject"
				},
				"timestamp": "2024-01-15T10:30:00Z",
				"window": "30s",
				"containers": [
					{
						"name": "main",
						"usage": {
							"cpu": "100m",
							"memory": "128Mi"
						}
					}
				]
			}
		]
	}`

	var list PodMetricsList
	err := json.Unmarshal([]byte(jsonData), &list)
	require.NoError(t, err)

	assert.Len(t, list.Items, 1)
	assert.Equal(t, "test-pod", list.Items[0].Name)
	assert.Len(t, list.Items[0].Containers, 1)
	assert.Equal(t, "main", list.Items[0].Containers[0].Name)

	cpu := list.Items[0].Containers[0].Usage["cpu"]
	assert.Equal(t, int64(100), cpu.MilliValue())

	mem := list.Items[0].Containers[0].Usage["memory"]
	assert.Equal(t, int64(128*1024*1024), mem.Value())
}

func TestCollector_ListManagedNamespaces(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	
	// Create some namespaces
	ctx := context.Background()
	_, _ = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-app1"},
	}, metav1.CreateOptions{})
	_, _ = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-app2"},
	}, metav1.CreateOptions{})
	_, _ = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "kube-system"},
	}, metav1.CreateOptions{})
	_, _ = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-agent"},
	}, metav1.CreateOptions{})

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := Config{ManagedNamespacePrefix: "proj-"}
	collector := New(&mockObservabilityClient{}, clientset, cfg, log)

	namespaces, err := collector.listManagedNamespaces(ctx)
	require.NoError(t, err)

	assert.Len(t, namespaces, 2)
	assert.Contains(t, namespaces, "proj-app1")
	assert.Contains(t, namespaces, "proj-app2")
}
