package secrets

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler adapts the secrets Service to Fiber HTTP handlers.
type Handler struct {
	svc      *Service
	verifier TokenVerifier
}

// NewHandler constructs an HTTP handler.
func NewHandler(svc *Service, verifier TokenVerifier) *Handler {
	return &Handler{svc: svc, verifier: verifier}
}

// TokenVerifier validates JWT tokens.
type TokenVerifier interface {
	Verify(token string) (Identity, error)
}

// Identity represents an authenticated user.
type Identity struct {
	UserID string
	Email  string
}

func parseBody[T any](c *fiber.Ctx) (T, error) {
	var body T
	if err := c.BodyParser(&body); err != nil {
		return body, apperrors.Validation("invalid request body")
	}
	return body, nil
}

func pageRequest(c *fiber.Ctx) database.PageRequest {
	return database.PageRequest{Limit: c.QueryInt("limit", 0), Cursor: c.Query("cursor")}
}

func orgID(c *fiber.Ctx) string {
	return c.Params("orgId")
}

func projectID(c *fiber.Ctx) string {
	return c.Params("projectId")
}

func secretID(c *fiber.Ctx) string {
	return c.Params("secretId")
}

// Errors
var (
	errOrgRequired     = apperrors.Validation("organization ID is required")
	errProjectRequired = apperrors.Validation("project ID is required")
	errInvalidToken    = apperrors.Unauthorized("invalid or missing access token")
)

// ----------------------------------------------------------------------------
// Secrets API Handlers
// ----------------------------------------------------------------------------

// CreateSecret handles POST /v1/organizations/:orgId/projects/:projectId/secrets
func (h *Handler) CreateSecret(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	project := projectID(c)
	if project == "" {
		return errProjectRequired
	}

	req, err := parseBody[CreateSecretRequest](c)
	if err != nil {
		return err
	}

	secret, err := h.svc.CreateSecret(c.UserContext(), org, callerIdentity(c).UserID, project, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(secret.ToView())
}

// GetSecret handles GET /v1/organizations/:orgId/projects/:projectId/secrets/:secretId
func (h *Handler) GetSecret(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	project := projectID(c)
	if project == "" {
		return errProjectRequired
	}

	secret, err := h.svc.GetSecret(c.UserContext(), org, callerIdentity(c).UserID, project, secretID(c))
	if err != nil {
		return err
	}

	return c.JSON(secret.ToView())
}

// ListSecrets handles GET /v1/organizations/:orgId/projects/:projectId/secrets
func (h *Handler) ListSecrets(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	project := projectID(c)
	if project == "" {
		return errProjectRequired
	}

	page, err := h.svc.ListSecrets(c.UserContext(), org, callerIdentity(c).UserID, project, pageRequest(c))
	if err != nil {
		return err
	}

	views := make([]SecretView, 0, len(page.Items))
	for _, s := range page.Items {
		views = append(views, s.ToView())
	}

	return c.JSON(SecretListView{
		Items:      views,
		Total:      len(views),
		HasMore:    page.NextCursor != "",
		NextCursor: page.NextCursor,
	})
}

// UpdateSecret handles PATCH /v1/organizations/:orgId/projects/:projectId/secrets/:secretId
func (h *Handler) UpdateSecret(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	project := projectID(c)
	if project == "" {
		return errProjectRequired
	}

	req, err := parseBody[UpdateSecretRequest](c)
	if err != nil {
		return err
	}

	secret, err := h.svc.UpdateSecret(c.UserContext(), org, callerIdentity(c).UserID, project, secretID(c), req)
	if err != nil {
		return err
	}

	return c.JSON(secret.ToView())
}

// DeleteSecret handles DELETE /v1/organizations/:orgId/projects/:projectId/secrets/:secretId
func (h *Handler) DeleteSecret(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	project := projectID(c)
	if project == "" {
		return errProjectRequired
	}

	if err := h.svc.DeleteSecret(c.UserContext(), org, callerIdentity(c).UserID, project, secretID(c)); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}
