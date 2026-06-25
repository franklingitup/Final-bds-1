// Package notification delivers email/webhook/Slack/in-app notifications and
// subscribes to broad domain events. See docs/03-engineering-design.md (1.11).
package notification

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts notification routes onto the app.
func RegisterRoutes(app *fiber.App) {
	g := app.Group("/v1")
	_ = g
	// TODO: channels, webhooks, preferences; retry worker for deliveries.
}
