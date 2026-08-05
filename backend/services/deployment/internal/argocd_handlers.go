package deployment

import (
	"github.com/gofiber/fiber/v2"
)

// ArgoHandler adapts the ArgoService to Fiber HTTP handlers. It is mounted only
// when GitOps (Argo CD) is enabled; the existing deployment API is unaffected.
type ArgoHandler struct {
	svc *ArgoService
}

// NewArgoHandler constructs an ArgoHandler.
func NewArgoHandler(svc *ArgoService) *ArgoHandler { return &ArgoHandler{svc: svc} }

// RegisterApplication binds a deployment to an Argo CD Application.
//
// Route: POST /v1/organizations/:orgId/deployments/:deploymentId/gitops
func (h *ArgoHandler) RegisterApplication(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[GitOpsSource](c)
	if err != nil {
		return err
	}
	rec, err := h.svc.RegisterApplication(c.UserContext(), org, callerIdentity(c).UserID, deploymentID(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toArgoApplicationView(rec))
}

// GetApplication returns the GitOps binding for a deployment.
//
// Route: GET /v1/organizations/:orgId/deployments/:deploymentId/gitops
func (h *ArgoHandler) GetApplication(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	rec, err := h.svc.GetBinding(c.UserContext(), org, callerIdentity(c).UserID, deploymentID(c))
	if err != nil {
		return err
	}
	return c.JSON(toArgoApplicationView(rec))
}

// Sync triggers an Argo CD sync for a deployment's Application.
//
// Route: POST /v1/organizations/:orgId/deployments/:deploymentId/gitops/sync
func (h *ArgoHandler) Sync(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[ArgoSyncRequest](c)
	if err != nil {
		return err
	}
	rec, err := h.svc.Sync(c.UserContext(), org, callerIdentity(c).UserID, deploymentID(c), req)
	if err != nil {
		return err
	}
	return c.JSON(toArgoApplicationView(rec))
}

// Rollback reverts a deployment's Argo CD Application to a previous revision.
//
// Route: POST /v1/organizations/:orgId/deployments/:deploymentId/gitops/rollback
func (h *ArgoHandler) Rollback(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[ArgoRollbackRequest](c)
	if err != nil {
		return err
	}
	rec, err := h.svc.Rollback(c.UserContext(), org, callerIdentity(c).UserID, deploymentID(c), req)
	if err != nil {
		return err
	}
	return c.JSON(toArgoApplicationView(rec))
}

// RegisterArgoRoutes mounts the GitOps routes under the existing authenticated
// deployment group. It is additive: no existing route is changed.
func RegisterArgoRoutes(app *fiber.App, h *ArgoHandler, auth fiber.Handler) {
	gitops := app.Group("/v1/organizations/:orgId/deployments/:deploymentId/gitops", auth)
	gitops.Post("", h.RegisterApplication)
	gitops.Get("", h.GetApplication)
	gitops.Post("/sync", h.Sync)
	gitops.Post("/rollback", h.Rollback)
}
