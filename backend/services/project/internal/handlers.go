package project

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler adapts the project Service to Fiber HTTP handlers.
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

func orgID(c *fiber.Ctx) string {
	return c.Params("orgId")
}

func projectID(c *fiber.Ctx) string {
	return c.Params("projectId")
}

// ----------------------------------------------------------------------------
// Projects
// ----------------------------------------------------------------------------

func (h *Handler) CreateProject(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[CreateProjectRequest](c)
	if err != nil {
		return err
	}
	p, err := h.svc.CreateProject(c.UserContext(), org, callerIdentity(c).UserID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toProjectView(p))
}

func (h *Handler) GetProject(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	p, err := h.svc.GetProject(c.UserContext(), org, callerIdentity(c).UserID, projectID(c))
	if err != nil {
		return err
	}
	return c.JSON(toProjectView(p))
}

func (h *Handler) ListProjects(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	page, err := h.svc.ListProjects(c.UserContext(), org, callerIdentity(c).UserID, pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]ProjectView, 0, len(page.Items))
	for _, p := range page.Items {
		views = append(views, toProjectView(&p))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) UpdateProject(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[UpdateProjectRequest](c)
	if err != nil {
		return err
	}
	p, err := h.svc.UpdateProject(c.UserContext(), org, callerIdentity(c).UserID, projectID(c), req)
	if err != nil {
		return err
	}
	return c.JSON(toProjectView(p))
}

func (h *Handler) DeleteProject(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	if err := h.svc.DeleteProject(c.UserContext(), org, callerIdentity(c).UserID, projectID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Members
// ----------------------------------------------------------------------------

func (h *Handler) AddMember(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[AddMemberRequest](c)
	if err != nil {
		return err
	}
	m, err := h.svc.AddMember(c.UserContext(), org, callerIdentity(c).UserID, projectID(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toMemberView(*m))
}

func (h *Handler) ListMembers(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	page, err := h.svc.ListMembers(c.UserContext(), org, callerIdentity(c).UserID, projectID(c), pageRequest(c))
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
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	if err := h.svc.RemoveMember(c.UserContext(), org, callerIdentity(c).UserID, projectID(c), c.Params("userId")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ChangeMemberRole(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[ChangeRoleRequest](c)
	if err != nil {
		return err
	}
	m, err := h.svc.ChangeRole(c.UserContext(), org, callerIdentity(c).UserID, projectID(c), c.Params("userId"), req.Role)
	if err != nil {
		return err
	}
	return c.JSON(toMemberView(*m))
}
