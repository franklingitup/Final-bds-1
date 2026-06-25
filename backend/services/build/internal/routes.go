// Package build builds container images from Git or uploaded source and pushes
// them to the registry. See docs/04-api-spec.md section 5.
package build

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts build routes onto the app.
func RegisterRoutes(app *fiber.App) {
	g := app.Group("/v1")
	_ = g
	// TODO: builds, build logs, source uploads; build worker consumes BuildRequested.
}
