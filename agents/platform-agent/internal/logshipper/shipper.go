// Package logshipper implements log tailing and shipping to the control plane.
//
// The shipper tails logs from pods in managed namespaces and ships them in
// batches to the control plane's observability service. It uses a concurrency
// cap to prevent resource exhaustion on clusters with many pods.
package logshipper

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/metrics"
)

// Config holds log shipper configuration.
type Config struct {
	// BatchInterval is how often to batch and ship logs.
	BatchInterval time.Duration
	// MaxTailedPods caps how many pods to actively tail at once.
	MaxTailedPods int
	// ManagedNamespacePrefix filters namespaces to collect from.
	ManagedNamespacePrefix string
	// AgentCredentials for authenticating with the Observability Service.
	AgentCredentials controlplane.AgentCredentials
	// IsLeader gates log shipping on leadership. When nil (leader election disabled)
	// every cycle proceeds; when set, shipping is skipped when not leader.
	IsLeader func() bool
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BatchInterval:          5 * time.Second,
		MaxTailedPods:          50,
		ManagedNamespacePrefix: "proj-",
	}
}

// ObservabilityClient sends logs to the control plane.
type ObservabilityClient interface {
	IngestLogs(ctx context.Context, creds controlplane.AgentCredentials, req controlplane.IngestLogsRequest) error
}

// podKey uniquely identifies a pod for tracking.
type podKey struct {
	namespace string
	name      string
}

func (k podKey) String() string { return k.namespace + "/" + k.name }

// tailedPod tracks a pod being actively tailed.
type tailedPod struct {
	key          podKey
	deploymentID string
	startTime    time.Time
	cancel       context.CancelFunc
}

// Shipper tails logs from managed pods and ships them to the control plane.
type Shipper struct {
	client    ObservabilityClient
	clientset kubernetes.Interface
	cfg       Config
	log       *slog.Logger

	mu          sync.Mutex
	tailedPods  map[podKey]*tailedPod
	logBuffer   []controlplane.IngestLogEntry
	capExceeded bool // tracks if we've warned about cap being exceeded
}

// New creates a new Shipper.
func New(client ObservabilityClient, clientset kubernetes.Interface, cfg Config, log *slog.Logger) *Shipper {
	return &Shipper{
		client:     client,
		clientset:  clientset,
		cfg:        cfg,
		log:        log,
		tailedPods: make(map[podKey]*tailedPod),
	}
}

// Run starts the log shipping loop and blocks until context is cancelled.
func (s *Shipper) Run(ctx context.Context) error {
	// Start watching pods in managed namespaces
	go s.watchPods(ctx)

	// Start the batch shipping loop
	s.batchShippingLoop(ctx)

	return ctx.Err()
}

// watchPods watches for pod events in managed namespaces and starts/stops tailers.
func (s *Shipper) watchPods(ctx context.Context) {
	// List existing pods first
	s.syncExistingPods(ctx)

	// Watch for pod changes
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Watch pods in all namespaces (we'll filter by prefix)
		watcher, err := s.clientset.CoreV1().Pods("").Watch(ctx, metav1.ListOptions{})
		if err != nil {
			s.log.Error("failed to watch pods", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		s.handlePodEvents(ctx, watcher)
		watcher.Stop()
	}
}

// syncExistingPods lists all existing pods and starts tailers for them.
func (s *Shipper) syncExistingPods(ctx context.Context) {
	// List all pods in managed namespaces
	namespaces, err := s.listManagedNamespaces(ctx)
	if err != nil {
		s.log.Error("failed to list managed namespaces for pod sync", "error", err)
		return
	}

	for _, ns := range namespaces {
		pods, err := s.clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			s.log.Warn("failed to list pods in namespace", "namespace", ns, "error", err)
			continue
		}

		for i := range pods.Items {
			s.handlePodAdded(ctx, &pods.Items[i])
		}
	}
}

// handlePodEvents processes pod watch events.
func (s *Shipper) handlePodEvents(ctx context.Context, watcher watch.Interface) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return
			}

			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}

			// Filter to managed namespaces
			if !strings.HasPrefix(pod.Namespace, s.cfg.ManagedNamespacePrefix) {
				continue
			}

			switch event.Type {
			case watch.Added, watch.Modified:
				s.handlePodAdded(ctx, pod)
			case watch.Deleted:
				s.handlePodDeleted(pod)
			}
		}
	}
}

// handlePodAdded starts tailing a pod if it's eligible and we have capacity.
func (s *Shipper) handlePodAdded(ctx context.Context, pod *corev1.Pod) {
	// Check if pod is running and has the platform label
	if pod.Status.Phase != corev1.PodRunning {
		return
	}

	deploymentID := pod.Labels["bdsplatform.io/deployment-id"]
	if deploymentID == "" {
		return
	}

	key := podKey{namespace: pod.Namespace, name: pod.Name}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Already tailing this pod
	if _, exists := s.tailedPods[key]; exists {
		return
	}

	// Check capacity
	if len(s.tailedPods) >= s.cfg.MaxTailedPods {
		// Capacity exceeded - prioritize newest pods by removing oldest
		s.handleCapExceeded(ctx, pod, deploymentID)
		return
	}

	s.startTailing(ctx, pod, deploymentID)
}

// handleCapExceeded handles the case when we can't tail more pods.
// It implements the "prioritize newest pods" policy.
func (s *Shipper) handleCapExceeded(ctx context.Context, newPod *corev1.Pod, deploymentID string) {
	// Log warning (once per breach, not per pod)
	if !s.capExceeded {
		s.capExceeded = true
		s.log.Warn("log shipper: pod cap exceeded, newest pods are prioritized; oldest pods will not be tailed",
			"max_tailed_pods", s.cfg.MaxTailedPods,
			"current_tailed", len(s.tailedPods))
		metrics.LogShipperCapExceeded.Inc()
	}

	// Find the oldest currently-tailed pod
	var oldestKey podKey
	var oldestTime time.Time
	for k, tp := range s.tailedPods {
		if oldestTime.IsZero() || tp.startTime.Before(oldestTime) {
			oldestTime = tp.startTime
			oldestKey = k
		}
	}

	// If the new pod is newer than the oldest, swap them
	newPodStartTime := getPodStartTime(newPod)
	if newPodStartTime.After(oldestTime) {
		s.log.Debug("swapping out older pod to tail newer pod",
			"removed", oldestKey.String(),
			"added", newPod.Namespace+"/"+newPod.Name)

		// Stop tailing the oldest
		if tp, exists := s.tailedPods[oldestKey]; exists {
			tp.cancel()
			delete(s.tailedPods, oldestKey)
		}

		// Start tailing the new pod
		s.startTailing(ctx, newPod, deploymentID)
	}

	s.updateSkippedMetric()
}

// startTailing starts a log tailer goroutine for a pod.
func (s *Shipper) startTailing(ctx context.Context, pod *corev1.Pod, deploymentID string) {
	key := podKey{namespace: pod.Namespace, name: pod.Name}
	tailCtx, cancel := context.WithCancel(ctx)

	tp := &tailedPod{
		key:          key,
		deploymentID: deploymentID,
		startTime:    getPodStartTime(pod),
		cancel:       cancel,
	}
	s.tailedPods[key] = tp

	// Start tailer for each container
	for _, container := range pod.Spec.Containers {
		go s.tailContainer(tailCtx, pod, container.Name, deploymentID)
	}

	metrics.LogShipperPodsTailed.Set(float64(len(s.tailedPods)))
	s.log.Debug("started tailing pod",
		"namespace", pod.Namespace,
		"pod", pod.Name,
		"deployment_id", deploymentID,
		"containers", len(pod.Spec.Containers))
}

// handlePodDeleted stops tailing a deleted pod.
func (s *Shipper) handlePodDeleted(pod *corev1.Pod) {
	key := podKey{namespace: pod.Namespace, name: pod.Name}

	s.mu.Lock()
	defer s.mu.Unlock()

	if tp, exists := s.tailedPods[key]; exists {
		tp.cancel()
		delete(s.tailedPods, key)
		metrics.LogShipperPodsTailed.Set(float64(len(s.tailedPods)))
		s.log.Debug("stopped tailing deleted pod", "namespace", pod.Namespace, "pod", pod.Name)
	}

	// Reset cap exceeded flag if we're now under capacity
	if len(s.tailedPods) < s.cfg.MaxTailedPods {
		s.capExceeded = false
	}

	s.updateSkippedMetric()
}

// tailContainer tails logs from a specific container.
func (s *Shipper) tailContainer(ctx context.Context, pod *corev1.Pod, containerName, deploymentID string) {
	sinceTime := metav1.NewTime(time.Now())

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		stream, err := s.clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			Container:  containerName,
			Follow:     true,
			Timestamps: true,
			SinceTime:  &sinceTime,
		}).Stream(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Debug("failed to start log stream, retrying",
				"namespace", pod.Namespace,
				"pod", pod.Name,
				"container", containerName,
				"error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		s.processLogStream(ctx, stream, pod, containerName, deploymentID, &sinceTime)
		stream.Close()

		// Brief delay before reconnecting
		if ctx.Err() != nil {
			return
		}
		time.Sleep(time.Second)
	}
}

// processLogStream reads lines from a log stream and buffers them.
func (s *Shipper) processLogStream(ctx context.Context, stream io.ReadCloser, pod *corev1.Pod, containerName, deploymentID string, sinceTime *metav1.Time) {
	reader := bufio.NewReader(stream)
	projectSlug := strings.TrimPrefix(pod.Namespace, s.cfg.ManagedNamespacePrefix)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF || ctx.Err() != nil {
				return
			}
			s.log.Debug("error reading log line", "error", err)
			return
		}

		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}

		// Parse timestamp from the line (format: "2024-01-15T10:30:00.123456789Z log content")
		ts, content := parseLogLine(line)
		if ts.IsZero() {
			ts = time.Now()
			content = line
		}

		// Update sinceTime for reconnection
		*sinceTime = metav1.NewTime(ts.Add(time.Nanosecond))

		// Add to buffer
		s.addLogLine(pod.Namespace, pod.Name, containerName, projectSlug, deploymentID, ts, content)
	}
}

// addLogLine adds a log line to the buffer.
func (s *Shipper) addLogLine(namespace, podName, containerName, projectSlug, deploymentID string, ts time.Time, line string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find or create stream entry for this pod/container
	streamLabels := map[string]string{
		"namespace":     namespace,
		"pod":           podName,
		"container":     containerName,
		"project_slug":  projectSlug,
		"deployment_id": deploymentID,
	}

	// Look for existing stream
	var entry *controlplane.IngestLogEntry
	for i := range s.logBuffer {
		if mapsEqual(s.logBuffer[i].Stream, streamLabels) {
			entry = &s.logBuffer[i]
			break
		}
	}

	if entry == nil {
		s.logBuffer = append(s.logBuffer, controlplane.IngestLogEntry{
			Stream: streamLabels,
			Values: [][]string{},
		})
		entry = &s.logBuffer[len(s.logBuffer)-1]
	}

	// Loki expects nanosecond Unix timestamp as a STRING
	tsNano := strconv.FormatInt(ts.UnixNano(), 10)
	entry.Values = append(entry.Values, []string{tsNano, line})
}

// batchShippingLoop periodically ships buffered logs.
func (s *Shipper) batchShippingLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.BatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final flush on shutdown
			s.shipLogs(ctx)
			return
		case <-ticker.C:
			// Leadership gate: followers do not ship logs
			if s.cfg.IsLeader != nil && !s.cfg.IsLeader() {
				s.log.Debug("skipping log shipping cycle: not leader")
				continue
			}
			// Log every cycle so "loop alive but idle pod" is distinguishable
			// from "loop is dead". Fires even when buffer is empty.
			s.log.Debug("log shipping cycle",
				"buffered_streams", s.bufferedStreamCount(),
				"buffered_lines", s.bufferedLineCount())
			s.shipLogs(ctx)
		}
	}
}

// bufferedStreamCount returns the number of distinct log streams currently buffered.
// Caller must NOT hold s.mu.
func (s *Shipper) bufferedStreamCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.logBuffer)
}

// bufferedLineCount returns the total number of log lines currently buffered.
// Caller must NOT hold s.mu.
func (s *Shipper) bufferedLineCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, entry := range s.logBuffer {
		total += len(entry.Values)
	}
	return total
}

// shipLogs sends buffered logs to the control plane.
func (s *Shipper) shipLogs(ctx context.Context) {
	s.mu.Lock()
	if len(s.logBuffer) == 0 {
		s.mu.Unlock()
		// Nothing to ship this cycle — buffer is empty (pod idle or no lines yet).
		// This is normal and logged by the caller; no action needed.
		return
	}

	// Swap buffer
	toShip := s.logBuffer
	s.logBuffer = nil
	s.mu.Unlock()

	// Count lines for metrics
	var lineCount int
	for _, entry := range toShip {
		lineCount += len(entry.Values)
	}

	req := controlplane.IngestLogsRequest{
		ClusterID: s.cfg.AgentCredentials.ClusterID,
		Streams:   toShip,
	}

	if err := s.client.IngestLogs(ctx, s.cfg.AgentCredentials, req); err != nil {
		s.log.Error("failed to ship logs", "error", err, "streams", len(toShip), "lines", lineCount)
		metrics.LogShipperBatchesFailures.Inc()
		return
	}

	metrics.LogShipperBatchesSent.Inc()
	metrics.LogShipperLinesShipped.Add(float64(lineCount))
	s.log.Debug("shipped logs", "streams", len(toShip), "lines", lineCount)
}

// listManagedNamespaces returns namespaces matching the managed prefix.
func (s *Shipper) listManagedNamespaces(ctx context.Context) ([]string, error) {
	nsList, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var managed []string
	for _, ns := range nsList.Items {
		if strings.HasPrefix(ns.Name, s.cfg.ManagedNamespacePrefix) {
			managed = append(managed, ns.Name)
		}
	}
	return managed, nil
}

// updateSkippedMetric updates the pods_skipped gauge.
func (s *Shipper) updateSkippedMetric() {
	// This is an approximation - we'd need to count all eligible pods
	// For now, just set based on whether we're at capacity
	if len(s.tailedPods) >= s.cfg.MaxTailedPods {
		// We're at cap, but we don't know exactly how many pods are being skipped
		// This would require periodic scanning of all pods
		metrics.LogShipperPodsSkipped.Set(1) // At least 1 is being skipped
	} else {
		metrics.LogShipperPodsSkipped.Set(0)
	}
}

// TailedPodCount returns the number of pods currently being tailed.
func (s *Shipper) TailedPodCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tailedPods)
}

// getPodStartTime returns the pod's start time or creation time.
func getPodStartTime(pod *corev1.Pod) time.Time {
	if pod.Status.StartTime != nil {
		return pod.Status.StartTime.Time
	}
	return pod.CreationTimestamp.Time
}

// parseLogLine parses a Kubernetes log line with RFC3339 timestamp prefix.
func parseLogLine(line string) (time.Time, string) {
	// Format: "2024-01-15T10:30:00.123456789Z log content here"
	if len(line) < 30 {
		return time.Time{}, line
	}

	// Find the space after the timestamp
	idx := strings.Index(line, " ")
	if idx < 20 { // timestamps are at least 20 chars
		return time.Time{}, line
	}

	ts, err := time.Parse(time.RFC3339Nano, line[:idx])
	if err != nil {
		return time.Time{}, line
	}

	return ts, line[idx+1:]
}

// mapsEqual compares two string maps for equality.
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// Unused but demonstrates the priority sorting approach
var _ = sort.Interface(podsByStartTime{})

type podsByStartTime []*tailedPod

func (p podsByStartTime) Len() int           { return len(p) }
func (p podsByStartTime) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p podsByStartTime) Less(i, j int) bool { return p[i].startTime.Before(p[j].startTime) }
