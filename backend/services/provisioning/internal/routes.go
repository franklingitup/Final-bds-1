// Package provisioning generates cloud-specific cluster config and install
// commands, and tracks install sessions. See docs/07-cluster-engine-design.md.
package provisioning

import "github.com/gofiber/fiber/v2"

// RegisterRoutesWithDeps registers routes with injected dependencies.
func RegisterRoutesWithDeps(app *fiber.App, h *Handler, verifier TokenVerifier) {
	auth := RequireAuth(verifier)

	v1 := app.Group("/v1")

	// Public bootstrap endpoints (token-based auth)
	bootstrap := v1.Group("/bootstrap")
	bootstrap.Get("/:token/manifest.yaml", h.GetBootstrapManifest)
	bootstrap.Post("/:token/agent", h.ReportAgentConnection)

	// Session step updates (token-based auth)
	sessions := v1.Group("/sessions")
	sessions.Post("/:sessionToken/steps/:stepNumber", h.UpdateStep)

	// Organization-scoped routes (JWT auth)
	orgs := v1.Group("/organizations/:orgId", auth)

	// Cloud credentials
	creds := orgs.Group("/credentials")
	creds.Post("", h.CreateCredential)
	creds.Get("", h.ListCredentials)
	creds.Post("/:credentialId/validate", h.ValidateCredential)
	creds.Delete("/:credentialId", h.DeleteCredential)

	// Cluster templates
	templates := orgs.Group("/templates")
	templates.Get("", h.ListTemplates)

	// Provisioning requests
	prov := orgs.Group("/provisioning")
	prov.Post("", h.CreateProvisioningRequest)
	prov.Get("", h.ListProvisioningRequests)
	prov.Get("/:requestId", h.GetProvisioningRequest)
	prov.Post("/:requestId/terraform", h.GenerateTerraform)
	prov.Post("/:requestId/start", h.StartProvisioning)
	prov.Get("/:requestId/events", h.ListEvents)

	// Install sessions
	installSessions := orgs.Group("/sessions")
	installSessions.Get("/:sessionId", h.GetInstallSession)

	// Provider info
	providers := orgs.Group("/providers")
	providers.Get("/:provider/regions", h.ListRegions)
	providers.Get("/:provider/machine-types", h.ListMachineTypes)
	providers.Get("/:provider/kubernetes-versions", h.ListKubernetesVersions)
}

// RegisterRoutes mounts provisioning routes onto the app.
// This is a stub for the httpserver package compatibility.
func RegisterRoutes(app *fiber.App) {
	g := app.Group("/v1")
	_ = g
	// Routes are registered via RegisterRoutesWithDeps after service initialization.
}
