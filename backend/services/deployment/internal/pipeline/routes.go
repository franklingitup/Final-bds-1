package pipeline

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts pipeline routes onto the app.
func RegisterRoutes(app *fiber.App, h *Handler, requireAuth fiber.Handler) {
	v1 := app.Group("/v1")

	// Authenticated routes
	authenticated := v1.Group("", requireAuth)

	// Pipeline operations at deployment level
	deployments := authenticated.Group("/organizations/:orgId/deployments/:deploymentId")
	deployments.Post("/deploy", h.QuickDeploy)
	deployments.Post("/rollback", h.TriggerRollback)
	deployments.Get("/desired-state", h.GetDesiredState)
	deployments.Get("/metrics", h.GetDeploymentMetrics)
	deployments.Get("/pipelines", h.ListPipelineRuns)
	deployments.Post("/pipelines", h.TriggerPipeline)

	// Individual pipeline operations
	pipelines := authenticated.Group("/organizations/:orgId/pipelines/:pipelineId")
	pipelines.Get("", h.GetPipelineRun)
	pipelines.Post("/cancel", h.CancelPipeline)
	pipelines.Get("/events", h.GetPipelineEvents)

	// Org-level pipeline trigger
	authenticated.Post("/organizations/:orgId/pipelines", h.TriggerPipeline)

	// Agent endpoints (should be protected with service-to-service auth in production)
	agent := v1.Group("/agent/organizations/:orgId/clusters/:clusterId")
	agent.Get("/desired-state", h.GetAgentDesiredState)
	agent.Post("/sync", h.ReportSync)
	agent.Post("/metrics", h.ReportMetrics)
}
