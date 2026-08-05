package deployment

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/bdsplatform/platform/backend/libs/argocd"
)

// GitOps (Argo CD) engine metrics. Registered on the default Prometheus registry
// via promauto and exposed through the shared httpserver /metrics endpoint. They
// are low-cardinality: only the sync-total counter and the health gauge carry a
// bounded label (outcome / status), never per-deployment identifiers.
var (
	// deploymentSyncTotal counts Argo CD sync operations observed by the engine,
	// labelled started|completed|failed.
	deploymentSyncTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "deployment_sync_total",
		Help: "Argo CD sync operations observed by the deployment engine, by outcome.",
	}, []string{"outcome"})

	// deploymentSyncDuration observes, in seconds, the wall-clock time of an Argo
	// CD sync operation (started -> succeeded/failed) as observed by the monitor.
	deploymentSyncDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "deployment_sync_duration_seconds",
		Help:    "Duration of Argo CD sync operations, in seconds.",
		Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600},
	})

	// deploymentSyncFailuresTotal counts failed Argo CD sync operations.
	deploymentSyncFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "deployment_sync_failures_total",
		Help: "Total number of failed Argo CD sync operations.",
	})

	// deploymentHealthStatus is a gauge set to 1 for the current Argo CD health
	// status bucket and 0 for the others, giving a live per-status count.
	deploymentHealthStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "deployment_health_status",
		Help: "Number of managed Argo CD applications currently in each health status.",
	}, []string{"status"})

	// deploymentDriftTotal counts drift-detection events (OutOfSync / degraded).
	deploymentDriftTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "deployment_drift_total",
		Help: "Total number of Argo CD drift-detection events.",
	})

	// deploymentRollbacksTotal counts Argo CD rollbacks triggered by the engine.
	deploymentRollbacksTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "deployment_rollbacks_total",
		Help: "Total number of Argo CD rollbacks triggered by the deployment engine.",
	})
)

// Sync outcome labels for deployment_sync_total.
const (
	syncOutcomeStarted   = "started"
	syncOutcomeCompleted = "completed"
	syncOutcomeFailed    = "failed"
)

// recordHealthGauge sets the health gauge for a status transition: it clears the
// previous bucket and increments the new one so the gauge reflects the live
// distribution across managed applications.
func recordHealthGauge(from, to string) {
	if from != "" && from != to {
		deploymentHealthStatus.WithLabelValues(healthLabel(from)).Dec()
	}
	if to != "" && from != to {
		deploymentHealthStatus.WithLabelValues(healthLabel(to)).Inc()
	}
}

// healthLabel normalizes an Argo CD health status to a stable, bounded label.
func healthLabel(s string) string {
	switch s {
	case argocd.HealthStatusHealthy, argocd.HealthStatusProgressing, argocd.HealthStatusDegraded,
		argocd.HealthStatusSuspended, argocd.HealthStatusMissing:
		return s
	default:
		return argocd.HealthStatusUnknown
	}
}
