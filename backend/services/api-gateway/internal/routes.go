// Package gateway wires the API gateway: auth enforcement, tenant context
// injection, rate limiting, and routing to backend services.
// Business logic is intentionally omitted in this skeleton.
package gateway

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts gateway routes onto the app.
func RegisterRoutes(app *fiber.App) {
	v1 := app.Group("/v1")
	_ = v1
	// TODO: validate access tokens, inject tenant context, proxy to services.
}
