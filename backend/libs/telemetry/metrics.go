package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HTTPRequests counts handled HTTP requests labeled by method, route and status.
var HTTPRequests = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	},
	[]string{"method", "route", "status"},
)

// HTTPDuration observes request latency in seconds.
var HTTPDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	},
	[]string{"method", "route"},
)

// MetricsHandler returns the Prometheus scrape handler.
func MetricsHandler() http.Handler { return promhttp.Handler() }
