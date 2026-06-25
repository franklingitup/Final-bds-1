// Package tenant owns organizations, memberships, and RBAC. See
// docs/04-api-spec.md section 2 and docs/06-security-design.md section 2.
package tenant

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts tenant routes onto the app using the given handler. All
// routes require authentication; per-operation authorization is enforced in the
// service layer via libs/authz against the caller's membership role.
func RegisterRoutes(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1", h.RequireAuth())

	orgs := v1.Group("/organizations")
	orgs.Get("", h.ListOrganizations)                      // API-CRIT-01
	orgs.Post("", h.CreateOrganization)
	orgs.Get("/by-slug/:slug", h.GetOrganizationBySlug)    // API-CRIT-02
	orgs.Get("/:orgId", h.GetOrganization)
	orgs.Patch("/:orgId", h.UpdateOrganization)
	orgs.Delete("/:orgId", h.DeleteOrganization)

	// Members.
	orgs.Get("/:orgId/members", h.ListMembers)
	orgs.Patch("/:orgId/members/:userId", h.ChangeRole)
	orgs.Delete("/:orgId/members/:userId", h.RemoveMember)

	// Invitations.
	orgs.Post("/:orgId/invitations", h.InviteMember)
	orgs.Get("/:orgId/invitations", h.ListInvitations)
	orgs.Delete("/:orgId/invitations/:id", h.RevokeInvitation)

	// Accept is keyed on the invitation token, not an org path.
	v1.Post("/invitations/accept", h.AcceptInvite)
}
