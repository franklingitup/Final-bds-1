// Package audit consumes platform domain events and records an immutable,
// append-only audit trail, serving tenant-scoped queries over it. See
// docs/06-security-design.md section 7 and docs/12-event-catalog.md.
package audit

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts the audit query API onto the app. All routes require a
// Bearer access token issued by the auth service; results are RLS-scoped to the
// organization in the path.
func RegisterRoutes(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1", h.RequireAuth())
	logs := v1.Group("/organizations/:orgId/audit-logs")
	logs.Get("", h.ListAuditLogs)
	logs.Get("/:eventId", h.GetAuditLog)
}
