package auth

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler adapts the AuthService to Fiber HTTP handlers.
type Handler struct {
	svc *Service
}

// NewHandler constructs an HTTP handler over the service.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) requestMeta(c *fiber.Ctx) RequestMeta {
	return RequestMeta{UserAgent: c.Get(fiber.HeaderUserAgent), IP: c.IP()}
}

func parseBody[T any](c *fiber.Ctx) (T, error) {
	var body T
	if err := c.BodyParser(&body); err != nil {
		return body, apperrors.Validation("invalid request body")
	}
	return body, nil
}

func pageRequest(c *fiber.Ctx) database.PageRequest {
	return database.PageRequest{
		Limit:  c.QueryInt("limit", 0),
		Cursor: c.Query("cursor"),
	}
}

// ----------------------------------------------------------------------------
// Public auth endpoints
// ----------------------------------------------------------------------------

func (h *Handler) Signup(c *fiber.Ctx) error {
	req, err := parseBody[SignupRequest](c)
	if err != nil {
		return err
	}
	pair, err := h.svc.Signup(c.UserContext(), req, h.requestMeta(c))
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(pair)
}

func (h *Handler) Login(c *fiber.Ctx) error {
	req, err := parseBody[LoginRequest](c)
	if err != nil {
		return err
	}
	pair, err := h.svc.Login(c.UserContext(), req, h.requestMeta(c))
	if err != nil {
		return err
	}
	return c.JSON(pair)
}

func (h *Handler) Refresh(c *fiber.Ctx) error {
	req, err := parseBody[RefreshRequest](c)
	if err != nil {
		return err
	}
	pair, err := h.svc.Refresh(c.UserContext(), req, h.requestMeta(c))
	if err != nil {
		return err
	}
	return c.JSON(pair)
}

func (h *Handler) Logout(c *fiber.Ctx) error {
	req, err := parseBody[LogoutRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.Logout(c.UserContext(), req); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) VerifyEmail(c *fiber.Ctx) error {
	req, err := parseBody[VerifyEmailRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.VerifyEmail(c.UserContext(), req.Token); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"verified": true})
}

func (h *Handler) ResendVerification(c *fiber.Ctx) error {
	req, err := parseBody[ResendVerificationRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.ResendVerification(c.UserContext(), req.Email); err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "sent"})
}

func (h *Handler) RequestPasswordReset(c *fiber.Ctx) error {
	req, err := parseBody[PasswordResetRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.RequestPasswordReset(c.UserContext(), req.Email); err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "sent"})
}

func (h *Handler) ConfirmPasswordReset(c *fiber.Ctx) error {
	req, err := parseBody[PasswordResetConfirmRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.ConfirmPasswordReset(c.UserContext(), req.Token, req.NewPassword); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"status": "reset"})
}

// ----------------------------------------------------------------------------
// Authenticated user endpoints
// ----------------------------------------------------------------------------

func (h *Handler) Me(c *fiber.Ctx) error {
	profile, err := h.svc.Me(c.UserContext(), currentUserID(c))
	if err != nil {
		return err
	}
	return c.JSON(profile)
}

func (h *Handler) SetupMFA(c *fiber.Ctx) error {
	result, err := h.svc.SetupMFA(c.UserContext(), currentUserID(c))
	if err != nil {
		return err
	}
	return c.JSON(result)
}

func (h *Handler) EnableMFA(c *fiber.Ctx) error {
	req, err := parseBody[MFAEnableRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.EnableMFA(c.UserContext(), currentUserID(c), req.Code); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"mfaEnabled": true})
}

func (h *Handler) DisableMFA(c *fiber.Ctx) error {
	req, err := parseBody[MFADisableRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.DisableMFA(c.UserContext(), currentUserID(c), req.Code); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"mfaEnabled": false})
}

// ----------------------------------------------------------------------------
// Service accounts & API tokens (org-scoped)
// ----------------------------------------------------------------------------

type serviceAccountView struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"orgId"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

func toServiceAccountView(sa ServiceAccount) serviceAccountView {
	return serviceAccountView{
		ID:          sa.ID,
		OrgID:       sa.OrgID,
		Name:        sa.Name,
		Description: sa.Description,
		Status:      sa.Status,
		CreatedAt:   sa.CreatedAt,
	}
}

type apiTokenView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func toAPITokenView(t APIToken) apiTokenView {
	return apiTokenView{
		ID:         t.ID,
		Name:       t.Name,
		Prefix:     t.Prefix,
		Scopes:     t.Scopes,
		ExpiresAt:  t.ExpiresAt,
		LastUsedAt: t.LastUsedAt,
		RevokedAt:  t.RevokedAt,
		CreatedAt:  t.CreatedAt,
	}
}

func (h *Handler) CreateServiceAccount(c *fiber.Ctx) error {
	req, err := parseBody[CreateServiceAccountRequest](c)
	if err != nil {
		return err
	}
	sa, err := h.svc.CreateServiceAccount(c.UserContext(), c.Params("orgId"), currentUserID(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toServiceAccountView(*sa))
}

func (h *Handler) ListServiceAccounts(c *fiber.Ctx) error {
	page, err := h.svc.ListServiceAccounts(c.UserContext(), c.Params("orgId"), currentUserID(c), pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]serviceAccountView, 0, len(page.Items))
	for _, sa := range page.Items {
		views = append(views, toServiceAccountView(sa))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) DeleteServiceAccount(c *fiber.Ctx) error {
	if err := h.svc.DeleteServiceAccount(c.UserContext(), c.Params("orgId"), currentUserID(c), c.Params("id")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) CreateAPIToken(c *fiber.Ctx) error {
	req, err := parseBody[CreateAPITokenRequest](c)
	if err != nil {
		return err
	}
	result, err := h.svc.CreateAPIToken(c.UserContext(), c.Params("orgId"), currentUserID(c), c.Params("id"), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(result)
}

func (h *Handler) ListAPITokens(c *fiber.Ctx) error {
	page, err := h.svc.ListAPITokens(c.UserContext(), c.Params("orgId"), currentUserID(c), pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]apiTokenView, 0, len(page.Items))
	for _, t := range page.Items {
		views = append(views, toAPITokenView(t))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) RevokeAPIToken(c *fiber.Ctx) error {
	if err := h.svc.RevokeAPIToken(c.UserContext(), c.Params("orgId"), currentUserID(c), c.Params("id")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
