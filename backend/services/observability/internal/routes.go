// Package observability ingests logs/metrics from agents and serves query APIs.
// See docs/04-api-spec.md section 8. Bulk data lives in Loki/Prometheus.
package observability

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts observability routes onto the app.
func RegisterRoutes(app *fiber.App) {
	g := app.Group("/v1")
	_ = g
	// TODO: agent ingest endpoints + logs/metrics/events query (separate ingest/query pools).
}
