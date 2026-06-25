package tenant

import (
	"context"
	"time"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// Tenant domain event types (canonical names, version 1). The published subject
// appends the version, e.g. "tenant.organization.created" v1 -> subject
// "<prefix>.tenant.organization.created.v1" and catalog name
// "tenant.organization.created.v1". The Tenant Service is the single owner of
// the "tenant.*" namespace.
const (
	EventOrganizationCreated = "tenant.organization.created"
	EventOrganizationUpdated = "tenant.organization.updated"
	EventOrganizationDeleted = "tenant.organization.deleted"
	EventMemberInvited       = "tenant.member.invited"
	EventMemberRemoved       = "tenant.member.removed"
	EventRoleChanged         = "tenant.role.changed"
	EventInvitationAccepted  = "tenant.invitation.accepted"
	EventInvitationRevoked   = "tenant.invitation.revoked"

	eventVersion = 1
)

// Event payloads carry domain facts only. Envelope metadata (eventId,
// occurredAt, correlationId, traceparent, actor, orgId) is never duplicated
// here, and no secret (e.g. the invitation token) is included. See
// docs/13-event-remediation-plan.md sections C and E.

type orgCreatedPayload struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	OwnerID string `json:"ownerId"`
}

type orgUpdatedPayload struct {
	Name string `json:"name"`
	Plan string `json:"plan"`
}

type orgDeletedPayload struct {
	DeletedBy string `json:"deletedBy"`
}

// memberInvitedPayload references a pending delivery rather than embedding the
// invitation token. A consumer (Notification Service) exchanges deliveryRef for
// the sealed token via a restricted internal Tenant API.
type memberInvitedPayload struct {
	InvitationID string    `json:"invitationId"`
	Email        string    `json:"email"`
	Role         Role      `json:"role"`
	InvitedBy    string    `json:"invitedBy"`
	ExpiresAt    time.Time `json:"expiresAt"`
	DeliveryRef  string    `json:"deliveryRef"`
}

type memberRemovedPayload struct {
	UserID    string `json:"userId"`
	RemovedBy string `json:"removedBy"`
}

type roleChangedPayload struct {
	UserID    string `json:"userId"`
	OldRole   Role   `json:"oldRole"`
	NewRole   Role   `json:"newRole"`
	ChangedBy string `json:"changedBy"`
}

// invitationAcceptedPayload records who accepted which invitation and the role
// they were granted. The membership is created in the same transaction.
type invitationAcceptedPayload struct {
	InvitationID string `json:"invitationId"`
	UserID       string `json:"userId"`
	Role         Role   `json:"role"`
}

type invitationRevokedPayload struct {
	InvitationID string `json:"invitationId"`
	RevokedBy    string `json:"revokedBy"`
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
