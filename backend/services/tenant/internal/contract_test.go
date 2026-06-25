package tenant

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bdsplatform/platform/backend/libs/events"
)

type eventContract struct {
	typeName      string
	version       int
	catalogName   string
	samplePayload any
}

// tenantContracts is the authoritative list of events the Tenant Service
// produces, asserted against the framework so names, versions, payload schemas,
// and ownership cannot silently drift from the catalog.
var tenantContracts = []eventContract{
	{EventOrganizationCreated, eventVersion, "tenant.organization.created.v1", orgCreatedPayload{Name: "n", Slug: "s", OwnerID: "u"}},
	{EventOrganizationUpdated, eventVersion, "tenant.organization.updated.v1", orgUpdatedPayload{Name: "n", Plan: "free"}},
	{EventOrganizationDeleted, eventVersion, "tenant.organization.deleted.v1", orgDeletedPayload{DeletedBy: "u"}},
	{EventMemberInvited, eventVersion, "tenant.member.invited.v1", memberInvitedPayload{InvitationID: "i", Email: "e@x.io", Role: RoleDeveloper, InvitedBy: "u", ExpiresAt: time.Now(), DeliveryRef: "i"}},
	{EventMemberRemoved, eventVersion, "tenant.member.removed.v1", memberRemovedPayload{UserID: "u", RemovedBy: "a"}},
	{EventRoleChanged, eventVersion, "tenant.role.changed.v1", roleChangedPayload{UserID: "u", OldRole: RoleViewer, NewRole: RoleDeveloper, ChangedBy: "a"}},
	{EventInvitationAccepted, eventVersion, "tenant.invitation.accepted.v1", invitationAcceptedPayload{InvitationID: "i", UserID: "u", Role: RoleDeveloper}},
	{EventInvitationRevoked, eventVersion, "tenant.invitation.revoked.v1", invitationRevokedPayload{InvitationID: "i", RevokedBy: "a"}},
}

func TestTenantEventNamesAndOwnership(t *testing.T) {
	for _, c := range tenantContracts {
		assertCanonicalName(t, c.typeName)
		assertOwnedBy(t, c.typeName, "tenant")
		if got := events.CatalogName(c.typeName, c.version); got != c.catalogName {
			t.Errorf("%s catalog name = %q, want %q", c.typeName, got, c.catalogName)
		}
	}
}

func TestTenantEventPayloadsAreClean(t *testing.T) {
	for _, c := range tenantContracts {
		e, err := events.New(c.typeName, c.version, "org-1", c.samplePayload)
		if err != nil {
			t.Fatalf("New(%s): %v", c.typeName, err)
		}
		assertPayloadClean(t, c.typeName, e.Payload)
	}
}

// ----------------------------------------------------------------------------
// Contract assertions.
// ----------------------------------------------------------------------------

var metadataKeys = map[string]bool{
	"eventId": true, "eventName": true, "type": true, "version": true,
	"orgId": true, "occurredAt": true, "correlationId": true,
	"traceparent": true, "actor": true, "resource": true,
}

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
