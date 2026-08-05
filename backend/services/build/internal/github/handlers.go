package github

import (
	"io"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler adapts the GitHub Service to Fiber HTTP handlers.
type Handler struct {
	svc      *Service
	verifier TokenVerifier
}

// TokenVerifier verifies JWT tokens.
type TokenVerifier interface {
	Verify(token string) (Identity, error)
}

// Identity represents an authenticated user.
type Identity struct {
	UserID string
	Email  string
}

// NewHandler constructs an HTTP handler.
func NewHandler(svc *Service, verifier TokenVerifier) *Handler {
	return &Handler{svc: svc, verifier: verifier}
}

func parseBody[T any](c *fiber.Ctx) (T, error) {
	var body T
	if err := c.BodyParser(&body); err != nil {
		return body, apperrors.Validation("invalid request body")
	}
	return body, nil
}

func pageRequest(c *fiber.Ctx) database.PageRequest {
	return database.PageRequest{Limit: c.QueryInt("limit", 0), Cursor: c.Query("cursor")}
}

func orgID(c *fiber.Ctx) string        { return c.Params("orgId") }
func connectionID(c *fiber.Ctx) string { return c.Params("connectionId") }
func repositoryID(c *fiber.Ctx) string { return c.Params("repositoryId") }
func webhookID(c *fiber.Ctx) string    { return c.Params("webhookId") }

// ----------------------------------------------------------------------------
// OAuth Flow
// ----------------------------------------------------------------------------

// GetOAuthURL returns the GitHub OAuth URL.
func (h *Handler) GetOAuthURL(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	userID := callerIdentity(c).UserID
	
	var redirectURL *string
	if r := c.Query("redirectUrl"); r != "" {
		redirectURL = &r
	}
	
	resp, err := h.svc.GetOAuthURL(c.UserContext(), org, userID, redirectURL)
	if err != nil {
		return err
	}
	return c.JSON(resp)
}

// HandleOAuthCallback handles the OAuth callback from GitHub.
func (h *Handler) HandleOAuthCallback(c *fiber.Ctx) error {
	code := c.Query("code")
	state := c.Query("state")
	
	if code == "" || state == "" {
		return apperrors.Validation("code and state are required")
	}
	
	conn, err := h.svc.HandleOAuthCallback(c.UserContext(), code, state)
	if err != nil {
		return err
	}
	
	return c.JSON(ToConnectionView(conn))
}

// ----------------------------------------------------------------------------
// Connections
// ----------------------------------------------------------------------------

// CreatePATConnection creates a new PAT connection.
func (h *Handler) CreatePATConnection(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	req, err := parseBody[CreateConnectionRequest](c)
	if err != nil {
		return err
	}
	
	conn, err := h.svc.CreatePATConnection(c.UserContext(), org, callerIdentity(c).UserID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(ToConnectionView(conn))
}

// GetConnection returns a connection by ID.
func (h *Handler) GetConnection(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	conn, err := h.svc.GetConnection(c.UserContext(), org, callerIdentity(c).UserID, connectionID(c))
	if err != nil {
		return err
	}
	return c.JSON(ToConnectionView(conn))
}

// ListConnections returns all connections.
func (h *Handler) ListConnections(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	page, err := h.svc.ListConnections(c.UserContext(), org, callerIdentity(c).UserID, pageRequest(c))
	if err != nil {
		return err
	}
	
	views := make([]ConnectionView, 0, len(page.Items))
	for _, conn := range page.Items {
		views = append(views, ToConnectionView(&conn))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

// DeleteConnection deletes a connection.
func (h *Handler) DeleteConnection(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	if err := h.svc.DeleteConnection(c.UserContext(), org, callerIdentity(c).UserID, connectionID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ValidateConnection validates a connection.
func (h *Handler) ValidateConnection(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	conn, err := h.svc.ValidateConnection(c.UserContext(), org, callerIdentity(c).UserID, connectionID(c))
	if err != nil {
		return err
	}
	return c.JSON(ToConnectionView(conn))
}

// ListUserGitHubRepositories lists repositories from GitHub.
func (h *Handler) ListUserGitHubRepositories(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	page := c.QueryInt("page", 1)
	perPage := c.QueryInt("perPage", 30)
	
	repos, err := h.svc.ListUserGitHubRepositories(c.UserContext(), org, callerIdentity(c).UserID, connectionID(c), page, perPage)
	if err != nil {
		return err
	}
	
	// Convert to simple view
	views := make([]fiber.Map, len(repos))
	for i, r := range repos {
		views[i] = fiber.Map{
			"id":            r.ID,
			"name":          r.Name,
			"fullName":      r.FullName,
			"owner":         r.Owner.Login,
			"description":   r.Description,
			"htmlUrl":       r.HTMLURL,
			"cloneUrl":      r.CloneURL,
			"defaultBranch": r.DefaultBranch,
			"isPrivate":     r.Private,
			"language":      r.Language,
			"starsCount":    r.StargazersCount,
		}
	}
	return c.JSON(fiber.Map{"items": views})
}

// ----------------------------------------------------------------------------
// Repositories
// ----------------------------------------------------------------------------

// ConnectRepository connects a GitHub repository.
func (h *Handler) ConnectRepository(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	req, err := parseBody[ConnectRepositoryRequest](c)
	if err != nil {
		return err
	}
	
	repo, err := h.svc.ConnectRepository(c.UserContext(), org, callerIdentity(c).UserID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(ToRepositoryView(repo))
}

// GetRepository returns a repository by ID.
func (h *Handler) GetRepository(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	repo, err := h.svc.GetRepository(c.UserContext(), org, callerIdentity(c).UserID, repositoryID(c))
	if err != nil {
		return err
	}
	return c.JSON(ToRepositoryView(repo))
}

// ListRepositories returns all repositories.
func (h *Handler) ListRepositories(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	page, err := h.svc.ListRepositories(c.UserContext(), org, callerIdentity(c).UserID, pageRequest(c))
	if err != nil {
		return err
	}
	
	views := make([]RepositoryView, 0, len(page.Items))
	for _, repo := range page.Items {
		views = append(views, ToRepositoryView(&repo))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

// DeleteRepository deletes a repository.
func (h *Handler) DeleteRepository(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	if err := h.svc.DeleteRepository(c.UserContext(), org, callerIdentity(c).UserID, repositoryID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SyncRepository syncs a repository from GitHub.
func (h *Handler) SyncRepository(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	repo, err := h.svc.SyncRepository(c.UserContext(), org, callerIdentity(c).UserID, repositoryID(c))
	if err != nil {
		return err
	}
	return c.JSON(ToRepositoryView(repo))
}

// ----------------------------------------------------------------------------
// Branches & Commits
// ----------------------------------------------------------------------------

// ListBranches returns branches for a repository.
func (h *Handler) ListBranches(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	page := c.QueryInt("page", 1)
	perPage := c.QueryInt("perPage", 30)
	
	branches, err := h.svc.ListBranches(c.UserContext(), org, callerIdentity(c).UserID, repositoryID(c), page, perPage)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": branches})
}

// ListCommits returns commits for a repository.
func (h *Handler) ListCommits(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	branch := c.Query("branch", "")
	page := c.QueryInt("page", 1)
	perPage := c.QueryInt("perPage", 30)
	
	commits, err := h.svc.ListCommits(c.UserContext(), org, callerIdentity(c).UserID, repositoryID(c), branch, page, perPage)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": commits})
}

// GetDefaultBranch returns the default branch for a repository.
func (h *Handler) GetDefaultBranch(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	branch, err := h.svc.GetDefaultBranch(c.UserContext(), org, callerIdentity(c).UserID, repositoryID(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"defaultBranch": branch})
}

// ----------------------------------------------------------------------------
// Webhooks
// ----------------------------------------------------------------------------

// RegisterWebhook registers a webhook.
func (h *Handler) RegisterWebhook(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	req, err := parseBody[RegisterWebhookRequest](c)
	if err != nil {
		req = RegisterWebhookRequest{}
	}
	
	webhook, err := h.svc.RegisterWebhook(c.UserContext(), org, callerIdentity(c).UserID, repositoryID(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(ToWebhookView(webhook))
}

// GetWebhook returns a webhook by ID.
func (h *Handler) GetWebhook(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	webhook, err := h.svc.GetWebhook(c.UserContext(), org, callerIdentity(c).UserID, webhookID(c))
	if err != nil {
		return err
	}
	return c.JSON(ToWebhookView(webhook))
}

// DeleteWebhook deletes a webhook.
func (h *Handler) DeleteWebhook(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	if err := h.svc.DeleteWebhook(c.UserContext(), org, callerIdentity(c).UserID, webhookID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// HandleWebhookDelivery handles an incoming webhook from GitHub.
func (h *Handler) HandleWebhookDelivery(c *fiber.Ctx) error {
	repoID := repositoryID(c)
	if repoID == "" {
		return apperrors.Validation("repository ID required")
	}
	
	deliveryID := c.Get("X-GitHub-Delivery")
	eventType := c.Get("X-GitHub-Event")
	signature := c.Get("X-Hub-Signature-256")
	
	if eventType == "" {
		return apperrors.Validation("X-GitHub-Event header required")
	}
	
	payload, err := io.ReadAll(c.Request().BodyStream())
	if err != nil {
		return apperrors.Validation("failed to read request body")
	}
	
	delivery, err := h.svc.HandleWebhookDelivery(c.UserContext(), repoID, deliveryID, eventType, signature, payload)
	if err != nil {
		return err
	}
	
	// Return success regardless of signature validation (GitHub expects 200)
	return c.JSON(fiber.Map{
		"id":              delivery.ID,
		"signatureValid":  delivery.SignatureValid,
		"status":          delivery.Status,
	})
}

// ListWebhookDeliveries returns deliveries for a webhook.
func (h *Handler) ListWebhookDeliveries(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return apperrors.Validation("organization ID required")
	}
	
	page, err := h.svc.ListWebhookDeliveries(c.UserContext(), org, callerIdentity(c).UserID, webhookID(c), pageRequest(c))
	if err != nil {
		return err
	}
	
	return c.JSON(fiber.Map{"items": page.Items, "nextCursor": page.NextCursor})
}
