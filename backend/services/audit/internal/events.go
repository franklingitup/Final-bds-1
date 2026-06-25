package audit

import "github.com/bdsplatform/platform/backend/libs/events"

// ptr returns a pointer to s, or nil when s is empty, so optional envelope
// fields persist as SQL NULL rather than empty strings.
func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// recordFromEnvelope projects a consumed event envelope onto an audit record.
// Envelope metadata (actor, resource, org, occurrence time) is lifted into
// dedicated columns; the event body is stored verbatim as the payload.
func recordFromEnvelope(e events.Envelope) AuditLog {
	return AuditLog{
		EventID:      e.EventID,
		EventType:    e.Type,
		OrgID:        e.OrgID,
		ActorID:      ptr(e.Actor.ID),
		ResourceType: ptr(e.Resource.Type),
		ResourceID:   ptr(e.Resource.ID),
		OccurredAt:   e.OccurredAt,
		Payload:      e.Payload,
	}
}
