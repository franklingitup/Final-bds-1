package a

import "github.com/gofiber/fiber/v2"

type Handler struct{}

func (h *Handler) RequireAuth() fiber.Handler {
	return func(c *fiber.Ctx) error { return c.Next() }
}

func Authentication() fiber.Handler {
	return func(c *fiber.Ctx) error { return c.Next() }
}

// Bad patterns - should be flagged

func badEmptyPathOnV1(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1")
	v1.Group("", h.RequireAuth()) // want "blanket auth middleware"
}

func badRootWithAuth(app *fiber.App) {
	app.Group("/v1", Authentication()) // want "blanket auth middleware"
}

func badAPIPrefix(app *fiber.App, h *Handler) {
	api := app.Group("/api", h.RequireAuth()) // want "blanket auth middleware"
	_ = api
}

func badV2Prefix(app *fiber.App, h *Handler) {
	app.Group("/v2", h.RequireAuth()) // want "blanket auth middleware"
}

// Good patterns - should NOT be flagged

func goodScopedAuth(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1")
	v1.Group("/organizations/:orgId", h.RequireAuth()) // OK - scoped to specific path
}

func goodEmptyNoMiddleware(app *fiber.App) {
	v1 := app.Group("/v1")
	v1.Group("") // OK - no middleware
}

func goodDeepPath(app *fiber.App, h *Handler) {
	app.Group("/v1/admin/protected", h.RequireAuth()) // OK - specific path
}

func goodNestedGroupWithEmptyPath(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1")
	auth := v1.Group("/auth")
	// This is SAFE - empty path on a nested group only affects /v1/auth/*
	auth.Group("", h.RequireAuth()) // OK - nested under /v1/auth
}

func goodSubgroupAuth(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1")
	users := v1.Group("/users")
	// Safe - only affects /v1/users/*
	users.Group("", h.RequireAuth()) // OK - nested under /v1/users
}
