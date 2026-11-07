// Package secrets provides secure project-scoped secret management.
// See docs/20-secrets-service.md for architecture and security model.
package secrets

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts secrets routes onto the app.
// All routes require authentication; per-operation authorization is enforced
// in the service layer via libs/authz against the caller's project membership role.
//
// Endpoints:
//   POST   /v1/organizations/:orgId/projects/:projectId/secrets
//   GET    /v1/organizations/:orgId/projects/:projectId/secrets
//   GET    /v1/organizations/:orgId/projects/:projectId/secrets/:secretId
//   PATCH  /v1/organizations/:orgId/projects/:projectId/secrets/:secretId
//   DELETE /v1/organizations/:orgId/projects/:projectId/secrets/:secretId
func RegisterRoutes(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1")

	// User JWT must be scoped to org routes only. Group("/v1", RequireAuth)
	// registers Fiber USE /v1, which intercepts later-mounted agent routes
	// (GET /v1/agent/clusters/:clusterId/secrets) before AgentAuthMiddleware.
	orgs := v1.Group("/organizations/:orgId", h.RequireAuth())

	// Secrets scoped under projects.
	secrets := orgs.Group("/projects/:projectId/secrets")
	secrets.Post("", h.CreateSecret)
	secrets.Get("", h.ListSecrets)
	secrets.Get("/:secretId", h.GetSecret)
	secrets.Patch("/:secretId", h.UpdateSecret)
	secrets.Delete("/:secretId", h.DeleteSecret)
}
