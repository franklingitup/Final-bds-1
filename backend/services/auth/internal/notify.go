package auth

import "context"

// TokenDelivery is a one-time secret handed to a secure out-of-band delivery
// channel (e.g. transactional email). The plaintext Token MUST NOT be placed on
// the event bus; domain events carry only DeliveryRef. See remediation plan E.
type TokenDelivery struct {
	Purpose     string // email_verification | password_reset
	UserID      string
	Email       string
	Token       string // plaintext; secure channel only
	DeliveryRef string // stable reference also published on the domain event
}

// Notifier delivers one-time tokens over a secure channel. It is intentionally
// separate from the event stream so secrets never transit the broker, DLQ,
// replay archives, or audit sinks. A nil notifier disables delivery (useful
// until the Notification Service is wired).
type Notifier interface {
	DeliverToken(ctx context.Context, d TokenDelivery)
}

// notify hands a one-time token to the secure delivery channel best-effort. The
// durable record is the delivery-reference event already committed via the
// outbox; this hand-off only pushes the secret out of band.
func (s *Service) notify(ctx context.Context, d TokenDelivery) {
	if s.notifier == nil {
		return
	}
	s.notifier.DeliverToken(ctx, d)
}
