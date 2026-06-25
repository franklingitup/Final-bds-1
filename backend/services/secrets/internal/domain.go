package secrets

import (
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// Secret represents an encrypted project-scoped secret.
// Plaintext values are NEVER stored or returned.
type Secret struct {
	database.TenantModel
	ProjectID      string     `db:"project_id"`
	Name           string     `db:"name"`
	Description    *string    `db:"description"`
	EncryptedValue []byte     `db:"encrypted_value"`
	ValueHash      string     `db:"value_hash"`
	CreatedBy      *string    `db:"created_by"`
	UpdatedBy      *string    `db:"updated_by"`
	DeletedAt      *time.Time `db:"deleted_at"`
}

// SecretAccessLog records access to secrets for audit purposes.
type SecretAccessLog struct {
	ID          string    `db:"id"`
	OrgID       string    `db:"org_id"`
	SecretID    string    `db:"secret_id"`
	Action      string    `db:"action"`
	PerformedBy *string   `db:"performed_by"`
	PerformedAt time.Time `db:"performed_at"`
	Metadata    []byte    `db:"metadata"`
}

// Access actions.
const (
	ActionCreated  = "created"
	ActionUpdated  = "updated"
	ActionDeleted  = "deleted"
	ActionAccessed = "accessed"
)

// ----------------------------------------------------------------------------
// Request DTOs
// ----------------------------------------------------------------------------

// CreateSecretRequest is the payload for creating a secret.
type CreateSecretRequest struct {
	Name        string  `json:"name"`
	Value       string  `json:"value"`       // Plaintext value (only in request)
	Description *string `json:"description,omitempty"`
}

// UpdateSecretRequest is the payload for updating a secret.
type UpdateSecretRequest struct {
	Value       *string `json:"value,omitempty"`       // New plaintext value (only in request)
	Description *string `json:"description,omitempty"`
}

// ----------------------------------------------------------------------------
// Response DTOs (NEVER include plaintext values)
// ----------------------------------------------------------------------------

// SecretView is the API response model for a secret.
// CRITICAL: This NEVER includes the plaintext value.
type SecretView struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ToView converts a Secret to its API view representation.
// CRITICAL: Plaintext value is never included.
func (s *Secret) ToView() SecretView {
	desc := ""
	if s.Description != nil {
		desc = *s.Description
	}
	return SecretView{
		ID:          s.ID,
		ProjectID:   s.ProjectID,
		Name:        s.Name,
		Description: desc,
		Version:     s.Version,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}
}

// SecretListView is the response for listing secrets.
type SecretListView struct {
	Items      []SecretView `json:"items"`
	Total      int          `json:"total"`
	HasMore    bool         `json:"hasMore"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

// ----------------------------------------------------------------------------
// Agent Sync DTOs (includes decrypted values for agent-only endpoint)
// ----------------------------------------------------------------------------

// AgentSecret is returned by the agent sync endpoint.
// This is the ONLY place where decrypted values are returned,
// and only to authenticated agents over a secure channel.
type AgentSecret struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Value     string `json:"value"` // Decrypted plaintext value
	Version   int64  `json:"version"`
}

// AgentSecretsResponse is the response for the agent secrets endpoint.
type AgentSecretsResponse struct {
	Secrets []AgentSecret `json:"secrets"`
}
