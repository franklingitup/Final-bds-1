package pipeline

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler adapts the Pipeline Service to Fiber HTTP handlers.
type Handler struct {
	svc *Service
}

// NewHandler constructs a pipeline HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
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

func orgID(c *fiber.Ctx) string        { return c.Params("orgId") }
func deploymentID(c *fiber.Ctx) string { return c.Params("deploymentId") }
func pipelineID(c *fiber.Ctx) string   { return c.Params("pipelineId") }
func clusterID(c *fiber.Ctx) string    { return c.Params("clusterId") }

// callerID extracts the user ID from context (set by auth middleware).
func callerID(c *fiber.Ctx) string {
	if id, ok := c.Locals("user_id").(string); ok {
		return id
	}
	return ""
}

// ----------------------------------------------------------------------------
// Pipeline Operations
// ----------------------------------------------------------------------------

// TriggerPipeline triggers a new deployment pipeline.
func (h *Handler) TriggerPipeline(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	req, err := parseBody[TriggerPipelineRequest](c)
	if err != nil {
		return err
	}

	// If deployment ID is in path, use it
	if depID := deploymentID(c); depID != "" {
		req.DeploymentID = depID
	}

	pr, err := h.svc.TriggerPipeline(c.UserContext(), org, callerID(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(ToPipelineRunView(pr))
}

// GetPipelineRun returns a pipeline run by ID.
func (h *Handler) GetPipelineRun(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	pr, err := h.svc.GetPipelineRun(c.UserContext(), org, callerID(c), pipelineID(c))
	if err != nil {
		return err
	}
	return c.JSON(ToPipelineRunView(pr))
}

// ListPipelineRuns returns pipeline runs for a deployment.
func (h *Handler) ListPipelineRuns(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	page, err := h.svc.ListPipelineRuns(c.UserContext(), org, callerID(c), deploymentID(c), pageRequest(c))
	if err != nil {
		return err
	}

	views := make([]PipelineRunView, 0, len(page.Items))
	for _, pr := range page.Items {
		views = append(views, ToPipelineRunView(&pr))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

// GetPipelineEvents returns events for a pipeline run.
func (h *Handler) GetPipelineEvents(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	page, err := h.svc.GetPipelineEvents(c.UserContext(), org, callerID(c), pipelineID(c), pageRequest(c))
	if err != nil {
		return err
	}

	views := make([]PipelineEventView, 0, len(page.Items))
	for _, pe := range page.Items {
		views = append(views, ToPipelineEventView(&pe))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

// CancelPipeline cancels an active pipeline.
func (h *Handler) CancelPipeline(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	if err := h.svc.CancelPipeline(c.UserContext(), org, callerID(c), pipelineID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// QuickDeploy triggers a deployment with a specific image.
func (h *Handler) QuickDeploy(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	var req struct {
		Image string `json:"image"`
	}
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}
	if req.Image == "" {
		return apperrors.Validation("image is required")
	}

	pr, err := h.svc.QuickDeploy(c.UserContext(), org, callerID(c), deploymentID(c), req.Image)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(ToPipelineRunView(pr))
}

// TriggerRollback triggers a rollback to a previous release.
func (h *Handler) TriggerRollback(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	var req struct {
		TargetRevision *int `json:"targetRevision"`
	}
	_ = c.BodyParser(&req) // Optional body

	pr, err := h.svc.TriggerRollback(c.UserContext(), org, callerID(c), deploymentID(c), req.TargetRevision)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(ToPipelineRunView(pr))
}

// ----------------------------------------------------------------------------
// Desired State
// ----------------------------------------------------------------------------

// GetDesiredState returns the desired state for a deployment.
func (h *Handler) GetDesiredState(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	ds, err := h.svc.GetDesiredState(c.UserContext(), org, callerID(c), deploymentID(c))
	if err != nil {
		return err
	}
	return c.JSON(ToDesiredStateView(ds))
}

// GetDeploymentMetrics returns metrics for a deployment.
func (h *Handler) GetDeploymentMetrics(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	dm, err := h.svc.GetDeploymentMetrics(c.UserContext(), org, callerID(c), deploymentID(c))
	if err != nil {
		return err
	}
	return c.JSON(ToDeploymentMetricsView(dm))
}

// ----------------------------------------------------------------------------
// Agent Endpoints (called by platform-agent)
// ----------------------------------------------------------------------------

// GetAgentDesiredState returns pending desired states for an agent's cluster.
func (h *Handler) GetAgentDesiredState(c *fiber.Ctx) error {
	org := orgID(c)
	cluster := clusterID(c)
	if org == "" || cluster == "" {
		return apperrors.Validation("organization ID and cluster ID required")
	}

	states, err := h.svc.GetDesiredStateForAgent(c.UserContext(), org, cluster)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": states})
}

// ReportSync handles sync status reports from agents.
func (h *Handler) ReportSync(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	req, err := parseBody[ReportSyncRequest](c)
	if err != nil {
		return err
	}

	if err := h.svc.ReportSync(c.UserContext(), org, req); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ReportMetrics handles metrics reports from agents.
func (h *Handler) ReportMetrics(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}

	req, err := parseBody[ReportMetricsRequest](c)
	if err != nil {
		return err
	}

	if err := h.svc.ReportMetrics(c.UserContext(), org, req); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
