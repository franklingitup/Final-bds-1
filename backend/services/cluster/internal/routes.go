// Package cluster owns Kubernetes cluster registration and management.
// Phase 1: Existing cluster registration only (no EKS/GKE/AKS creation).
package cluster

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts cluster routes onto the app.
// The optional registrationLimiter middleware is applied only to the
// /agent/register endpoint to prevent brute-force attacks on registration tokens.
func RegisterRoutes(app *fiber.App, h *Handler, registrationLimiter fiber.Handler) {
	// Internal API (service-to-service, protected by X-Internal-Token).
	// This must be registered before any blanket auth middleware.
	internal := app.Group("/internal", h.requireInternalToken())
	internal.Post("/organizations/:orgId/clusters", h.CreateClusterWithToken)
	internal.Get("/organizations/:orgId/clusters/:clusterId", h.GetClusterInternal)

	v1 := app.Group("/v1")

	// Existing-cluster agent manifest (capability-based registration token).
	v1.Get("/agent-install/:token/manifest.yaml", h.GetInstallManifest)

	// Agent registration endpoint (capability-based, no auth required).
	// Rate-limited to prevent brute-force attacks on registration tokens.
	if registrationLimiter != nil {
		v1.Post("/agent/register", registrationLimiter, h.RegisterAgent)
	} else {
		v1.Post("/agent/register", h.RegisterAgent)
	}
	// Agent recovery endpoint (capability-based; installation token in header).
	// Lets an agent that lost local state rebuild it without a new token.
	v1.Get("/agent/recover", h.RecoverAgent)

	// User JWT must be scoped to org routes only. A Group("", RequireAuth) on
	// /v1 registers Fiber USE /v1, which intercepts later-mounted agent
	// routes (POST /v1/agent/clusters/:clusterId/heartbeat) before
	// AgentAuthMiddleware can run.
	authenticated := v1.Group("/organizations/:orgId", h.RequireAuth())

	// Clusters scoped under organizations.
	clusters := authenticated.Group("/clusters")
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
