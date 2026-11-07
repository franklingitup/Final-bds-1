// Package deployment implements workload deployment management.
package deployment

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts deployment routes onto the app.
func RegisterRoutes(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1")

	// User JWT must be scoped to org routes only. A Group("", RequireAuth) on
	// /v1 registers Fiber USE /v1, which intercepts later-mounted agent
	// routes before AgentAuthMiddleware can run.
	authenticated := v1.Group("/organizations/:orgId", h.RequireAuth())

	// Applications (scoped to org/project).
	apps := authenticated.Group("/projects/:projectId/applications")
	apps.Post("", h.CreateApplication)
	apps.Get("", h.ListApplications)

	// Single application operations.
	singleApp := authenticated.Group("/applications/:appId")
	singleApp.Get("", h.GetApplication)
	singleApp.Patch("", h.UpdateApplication)
	singleApp.Delete("", h.DeleteApplication)

	// Deployments for an application.
	appDeps := singleApp.Group("/deployments")
	appDeps.Get("", h.ListDeployments)

	// Deployments at org level (create and list all).
	deps := authenticated.Group("/deployments")
	deps.Get("", h.ListOrgDeployments) // API-CRIT-03: List all org deployments
	deps.Post("", h.CreateDeployment)

	// Single deployment operations.
	dep := authenticated.Group("/deployments/:deploymentId")
	dep.Get("", h.GetDeployment)
	dep.Patch("", h.UpdateDeployment)
	dep.Delete("", h.DeleteDeployment) // API-CRIT-04: Delete deployment
	dep.Post("/rollback", h.Rollback)

	// Releases for a deployment.
	releases := dep.Group("/releases")
	releases.Get("", h.ListReleases)
	releases.Get("/:releaseId", h.GetRelease)

	// Status update endpoint (for user/UI).
	dep.Post("/releases/:releaseId/status", h.UpdateDeploymentStatus)

	// Cluster-scoped deployment list (legacy endpoint, kept for backward compatibility).
	clusterDeps := authenticated.Group("/clusters/:clusterId/deployments")
	clusterDeps.Get("", h.ListDeploymentsByCluster)

	// Environments (scoped to an application).
	envs := singleApp.Group("/environments")
	envs.Post("", h.CreateEnvironment)
	envs.Get("", h.ListEnvironments)

	// Single environment operations.
	singleEnv := authenticated.Group("/environments/:environmentId")
	singleEnv.Get("", h.GetEnvironment)
	singleEnv.Patch("", h.UpdateEnvironment)
	singleEnv.Delete("", h.DeleteEnvironment)

	// Promotions (create + list at application scope; approve/reject at org scope).
	appPromos := singleApp.Group("/promotions")
	appPromos.Get("", h.ListPromotions)

	promos := authenticated.Group("/promotions")
	promos.Post("", h.RequestPromotion)

	singlePromo := promos.Group("/:promotionId")
	singlePromo.Get("", h.GetPromotion)
	singlePromo.Post("/approve", h.ApprovePromotion)
	singlePromo.Post("/reject", h.RejectPromotion)
}

// RegisterAgentRoutes mounts agent-specific routes with cluster credential authentication.
func RegisterAgentRoutes(app *fiber.App, agentHandler *AgentHandler, agentAuth fiber.Handler, releases ReleaseStore, deployments DeploymentStore) {
	v1 := app.Group("/v1")

	// Agent routes (cluster credential authentication).
	agent := v1.Group("/agent", agentAuth)

	// Desired state endpoint for reconciliation.
	agent.Get("/clusters/:clusterId/desired-state", agentHandler.GetDesiredState)

	// Status update endpoint for agents.
	agent.Post("/deployments/:deploymentId/releases/:releaseId/status", func(c *fiber.Ctx) error {
		return agentHandler.UpdateDeploymentStatus(c, releases, deployments)
	})

	// Rollout progress endpoint (deployment engine). Additive: agents that do
	// not report progress simply never call it.
	agent.Post("/deployments/:deploymentId/releases/:releaseId/progress", func(c *fiber.Ctx) error {
		return agentHandler.UpdateDeploymentProgress(c, releases, deployments)
	})
}
