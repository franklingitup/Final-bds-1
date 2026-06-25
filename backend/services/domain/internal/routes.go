// Package domain manages custom domains, DNS verification, ingress bindings, and
// TLS certificate lifecycle. See docs/04-api-spec.md section 7.
package domain

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts domain routes onto the app.
func RegisterRoutes(app *fiber.App) {
	g := app.Group("/v1")
	_ = g
	// TODO: domains, verify, bindings, certificates; cron worker for renewal polling.
}
