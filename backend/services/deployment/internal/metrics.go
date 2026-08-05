package deployment

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Deployment engine (rollout) metrics. Registered on the default Prometheus
// registry via promauto and exposed through the shared httpserver /metrics
// endpoint. They are process-global and low-cardinality (no per-deployment
// labels) to stay bounded.
var (
	// rolloutDuration observes, in seconds, the wall-clock time from a
	// release's start until it reached a healthy rollout.
	rolloutDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "deployment_rollout_duration_seconds",
		Help:    "Time from release start to a healthy rollout, in seconds.",
		Buckets: []float64{5, 15, 30, 60, 120, 300, 600, 1200},
	})

	// rolloutSuccessTotal counts rollouts that reached a healthy state.
	rolloutSuccessTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "deployment_rollout_success_total",
		Help: "Total number of rollouts that reached a healthy state.",
	})

	// rolloutFailureTotal counts rollouts that reached a failed state
	// (including timeouts).
	rolloutFailureTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "deployment_rollout_failure_total",
		Help: "Total number of rollouts that reached a failed state.",
	})

	// rolloutTimeoutTotal counts rollouts that failed specifically because the
	// progress deadline was exceeded.
	rolloutTimeoutTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "deployment_rollout_timeout_total",
		Help: "Total number of rollouts that failed because the progress deadline was exceeded.",
	})

	// rolloutProgressPercentage reflects the most recently reported rollout
	// completion percentage across all deployments.
	rolloutProgressPercentage = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "deployment_progress_percentage",
		Help: "Most recently reported rollout completion percentage (0-100).",
	})
)
