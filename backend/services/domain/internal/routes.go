// Package domain manages custom domains, DNS verification, ingress bindings, and
// TLS certificate lifecycle. See docs/04-api-spec.md section 7.
package domain

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts domain routes onto the app with the given handler and auth middleware.
func RegisterRoutes(app *fiber.App, h *Handler, auth fiber.Handler) {
	v1 := app.Group("/v1")

	// Domain management (requires auth)
	domains := v1.Group("/organizations/:orgId/domains", auth)
	domains.Post("", h.CreateDomain)
	domains.Get("", h.ListDomains)
	domains.Get("/:domainId", h.GetDomain)
	domains.Patch("/:domainId", h.UpdateDomain)
	domains.Delete("/:domainId", h.DeleteDomain)

	// Verification
	domains.Post("/:domainId/verify", h.VerifyDomain)

	// Certificates
	domains.Post("/:domainId/certificate", h.IssueCertificate)
	domains.Get("/:domainId/certificate", h.GetCertificate)

	// Ingress
	domains.Post("/:domainId/ingress", h.CreateIngress)
	domains.Get("/:domainId/ingress", h.GetIngress)

	// Events
	domains.Get("/:domainId/events", h.ListDomainEvents)

	// Domains by deployment
	v1.Get("/organizations/:orgId/deployments/:deploymentId/domains", auth, h.ListDomainsByDeployment)

	// Agent endpoints (use agent auth middleware instead)
	agent := v1.Group("/agent")
	agent.Get("/clusters/:clusterId/ingresses", h.GetIngressesForAgent)
	agent.Post("/ingresses/:ingressId/sync", h.ReportIngressSync)

	// ACME HTTP-01 challenge endpoint (no auth required - accessed by Let's Encrypt)
	app.Get("/.well-known/acme-challenge/:token", h.GetACMEChallenge)
}
