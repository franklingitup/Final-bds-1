// Package provisioning generates cloud-specific cluster config and install
// commands, and tracks install sessions. See docs/07-cluster-engine-design.md.
package provisioning

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts provisioning routes onto the app.
func RegisterRoutes(app *fiber.App) {
	g := app.Group("/v1")
	_ = g
	// TODO: provision, install-sessions, provisioning templates + telemetry ingest.
}
