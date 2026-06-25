package audit

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler adapts the audit Service to Fiber HTTP handlers.
type Handler struct {
	svc      *Service
	verifier *TokenVerifier
}

// NewHandler constructs an HTTP handler.
func NewHandler(svc *Service, verifier *TokenVerifier) *Handler {
	return &Handler{svc: svc, verifier: verifier}
}

func pageRequest(c *fiber.Ctx) database.PageRequest {
	return database.PageRequest{Limit: c.QueryInt("limit", 0), Cursor: c.Query("cursor")}
}

// parseFilter reads the supported query parameters into an AuditFilter. Time
// bounds accept RFC3339 timestamps; a malformed value is a validation error.
func parseFilter(c *fiber.Ctx) (AuditFilter, error) {
	f := AuditFilter{
		EventType:    c.Query("eventType"),
		Domain:       c.Query("domain"),
		ActorID:      c.Query("actorId"),
		ResourceType: c.Query("resourceType"),
		ResourceID:   c.Query("resourceId"),
	}
	if v := c.Query("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return AuditFilter{}, apperrors.Validation("invalid 'from' timestamp; expected RFC3339")
		}
		f.From = &t
	}
	if v := c.Query("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return AuditFilter{}, apperrors.Validation("invalid 'to' timestamp; expected RFC3339")
		}
		f.To = &t
	}
	return f, nil
}

// ListAuditLogs returns a filtered, paginated page of audit records for an org.
func (h *Handler) ListAuditLogs(c *fiber.Ctx) error {
	orgID := c.Params("orgId")
	if orgID == "" {
		return errOrgRequired
	}
	filter, err := parseFilter(c)
	if err != nil {
		return err
	}
	page, err := h.svc.ListLogs(c.UserContext(), orgID, callerIdentity(c).UserID, filter, pageRequest(c))
	if err != nil {
		return err
	}
	views := make([]AuditLogView, 0, len(page.Items))
	for _, a := range page.Items {
		views = append(views, toAuditLogView(a))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

// GetAuditLog returns a single audit record by its source event id.
func (h *Handler) GetAuditLog(c *fiber.Ctx) error {
	orgID := c.Params("orgId")
	if orgID == "" {
		return errOrgRequired
	}
	rec, err := h.svc.GetLog(c.UserContext(), orgID, callerIdentity(c).UserID, c.Params("eventId"))
	if err != nil {
		return err
	}
	return c.JSON(toAuditLogView(rec))
}
