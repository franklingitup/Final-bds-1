// Package deployment implements workload deployment management.
package deployment

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts deployment routes onto the app.
func RegisterRoutes(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1")

	// Authenticated routes (user JWT).
	authenticated := v1.Group("", h.RequireAuth())

	// Applications (scoped to org/project).
	apps := authenticated.Group("/organizations/:orgId/projects/:projectId/applications")
	apps.Post("", h.CreateApplication)
	apps.Get("", h.ListApplications)

	// Single application operations.
	singleApp := authenticated.Group("/organizations/:orgId/applications/:appId")
	singleApp.Get("", h.GetApplication)
	singleApp.Patch("", h.UpdateApplication)
	singleApp.Delete("", h.DeleteApplication)

	// Deployments for an application.
	appDeps := singleApp.Group("/deployments")
	appDeps.Get("", h.ListDeployments)

	// Deployments at org level (create and list all).
	deps := authenticated.Group("/organizations/:orgId/deployments")
	deps.Get("", h.ListOrgDeployments)     // API-CRIT-03: List all org deployments
	deps.Post("", h.CreateDeployment)

	// Single deployment operations.
	dep := authenticated.Group("/organizations/:orgId/deployments/:deploymentId")
	dep.Get("", h.GetDeployment)
	dep.Patch("", h.UpdateDeployment)
	dep.Delete("", h.DeleteDeployment)      // API-CRIT-04: Delete deployment
	dep.Post("/rollback", h.Rollback)

	// Releases for a deployment.
	releases := dep.Group("/releases")
	releases.Get("", h.ListReleases)
	releases.Get("/:releaseId", h.GetRelease)

	// Status update endpoint (for user/UI).
	dep.Post("/releases/:releaseId/status", h.UpdateDeploymentStatus)

	// Cluster-scoped deployment list (legacy endpoint, kept for backward compatibility).
	clusterDeps := authenticated.Group("/organizations/:orgId/clusters/:clusterId/deployments")
	clusterDeps.Get("", h.ListDeploymentsByCluster)
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
}
