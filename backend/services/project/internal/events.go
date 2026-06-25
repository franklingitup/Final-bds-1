package project

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// Project domain event types (canonical names, version 1). The published subject
// appends the version, e.g. "project.created" v1 -> subject
// "<prefix>.project.created.v1" and catalog name "project.created.v1".
// The Project Service is the single owner of the "project.*" namespace.
const (
	EventProjectCreated = "project.created"
	EventProjectUpdated = "project.updated"
	EventProjectDeleted = "project.deleted"
	EventMemberAdded    = "project.member.added"
	EventMemberRemoved  = "project.member.removed"
	EventRoleChanged    = "project.role.changed"

	eventVersion = 1
)

// Event payloads carry domain facts only. Envelope metadata (eventId,
// occurredAt, correlationId, traceparent, actor, orgId) is never duplicated
// here. See docs/13-event-remediation-plan.md sections C and E.

type projectCreatedPayload struct {
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	CreatedBy   string `json:"createdBy,omitempty"`
}

type projectUpdatedPayload struct {
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type projectDeletedPayload struct {
	ProjectID string `json:"projectId"`
	DeletedBy string `json:"deletedBy"`
}

type memberAddedPayload struct {
	ProjectID string `json:"projectId"`
	UserID    string `json:"userId"`
	Role      Role   `json:"role"`
	AddedBy   string `json:"addedBy"`
}

type memberRemovedPayload struct {
	ProjectID string `json:"projectId"`
	UserID    string `json:"userId"`
	RemovedBy string `json:"removedBy"`
}

type roleChangedPayload struct {
	ProjectID string `json:"projectId"`
	UserID    string `json:"userId"`
	OldRole   Role   `json:"oldRole"`
	NewRole   Role   `json:"newRole"`
	ChangedBy string `json:"changedBy"`
}

// enqueue builds an envelope and writes it to the transactional outbox within
// the caller's transaction, so the event commits atomically with the state
// change. The relay publishes it to the broker later.
func (s *Service) enqueue(ctx context.Context, eventType, orgID string, payload any, opts ...events.Option) error {
	e, err := events.New(eventType, eventVersion, orgID, payload, opts...)
	if err != nil {
		return err
	}
	return s.outbox.Enqueue(ctx, e)
}
