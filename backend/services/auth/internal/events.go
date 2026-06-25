package auth

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// Auth domain event types (canonical names, version 1).
//
// The published NATS subject appends the version, e.g. type "auth.user.created"
// v1 -> subject "<prefix>.auth.user.created.v1" and catalog name
// "auth.user.created.v1". The Auth Service is the single owner of the "auth.*"
// namespace: no other service may publish these subjects.
//
// Names use lowercase dot-separated, past-tense tokens with no underscores, per
// docs/13-event-remediation-plan.md section A.
const (
	// EventUserCreated is emitted when a user account is created.
	EventUserCreated = "auth.user.created"
	// EventEmailVerificationRequested signals that an email-verification token
	// was issued and should be delivered out-of-band. It carries a delivery
	// reference, never the token value.
	EventEmailVerificationRequested = "auth.user.email.verification.requested"
	// EventEmailVerified is emitted when a user verifies their email.
	EventEmailVerified = "auth.user.email.verified"
	// EventPasswordReset covers the password-reset lifecycle. The payload Phase
	// distinguishes "requested" (token issued) from "completed" (password set).
	EventPasswordReset = "auth.user.password.reset"
	// EventLoginSucceeded / EventLoginFailed report authentication outcomes.
	EventLoginSucceeded = "auth.login.succeeded"
	EventLoginFailed    = "auth.login.failed"
	// EventTokenRevoked populates the gateway/session denylist.
	EventTokenRevoked = "auth.token.revoked"
	// EventTokenRotated is emitted when a refresh session is rotated.
	EventTokenRotated = "auth.token.rotated"
	// EventMFASetupStarted / EventMFAEnabled / EventMFADisabled report the MFA
	// lifecycle for a user.
	EventMFASetupStarted = "auth.mfa.setup.started"
	EventMFAEnabled      = "auth.mfa.enabled"
	EventMFADisabled     = "auth.mfa.disabled"
	// EventServiceAccountCreated / EventServiceAccountDeleted report machine
	// identity lifecycle.
	EventServiceAccountCreated = "auth.service.account.created"
	EventServiceAccountDeleted = "auth.service.account.deleted"
	// EventAPITokenCreated / EventAPITokenRevoked report API token lifecycle.
	EventAPITokenCreated = "auth.api.token.created"
	EventAPITokenRevoked = "auth.api.token.revoked"

	// eventVersion is the schema version emitted by this service.
	eventVersion = 1

	// systemOrg scopes identity events that are not tied to a single tenant.
	systemOrg = "platform"
)

// Token delivery purposes carried on delivery-reference events.
const (
	PurposeEmailVerification = "email_verification"
	PurposePasswordResetMsg  = "password_reset"
)

// ----------------------------------------------------------------------------
// Event payloads.
//
// Payloads carry domain facts only. Envelope metadata (eventId, occurredAt,
// correlationId, traceparent, actor, orgId) is NEVER duplicated here, and no
// secret or one-time token value is ever included. See remediation plan C/E.
// ----------------------------------------------------------------------------

type userCreatedPayload struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

// emailVerificationRequestedPayload references a pending delivery rather than
// embedding the verification token. A consumer (Notification Service) exchanges
// deliveryRef for the sealed token via a restricted internal Auth API.
type emailVerificationRequestedPayload struct {
	UserID      string `json:"userId"`
	Email       string `json:"email"`
	Purpose     string `json:"purpose"`
	DeliveryRef string `json:"deliveryRef"`
}

type emailVerifiedPayload struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
}

// passwordResetPayload covers both phases. For "requested" it carries a delivery
// reference (never the reset token); for "completed" it carries only the user.
type passwordResetPayload struct {
	UserID      string `json:"userId"`
	Phase       string `json:"phase"` // requested | completed
	Email       string `json:"email,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	DeliveryRef string `json:"deliveryRef,omitempty"`
}

type loginSucceededPayload struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
}

type loginFailedPayload struct {
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	Attempts int    `json:"attempts"`
}

type tokenRevokedPayload struct {
	UserID    string `json:"userId"`
	SessionID string `json:"sessionId,omitempty"`
}

// tokenRotatedPayload describes a refresh-session rotation: the replaced session
// and its replacement. Only opaque session ids are carried, never token values.
type tokenRotatedPayload struct {
	UserID            string `json:"userId"`
	SessionID         string `json:"sessionId,omitempty"`         // new session
	ReplacedSessionID string `json:"replacedSessionId,omitempty"` // rotated-out session
}

// mfaPayload is the common body for the MFA lifecycle events
// (setup started, enabled, disabled). It carries no TOTP secret.
type mfaPayload struct {
	UserID string `json:"userId"`
}

type serviceAccountCreatedPayload struct {
	ServiceAccountID string `json:"serviceAccountId"`
	Name             string `json:"name"`
	CreatedBy        string `json:"createdBy,omitempty"`
}

type serviceAccountDeletedPayload struct {
	ServiceAccountID string `json:"serviceAccountId"`
}

type apiTokenCreatedPayload struct {
	APITokenID       string `json:"apiTokenId"`
	ServiceAccountID string `json:"serviceAccountId"`
	Name             string `json:"name"`
}

type apiTokenRevokedPayload struct {
	APITokenID string `json:"apiTokenId"`
}

// enqueue builds an envelope and writes it to the transactional outbox within
// the caller's transaction, so the event commits atomically with the state
// change. The relay publishes it to the broker later. Identity events that are
// not tied to a tenant are scoped to the logical systemOrg.
func (s *Service) enqueue(ctx context.Context, eventType, orgID string, payload any, opts ...events.Option) error {
	if orgID == "" {
		orgID = systemOrg
	}
	e, err := events.New(eventType, eventVersion, orgID, payload, opts...)
	if err != nil {
		return err
	}
	return s.outbox.Enqueue(ctx, e)
}
