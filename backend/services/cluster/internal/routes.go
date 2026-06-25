// Package cluster owns Kubernetes cluster registration and management.
// Phase 1: Existing cluster registration only (no EKS/GKE/AKS creation).
package cluster

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts cluster routes onto the app.
func RegisterRoutes(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1")

	// Agent registration endpoint (capability-based, no auth required).
	v1.Post("/agent/register", h.RegisterAgent)

	// Authenticated routes.
	authenticated := v1.Group("", h.RequireAuth())

	// Clusters scoped under organizations.
	clusters := authenticated.Group("/organizations/:orgId/clusters")
	clusters.Post("", h.CreateCluster)
	clusters.Get("", h.ListClusters)
	clusters.Get("/:clusterId", h.GetCluster)
	clusters.Patch("/:clusterId", h.UpdateCluster)
	clusters.Delete("/:clusterId", h.DeleteCluster)

	// Registration tokens.
	clusters.Post("/:clusterId/tokens", h.GenerateRegistrationToken)
	clusters.Delete("/:clusterId/tokens/:tokenId", h.RevokeRegistrationToken)

	// Heartbeat history (read-only).
	clusters.Get("/:clusterId/heartbeats", h.GetHeartbeats)

	// NOTE: User-facing heartbeat route REMOVED (SEC-CRIT-03).
	// Agent heartbeats now use only the credential-based route:
	// POST /v1/agent/clusters/:clusterId/heartbeat (via RegisterAgentRoutes)
}
