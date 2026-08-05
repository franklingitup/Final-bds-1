// Package observability ingests logs/metrics from agents and serves query APIs.
// See docs/04-api-spec.md section 8. Bulk data lives in Loki/Prometheus.
package observability

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts observability routes onto the app with the given handler and auth middleware.
func RegisterRoutes(app *fiber.App, h *Handler, auth fiber.Handler) {
	v1 := app.Group("/v1")

	// Health check (public)
	app.Get("/health", h.HealthCheck)

	// Metrics (requires auth)
	metrics := v1.Group("/organizations/:orgId/metrics", auth)
	metrics.Get("/query", h.QueryMetrics)
	metrics.Post("/query", h.QueryMetrics)
	metrics.Get("/query_instant", h.QueryInstantMetrics)
	metrics.Get("/:resourceType/:resourceId", h.GetResourceMetrics)

	// Logs (requires auth)
	logs := v1.Group("/organizations/:orgId/logs", auth)
	logs.Get("/query", h.QueryLogs)
	logs.Post("/query", h.QueryLogs)
	logs.Get("/streams", h.GetLogStreams)

	// Dashboards (requires auth)
	dashboards := v1.Group("/organizations/:orgId/dashboards", auth)
	dashboards.Post("", h.CreateDashboard)
	dashboards.Get("", h.ListDashboards)
	dashboards.Get("/:dashboardId", h.GetDashboard)
	dashboards.Put("/:dashboardId", h.UpdateDashboard)
	dashboards.Delete("/:dashboardId", h.DeleteDashboard)

	// Health checks (requires auth)
	health := v1.Group("/organizations/:orgId/health", auth)
	health.Get("/summary", h.GetHealthSummary)
	health.Get("/:resourceType/:resourceId", h.GetResourceHealth)

	// Events (requires auth)
	events := v1.Group("/organizations/:orgId/events", auth)
	events.Get("", h.ListEvents)

	// Alerts (requires auth)
	alerts := v1.Group("/organizations/:orgId/alerts", auth)
	alerts.Post("/rules", h.CreateAlertRule)
	alerts.Get("/rules", h.ListAlertRules)
	alerts.Get("/firing", h.GetFiringAlerts)

	// Overview (requires auth)
	v1.Get("/organizations/:orgId/observability/overview", auth, h.GetOverview)

	// Agent ingest endpoints (credential-based, no user auth)
	agent := v1.Group("/agent")
	agent.Post("/metrics", h.IngestMetrics)
	agent.Post("/logs", h.IngestLogs)
	agent.Post("/health", h.ReportHealth)
}
