package deployment

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler adapts the deployment Service to Fiber HTTP handlers.
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

func orgID(c *fiber.Ctx) string       { return c.Params("orgId") }
func projectID(c *fiber.Ctx) string   { return c.Params("projectId") }
func appID(c *fiber.Ctx) string       { return c.Params("appId") }
func deploymentID(c *fiber.Ctx) string { return c.Params("deploymentId") }
func releaseID(c *fiber.Ctx) string   { return c.Params("releaseId") }

// ----------------------------------------------------------------------------
// Applications
// ----------------------------------------------------------------------------

func (h *Handler) CreateApplication(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	proj := projectID(c)
	if proj == "" {
		return errProjectRequired
	}
	req, err := parseBody[CreateApplicationRequest](c)
	if err != nil {
		return err
	}
	app, err := h.svc.CreateApplication(c.UserContext(), org, proj, callerIdentity(c).UserID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toApplicationView(app))
}

func (h *Handler) GetApplication(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	app, err := h.svc.GetApplication(c.UserContext(), org, callerIdentity(c).UserID, appID(c))
	if err != nil {
		return err
	}
	return c.JSON(toApplicationView(app))
}

func (h *Handler) ListApplications(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	proj := projectID(c)
	if proj == "" {
		return errProjectRequired
	}
	page, err := h.svc.ListApplications(c.UserContext(), org, callerIdentity(c).UserID, proj, pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]ApplicationView, 0, len(page.Items))
	for _, app := range page.Items {
		views = append(views, toApplicationView(&app))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) UpdateApplication(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[UpdateApplicationRequest](c)
	if err != nil {
		return err
	}
	app, err := h.svc.UpdateApplication(c.UserContext(), org, callerIdentity(c).UserID, appID(c), req)
	if err != nil {
		return err
	}
	return c.JSON(toApplicationView(app))
}

func (h *Handler) DeleteApplication(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	if err := h.svc.DeleteApplication(c.UserContext(), org, callerIdentity(c).UserID, appID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Deployments
// ----------------------------------------------------------------------------

func (h *Handler) CreateDeployment(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[CreateDeploymentRequest](c)
	if err != nil {
		return err
	}
	dep, rel, err := h.svc.CreateDeployment(c.UserContext(), org, callerIdentity(c).UserID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"deployment": toDeploymentView(dep, &rel.Revision),
		"release":    toReleaseView(rel),
	})
}

func (h *Handler) GetDeployment(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	dep, rev, err := h.svc.GetDeployment(c.UserContext(), org, callerIdentity(c).UserID, deploymentID(c))
	if err != nil {
		return err
	}
	return c.JSON(toDeploymentView(dep, rev))
}

func (h *Handler) ListDeployments(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	app := appID(c)
	if app == "" {
		return errAppNotFound
	}
	page, err := h.svc.ListDeployments(c.UserContext(), org, callerIdentity(c).UserID, app, pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]DeploymentView, 0, len(page.Items))
	for _, dep := range page.Items {
		views = append(views, toDeploymentView(&dep, nil))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) ListDeploymentsByCluster(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	clusterID := c.Params("clusterId")
	page, err := h.svc.ListDeploymentsByCluster(c.UserContext(), org, callerIdentity(c).UserID, clusterID, pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]DeploymentView, 0, len(page.Items))
	for _, dep := range page.Items {
		views = append(views, toDeploymentView(&dep, nil))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

// ListOrgDeployments returns all deployments in the organization.
func (h *Handler) ListOrgDeployments(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	page, err := h.svc.ListOrgDeployments(c.UserContext(), org, callerIdentity(c).UserID, pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]DeploymentView, 0, len(page.Items))
	for _, dep := range page.Items {
		views = append(views, toDeploymentView(&dep, nil))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

// DeleteDeployment soft-deletes a deployment.
func (h *Handler) DeleteDeployment(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	if err := h.svc.DeleteDeployment(c.UserContext(), org, callerIdentity(c).UserID, deploymentID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) UpdateDeployment(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[UpdateDeploymentRequest](c)
	if err != nil {
		return err
	}
	dep, rel, err := h.svc.UpdateDeployment(c.UserContext(), org, callerIdentity(c).UserID, deploymentID(c), req)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"deployment": toDeploymentView(dep, &rel.Revision),
		"release":    toReleaseView(rel),
	})
}

func (h *Handler) Rollback(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[RollbackRequest](c)
	if err != nil {
		return err
	}
	dep, rel, err := h.svc.Rollback(c.UserContext(), org, callerIdentity(c).UserID, deploymentID(c), req)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"deployment": toDeploymentView(dep, &rel.Revision),
		"release":    toReleaseView(rel),
	})
}

// ----------------------------------------------------------------------------
// Status Updates (user-facing - requires authorization)
// ----------------------------------------------------------------------------

func (h *Handler) UpdateDeploymentStatus(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	userID := callerIdentity(c).UserID
	depID := deploymentID(c)
	relID := releaseID(c)

	req, err := parseBody[UpdateStatusRequest](c)
	if err != nil {
		return err
	}

	switch req.Status {
	case "started", "deploying":
		err = h.svc.MarkDeploymentStarted(c.UserContext(), org, userID, depID, relID)
	case "succeeded":
		readyReplicas := 0
		if req.ReadyReplicas != nil {
			readyReplicas = *req.ReadyReplicas
		}
		err = h.svc.MarkDeploymentSucceeded(c.UserContext(), org, userID, depID, relID, readyReplicas)
	case "failed":
		errorMsg := ""
		if req.ErrorMessage != nil {
			errorMsg = *req.ErrorMessage
		}
		err = h.svc.MarkDeploymentFailed(c.UserContext(), org, userID, depID, relID, errorMsg)
	default:
		return apperrors.Validation("status must be deploying, succeeded, or failed")
	}

	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"status": "ok"})
}

// ----------------------------------------------------------------------------
// Releases
// ----------------------------------------------------------------------------

func (h *Handler) ListReleases(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	page, err := h.svc.ListReleases(c.UserContext(), org, callerIdentity(c).UserID, deploymentID(c), pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]ReleaseView, 0, len(page.Items))
	for _, rel := range page.Items {
		views = append(views, toReleaseView(&rel))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) GetRelease(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	rel, err := h.svc.GetRelease(c.UserContext(), org, callerIdentity(c).UserID, releaseID(c))
	if err != nil {
		return err
	}
	return c.JSON(toReleaseView(rel))
}
