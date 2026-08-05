// Package project owns projects and their memberships. See
// docs/04-api-spec.md section 3 and docs/06-security-design.md section 2.
package project

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts project routes onto the app using the given handler. All
// routes require authentication; per-operation authorization is enforced in the
// service layer via libs/authz against the caller's project membership role.
func RegisterRoutes(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1", h.RequireAuth())

	// Projects scoped under organizations.
	projects := v1.Group("/organizations/:orgId/projects")
	projects.Post("", h.CreateProject)
	projects.Get("", h.ListProjects)
	// Lookup by slug must be registered before the :projectId param route so
	// "by-slug" is not captured as a projectId.
	projects.Get("/by-slug/:slug", h.GetProjectBySlug)
	projects.Get("/:projectId", h.GetProject)
	projects.Patch("/:projectId", h.UpdateProject)
	projects.Delete("/:projectId", h.DeleteProject)

	// Project members.
	members := projects.Group("/:projectId/members")
	members.Post("", h.AddMember)
	members.Get("", h.ListMembers)
	members.Patch("/:userId", h.ChangeMemberRole)
	members.Delete("/:userId", h.RemoveMember)
}
