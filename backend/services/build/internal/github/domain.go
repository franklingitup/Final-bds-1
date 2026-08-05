// Package github provides GitHub integration for the build service.
package github

import (
	"encoding/json"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// Connection type constants.
const (
	ConnectionTypeOAuth = "oauth"
	ConnectionTypePAT   = "pat"
	ConnectionTypeApp   = "app"
)

// Connection status constants.
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
	StatusExpired = "expired"
	StatusInvalid = "invalid"
)

// Webhook status constants.
const (
	WebhookStatusActive   = "active"
	WebhookStatusInactive = "inactive"
	WebhookStatusFailed   = "failed"
)

// Delivery status constants.
const (
	DeliveryStatusReceived  = "received"
	DeliveryStatusProcessed = "processed"
	DeliveryStatusFailed    = "failed"
	DeliveryStatusIgnored   = "ignored"
)

// GitHubConnection represents a connection to GitHub (OAuth or PAT).
type GitHubConnection struct {
	database.TenantModel
	ConnectionType  string     `db:"connection_type"`
	Name            string     `db:"name"`
	GitHubUserID    *int64     `db:"github_user_id"`
	GitHubUsername  *string    `db:"github_username"`
	GitHubAvatar    *string    `db:"github_avatar"`
	AccessToken     []byte     `db:"access_token"`
	RefreshToken    []byte     `db:"refresh_token"`
	TokenExpiresAt  *time.Time `db:"token_expires_at"`
	Scopes          []string   `db:"scopes"`
	TokenHash       *string    `db:"token_hash"`
	LastUsedAt      *time.Time `db:"last_used_at"`
	LastValidatedAt *time.Time `db:"last_validated_at"`
	Status          string     `db:"status"`
	ErrorMessage    *string    `db:"error_message"`
	CreatedBy       *string    `db:"created_by"`
}

// GitHubRepository represents cached metadata about a GitHub repository.
type GitHubRepository struct {
	database.TenantModel
	ConnectionID    string          `db:"connection_id"`
	GitHubRepoID    int64           `db:"github_repo_id"`
	Owner           string          `db:"owner"`
	Name            string          `db:"name"`
	FullName        string          `db:"full_name"`
	Description     *string         `db:"description"`
	HTMLURL         string          `db:"html_url"`
	CloneURL        string          `db:"clone_url"`
	SSHURL          *string         `db:"ssh_url"`
	DefaultBranch   string          `db:"default_branch"`
	IsPrivate       bool            `db:"is_private"`
	IsFork          bool            `db:"is_fork"`
	IsArchived      bool            `db:"is_archived"`
	StarsCount      int             `db:"stars_count"`
	ForksCount      int             `db:"forks_count"`
	WatchersCount   int             `db:"watchers_count"`
	OpenIssuesCount int             `db:"open_issues_count"`
	Topics          []string        `db:"topics"`
	Language        *string         `db:"language"`
	Languages       json.RawMessage `db:"languages"`
	Permissions     json.RawMessage `db:"permissions"`
	LastSyncedAt    *time.Time      `db:"last_synced_at"`
	SyncError       *string         `db:"sync_error"`
	CreatedBy       *string         `db:"created_by"`
}

// GitHubWebhook represents a registered webhook on a GitHub repository.
type GitHubWebhook struct {
	ID             string     `db:"id"`
	OrgID          string     `db:"org_id"`
	RepositoryID   string     `db:"repository_id"`
	GitHubHookID   int64      `db:"github_hook_id"`
	Events         []string   `db:"events"`
	WebhookURL     string     `db:"webhook_url"`
	Secret         []byte     `db:"secret"`
	SecretHash     string     `db:"secret_hash"`
	Status         string     `db:"status"`
	LastDeliveryAt *time.Time `db:"last_delivery_at"`
	LastError      *string    `db:"last_error"`
	DeliveryCount  int        `db:"delivery_count"`
	CreatedAt      time.Time  `db:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at"`
}

// GitHubWebhookDelivery represents a webhook delivery event.
type GitHubWebhookDelivery struct {
	ID                 string          `db:"id"`
	OrgID              string          `db:"org_id"`
	WebhookID          string          `db:"webhook_id"`
	GitHubDeliveryID   string          `db:"github_delivery_id"`
	EventType          string          `db:"event_type"`
	Action             *string         `db:"action"`
	Payload            json.RawMessage `db:"payload"`
	Headers            json.RawMessage `db:"headers"`
	Signature          *string         `db:"signature"`
	SignatureValid     bool            `db:"signature_valid"`
	Status             string          `db:"status"`
	ProcessedAt        *time.Time      `db:"processed_at"`
	ErrorMessage       *string         `db:"error_message"`
	SenderLogin        *string         `db:"sender_login"`
	SenderID           *int64          `db:"sender_id"`
	RepositoryName     *string         `db:"repository_name"`
	Ref                *string         `db:"ref"`
	ReceivedAt         time.Time       `db:"received_at"`
}

// GitHubOAuthState represents a temporary OAuth state for CSRF protection.
type GitHubOAuthState struct {
	ID          string    `db:"id"`
	OrgID       string    `db:"org_id"`
	UserID      string    `db:"user_id"`
	State       string    `db:"state"`
	RedirectURL *string   `db:"redirect_url"`
	CreatedAt   time.Time `db:"created_at"`
	ExpiresAt   time.Time `db:"expires_at"`
}

// ----------------------------------------------------------------------------
// Request DTOs
// ----------------------------------------------------------------------------

// CreateConnectionRequest is the request to create a GitHub connection.
type CreateConnectionRequest struct {
	Name           string  `json:"name"`
	ConnectionType string  `json:"connectionType,omitempty"` // oauth | pat
	AccessToken    *string `json:"accessToken,omitempty"`    // For PAT
}

// UpdateConnectionRequest is the request to update a GitHub connection.
type UpdateConnectionRequest struct {
	Name        *string `json:"name,omitempty"`
	AccessToken *string `json:"accessToken,omitempty"`
}

// ConnectRepositoryRequest is the request to connect a GitHub repository.
type ConnectRepositoryRequest struct {
	ConnectionID string `json:"connectionId"`
	Owner        string `json:"owner"`
	Repo         string `json:"repo"`
}

// RegisterWebhookRequest is the request to register a webhook.
type RegisterWebhookRequest struct {
	Events []string `json:"events,omitempty"`
}

// ----------------------------------------------------------------------------
// View Models
// ----------------------------------------------------------------------------

// ConnectionView is the API response for a GitHub connection.
type ConnectionView struct {
	ID              string     `json:"id"`
	OrgID           string     `json:"organizationId"`
	ConnectionType  string     `json:"connectionType"`
	Name            string     `json:"name"`
	GitHubUsername  *string    `json:"githubUsername,omitempty"`
	GitHubAvatar    *string    `json:"githubAvatar,omitempty"`
	Scopes          []string   `json:"scopes,omitempty"`
	Status          string     `json:"status"`
	LastUsedAt      *time.Time `json:"lastUsedAt,omitempty"`
	LastValidatedAt *time.Time `json:"lastValidatedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

func ToConnectionView(c *GitHubConnection) ConnectionView {
	return ConnectionView{
		ID:              c.ID,
		OrgID:           c.OrgID,
		ConnectionType:  c.ConnectionType,
		Name:            c.Name,
		GitHubUsername:  c.GitHubUsername,
		GitHubAvatar:    c.GitHubAvatar,
		Scopes:          c.Scopes,
		Status:          c.Status,
		LastUsedAt:      c.LastUsedAt,
		LastValidatedAt: c.LastValidatedAt,
		CreatedAt:       c.CreatedAt,
	}
}

// RepositoryView is the API response for a GitHub repository.
type RepositoryView struct {
	ID              string     `json:"id"`
	OrgID           string     `json:"organizationId"`
	ConnectionID    string     `json:"connectionId"`
	GitHubRepoID    int64      `json:"githubRepoId"`
	Owner           string     `json:"owner"`
	Name            string     `json:"name"`
	FullName        string     `json:"fullName"`
	Description     *string    `json:"description,omitempty"`
	HTMLURL         string     `json:"htmlUrl"`
	CloneURL        string     `json:"cloneUrl"`
	SSHURL          *string    `json:"sshUrl,omitempty"`
	DefaultBranch   string     `json:"defaultBranch"`
	IsPrivate       bool       `json:"isPrivate"`
	IsFork          bool       `json:"isFork"`
	IsArchived      bool       `json:"isArchived"`
	StarsCount      int        `json:"starsCount"`
	ForksCount      int        `json:"forksCount"`
	Language        *string    `json:"language,omitempty"`
	Topics          []string   `json:"topics,omitempty"`
	LastSyncedAt    *time.Time `json:"lastSyncedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

func ToRepositoryView(r *GitHubRepository) RepositoryView {
	return RepositoryView{
		ID:            r.ID,
		OrgID:         r.OrgID,
		ConnectionID:  r.ConnectionID,
		GitHubRepoID:  r.GitHubRepoID,
		Owner:         r.Owner,
		Name:          r.Name,
		FullName:      r.FullName,
		Description:   r.Description,
		HTMLURL:       r.HTMLURL,
		CloneURL:      r.CloneURL,
		SSHURL:        r.SSHURL,
		DefaultBranch: r.DefaultBranch,
		IsPrivate:     r.IsPrivate,
		IsFork:        r.IsFork,
		IsArchived:    r.IsArchived,
		StarsCount:    r.StarsCount,
		ForksCount:    r.ForksCount,
		Language:      r.Language,
		Topics:        r.Topics,
		LastSyncedAt:  r.LastSyncedAt,
		CreatedAt:     r.CreatedAt,
	}
}

// WebhookView is the API response for a webhook.
type WebhookView struct {
	ID             string     `json:"id"`
	RepositoryID   string     `json:"repositoryId"`
	GitHubHookID   int64      `json:"githubHookId"`
	Events         []string   `json:"events"`
	WebhookURL     string     `json:"webhookUrl"`
	Status         string     `json:"status"`
	LastDeliveryAt *time.Time `json:"lastDeliveryAt,omitempty"`
	DeliveryCount  int        `json:"deliveryCount"`
	CreatedAt      time.Time  `json:"createdAt"`
}

func ToWebhookView(w *GitHubWebhook) WebhookView {
	return WebhookView{
		ID:             w.ID,
		RepositoryID:   w.RepositoryID,
		GitHubHookID:   w.GitHubHookID,
		Events:         w.Events,
		WebhookURL:     w.WebhookURL,
		Status:         w.Status,
		LastDeliveryAt: w.LastDeliveryAt,
		DeliveryCount:  w.DeliveryCount,
		CreatedAt:      w.CreatedAt,
	}
}

// BranchView is the API response for a branch.
type BranchView struct {
	Name      string `json:"name"`
	SHA       string `json:"sha"`
	Protected bool   `json:"protected"`
}

// CommitView is the API response for a commit.
type CommitView struct {
	SHA       string    `json:"sha"`
	Message   string    `json:"message"`
	Author    string    `json:"author"`
	AuthorURL string    `json:"authorUrl,omitempty"`
	Date      time.Time `json:"date"`
	URL       string    `json:"url"`
}

// OAuthURLResponse is the response containing the OAuth URL.
type OAuthURLResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}
