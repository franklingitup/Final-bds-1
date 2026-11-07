package github

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts GitHub routes onto the app.
func RegisterRoutes(app *fiber.App, h *Handler) {
	v1 := app.Group("/v1")

	// OAuth callback (no auth required - GitHub redirects here)
	v1.Get("/github/oauth/callback", h.HandleOAuthCallback)

	// Webhook receiver (no auth - GitHub calls this)
	v1.Post("/webhooks/github/:repositoryId", h.HandleWebhookDelivery)

	// User JWT must be scoped to org routes only. A Group("", RequireAuth) on
	// /v1 registers Fiber USE /v1 and would intercept the public callback and
	// webhook routes above (and any later /v1 mounts).
	authenticated := v1.Group("/organizations/:orgId", h.RequireAuth())

	// OAuth initiation
	authenticated.Get("/github/oauth/authorize", h.GetOAuthURL)

	// Connections
	connections := authenticated.Group("/github/connections")
	connections.Post("", h.CreatePATConnection)
	connections.Get("", h.ListConnections)

	// Single connection operations
	singleConn := authenticated.Group("/github/connections/:connectionId")
	singleConn.Get("", h.GetConnection)
	singleConn.Delete("", h.DeleteConnection)
	singleConn.Post("/validate", h.ValidateConnection)
	singleConn.Get("/repositories", h.ListUserGitHubRepositories)

	// Connected repositories
	repos := authenticated.Group("/github/repositories")
	repos.Post("", h.ConnectRepository)
	repos.Get("", h.ListRepositories)

	// Single repository operations
	singleRepo := authenticated.Group("/github/repositories/:repositoryId")
	singleRepo.Get("", h.GetRepository)
	singleRepo.Delete("", h.DeleteRepository)
	singleRepo.Post("/sync", h.SyncRepository)
	singleRepo.Get("/branches", h.ListBranches)
	singleRepo.Get("/commits", h.ListCommits)
	singleRepo.Get("/default-branch", h.GetDefaultBranch)

	// Webhooks
	singleRepo.Post("/webhook", h.RegisterWebhook)

	// Webhook operations
	webhooks := authenticated.Group("/github/webhooks/:webhookId")
	webhooks.Get("", h.GetWebhook)
	webhooks.Delete("", h.DeleteWebhook)
	webhooks.Get("/deliveries", h.ListWebhookDeliveries)
}
