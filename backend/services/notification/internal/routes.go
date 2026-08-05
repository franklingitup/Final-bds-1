// Package notification delivers email/webhook/Slack/in-app notifications and
// subscribes to broad domain events. See docs/03-engineering-design.md (1.11).
package notification

import "github.com/gofiber/fiber/v2"

// RegisterRoutesWithDeps registers routes with injected dependencies.
func RegisterRoutesWithDeps(app *fiber.App, h *Handler, verifier TokenVerifier) {
	auth := RequireAuth(verifier)

	v1 := app.Group("/v1")

	// Organization-scoped routes
	orgs := v1.Group("/organizations/:orgId", auth)

	// Notification channels
	channels := orgs.Group("/channels")
	channels.Post("", h.CreateChannel)
	channels.Get("", h.ListChannels)
	channels.Get("/:channelId", h.GetChannel)
	channels.Patch("/:channelId", h.UpdateChannel)
	channels.Delete("/:channelId", h.DeleteChannel)
	channels.Post("/:channelId/test", h.TestChannel)

	// User preferences
	prefs := orgs.Group("/preferences")
	prefs.Get("", h.GetPreferences)
	prefs.Put("", h.UpdatePreference)

	// Notifications
	notifs := orgs.Group("/notifications")
	notifs.Post("", h.SendNotification)
	notifs.Get("", h.ListNotifications)
	notifs.Get("/:notificationId", h.GetNotification)

	// Webhooks
	webhooks := orgs.Group("/webhooks")
	webhooks.Post("", h.CreateWebhook)
	webhooks.Get("", h.ListWebhooks)
	webhooks.Delete("/:webhookId", h.DeleteWebhook)

	// Dead letter queue
	dlq := orgs.Group("/dlq")
	dlq.Get("", h.ListDLQ)
	dlq.Post("/replay", h.ReplayDLQ)
	dlq.Post("/discard", h.DiscardDLQ)
}

// RegisterRoutes mounts notification routes onto the app.
// This is a stub for the httpserver package compatibility.
func RegisterRoutes(app *fiber.App) {
	g := app.Group("/v1")
	_ = g
	// Routes are registered via RegisterRoutesWithDeps after service initialization.
}
