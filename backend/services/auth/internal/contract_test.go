package auth

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// eventContract describes the canonical contract for a single produced event.
type eventContract struct {
	typeName     string
	version      int
	catalogName  string
	samplePayload any
}

// authContracts is the authoritative list of events the Auth Service produces.
// It is asserted against the catalog and the framework so names, versions,
// payload schemas, and ownership cannot silently drift.
var authContracts = []eventContract{
	{EventUserCreated, eventVersion, "auth.user.created.v1", userCreatedPayload{UserID: "u", Email: "e@x.io", Name: "N"}},
	{EventEmailVerificationRequested, eventVersion, "auth.user.email.verification.requested.v1", emailVerificationRequestedPayload{UserID: "u", Email: "e@x.io", Purpose: PurposeEmailVerification, DeliveryRef: "d"}},
	{EventEmailVerified, eventVersion, "auth.user.email.verified.v1", emailVerifiedPayload{UserID: "u", Email: "e@x.io"}},
	{EventPasswordReset, eventVersion, "auth.user.password.reset.v1", passwordResetPayload{UserID: "u", Phase: "requested", DeliveryRef: "d"}},
	{EventLoginSucceeded, eventVersion, "auth.login.succeeded.v1", loginSucceededPayload{UserID: "u", Email: "e@x.io"}},
	{EventLoginFailed, eventVersion, "auth.login.failed.v1", loginFailedPayload{UserID: "u", Email: "e@x.io", Attempts: 1}},
	{EventTokenRevoked, eventVersion, "auth.token.revoked.v1", tokenRevokedPayload{UserID: "u", SessionID: "s"}},
	{EventTokenRotated, eventVersion, "auth.token.rotated.v1", tokenRotatedPayload{UserID: "u", SessionID: "s2", ReplacedSessionID: "s1"}},
	{EventMFASetupStarted, eventVersion, "auth.mfa.setup.started.v1", mfaPayload{UserID: "u"}},
	{EventMFAEnabled, eventVersion, "auth.mfa.enabled.v1", mfaPayload{UserID: "u"}},
	{EventMFADisabled, eventVersion, "auth.mfa.disabled.v1", mfaPayload{UserID: "u"}},
	{EventServiceAccountCreated, eventVersion, "auth.service.account.created.v1", serviceAccountCreatedPayload{ServiceAccountID: "sa", Name: "n", CreatedBy: "u"}},
	{EventServiceAccountDeleted, eventVersion, "auth.service.account.deleted.v1", serviceAccountDeletedPayload{ServiceAccountID: "sa"}},
	{EventAPITokenCreated, eventVersion, "auth.api.token.created.v1", apiTokenCreatedPayload{APITokenID: "t", ServiceAccountID: "sa", Name: "n"}},
	{EventAPITokenRevoked, eventVersion, "auth.api.token.revoked.v1", apiTokenRevokedPayload{APITokenID: "t"}},
}

func TestAuthEventNamesAndOwnership(t *testing.T) {
	for _, c := range authContracts {
		assertCanonicalName(t, c.typeName)
		assertOwnedBy(t, c.typeName, "auth")
		if got := events.CatalogName(c.typeName, c.version); got != c.catalogName {
			t.Errorf("%s catalog name = %q, want %q", c.typeName, got, c.catalogName)
		}
	}
}

func TestAuthEventPayloadsAreClean(t *testing.T) {
	for _, c := range authContracts {
		e, err := events.New(c.typeName, c.version, systemOrg, c.samplePayload)
		if err != nil {
			t.Fatalf("New(%s): %v", c.typeName, err)
		}
		assertPayloadClean(t, c.typeName, e.Payload)
	}
}

// ----------------------------------------------------------------------------
// Shared contract assertions (also used by other services' contract tests).
// ----------------------------------------------------------------------------

// metadataKeys are envelope fields that must never be duplicated in a payload.
var metadataKeys = map[string]bool{
	"eventId": true, "eventName": true, "type": true, "version": true,
	"orgId": true, "occurredAt": true, "correlationId": true,
	"traceparent": true, "actor": true, "resource": true,
}

// secretKeys are field names that must never carry a secret value on the bus.
var secretKeys = map[string]bool{
	"token": true, "secret": true, "password": true, "passwordhash": true,
	"verificationtoken": true, "resettoken": true, "invitationtoken": true,
	"apisecret": true, "refreshtoken": true, "accesstoken": true, "mfasecret": true,
}

func assertCanonicalName(t *testing.T, typeName string) {
	t.Helper()
	if typeName != strings.ToLower(typeName) {
		t.Errorf("event %q must be lowercase", typeName)
	}
	if strings.Contains(typeName, "_") {
		t.Errorf("event %q must not contain underscores", typeName)
	}
	if strings.Contains(typeName, " ") {
		t.Errorf("event %q must not contain spaces", typeName)
	}
	for _, tok := range strings.Split(typeName, ".") {
		if tok == "" {
			t.Errorf("event %q has an empty token", typeName)
		}
	}
}

func assertOwnedBy(t *testing.T, typeName, domain string) {
	t.Helper()
	if !strings.HasPrefix(typeName, domain+".") {
		t.Errorf("event %q is not owned by domain %q", typeName, domain)
	}
}

func assertPayloadClean(t *testing.T, typeName string, raw json.RawMessage) {
	t.Helper()
	if len(raw) == 0 {
		return
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s payload not an object: %v", typeName, err)
	}
	for k := range m {
		if metadataKeys[k] {
			t.Errorf("%s payload duplicates envelope metadata key %q", typeName, k)
		}
		if secretKeys[strings.ToLower(k)] {
			t.Errorf("%s payload exposes a secret key %q", typeName, k)
		}
	}
}
