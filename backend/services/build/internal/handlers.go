package build

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler adapts the build Service to Fiber HTTP handlers.
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

func orgID(c *fiber.Ctx) string     { return c.Params("orgId") }
func projectID(c *fiber.Ctx) string { return c.Params("projectId") }
func repoID(c *fiber.Ctx) string    { return c.Params("repoId") }
func buildID(c *fiber.Ctx) string   { return c.Params("buildId") }

// ----------------------------------------------------------------------------
// Repositories
// ----------------------------------------------------------------------------

func (h *Handler) CreateRepository(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	proj := projectID(c)
	if proj == "" {
		return errProjectRequired
	}
	req, err := parseBody[CreateRepositoryRequest](c)
	if err != nil {
		return err
	}
	req.ProjectID = proj
	repo, err := h.svc.CreateRepository(c.UserContext(), org, proj, callerIdentity(c).UserID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toRepositoryView(repo))
}

func (h *Handler) GetRepository(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	repo, err := h.svc.GetRepository(c.UserContext(), org, callerIdentity(c).UserID, repoID(c))
	if err != nil {
		return err
	}
	return c.JSON(toRepositoryView(repo))
}

func (h *Handler) ListRepositories(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	proj := projectID(c)
	if proj == "" {
		return errProjectRequired
	}
	page, err := h.svc.ListRepositories(c.UserContext(), org, callerIdentity(c).UserID, proj, pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]RepositoryView, 0, len(page.Items))
	for _, repo := range page.Items {
		views = append(views, toRepositoryView(&repo))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) UpdateRepository(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[UpdateRepositoryRequest](c)
	if err != nil {
		return err
	}
	repo, err := h.svc.UpdateRepository(c.UserContext(), org, callerIdentity(c).UserID, repoID(c), req)
	if err != nil {
		return err
	}
	return c.JSON(toRepositoryView(repo))
}

func (h *Handler) DeleteRepository(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	if err := h.svc.DeleteRepository(c.UserContext(), org, callerIdentity(c).UserID, repoID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Builds
// ----------------------------------------------------------------------------

func (h *Handler) CreateBuild(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[CreateBuildRequest](c)
	if err != nil {
		return err
	}
	build, err := h.svc.CreateBuild(c.UserContext(), org, callerIdentity(c).UserID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toBuildView(build))
}

func (h *Handler) GetBuild(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	build, err := h.svc.GetBuild(c.UserContext(), org, callerIdentity(c).UserID, buildID(c))
	if err != nil {
		return err
	}
	return c.JSON(toBuildView(build))
}

func (h *Handler) ListBuilds(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	page, err := h.svc.ListBuilds(c.UserContext(), org, callerIdentity(c).UserID, pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]BuildView, 0, len(page.Items))
	for _, build := range page.Items {
		views = append(views, toBuildView(&build))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) CancelBuild(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	build, err := h.svc.CancelBuild(c.UserContext(), org, callerIdentity(c).UserID, buildID(c))
	if err != nil {
		return err
	}
	return c.JSON(toBuildView(build))
}

func (h *Handler) RetryBuild(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[RetryBuildRequest](c)
	if err != nil {
		req = RetryBuildRequest{}
	}
	build, err := h.svc.RetryBuild(c.UserContext(), org, callerIdentity(c).UserID, buildID(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toBuildView(build))
}

// ----------------------------------------------------------------------------
// Build Logs
// ----------------------------------------------------------------------------

func (h *Handler) GetBuildLogs(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	page, err := h.svc.GetBuildLogs(c.UserContext(), org, callerIdentity(c).UserID, buildID(c), pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]BuildLogView, 0, len(page.Items))
	for _, log := range page.Items {
		views = append(views, toBuildLogView(&log))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

// ----------------------------------------------------------------------------
// Build Artifacts
// ----------------------------------------------------------------------------

func (h *Handler) GetBuildArtifact(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	artifact, err := h.svc.GetBuildArtifact(c.UserContext(), org, callerIdentity(c).UserID, buildID(c))
	if err != nil {
		return err
	}
	return c.JSON(toBuildArtifactView(artifact))
}
