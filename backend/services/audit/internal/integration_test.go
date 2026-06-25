//go:build integration

// Integration tests exercise the audit service against a real PostgreSQL
// instance (append-only audit_logs + RLS). They are skipped unless a database is
// reachable. Run with:
//
//	DATABASE_URL=postgres://... go test -tags=integration ./services/audit/...
//
// audit_logs is append-only (a trigger blocks DELETE), so these tests use a
// unique random org per run and assert only on their own org's rows; leftover
// rows from prior runs are isolated by row-level security and ignored.
package audit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/migrations"
)

func setupIntegration(t *testing.T) (*Service, *database.DB) {
	t.Helper()
	cfg, err := config.Load("audit")
	if err != nil {
		t.Skipf("config unavailable: %v", err)
	}
	ctx := context.Background()
	db, err := database.Connect(ctx, cfg)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	t.Cleanup(db.Close)

	fsys, err := migrations.Service("audit")
	if err != nil {
		t.Fatalf("migrations.Service(audit): %v", err)
	}
	migs, err := database.LoadMigrations(fsys)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	migrator, err := database.NewMigrator(db, "schema_migrations_audit", migs)
	if err != nil {
		t.Fatalf("new migrator: %v", err)
	}
	if _, err := migrator.Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	svc := NewService(Deps{Store: NewAuditLogStore(db), Tenant: db})
	return svc, db
}

func record(t *testing.T, svc *Service, eventType, orgID, actorID, resourceType, resourceID string) events.Envelope {
	t.Helper()
	e, err := events.New(eventType, 1, orgID, map[string]any{"sample": true},
		events.WithActor(events.Actor{Type: "user", ID: actorID}),
		events.WithResource(events.Resource{Type: resourceType, ID: resourceID}))
	if err != nil {
		t.Fatalf("new event: %v", err)
	}
	if _, err := svc.RecordEvent(context.Background(), e); err != nil {
		t.Fatalf("record %s: %v", eventType, err)
	}
	return e
}

func TestIntegration_RecordAndQuery(t *testing.T) {
	svc, _ := setupIntegration(t)
	ctx := context.Background()
	org := uuid.NewString()

	record(t, svc, "tenant.organization.created", org, "user-1", "organization", org)
	record(t, svc, "tenant.member.invited", org, "user-1", "invitation", uuid.NewString())
	authEvt := record(t, svc, "auth.user.created", org, "user-2", "user", "user-2")

	// List all for the org.
	page, err := svc.ListLogs(ctx, org, AuditFilter{}, database.PageRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("expected 3 records, got %d", len(page.Items))
	}

	// Filter by domain.
	tenantPage, err := svc.ListLogs(ctx, org, AuditFilter{Domain: "tenant"}, database.PageRequest{})
	if err != nil {
		t.Fatalf("list tenant: %v", err)
	}
	if len(tenantPage.Items) != 2 {
		t.Errorf("expected 2 tenant records, got %d", len(tenantPage.Items))
	}

	// Filter by exact event type.
	typePage, err := svc.ListLogs(ctx, org, AuditFilter{EventType: "auth.user.created"}, database.PageRequest{})
	if err != nil {
		t.Fatalf("list by type: %v", err)
	}
	if len(typePage.Items) != 1 || typePage.Items[0].EventID != authEvt.EventID {
		t.Errorf("expected the single auth event, got %+v", typePage.Items)
	}

	// Get by event id.
	got, err := svc.GetLog(ctx, org, authEvt.EventID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.EventType != "auth.user.created" || deref(got.ActorID) != "user-2" {
		t.Errorf("unexpected record: %+v", got)
	}
}

func TestIntegration_IdempotentInsert(t *testing.T) {
	svc, _ := setupIntegration(t)
	ctx := context.Background()
	org := uuid.NewString()

	e, err := events.New("deployment.started", 1, org, map[string]any{"v": 1},
		events.WithResource(events.Resource{Type: "deployment", ID: "d1"}))
	if err != nil {
		t.Fatalf("new event: %v", err)
	}

	first, err := svc.RecordEvent(ctx, e)
	if err != nil || !first {
		t.Fatalf("first insert: inserted=%v err=%v", first, err)
	}
	second, err := svc.RecordEvent(ctx, e)
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if second {
		t.Error("expected redelivered event to be a no-op insert")
	}

	page, err := svc.ListLogs(ctx, org, AuditFilter{}, database.PageRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 {
		t.Errorf("expected exactly one record, got %d", len(page.Items))
	}
}

func TestIntegration_TimeRangeFilterAndPagination(t *testing.T) {
	svc, _ := setupIntegration(t)
	ctx := context.Background()
	org := uuid.NewString()

	for i := 0; i < 5; i++ {
		record(t, svc, "project.created", org, "user-1", "project", uuid.NewString())
	}

	// First page of 2.
	page1, err := svc.ListLogs(ctx, org, AuditFilter{}, database.PageRequest{Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Items) != 2 || page1.NextCursor == "" {
		t.Fatalf("expected a full first page with a cursor, got %d items cursor=%q", len(page1.Items), page1.NextCursor)
	}
	// Second page continues from the cursor.
	page2, err := svc.ListLogs(ctx, org, AuditFilter{}, database.PageRequest{Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("expected 2 items on page 2, got %d", len(page2.Items))
	}
	if page1.Items[0].ID == page2.Items[0].ID {
		t.Error("expected distinct pages")
	}

	// 'to' bound in the past excludes everything.
	past := time.Now().Add(-time.Hour)
	empty, err := svc.ListLogs(ctx, org, AuditFilter{To: &past}, database.PageRequest{})
	if err != nil {
		t.Fatalf("past filter: %v", err)
	}
	if len(empty.Items) != 0 {
		t.Errorf("expected no records before now, got %d", len(empty.Items))
	}
}

func TestIntegration_RLSIsolatesOrgs(t *testing.T) {
	svc, _ := setupIntegration(t)
	ctx := context.Background()
	orgA := uuid.NewString()
	orgB := uuid.NewString()

	record(t, svc, "tenant.organization.created", orgA, "user-a", "organization", orgA)

	// Org B sees none of org A's records.
	page, err := svc.ListLogs(ctx, orgB, AuditFilter{}, database.PageRequest{})
	if err != nil {
		t.Fatalf("list orgB: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("expected RLS to hide other orgs' records, got %d", len(page.Items))
	}
}
