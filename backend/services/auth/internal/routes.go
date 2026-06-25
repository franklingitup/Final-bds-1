// Package auth owns identity, sessions, tokens, MFA, email verification,
// password reset, service accounts, and API tokens. See docs/04-api-spec.md
// section 1 and docs/06-security-design.md section 1.
package auth

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts auth routes onto the app using the given handler.
func RegisterRoutes(app *fiber.App, h *Handler) {
	auth := app.Group("/v1/auth")

	// Public endpoints.
	auth.Post("/signup", h.Signup)
	auth.Post("/login", h.Login)
	auth.Post("/refresh", h.Refresh)
	auth.Post("/verify-email", h.VerifyEmail)
	auth.Post("/resend-verification", h.ResendVerification)
	auth.Post("/password-reset/request", h.RequestPasswordReset)
	auth.Post("/password-reset/confirm", h.ConfirmPasswordReset)

	// Authenticated endpoints.
	authed := auth.Group("", h.RequireAuth())
	authed.Post("/logout", h.Logout)
	authed.Get("/me", h.Me)
	authed.Post("/mfa/setup", h.SetupMFA)
	authed.Post("/mfa/enable", h.EnableMFA)
	authed.Post("/mfa/disable", h.DisableMFA)

	// Org-scoped machine identities. Authorization of org membership/role is
	// performed at the gateway/tenant service; these handlers enforce tenant
	// data isolation via row-level security (database.WithTenant).
	orgs := app.Group("/v1/organizations/:orgId", h.RequireAuth())
	orgs.Post("/service-accounts", h.CreateServiceAccount)
	orgs.Get("/service-accounts", h.ListServiceAccounts)
	orgs.Delete("/service-accounts/:id", h.DeleteServiceAccount)
	orgs.Post("/service-accounts/:id/tokens", h.CreateAPIToken)
	orgs.Get("/api-tokens", h.ListAPITokens)
	orgs.Delete("/api-tokens/:id", h.RevokeAPIToken)
}
