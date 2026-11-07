package pipeline

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts pipeline routes onto the app.
// requireAuth protects user/JWT org routes. agentAuth must be the same
// cluster-credential middleware used by /v1/agent/clusters/:clusterId/...
// (deployment.AgentAuthMiddleware); it is applied to the agent group.
func RegisterRoutes(app *fiber.App, h *Handler, requireAuth, agentAuth fiber.Handler) {
	v1 := app.Group("/v1")

	// User JWT must be scoped to org routes only. A Group("", requireAuth) on
	// /v1 registers Fiber USE /v1, which intercepts later-mounted agent
	// routes in this same function.
	authenticated := v1.Group("/organizations/:orgId", requireAuth)

	// Pipeline operations at deployment level
	deployments := authenticated.Group("/deployments/:deploymentId")
	deployments.Post("/deploy", h.QuickDeploy)
	deployments.Post("/rollback", h.TriggerRollback)
	deployments.Get("/desired-state", h.GetDesiredState)
	deployments.Get("/metrics", h.GetDeploymentMetrics)
	deployments.Get("/pipelines", h.ListPipelineRuns)
	deployments.Post("/pipelines", h.TriggerPipeline)

	// Individual pipeline operations
	pipelines := authenticated.Group("/pipelines/:pipelineId")
	pipelines.Get("", h.GetPipelineRun)
	pipelines.Post("/cancel", h.CancelPipeline)
	pipelines.Get("/events", h.GetPipelineEvents)

	// Org-level pipeline trigger
	authenticated.Post("/pipelines", h.TriggerPipeline)

	// Agent endpoints: cluster credentials (X-Cluster-ID / X-Agent-ID), not user JWT.
	agent := v1.Group("/agent/organizations/:orgId/clusters/:clusterId", agentAuth)
	agent.Get("/desired-state", h.GetAgentDesiredState)
	agent.Post("/sync", h.ReportSync)
	agent.Post("/metrics", h.ReportMetrics)
}
