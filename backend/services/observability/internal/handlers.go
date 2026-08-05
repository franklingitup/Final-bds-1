package observability

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler wraps observability service for HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler creates a new observability handler.
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
// Metrics Handlers
// ----------------------------------------------------------------------------

// QueryMetrics queries Prometheus for metrics.
func (h *Handler) QueryMetrics(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	var req MetricsQueryRequest
	if err := c.BodyParser(&req); err != nil {
		// Try query params
		req = MetricsQueryRequest{
			Query: c.Query("query"),
			Start: c.Query("start", "-1h"),
			End:   c.Query("end"),
			Step:  c.Query("step"),
		}
	}

	if req.Query == "" {
		return apperrors.Validation("query is required")
	}

	result, err := h.svc.QueryMetrics(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.JSON(result)
}

// QueryInstantMetrics queries Prometheus for instant metrics.
func (h *Handler) QueryInstantMetrics(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	query := c.Query("query")
	if query == "" {
		return apperrors.Validation("query is required")
	}

	result, err := h.svc.QueryInstantMetrics(c.Context(), orgID, id.UserID, query)
	if err != nil {
		return err
	}

	return c.JSON(result)
}

// GetResourceMetrics returns aggregated metrics for a resource.
func (h *Handler) GetResourceMetrics(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	resourceType := c.Params("resourceType")
	resourceID := c.Params("resourceId")

	if resourceType == "" || resourceID == "" {
		return apperrors.Validation("resource type and ID are required")
	}

	result, err := h.svc.GetResourceMetrics(c.Context(), orgID, id.UserID, resourceType, resourceID)
	if err != nil {
		return err
	}

	return c.JSON(result)
}

// ----------------------------------------------------------------------------
// Logs Handlers
// ----------------------------------------------------------------------------

// QueryLogs queries Loki for logs.
func (h *Handler) QueryLogs(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	var req LogsQueryRequest
	if err := c.BodyParser(&req); err != nil {
		// Try query params
		req = LogsQueryRequest{
			Query:     c.Query("query"),
			Start:     c.Query("start", "-1h"),
			End:       c.Query("end"),
			Limit:     c.QueryInt("limit", 100),
			Direction: c.Query("direction", "backward"),
		}
	}

	if req.Query == "" {
		return apperrors.Validation("query is required")
	}

	result, err := h.svc.QueryLogs(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.JSON(result)
}

// GetLogStreams returns log stream metadata.
func (h *Handler) GetLogStreams(c *fiber.Ctx) error {
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

	result, err := h.svc.GetLogStreams(c.Context(), orgID, id.UserID, page)
	if err != nil {
		return err
	}

	views := make([]LogStreamView, len(result.Items))
	for i, s := range result.Items {
		views[i] = ToLogStreamView(&s)
	}

	return c.JSON(fiber.Map{
		"streams":    views,
		"nextCursor": result.NextCursor,
	})
}

// ----------------------------------------------------------------------------
// Dashboard Handlers
// ----------------------------------------------------------------------------

// CreateDashboard creates a new dashboard.
func (h *Handler) CreateDashboard(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	var req CreateDashboardRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	dash, err := h.svc.CreateDashboard(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToDashboardView(dash))
}

// GetDashboard returns a dashboard by ID.
func (h *Handler) GetDashboard(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	dashboardID := c.Params("dashboardId")
	if dashboardID == "" {
		return apperrors.Validation("dashboard ID is required")
	}

	dash, err := h.svc.GetDashboard(c.Context(), orgID, id.UserID, dashboardID)
	if err != nil {
		return err
	}

	return c.JSON(ToDashboardView(dash))
}

// ListDashboards returns dashboards for an organization.
func (h *Handler) ListDashboards(c *fiber.Ctx) error {
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

	result, err := h.svc.ListDashboards(c.Context(), orgID, id.UserID, page)
	if err != nil {
		return err
	}

	views := make([]DashboardView, len(result.Items))
	for i, d := range result.Items {
		views[i] = ToDashboardView(&d)
	}

	return c.JSON(fiber.Map{
		"dashboards": views,
		"nextCursor": result.NextCursor,
	})
}

// UpdateDashboard updates a dashboard.
func (h *Handler) UpdateDashboard(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	dashboardID := c.Params("dashboardId")
	if dashboardID == "" {
		return apperrors.Validation("dashboard ID is required")
	}

	var req CreateDashboardRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	dash, err := h.svc.UpdateDashboard(c.Context(), orgID, id.UserID, dashboardID, req)
	if err != nil {
		return err
	}

	return c.JSON(ToDashboardView(dash))
}

// DeleteDashboard deletes a dashboard.
func (h *Handler) DeleteDashboard(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	dashboardID := c.Params("dashboardId")
	if dashboardID == "" {
		return apperrors.Validation("dashboard ID is required")
	}

	if err := h.svc.DeleteDashboard(c.Context(), orgID, id.UserID, dashboardID); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Health Handlers
// ----------------------------------------------------------------------------

// GetHealthSummary returns health summary.
func (h *Handler) GetHealthSummary(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	summary, err := h.svc.GetHealthSummary(c.Context(), orgID, id.UserID)
	if err != nil {
		return err
	}

	return c.JSON(summary)
}

// GetResourceHealth returns health for a resource.
func (h *Handler) GetResourceHealth(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	resourceType := c.Params("resourceType")
	resourceID := c.Params("resourceId")

	checks, err := h.svc.GetResourceHealth(c.Context(), orgID, id.UserID, resourceType, resourceID)
	if err != nil {
		return err
	}

	views := make([]HealthCheckView, len(checks))
	for i, c := range checks {
		views[i] = ToHealthCheckView(&c)
	}

	return c.JSON(fiber.Map{"healthChecks": views})
}

// ----------------------------------------------------------------------------
// Events Handlers
// ----------------------------------------------------------------------------

// ListEvents returns observability events.
func (h *Handler) ListEvents(c *fiber.Ctx) error {
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

	result, err := h.svc.ListEvents(c.Context(), orgID, id.UserID, page)
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
// Alert Handlers
// ----------------------------------------------------------------------------

// CreateAlertRule creates an alert rule.
func (h *Handler) CreateAlertRule(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	var req CreateAlertRuleRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	rule, err := h.svc.CreateAlertRule(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToAlertRuleView(rule))
}

// ListAlertRules returns alert rules.
func (h *Handler) ListAlertRules(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	rules, err := h.svc.ListAlertRules(c.Context(), orgID, id.UserID)
	if err != nil {
		return err
	}

	views := make([]AlertRuleView, len(rules))
	for i, r := range rules {
		views[i] = ToAlertRuleView(&r)
	}

	return c.JSON(fiber.Map{"alertRules": views})
}

// GetFiringAlerts returns firing alerts.
func (h *Handler) GetFiringAlerts(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	alerts, err := h.svc.GetFiringAlerts(c.Context(), orgID, id.UserID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"firingAlerts": alerts})
}

// ----------------------------------------------------------------------------
// Overview Handlers
// ----------------------------------------------------------------------------

// GetOverview returns platform overview.
func (h *Handler) GetOverview(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}

	orgID := extractOrgID(c)
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	stats, err := h.svc.GetOverview(c.Context(), orgID, id.UserID)
	if err != nil {
		return err
	}

	return c.JSON(stats)
}

// ----------------------------------------------------------------------------
// Agent Ingest Handlers
// ----------------------------------------------------------------------------

// IngestMetrics ingests metrics from an agent.
func (h *Handler) IngestMetrics(c *fiber.Ctx) error {
	orgID := c.Get("X-Organization-ID")
	if orgID == "" {
		orgID = extractOrgID(c)
	}
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	var req IngestMetricsRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	if err := h.svc.IngestMetrics(c.Context(), orgID, req); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// IngestLogs ingests logs from an agent.
func (h *Handler) IngestLogs(c *fiber.Ctx) error {
	orgID := c.Get("X-Organization-ID")
	if orgID == "" {
		orgID = extractOrgID(c)
	}
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	var req IngestLogsRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	if err := h.svc.IngestLogs(c.Context(), orgID, req); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ReportHealth reports health from an agent.
func (h *Handler) ReportHealth(c *fiber.Ctx) error {
	orgID := c.Get("X-Organization-ID")
	if orgID == "" {
		orgID = extractOrgID(c)
	}
	if orgID == "" {
		return apperrors.Validation("organization ID is required")
	}

	var req ReportHealthRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	if err := h.svc.ReportHealth(c.Context(), orgID, req); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusOK)
}

// HealthCheck returns service health status.
func (h *Handler) HealthCheck(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": "healthy",
		"service": "observability",
	})
}
