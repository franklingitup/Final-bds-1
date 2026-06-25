package tenant

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler adapts the tenant Service to Fiber HTTP handlers.
type Handler struct {
	svc      *Service
	verifier *TokenVerifier
}

// NewHandler constructs an HTTP handler.
func NewHandler(svc *Service, verifier *TokenVerifier) *Handler {
	return &Handler{svc: svc, verifier: verifier}
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

// ----------------------------------------------------------------------------
// Organizations
// ----------------------------------------------------------------------------

func (h *Handler) ListOrganizations(c *fiber.Ctx) error {
	page, err := h.svc.ListOrganizations(c.UserContext(), callerIdentity(c).UserID, pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]OrganizationView, 0, len(page.Items))
	for _, o := range page.Items {
		views = append(views, toOrganizationView(&o))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) CreateOrganization(c *fiber.Ctx) error {
	req, err := parseBody[CreateOrganizationRequest](c)
	if err != nil {
		return err
	}
	org, err := h.svc.CreateOrganization(c.UserContext(), callerIdentity(c).UserID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toOrganizationView(org))
}

func (h *Handler) GetOrganizationBySlug(c *fiber.Ctx) error {
	org, err := h.svc.GetOrganizationBySlug(c.UserContext(), callerIdentity(c).UserID, c.Params("slug"))
	if err != nil {
		return err
	}
	return c.JSON(toOrganizationView(org))
}

func (h *Handler) GetOrganization(c *fiber.Ctx) error {
	org, err := h.svc.GetOrganization(c.UserContext(), callerIdentity(c).UserID, c.Params("orgId"))
	if err != nil {
		return err
	}
	return c.JSON(toOrganizationView(org))
}

func (h *Handler) UpdateOrganization(c *fiber.Ctx) error {
	req, err := parseBody[UpdateOrganizationRequest](c)
	if err != nil {
		return err
	}
	org, err := h.svc.UpdateOrganization(c.UserContext(), callerIdentity(c).UserID, c.Params("orgId"), req)
	if err != nil {
		return err
	}
	return c.JSON(toOrganizationView(org))
}

func (h *Handler) DeleteOrganization(c *fiber.Ctx) error {
	if err := h.svc.DeleteOrganization(c.UserContext(), callerIdentity(c).UserID, c.Params("orgId")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Members
// ----------------------------------------------------------------------------

func (h *Handler) ListMembers(c *fiber.Ctx) error {
	page, err := h.svc.ListMembers(c.UserContext(), callerIdentity(c).UserID, c.Params("orgId"), pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]MemberView, 0, len(page.Items))
	for _, m := range page.Items {
		views = append(views, toMemberView(m))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) RemoveMember(c *fiber.Ctx) error {
	if err := h.svc.RemoveMember(c.UserContext(), callerIdentity(c).UserID, c.Params("orgId"), c.Params("userId")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ChangeRole(c *fiber.Ctx) error {
	req, err := parseBody[ChangeRoleRequest](c)
	if err != nil {
		return err
	}
	member, err := h.svc.ChangeRole(c.UserContext(), callerIdentity(c).UserID, c.Params("orgId"), c.Params("userId"), req.Role)
	if err != nil {
		return err
	}
	return c.JSON(toMemberView(*member))
}

// ----------------------------------------------------------------------------
// Invitations
// ----------------------------------------------------------------------------

func (h *Handler) InviteMember(c *fiber.Ctx) error {
	req, err := parseBody[InviteMemberRequest](c)
	if err != nil {
		return err
	}
	inv, err := h.svc.InviteMember(c.UserContext(), callerIdentity(c).UserID, c.Params("orgId"), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"invitationId": inv.ID,
		"status":       inv.Status,
	})
}

func (h *Handler) ListInvitations(c *fiber.Ctx) error {
	page, err := h.svc.ListInvitations(c.UserContext(), callerIdentity(c).UserID, c.Params("orgId"), pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]InvitationView, 0, len(page.Items))
	for _, i := range page.Items {
		views = append(views, toInvitationView(i))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) RevokeInvitation(c *fiber.Ctx) error {
	if err := h.svc.RevokeInvitation(c.UserContext(), callerIdentity(c).UserID, c.Params("orgId"), c.Params("id")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) AcceptInvite(c *fiber.Ctx) error {
	req, err := parseBody[AcceptInviteRequest](c)
	if err != nil {
		return err
	}
	member, err := h.svc.AcceptInvite(c.UserContext(), callerIdentity(c), req.Token)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"orgId": member.OrgID,
		"role":  member.Role,
	})
}
