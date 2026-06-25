package project

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bdsplatform/platform/backend/libs/events"
)

type eventContract struct {
	typeName      string
	version       int
	catalogName   string
	samplePayload any
}

// projectContracts is the authoritative list of events the Project Service
// produces, asserted against the framework so names, versions, payload schemas,
// and ownership cannot silently drift from the catalog.
var projectContracts = []eventContract{
	{EventProjectCreated, eventVersion, "project.created.v1", projectCreatedPayload{ProjectID: "p", Name: "n", Slug: "s", Description: "d", CreatedBy: "u"}},
	{EventProjectUpdated, eventVersion, "project.updated.v1", projectUpdatedPayload{ProjectID: "p", Name: "n", Description: "d"}},
	{EventProjectDeleted, eventVersion, "project.deleted.v1", projectDeletedPayload{ProjectID: "p", DeletedBy: "u"}},
	{EventMemberAdded, eventVersion, "project.member.added.v1", memberAddedPayload{ProjectID: "p", UserID: "u", Role: RoleDeveloper, AddedBy: "a"}},
	{EventMemberRemoved, eventVersion, "project.member.removed.v1", memberRemovedPayload{ProjectID: "p", UserID: "u", RemovedBy: "a"}},
	{EventRoleChanged, eventVersion, "project.role.changed.v1", roleChangedPayload{ProjectID: "p", UserID: "u", OldRole: RoleViewer, NewRole: RoleDeveloper, ChangedBy: "a"}},
}

func TestProjectEventNamesAndOwnership(t *testing.T) {
	for _, c := range projectContracts {
		assertCanonicalName(t, c.typeName)
		assertOwnedBy(t, c.typeName, "project")
		if got := events.CatalogName(c.typeName, c.version); got != c.catalogName {
			t.Errorf("%s catalog name = %q, want %q", c.typeName, got, c.catalogName)
		}
	}
}

func TestProjectEventPayloadsAreClean(t *testing.T) {
	for _, c := range projectContracts {
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
