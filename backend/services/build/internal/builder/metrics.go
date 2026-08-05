package builder

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Kaniko build-engine metrics. These register against the default Prometheus
// registry, which the service already exposes via telemetry.MetricsHandler.
var (
	metricJobsStarted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "build_kaniko_jobs_started_total",
		Help: "Total number of Kaniko build jobs scheduled by the build worker.",
	})

	metricJobsSucceeded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "build_kaniko_jobs_succeeded_total",
		Help: "Total number of Kaniko build jobs that completed successfully.",
	})

	// metricJobsFailed is labelled by failure stage: spec | run | build | digest.
	metricJobsFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "build_kaniko_jobs_failed_total",
		Help: "Total number of Kaniko build jobs that failed, labelled by stage.",
	}, []string{"stage"})

	metricJobDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "build_kaniko_job_duration_seconds",
		Help:    "Wall-clock duration of Kaniko build jobs in seconds.",
		Buckets: []float64{5, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600},
	})
)
