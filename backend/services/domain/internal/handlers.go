package domain

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler wraps domain service for HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler creates a new domain handler.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log}
}

// Identity represents an authenticated user.
type Identity struct {
	UserID string
	Email  string
}

// TokenVerifier verifies JWT tokens.
type TokenVerifier interface {
	Verify(token string) (*Identity, error)
}

// extractIdentity extracts identity from context.
func extractIdentity(c *fiber.Ctx) (*Identity, error) {
	id, ok := c.Locals("identity").(*Identity)
	if !ok || id == nil {
		return nil, apperrors.Unauthenticated("authentication required")
	}
	return id, nil
}

// extractOrgID extracts organization ID from params or query.
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
// Domain Handlers
// ----------------------------------------------------------------------------

// CreateDomain creates a new custom domain.
func (h *Handler) CreateDomain(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	var req CreateDomainRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	if req.DeploymentID == "" {
		return apperrors.Validation("deploymentId is required")
	}
	if req.Domain == "" {
		return apperrors.Validation("domain is required")
	}

	dom, err := h.svc.CreateDomain(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToDomainView(dom, nil))
}

// GetDomain returns a domain by ID.
func (h *Handler) GetDomain(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	domainID := c.Params("domainId")
	if domainID == "" {
		return apperrors.Validation("domain ID is required")
	}

	dom, cert, err := h.svc.GetDomain(c.Context(), orgID, id.UserID, domainID)
	if err != nil {
		return err
	}

	return c.JSON(ToDomainView(dom, cert))
}

// ListDomains returns domains for an organization.
func (h *Handler) ListDomains(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	page := database.PageRequest{
		Limit:  c.QueryInt("limit", 20),
		Cursor: c.Query("cursor"),
	}

	result, err := h.svc.ListDomains(c.Context(), orgID, id.UserID, page)
	if err != nil {
		return err
	}

	views := make([]DomainView, len(result.Items))
	for i, d := range result.Items {
		views[i] = ToDomainView(&d, nil)
	}

	return c.JSON(fiber.Map{
		"domains":    views,
		"nextCursor": result.NextCursor,
	})
}

// ListDomainsByDeployment returns domains for a deployment.
func (h *Handler) ListDomainsByDeployment(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	deploymentID := c.Params("deploymentId")
	if deploymentID == "" {
		return apperrors.Validation("deployment ID is required")
	}

	page := database.PageRequest{
		Limit:  c.QueryInt("limit", 20),
		Cursor: c.Query("cursor"),
	}

	result, err := h.svc.ListDomainsByDeployment(c.Context(), orgID, id.UserID, deploymentID, page)
	if err != nil {
		return err
	}

	views := make([]DomainView, len(result.Items))
	for i, d := range result.Items {
		views[i] = ToDomainView(&d, nil)
	}

	return c.JSON(fiber.Map{
		"domains":    views,
		"nextCursor": result.NextCursor,
	})
}

// UpdateDomain updates a domain.
func (h *Handler) UpdateDomain(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	domainID := c.Params("domainId")
	if domainID == "" {
		return apperrors.Validation("domain ID is required")
	}

	var req UpdateDomainRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	dom, err := h.svc.UpdateDomain(c.Context(), orgID, id.UserID, domainID, req)
	if err != nil {
		return err
	}

	return c.JSON(ToDomainView(dom, nil))
}

// DeleteDomain deletes a domain.
func (h *Handler) DeleteDomain(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	domainID := c.Params("domainId")
	if domainID == "" {
		return apperrors.Validation("domain ID is required")
	}

	if err := h.svc.DeleteDomain(c.Context(), orgID, id.UserID, domainID); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Verification Handlers
// ----------------------------------------------------------------------------

// VerifyDomain verifies domain ownership.
func (h *Handler) VerifyDomain(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	domainID := c.Params("domainId")
	if domainID == "" {
		return apperrors.Validation("domain ID is required")
	}

	var req VerifyDomainRequest
	_ = c.BodyParser(&req) // Optional body

	dom, err := h.svc.VerifyDomain(c.Context(), orgID, id.UserID, domainID, req.Force)
	if err != nil {
		return err
	}

	cert, _ := h.svc.GetCertificate(c.Context(), orgID, id.UserID, domainID)
	return c.JSON(ToDomainView(dom, cert))
}

// ----------------------------------------------------------------------------
// Certificate Handlers
// ----------------------------------------------------------------------------

// IssueCertificate issues a TLS certificate for a domain.
func (h *Handler) IssueCertificate(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	domainID := c.Params("domainId")
	if domainID == "" {
		return apperrors.Validation("domain ID is required")
	}

	var req IssueCertificateRequest
	_ = c.BodyParser(&req) // Optional body

	cert, err := h.svc.IssueCertificate(c.Context(), orgID, id.UserID, domainID, req.ForceRenewal)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToCertView(cert))
}

// GetCertificate returns a certificate for a domain.
func (h *Handler) GetCertificate(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	domainID := c.Params("domainId")
	if domainID == "" {
		return apperrors.Validation("domain ID is required")
	}

	cert, err := h.svc.GetCertificate(c.Context(), orgID, id.UserID, domainID)
	if err != nil {
		return err
	}

	return c.JSON(ToCertView(cert))
}

// ----------------------------------------------------------------------------
// Ingress Handlers
// ----------------------------------------------------------------------------

// CreateIngress creates an Ingress for a domain.
func (h *Handler) CreateIngress(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	domainID := c.Params("domainId")
	if domainID == "" {
		return apperrors.Validation("domain ID is required")
	}

	ingress, err := h.svc.CreateIngress(c.Context(), orgID, id.UserID, domainID)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToIngressView(ingress))
}

// GetIngress returns an Ingress for a domain.
func (h *Handler) GetIngress(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	domainID := c.Params("domainId")
	if domainID == "" {
		return apperrors.Validation("domain ID is required")
	}

	ingress, err := h.svc.GetIngress(c.Context(), orgID, id.UserID, domainID)
	if err != nil {
		return err
	}

	return c.JSON(ToIngressView(ingress))
}

// ----------------------------------------------------------------------------
// Agent Handlers
// ----------------------------------------------------------------------------

// GetIngressesForAgent returns pending ingresses for a cluster.
func (h *Handler) GetIngressesForAgent(c *fiber.Ctx) error {
	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	clusterID := c.Params("clusterId")
	if clusterID == "" {
		return apperrors.Validation("cluster ID is required")
	}

	specs, err := h.svc.GetIngressesForAgent(c.Context(), orgID, clusterID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"ingresses": specs})
}

// ReportIngressSync reports ingress sync status from agent.
func (h *Handler) ReportIngressSync(c *fiber.Ctx) error {
	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	ingressID := c.Params("ingressId")
	if ingressID == "" {
		return apperrors.Validation("ingress ID is required")
	}

	var req struct {
		Status             string  `json:"status"`
		ObservedGeneration int64   `json:"observedGeneration"`
		Error              *string `json:"error,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	if err := h.svc.ReportIngressSync(c.Context(), orgID, ingressID, req.Status, req.ObservedGeneration, req.Error); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusOK)
}

// ----------------------------------------------------------------------------
// ACME Handlers
// ----------------------------------------------------------------------------

// GetACMEChallenge handles ACME HTTP-01 challenge requests.
func (h *Handler) GetACMEChallenge(c *fiber.Ctx) error {
	token := c.Params("token")
	if token == "" {
		return c.Status(fiber.StatusNotFound).SendString("Not found")
	}

	challenge, err := h.svc.GetACMEChallenge(c.Context(), token)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("Not found")
	}

	return c.Type("text/plain").SendString(challenge.KeyAuth)
}

// ----------------------------------------------------------------------------
// Event Handlers
// ----------------------------------------------------------------------------

// ListDomainEvents returns events for a domain.
func (h *Handler) ListDomainEvents(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	domainID := c.Params("domainId")
	if domainID == "" {
		return apperrors.Validation("domain ID is required")
	}

	page := database.PageRequest{
		Limit:  c.QueryInt("limit", 20),
		Cursor: c.Query("cursor"),
	}

	result, err := h.svc.ListDomainEvents(c.Context(), orgID, id.UserID, domainID, page)
	if err != nil {
		return err
	}

	views := make([]DomainEventView, len(result.Items))
	for i, e := range result.Items {
		views[i] = ToDomainEventView(&e)
	}

	return c.JSON(fiber.Map{
		"events":     views,
		"nextCursor": result.NextCursor,
	})
}
