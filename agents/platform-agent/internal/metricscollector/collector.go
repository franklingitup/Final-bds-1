// Package metricscollector implements resource metrics collection and shipping.
//
// The collector periodically queries metrics-server (if available) for pod resource
// usage in managed namespaces and ships them to the control plane's observability
// service. It gracefully handles clusters without metrics-server installed.
package metricscollector

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/metrics"
)

// PodMetricsList is the response from metrics-server's pod metrics API.
// We define this locally to avoid a dependency on k8s.io/metrics.
type PodMetricsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodMetrics `json:"items"`
}

// PodMetrics contains resource metrics for a single pod.
type PodMetrics struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Timestamp         metav1.Time        `json:"timestamp"`
	Window            metav1.Duration    `json:"window"`
	Containers        []ContainerMetrics `json:"containers"`
}

// ContainerMetrics contains resource metrics for a single container.
type ContainerMetrics struct {
	Name  string                      `json:"name"`
	Usage map[string]resource.Quantity `json:"usage"`
}

// Config holds metrics collector configuration.
type Config struct {
	// Interval between collection cycles.
	Interval time.Duration
	// ManagedNamespacePrefix filters namespaces to collect from.
	ManagedNamespacePrefix string
	// AgentCredentials for authenticating with the Observability Service.
	AgentCredentials controlplane.AgentCredentials
	// IsLeader gates collection on leadership. When nil (leader election disabled)
	// every cycle proceeds; when set, cycles are skipped when not leader.
	IsLeader func() bool
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Interval:               60 * time.Second,
		ManagedNamespacePrefix: "proj-",
	}
}

// ObservabilityClient sends metrics to the control plane.
type ObservabilityClient interface {
	IngestMetrics(ctx context.Context, creds controlplane.AgentCredentials, req controlplane.IngestMetricsRequest) error
}

// Collector collects resource metrics from pods and ships them.
type Collector struct {
	client    ObservabilityClient
	clientset kubernetes.Interface
	cfg       Config
	log       *slog.Logger

	mu                   sync.RWMutex
	metricsServerChecked bool
	metricsServerOK      bool
}

// New creates a new Collector.
func New(client ObservabilityClient, clientset kubernetes.Interface, cfg Config, log *slog.Logger) *Collector {
	return &Collector{
		client:    client,
		clientset: clientset,
		cfg:       cfg,
		log:       log,
	}
}

// Run starts the collection loop and blocks until context is cancelled.
func (c *Collector) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	// Run initial collection
	c.collect(ctx)

	for {
		select {
		case <-ctx.Done():
			c.log.Info("metrics collector stopped")
			return ctx.Err()
		case <-ticker.C:
			c.collect(ctx)
		}
	}
}

// collect performs a single collection cycle.
func (c *Collector) collect(ctx context.Context) {
	// Leadership gate: followers do not collect metrics
	if c.cfg.IsLeader != nil && !c.cfg.IsLeader() {
		metrics.ReconcileSkipped.Inc()
		c.log.Debug("skipping metrics collection cycle: not leader")
		return
	}

	start := time.Now()
	metrics.MetricsCollectorCycles.Inc()

	// Check metrics-server availability (once per collector lifecycle, then cached)
	if !c.checkMetricsServer(ctx) {
		return
	}

	// List managed namespaces
	namespaces, err := c.listManagedNamespaces(ctx)
	if err != nil {
		c.log.Error("failed to list managed namespaces", "error", err)
		return
	}

	if len(namespaces) == 0 {
		c.log.Debug("no managed namespaces found")
		return
	}

	// Collect metrics from all managed namespaces
	var allMetrics []controlplane.IngestMetricData
	for _, ns := range namespaces {
		nsMetrics, err := c.collectNamespaceMetrics(ctx, ns)
		if err != nil {
			c.log.Warn("failed to collect metrics from namespace", "namespace", ns, "error", err)
			continue
		}
		allMetrics = append(allMetrics, nsMetrics...)
	}

	if len(allMetrics) == 0 {
		c.log.Debug("no metrics collected")
		metrics.MetricsCollectorCycleDuration.Observe(time.Since(start).Seconds())
		return
	}

	// Ship metrics to control plane
	req := controlplane.IngestMetricsRequest{
		ClusterID: c.cfg.AgentCredentials.ClusterID,
		Metrics:   allMetrics,
	}

	if err := c.client.IngestMetrics(ctx, c.cfg.AgentCredentials, req); err != nil {
		c.log.Error("failed to ingest metrics", "error", err, "metric_count", len(allMetrics))
		metrics.MetricsCollectorIngestFailure.Inc()
	} else {
		c.log.Debug("metrics ingested successfully", "metric_count", len(allMetrics))
		metrics.MetricsCollectorIngestSuccess.Inc()
	}

	metrics.MetricsCollectorCycleDuration.Observe(time.Since(start).Seconds())
}

// checkMetricsServer checks if metrics-server is available.
// Logs a warning once if unavailable.
func (c *Collector) checkMetricsServer(ctx context.Context) bool {
	c.mu.RLock()
	if c.metricsServerChecked {
		ok := c.metricsServerOK
		c.mu.RUnlock()
		return ok
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.metricsServerChecked {
		return c.metricsServerOK
	}

	// Check if metrics.k8s.io API is available
	apiGroups, err := c.clientset.Discovery().ServerGroups()
	if err != nil {
		c.log.Warn("metrics-server not detected: failed to query API groups",
			"error", err)
		c.metricsServerChecked = true
		c.metricsServerOK = false
		metrics.MetricsServerAvailable.Set(0)
		return false
	}

	found := false
	for _, group := range apiGroups.Groups {
		if group.Name == "metrics.k8s.io" {
			found = true
			break
		}
	}

	if !found {
		c.log.Warn("metrics-server not detected; resource metrics unavailable for this cluster",
			"hint", "Install metrics-server to enable CPU/memory metrics collection")
		c.metricsServerChecked = true
		c.metricsServerOK = false
		metrics.MetricsServerAvailable.Set(0)
		return false
	}

	c.log.Info("metrics-server detected, resource metrics collection enabled")
	c.metricsServerChecked = true
	c.metricsServerOK = true
	metrics.MetricsServerAvailable.Set(1)
	return true
}

// listManagedNamespaces returns namespaces matching the managed prefix.
func (c *Collector) listManagedNamespaces(ctx context.Context) ([]string, error) {
	nsList, err := c.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var managed []string
	for _, ns := range nsList.Items {
		if strings.HasPrefix(ns.Name, c.cfg.ManagedNamespacePrefix) {
			managed = append(managed, ns.Name)
		}
	}
	return managed, nil
}

// collectNamespaceMetrics collects pod metrics from a namespace.
func (c *Collector) collectNamespaceMetrics(ctx context.Context, namespace string) ([]controlplane.IngestMetricData, error) {
	// Query metrics-server via raw REST API
	data, err := c.clientset.CoreV1().RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1").
		Resource("pods").
		Namespace(namespace).
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}

	var podMetrics PodMetricsList
	if err := json.Unmarshal(data, &podMetrics); err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	var result []controlplane.IngestMetricData

	for _, pm := range podMetrics.Items {
		// Get the deployment ID from pod labels
		pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, pm.Name, metav1.GetOptions{})
		if err != nil {
			c.log.Debug("failed to get pod for metrics", "pod", pm.Name, "namespace", namespace, "error", err)
			continue
		}

		deploymentID := pod.Labels["bdsplatform.io/deployment-id"]
		if deploymentID == "" {
			// Not a platform-managed pod
			continue
		}

		metrics.MetricsCollectorPodsCollected.Inc()

		// Extract project slug from namespace (proj-<slug>)
		projectSlug := strings.TrimPrefix(namespace, c.cfg.ManagedNamespacePrefix)

		for _, container := range pm.Containers {
			labels := map[string]string{
				"namespace":     namespace,
				"pod":           pm.Name,
				"container":     container.Name,
				"project_slug":  projectSlug,
				"deployment_id": deploymentID,
			}

			// CPU usage (in millicores -> cores)
			if cpu, ok := container.Usage["cpu"]; ok {
				cpuMillis := cpu.MilliValue()
				result = append(result, controlplane.IngestMetricData{
					Name:         "container_cpu_usage_cores",
					ResourceType: "deployment",
					ResourceID:   deploymentID,
					Labels:       labels,
					Value:        float64(cpuMillis) / 1000.0,
					Timestamp:    now,
				})
			}

			// Memory usage (in bytes)
			if mem, ok := container.Usage["memory"]; ok {
				memBytes := mem.Value()
				result = append(result, controlplane.IngestMetricData{
					Name:         "container_memory_usage_bytes",
					ResourceType: "deployment",
					ResourceID:   deploymentID,
					Labels:       labels,
					Value:        float64(memBytes),
					Timestamp:    now,
				})
			}
		}
	}

	return result, nil
}

// HasMetricsServer returns whether metrics-server was detected.
// Only valid after at least one collection cycle.
func (c *Collector) HasMetricsServer() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metricsServerOK
}
