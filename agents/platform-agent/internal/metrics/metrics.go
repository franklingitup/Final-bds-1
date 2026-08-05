// Package metrics defines the Prometheus metrics exposed by the platform agent
// and a self-contained HTTP handler to serve them.
//
// The agent historically exposed no metrics endpoint. This package is additive:
// the metrics are always registered (cheap, no behaviour change), and the HTTP
// server is only started when an address is configured (see config.MetricsAddr),
// so a leader-election-disabled agent keeps its previous no-HTTP-server profile.
//
// A dedicated registry is used rather than the global default so that
// registration is deterministic across test binaries and cannot collide with
// metrics registered elsewhere in the process.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registry = prometheus.NewRegistry()

	// IsLeader is 1 when this replica currently holds leadership, 0 otherwise.
	IsLeader = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agent_is_leader",
		Help: "Whether this agent replica currently holds the leader lease (1) or is a follower (0).",
	})

	// LeaderTransitions counts leadership state changes observed by this
	// replica (acquired and lost both increment it).
	LeaderTransitions = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_leader_transitions_total",
		Help: "Total number of leadership transitions (acquired or lost) observed by this replica.",
	})

	// LeaderElectionDuration records how long this replica waited, in seconds,
	// from starting to participate in an election until it acquired leadership.
	LeaderElectionDuration = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agent_leader_election_duration_seconds",
		Help: "Seconds spent participating in leader election before this replica most recently acquired leadership.",
	})

	// ReconcileSkipped counts reconcile/sync cycles skipped because this
	// replica was a follower at the time.
	ReconcileSkipped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_reconcile_skipped_total",
		Help: "Total number of reconcile/sync cycles skipped because this replica was not the leader.",
	})

	// ---- Registration & recovery lifecycle ---------------------------------

	// RegistrationAttempts counts registration attempts made against the control
	// plane (each pass through the register/backoff loop increments it once).
	RegistrationAttempts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_registration_attempt_total",
		Help: "Total number of agent registration attempts against the control plane.",
	})

	// RegistrationSuccess counts fresh registrations that completed successfully
	// (a previously-unregistered agent obtained a cluster).
	RegistrationSuccess = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_registration_success_total",
		Help: "Total number of successful fresh agent registrations.",
	})

	// RegistrationRecovered counts registrations satisfied by recovering an
	// already-registered cluster (idempotent register or the recover endpoint),
	// typically after local state loss.
	RegistrationRecovered = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_registration_recovered_total",
		Help: "Total number of registrations recovered from an existing cluster after state loss.",
	})

	// RegistrationFailure counts registration attempts that failed (network
	// error, control plane unavailable, or unrecoverable token error).
	RegistrationFailure = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_registration_failure_total",
		Help: "Total number of failed agent registration attempts.",
	})

	// RecoverRequests counts calls the agent makes to the control-plane recovery
	// endpoint (GET /v1/agent/recover), whether triggered at startup, on a
	// registration conflict, or after heartbeat rejection.
	RecoverRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_recover_requests_total",
		Help: "Total number of control-plane recovery (GET /agent/recover) requests made by the agent.",
	})

	// RegistrationDuration observes, in seconds, how long establishing
	// registration took (from the first attempt until success or recovery).
	RegistrationDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "agent_registration_duration_seconds",
		Help:    "Time taken to establish registration (fresh or recovered), in seconds.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
	})

	// StateLoad counts attempts to load persisted agent state from disk.
	StateLoad = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_state_load_total",
		Help: "Total number of agent state-file load operations.",
	})

	// StateLoadFailure counts state-file loads that failed (missing files are
	// not failures; unreadable or corrupt files are).
	StateLoadFailure = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_state_load_failures_total",
		Help: "Total number of agent state-file load failures (unreadable or corrupt state).",
	})

	// StateSave counts attempts to persist agent state to disk.
	StateSave = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_state_save_total",
		Help: "Total number of agent state-file save operations.",
	})

	// StateSaveFailure counts state-file saves that failed (e.g. read-only
	// volume, full disk, rename failure).
	StateSaveFailure = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_state_save_failures_total",
		Help: "Total number of agent state-file save failures.",
	})

	// HeartbeatSuccess counts heartbeats accepted by the control plane.
	HeartbeatSuccess = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_heartbeat_success_total",
		Help: "Total number of heartbeats successfully delivered to the control plane.",
	})

	// HeartbeatFailure counts heartbeats rejected or failed in transit.
	HeartbeatFailure = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_heartbeat_failure_total",
		Help: "Total number of heartbeats that failed or were rejected by the control plane.",
	})

	// ConfigMapApply counts ConfigMap create/update operations applied to the
	// cluster (no-ops are not counted).
	ConfigMapApply = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_configmap_apply_total",
		Help: "Total number of ConfigMap create/update operations applied by the agent.",
	})

	// ConfigMapApplyDuration observes the wall-clock duration of each ConfigMap
	// apply call (create/update/no-op), in seconds.
	ConfigMapApplyDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "agent_configmap_apply_duration_seconds",
		Help:    "Duration of ConfigMap apply operations in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// ConfigMapDrift counts ConfigMaps that were found drifted from desired
	// state and corrected via update.
	ConfigMapDrift = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_configmap_drift_total",
		Help: "Total number of ConfigMaps found drifted from desired state and corrected.",
	})

	// ConfigMapDelete counts ConfigMaps deleted during orphan garbage collection.
	ConfigMapDelete = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_configmap_delete_total",
		Help: "Total number of platform-owned ConfigMaps deleted by the agent.",
	})

	// PVCApply counts PersistentVolumeClaim create/update operations applied to
	// the cluster (no-ops and immutable-skips are not counted).
	PVCApply = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_pvc_apply_total",
		Help: "Total number of PersistentVolumeClaim create/update operations applied by the agent.",
	})

	// PVCApplyDuration observes the wall-clock duration of each PVC apply call.
	PVCApplyDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "agent_pvc_apply_duration_seconds",
		Help:    "Duration of PersistentVolumeClaim apply operations in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// PVCDrift counts PVCs found drifted on a legal field and corrected via update.
	PVCDrift = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_pvc_drift_total",
		Help: "Total number of PersistentVolumeClaims found drifted from desired state and corrected.",
	})

	// PVCDelete counts PVCs deleted during orphan garbage collection.
	PVCDelete = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_pvc_delete_total",
		Help: "Total number of platform-owned PersistentVolumeClaims deleted by the agent.",
	})

	// PVCImmutableChange counts desired-state changes to immutable PVC fields
	// (or storage shrink) that were logged and skipped rather than applied.
	PVCImmutableChange = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_pvc_immutable_change_total",
		Help: "Total number of PVC updates skipped because an immutable field changed.",
	})

	// IngressApply counts Ingress create/update operations applied to the
	// cluster (no-ops are not counted).
	IngressApply = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_ingress_apply_total",
		Help: "Total number of Ingress create/update operations applied by the agent.",
	})

	// IngressApplyDuration observes the wall-clock duration of each Ingress apply.
	IngressApplyDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "agent_ingress_apply_duration_seconds",
		Help:    "Duration of Ingress apply operations in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// IngressDrift counts Ingresses found drifted from desired state and corrected.
	IngressDrift = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_ingress_drift_total",
		Help: "Total number of Ingresses found drifted from desired state and corrected.",
	})

	// IngressDelete counts Ingresses deleted during orphan garbage collection.
	IngressDelete = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_ingress_delete_total",
		Help: "Total number of platform-owned Ingresses deleted by the agent.",
	})
)

func init() {
	registry.MustRegister(
		IsLeader,
		LeaderTransitions,
		LeaderElectionDuration,
		ReconcileSkipped,
		RegistrationAttempts,
		RegistrationSuccess,
		RegistrationRecovered,
		RegistrationFailure,
		RecoverRequests,
		RegistrationDuration,
		StateLoad,
		StateLoadFailure,
		StateSave,
		StateSaveFailure,
		HeartbeatSuccess,
		HeartbeatFailure,
		ConfigMapApply,
		ConfigMapApplyDuration,
		ConfigMapDrift,
		ConfigMapDelete,
		PVCApply,
		PVCApplyDuration,
		PVCDrift,
		PVCDelete,
		PVCImmutableChange,
		IngressApply,
		IngressApplyDuration,
		IngressDrift,
		IngressDelete,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// Registry returns the agent's dedicated Prometheus registry.
func Registry() *prometheus.Registry { return registry }

// Handler returns an http.Handler that serves the agent metrics registry in
// the Prometheus text exposition format.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}
