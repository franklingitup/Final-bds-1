package provisioning

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler implements HTTP handlers for the provisioning service.
type Handler struct {
	svc *Service
}

// NewHandler creates a new handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Identity represents an authenticated user.
type Identity struct {
	UserID string
	Email  string
}

func extractIdentity(c *fiber.Ctx) (*Identity, error) {
	id, ok := c.Locals("identity").(*Identity)
	if !ok || id == nil {
		return nil, apperrors.Unauthenticated("authentication required")
	}
	return id, nil
}

func extractOrgID(c *fiber.Ctx) string {
	orgID := c.Params("orgId")
	if orgID == "" {
		orgID = c.Query("organizationId")
	}
	if orgID == "" {
		orgID = c.Get("X-Organization-ID")
	}
	return orgID
}

// ----------------------------------------------------------------------------
// Credentials
// ----------------------------------------------------------------------------

// CreateCredential handles POST /organizations/:orgId/credentials.
func (h *Handler) CreateCredential(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)

	var req CreateCredentialRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	cred, err := h.svc.CreateCredential(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToCredentialView(cred))
}

// ListCredentials handles GET /organizations/:orgId/credentials.
func (h *Handler) ListCredentials(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)

	creds, err := h.svc.ListCredentials(c.Context(), orgID, id.UserID)
	if err != nil {
		return err
	}

	views := make([]CredentialView, len(creds))
	for i, cr := range creds {
		views[i] = ToCredentialView(&cr)
	}

	return c.JSON(fiber.Map{"credentials": views})
}

// ValidateCredential handles POST /organizations/:orgId/credentials/:credentialId/validate.
func (h *Handler) ValidateCredential(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	credentialID := c.Params("credentialId")

	cred, err := h.svc.ValidateCredential(c.Context(), orgID, id.UserID, credentialID)
	if err != nil {
		// Return credential view even on validation error
		if cred != nil {
			return c.JSON(ToCredentialView(cred))
		}
		return err
	}

	return c.JSON(ToCredentialView(cred))
}

// DeleteCredential handles DELETE /organizations/:orgId/credentials/:credentialId.
func (h *Handler) DeleteCredential(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	credentialID := c.Params("credentialId")

	if err := h.svc.DeleteCredential(c.Context(), orgID, id.UserID, credentialID); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Templates
// ----------------------------------------------------------------------------

// ListTemplates handles GET /organizations/:orgId/templates.
func (h *Handler) ListTemplates(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	provider := c.Query("provider")

	var providerPtr *string
	if provider != "" {
		providerPtr = &provider
	}

	templates, err := h.svc.ListTemplates(c.Context(), orgID, id.UserID, providerPtr)
	if err != nil {
		return err
	}

	views := make([]TemplateView, len(templates))
	for i, t := range templates {
		views[i] = ToTemplateView(&t)
	}

	return c.JSON(fiber.Map{"templates": views})
}

// ----------------------------------------------------------------------------
// Provisioning Requests
// ----------------------------------------------------------------------------

// CreateProvisioningRequest handles POST /organizations/:orgId/provisioning.
func (h *Handler) CreateProvisioningRequest(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)

	var req CreateProvisioningRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	provReq, err := h.svc.CreateProvisioningRequest(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToProvisioningRequestView(provReq))
}

// ListProvisioningRequests handles GET /organizations/:orgId/provisioning.
func (h *Handler) ListProvisioningRequests(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)

	page := database.PageRequest{
		Cursor: c.Query("cursor"),
		Limit:  c.QueryInt("limit", 20),
	}

	result, err := h.svc.ListProvisioningRequests(c.Context(), orgID, id.UserID, page)
	if err != nil {
		return err
	}

	views := make([]ProvisioningRequestView, len(result.Items))
	for i, r := range result.Items {
		views[i] = ToProvisioningRequestView(&r)
	}

	return c.JSON(fiber.Map{
		"requests":   views,
		"nextCursor": result.NextCursor,
	})
}

// GetProvisioningRequest handles GET /organizations/:orgId/provisioning/:requestId.
func (h *Handler) GetProvisioningRequest(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	requestID := c.Params("requestId")

	req, err := h.svc.GetProvisioningRequest(c.Context(), orgID, id.UserID, requestID)
	if err != nil {
		return err
	}

	return c.JSON(ToProvisioningRequestView(req))
}

// GenerateTerraform handles POST /organizations/:orgId/provisioning/:requestId/terraform.
func (h *Handler) GenerateTerraform(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	requestID := c.Params("requestId")

	req, err := h.svc.GenerateTerraform(c.Context(), orgID, id.UserID, requestID)
	if err != nil {
		return err
	}

	return c.JSON(ToProvisioningRequestView(req))
}

// StartProvisioning handles POST /organizations/:orgId/provisioning/:requestId/start.
func (h *Handler) StartProvisioning(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	requestID := c.Params("requestId")

	session, err := h.svc.StartProvisioning(c.Context(), orgID, id.UserID, requestID)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToInstallSessionView(session))
}

// ----------------------------------------------------------------------------
// Install Sessions
// ----------------------------------------------------------------------------

// GetInstallSession handles GET /organizations/:orgId/provisioning/:requestId/session.
func (h *Handler) GetInstallSession(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	sessionID := c.Params("sessionId")

	session, steps, err := h.svc.GetInstallSession(c.Context(), orgID, id.UserID, sessionID)
	if err != nil {
		return err
	}

	stepViews := make([]StepView, len(steps))
	for i, s := range steps {
		stepViews[i] = ToStepView(&s)
	}

	return c.JSON(fiber.Map{
		"session": ToInstallSessionView(session),
		"steps":   stepViews,
	})
}

// UpdateStep handles POST /sessions/:sessionToken/steps/:stepNumber.
func (h *Handler) UpdateStep(c *fiber.Ctx) error {
	sessionToken := c.Params("sessionToken")
	stepNumber := c.Params("stepNumber")

	var stepNum int
	if _, err := fmt.Sscanf(stepNumber, "%d", &stepNum); err != nil {
		return apperrors.Validation("invalid step number")
	}

	var req UpdateStepRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	step, err := h.svc.UpdateStep(c.Context(), sessionToken, stepNum, req)
	if err != nil {
		return err
	}

	return c.JSON(ToStepView(step))
}

// ----------------------------------------------------------------------------
// Bootstrap
// ----------------------------------------------------------------------------

// GetBootstrapManifest handles GET /bootstrap/:token/manifest.yaml.
func (h *Handler) GetBootstrapManifest(c *fiber.Ctx) error {
	token := c.Params("token")

	manifest, err := h.svc.GetBootstrapManifest(c.Context(), token)
	if err != nil {
		return err
	}

	c.Set("Content-Type", "application/x-yaml")
	return c.SendString(manifest)
}

// ReportAgentConnection handles POST /bootstrap/:token/agent.
func (h *Handler) ReportAgentConnection(c *fiber.Ctx) error {
	token := c.Params("token")

	var req ReportAgentRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	if err := h.svc.ReportAgentConnection(c.Context(), token, req, c.IP()); err != nil {
		return err
	}

	return c.JSON(fiber.Map{"message": "agent registered"})
}

// ----------------------------------------------------------------------------
// Events
// ----------------------------------------------------------------------------

// ListEvents handles GET /organizations/:orgId/provisioning/:requestId/events.
func (h *Handler) ListEvents(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	requestID := c.Params("requestId")
	sessionID := c.Query("sessionId")

	page := database.PageRequest{
		Cursor: c.Query("cursor"),
		Limit:  c.QueryInt("limit", 50),
	}

	var reqPtr, sessPtr *string
	if requestID != "" {
		reqPtr = &requestID
	}
	if sessionID != "" {
		sessPtr = &sessionID
	}

	result, err := h.svc.ListEvents(c.Context(), orgID, id.UserID, reqPtr, sessPtr, page)
	if err != nil {
		return err
	}

	views := make([]EventView, len(result.Items))
	for i, e := range result.Items {
		views[i] = ToEventView(&e)
	}

	return c.JSON(fiber.Map{
		"events":     views,
		"nextCursor": result.NextCursor,
	})
}

// ----------------------------------------------------------------------------
// Provider Info
// ----------------------------------------------------------------------------

// ListRegions handles GET /providers/:provider/regions.
func (h *Handler) ListRegions(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	provider := c.Params("provider")
	credentialID := c.Query("credentialId")

	var credPtr *string
	if credentialID != "" {
		credPtr = &credentialID
	}

	regions, err := h.svc.ListRegions(c.Context(), orgID, id.UserID, provider, credPtr)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"regions": regions})
}

// ListMachineTypes handles GET /providers/:provider/machine-types.
func (h *Handler) ListMachineTypes(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	provider := c.Params("provider")
	region := c.Query("region", "us-east-1")

	types, err := h.svc.ListMachineTypes(c.Context(), orgID, id.UserID, provider, region)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"machineTypes": types})
}

// ListKubernetesVersions handles GET /providers/:provider/kubernetes-versions.
func (h *Handler) ListKubernetesVersions(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	provider := c.Params("provider")

	versions, err := h.svc.ListKubernetesVersions(c.Context(), orgID, id.UserID, provider)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"versions": versions})
}
