package tenant

import "context"

// InviteDelivery is a one-time invitation secret handed to a secure out-of-band
// delivery channel (e.g. transactional email). The plaintext Token MUST NOT be
// placed on the event bus; the tenant.member.invited event carries only
// DeliveryRef. See docs/13-event-remediation-plan.md section E.
type InviteDelivery struct {
	OrgID        string
	InvitationID string
	Email        string
	Role         Role
	Token        string // plaintext; secure channel only
	DeliveryRef  string // stable reference also published on the domain event
}

// Notifier delivers invitation tokens over a secure channel, separate from the
// event stream so secrets never transit the broker, DLQ, replay archives, or
// audit sinks. A nil notifier disables delivery (useful until the Notification
// Service is wired).
type Notifier interface {
	DeliverInvite(ctx context.Context, d InviteDelivery)
}

// notifyInvite hands an invitation token to the secure delivery channel
// best-effort. The durable record is the tenant.member.invited event already
// committed via the outbox; this hand-off only pushes the secret out of band.
func (s *Service) notifyInvite(ctx context.Context, d InviteDelivery) {
	if s.notifier == nil {
		return
	}
	s.notifier.DeliverInvite(ctx, d)
}
