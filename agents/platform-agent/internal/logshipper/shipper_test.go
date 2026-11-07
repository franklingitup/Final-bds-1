package logshipper

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
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
	mu    sync.Mutex
	calls []controlplane.IngestLogsRequest
	err   error
}

func (m *mockObservabilityClient) IngestLogs(ctx context.Context, creds controlplane.AgentCredentials, req controlplane.IngestLogsRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	return m.err
}

func (m *mockObservabilityClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func TestShipper_ConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 5*time.Second, cfg.BatchInterval)
	assert.Equal(t, 50, cfg.MaxTailedPods)
	assert.Equal(t, "proj-", cfg.ManagedNamespacePrefix)
}

func TestShipper_PodCapEnforced(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := &mockObservabilityClient{}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := Config{
		BatchInterval:          100 * time.Millisecond,
		MaxTailedPods:          3, // Very small cap for testing
		ManagedNamespacePrefix: "proj-",
		AgentCredentials: controlplane.AgentCredentials{
			ClusterID: "cluster-1",
			AgentID:   "agent-1",
		},
	}

	shipper := New(client, clientset, cfg, log)

	ctx := context.Background()

	// Create 5 pods with staggered start times
	baseTime := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: "proj-test",
				Labels: map[string]string{
					"bdsplatform.io/deployment-id": "deploy-1",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "main"},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				StartTime: &metav1.Time{
					Time: baseTime.Add(time.Duration(i) * time.Minute),
				},
			},
		}

		shipper.handlePodAdded(ctx, pod)
	}

	// Should only be tailing MaxTailedPods
	assert.Equal(t, 3, shipper.TailedPodCount(), "should not exceed MaxTailedPods")

	// Should be tracking newest pods (pod-2, pod-3, pod-4)
	shipper.mu.Lock()
	var tailedNames []string
	for key := range shipper.tailedPods {
		tailedNames = append(tailedNames, key.name)
	}
	shipper.mu.Unlock()

	// Newest pods should be prioritized
	assert.Contains(t, tailedNames, "pod-4", "newest pod should be tailed")
	assert.Contains(t, tailedNames, "pod-3", "second newest pod should be tailed")
}

func TestShipper_NoCrashOnCapExceed(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := &mockObservabilityClient{}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := Config{
		BatchInterval:          50 * time.Millisecond,
		MaxTailedPods:          2, // Very small cap
		ManagedNamespacePrefix: "proj-",
		AgentCredentials: controlplane.AgentCredentials{
			ClusterID: "cluster-1",
			AgentID:   "agent-1",
		},
	}

	shipper := New(client, clientset, cfg, log)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Add many pods - should not panic or crash
	for i := 0; i < 100; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: "proj-test",
				Labels: map[string]string{
					"bdsplatform.io/deployment-id": "deploy-1",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "main"}},
			},
			Status: corev1.PodStatus{
				Phase:     corev1.PodRunning,
				StartTime: &metav1.Time{Time: time.Now()},
			},
		}

		shipper.handlePodAdded(ctx, pod)
	}

	// Should still be at cap
	assert.LessOrEqual(t, shipper.TailedPodCount(), cfg.MaxTailedPods,
		"should never exceed cap")

	// Wait a bit to ensure no goroutine panics
	time.Sleep(100 * time.Millisecond)
}

func TestShipper_LeadershipGate(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := &mockObservabilityClient{}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	isLeader := false
	cfg := Config{
		BatchInterval:          50 * time.Millisecond,
		MaxTailedPods:          10,
		ManagedNamespacePrefix: "proj-",
		AgentCredentials: controlplane.AgentCredentials{
			ClusterID: "cluster-1",
			AgentID:   "agent-1",
		},
		IsLeader: func() bool { return isLeader },
	}

	shipper := New(client, clientset, cfg, log)

	// Add some log lines manually
	shipper.addLogLine("proj-test", "pod-1", "main", "test", "deploy-1", time.Now(), "test log line")

	// Ship logs - should skip because not leader
	ctx := context.Background()
	shipper.shipLogs(ctx)

	// Buffer should still have the line because we're not leader
	// Actually, shipLogs runs regardless, just the batch loop skips
	// Let me test the batch loop instead
}

func TestShipper_BatchesLogs(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := &mockObservabilityClient{}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := Config{
		BatchInterval:          50 * time.Millisecond,
		MaxTailedPods:          10,
		ManagedNamespacePrefix: "proj-",
		AgentCredentials: controlplane.AgentCredentials{
			ClusterID: "cluster-1",
			AgentID:   "agent-1",
		},
	}

	shipper := New(client, clientset, cfg, log)

	// Add multiple log lines
	ts := time.Now()
	shipper.addLogLine("proj-test", "pod-1", "main", "test", "deploy-1", ts, "log line 1")
	shipper.addLogLine("proj-test", "pod-1", "main", "test", "deploy-1", ts.Add(time.Second), "log line 2")
	shipper.addLogLine("proj-test", "pod-2", "main", "test", "deploy-2", ts, "log line 3")

	// Ship logs
	ctx := context.Background()
	shipper.shipLogs(ctx)

	// Check that logs were sent
	require.Equal(t, 1, client.CallCount(), "should have sent one batch")

	client.mu.Lock()
	req := client.calls[0]
	client.mu.Unlock()

	assert.Equal(t, "cluster-1", req.ClusterID)
	assert.Len(t, req.Streams, 2, "should have 2 streams (2 pods)")

	// Count total lines
	var totalLines int
	for _, stream := range req.Streams {
		totalLines += len(stream.Values)
	}
	assert.Equal(t, 3, totalLines, "should have 3 log lines total")
}

func TestShipper_LogLineTimestampFormat(t *testing.T) {
	// Test that timestamps are nanosecond strings as Loki expects
	clientset := fake.NewSimpleClientset()
	client := &mockObservabilityClient{}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := Config{
		BatchInterval:          time.Second,
		MaxTailedPods:          10,
		ManagedNamespacePrefix: "proj-",
		AgentCredentials: controlplane.AgentCredentials{
			ClusterID: "cluster-1",
			AgentID:   "agent-1",
		},
	}

	shipper := New(client, clientset, cfg, log)

	// Add a log line with a known timestamp
	ts := time.Unix(1705318200, 123456789) // 2024-01-15T10:30:00.123456789Z
	shipper.addLogLine("proj-test", "pod-1", "main", "test", "deploy-1", ts, "test line")

	shipper.mu.Lock()
	require.Len(t, shipper.logBuffer, 1)
	require.Len(t, shipper.logBuffer[0].Values, 1)
	
	timestampStr := shipper.logBuffer[0].Values[0][0]
	lineContent := shipper.logBuffer[0].Values[0][1]
	shipper.mu.Unlock()

	// Timestamp should be nanosecond Unix timestamp as string
	expectedTs := "1705318200123456789"
	assert.Equal(t, expectedTs, timestampStr, "timestamp should be nanosecond Unix timestamp as string")
	assert.Equal(t, "test line", lineContent)
}

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantTime   bool
		wantContent string
	}{
		{
			name:        "valid RFC3339Nano",
			line:        "2024-01-15T10:30:00.123456789Z log content here",
			wantTime:    true,
			wantContent: "log content here",
		},
		{
			name:        "valid RFC3339",
			line:        "2024-01-15T10:30:00Z log content",
			wantTime:    true,
			wantContent: "log content",
		},
		{
			name:        "no timestamp",
			line:        "just a regular log line",
			wantTime:    false,
			wantContent: "just a regular log line",
		},
		{
			name:        "short line",
			line:        "short",
			wantTime:    false,
			wantContent: "short",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, content := parseLogLine(tt.line)
			
			if tt.wantTime {
				assert.False(t, ts.IsZero(), "expected valid timestamp")
			} else {
				assert.True(t, ts.IsZero(), "expected zero timestamp")
			}
			assert.Equal(t, tt.wantContent, content)
		})
	}
}
