package secrets

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// Event types for the Secrets domain.
// NOTE (SEC-CRIT-05): Version is specified in NewEnvelope, NOT in the type string.
// Using "secret.created" + version:1 produces "evt.secret.created.v1"
// Previously used "secret.created.v1" which produced "evt.secret.created.v1.v1"
const (
	EventSecretCreated = "secret.created"
	EventSecretUpdated = "secret.updated"
	EventSecretDeleted = "secret.deleted"
)

// Event payloads.
// CRITICAL: Never include plaintext secret values in events.

type secretCreatedPayload struct {
	SecretID  string `json:"secretId"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Version   int64  `json:"version"`
}

type secretUpdatedPayload struct {
	SecretID  string `json:"secretId"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Version   int64  `json:"version"`
}

type secretDeletedPayload struct {
	SecretID  string `json:"secretId"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	DeletedBy string `json:"deletedBy"`
}

// Event version for all secrets events.
const eventVersion = 1

// enqueue enqueues an event through the transactional outbox.
// Events are published atomically with the database transaction.
func (s *Service) enqueue(ctx context.Context, eventType, orgID string, payload any, opts ...events.Option) error {
	env, err := events.New(eventType, eventVersion, orgID, payload, opts...)
	if err != nil {
		return err
	}
	return s.outbox.Enqueue(ctx, env)
}
