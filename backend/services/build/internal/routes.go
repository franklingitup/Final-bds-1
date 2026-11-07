// Package build builds container images from Git or uploaded source and pushes
// them to the registry.
package build

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts build routes onto the app.
func RegisterRoutes(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1")

	// User JWT must be scoped to org routes only. A Group("", RequireAuth) on
	// /v1 registers Fiber USE /v1, which intercepts later-mounted GitHub
	// callback and webhook routes registered in main.go after this function.
	authenticated := v1.Group("/organizations/:orgId", h.RequireAuth())

	// Git Repositories (scoped to org/project).
	repos := authenticated.Group("/projects/:projectId/repositories")
	repos.Post("", h.CreateRepository)
	repos.Get("", h.ListRepositories)

	// Single repository operations.
	singleRepo := authenticated.Group("/repositories/:repoId")
	singleRepo.Get("", h.GetRepository)
	singleRepo.Patch("", h.UpdateRepository)
	singleRepo.Delete("", h.DeleteRepository)

	// Builds at org level.
	builds := authenticated.Group("/builds")
	builds.Post("", h.CreateBuild)
	builds.Get("", h.ListBuilds)

	// Single build operations.
	build := authenticated.Group("/builds/:buildId")
	build.Get("", h.GetBuild)
	build.Post("/cancel", h.CancelBuild)
	build.Post("/retry", h.RetryBuild)
	build.Get("/logs", h.GetBuildLogs)
	build.Get("/artifact", h.GetBuildArtifact)
}
