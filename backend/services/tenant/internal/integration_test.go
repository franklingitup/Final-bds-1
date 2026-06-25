//go:build integration

// Integration tests exercise the tenant service against a real PostgreSQL
// instance (RLS + transactional outbox). They are skipped unless a database is
// reachable. Run with:
//
//	DATABASE_URL=postgres://... go test -tags=integration ./services/tenant/...
package tenant

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/migrations"
)

func setupIntegration(t *testing.T) (*Service, *database.DB, *fakeNotifier) {
	t.Helper()
	cfg, err := config.Load("tenant")
	if err != nil {
		t.Skipf("config unavailable: %v", err)
	}
	ctx := context.Background()
	db, err := database.Connect(ctx, cfg)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	t.Cleanup(db.Close)

	for _, m := range []struct{ service, table string }{
		{"tenant", "schema_migrations_tenant"},
		{"outbox", "schema_migrations_outbox"},
	} {
		fsys, err := migrations.Service(m.service)
		if err != nil {
			t.Fatalf("migrations.Service(%s): %v", m.service, err)
		}
		migs, err := database.LoadMigrations(fsys)
		if err != nil {
			t.Fatalf("load migrations: %v", err)
		}
		migrator, err := database.NewMigrator(db, m.table, migs)
		if err != nil {
			t.Fatalf("new migrator: %v", err)
		}
		if _, err := migrator.Up(ctx); err != nil {
			t.Fatalf("migrate up: %v", err)
		}
	}

	notifier := &fakeNotifier{}
	svc := NewService(Deps{
		Orgs:        NewOrganizationStore(db),
		Members:     NewMemberStore(db),
		Invitations: NewInvitationStore(db),
		Outbox:      events.NewPostgresOutbox(db, "outbox"),
		Tenant:      db,
		Notifier:    notifier,
	})
	return svc, db, notifier
}

func TestIntegration_OrganizationLifecycle(t *testing.T) {
	svc, db, notifier := setupIntegration(t)
	ctx := context.Background()
	owner := uuid.NewString()

	org, err := svc.CreateOrganization(ctx, owner, CreateOrganizationRequest{
		Name: "Integration Org",
		Slug: "int-" + uuid.NewString()[:8],
	})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithTenant(ctx, org.ID, func(ctx context.Context) error {
			_, err := db.Conn(ctx).Exec(ctx, "DELETE FROM organizations WHERE id = $1", org.ID)
			return err
		})
	})

	// Owner membership persisted under RLS scope.
	if _, err := svc.GetOrganization(ctx, owner, org.ID); err != nil {
		t.Fatalf("get org as owner: %v", err)
	}

	// created event landed in the outbox transactionally.
	assertOutboxHas(t, db, org.ID, EventOrganizationCreated)

	// Invite + accept.
	inviteEmail := "integration+" + uuid.NewString()[:8] + "@example.com"
	if _, err := svc.InviteMember(ctx, owner, org.ID, InviteMemberRequest{Email: inviteEmail, Role: RoleDeveloper}); err != nil {
		t.Fatalf("invite: %v", err)
	}
	// The invited event must NOT carry the token; the token is delivered only
	// over the secure channel captured by the fake notifier.
	assertOutboxHas(t, db, org.ID, EventMemberInvited)
	assertInviteEventHasNoToken(t, db, org.ID)
	token := notifier.lastToken()
	if token == "" {
		t.Fatal("expected invite token delivered out-of-band")
	}

	invitee := uuid.NewString()
	member, err := svc.AcceptInvite(ctx, Identity{UserID: invitee, Email: inviteEmail}, token)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if member.Role != RoleDeveloper {
		t.Fatalf("role = %s", member.Role)
	}
	assertOutboxHas(t, db, org.ID, EventInvitationAccepted)

	// Change role then remove.
	if _, err := svc.ChangeRole(ctx, owner, org.ID, invitee, RoleViewer); err != nil {
		t.Fatalf("change role: %v", err)
	}
	assertOutboxHas(t, db, org.ID, EventRoleChanged)

	if err := svc.RemoveMember(ctx, owner, org.ID, invitee); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	assertOutboxHas(t, db, org.ID, EventMemberRemoved)
}

func TestIntegration_InvitationRevokeAndOrgDelete(t *testing.T) {
	svc, db, _ := setupIntegration(t)
	ctx := context.Background()
	owner := uuid.NewString()

	org, err := svc.CreateOrganization(ctx, owner, CreateOrganizationRequest{
		Name: "Revoke Org",
		Slug: "rev-" + uuid.NewString()[:8],
	})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		_ = db.WithTenant(ctx, org.ID, func(ctx context.Context) error {
			_, err := db.Conn(ctx).Exec(ctx, "DELETE FROM organizations WHERE id = $1", org.ID)
			return err
		})
	})

	// Invite then revoke before acceptance.
	inviteEmail := "revoke+" + uuid.NewString()[:8] + "@example.com"
	inv, err := svc.InviteMember(ctx, owner, org.ID, InviteMemberRequest{Email: inviteEmail, Role: RoleDeveloper})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if err := svc.RevokeInvitation(ctx, owner, org.ID, inv.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	assertOutboxHas(t, db, org.ID, EventInvitationRevoked)

	// Idempotent revoke must not enqueue a second event.
	before := outboxCount(t, db, org.ID, EventInvitationRevoked)
	if err := svc.RevokeInvitation(ctx, owner, org.ID, inv.ID); err != nil {
		t.Fatalf("idempotent revoke: %v", err)
	}
	if after := outboxCount(t, db, org.ID, EventInvitationRevoked); after != before {
		t.Errorf("expected no new revoked event, before=%d after=%d", before, after)
	}

	// Delete the organization (owner only).
	if err := svc.DeleteOrganization(ctx, owner, org.ID); err != nil {
		t.Fatalf("delete org: %v", err)
	}
	deleted = true
	assertOutboxHas(t, db, org.ID, EventOrganizationDeleted)
}

func TestIntegration_CrossTenantIsolation(t *testing.T) {
	svc, _, _ := setupIntegration(t)
	ctx := context.Background()

	ownerA := uuid.NewString()
	orgA, err := svc.CreateOrganization(ctx, ownerA, CreateOrganizationRequest{Name: "A", Slug: "a-" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatalf("create orgA: %v", err)
	}

	// A user with no membership in orgA cannot read it.
	if _, err := svc.GetOrganization(ctx, uuid.NewString(), orgA.ID); err != errNotMember {
		t.Errorf("expected errNotMember for non-member, got %v", err)
	}
}

func assertOutboxHas(t *testing.T, db *database.DB, orgID, eventType string) {
	t.Helper()
	var n int
	err := db.Pool.QueryRow(context.Background(),
		"SELECT count(*) FROM outbox WHERE org_id = $1 AND event_type = $2", orgID, eventType).Scan(&n)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if n == 0 {
		t.Errorf("expected an outbox row for %s", eventType)
	}
}

func outboxCount(t *testing.T, db *database.DB, orgID, eventType string) int {
	t.Helper()
	var n int
	err := db.Pool.QueryRow(context.Background(),
		"SELECT count(*) FROM outbox WHERE org_id = $1 AND event_type = $2", orgID, eventType).Scan(&n)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	return n
}

// assertInviteEventHasNoToken verifies the persisted invited event payload
// references a delivery, not a plaintext token (remediation plan section E).
func assertInviteEventHasNoToken(t *testing.T, db *database.DB, orgID string) {
	t.Helper()
	var body []byte
	err := db.Pool.QueryRow(context.Background(),
		"SELECT envelope FROM outbox WHERE org_id = $1 AND event_type = $2 ORDER BY created_at DESC LIMIT 1",
		orgID, EventMemberInvited).Scan(&body)
	if err != nil {
		t.Fatalf("query invite event: %v", err)
	}
	var env events.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	payload, err := events.DecodePayload[memberInvitedPayload](env)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.DeliveryRef == "" || payload.InvitationID == "" {
		t.Errorf("expected invitationId and deliveryRef in payload, got %+v", payload)
	}
	// The raw envelope JSON must not contain a "token" field.
	if bytesContainsToken(body) {
		t.Errorf("invited event payload must not contain a token: %s", body)
	}
}

func bytesContainsToken(b []byte) bool {
	return jsonHasKey(b, "token")
}

func jsonHasKey(b []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	if _, ok := m[key]; ok {
		return true
	}
	if p, ok := m["payload"]; ok {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(p, &inner); err == nil {
			if _, ok := inner[key]; ok {
				return true
			}
		}
	}
	return false
}
