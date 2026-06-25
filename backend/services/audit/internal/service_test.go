package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// ----------------------------------------------------------------------------
// Fakes
// ----------------------------------------------------------------------------

// fakeRunner runs the unit of work inline; RLS scoping is exercised by the
// integration tests, not the in-memory fakes.
type fakeRunner struct{ lastOrg string }

func (r *fakeRunner) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	r.lastOrg = orgID
	return fn(ctx)
}

// fakeStore is an in-memory AuditLogStore that mimics the idempotent insert and
// a subset of the filtering performed by the SQL repository.
type fakeStore struct {
	byEvent map[string]AuditLog
	order   []string
}

func newFakeStore() *fakeStore { return &fakeStore{byEvent: map[string]AuditLog{}} }

func (s *fakeStore) Insert(_ context.Context, rec AuditLog) (bool, error) {
	if _, ok := s.byEvent[rec.EventID]; ok {
		return false, nil
	}
	rec.ID = uuid.NewString()
	rec.CreatedAt = time.Now()
	s.byEvent[rec.EventID] = rec
	s.order = append(s.order, rec.EventID)
	return true, nil
}

func (s *fakeStore) GetByEventID(_ context.Context, eventID string) (AuditLog, error) {
	if rec, ok := s.byEvent[eventID]; ok {
		return rec, nil
	}
	return AuditLog{}, apperrors.NotFound("audit log not found")
}

func (s *fakeStore) List(_ context.Context, f AuditFilter, page database.PageRequest) (database.Page[AuditLog], error) {
	page = page.Normalize()
	var items []AuditLog
	for _, id := range s.order {
		rec := s.byEvent[id]
		if f.EventType != "" && rec.EventType != f.EventType {
			continue
		}
		if f.Domain != "" && domainOf(rec.EventType) != f.Domain {
			continue
		}
		if f.ActorID != "" && deref(rec.ActorID) != f.ActorID {
			continue
		}
		if f.ResourceType != "" && deref(rec.ResourceType) != f.ResourceType {
			continue
		}
		items = append(items, rec)
	}
	return database.BuildPage(items, page.Limit, func(a AuditLog) database.Cursor { return a.Cursor() }), nil
}

// ----------------------------------------------------------------------------
// Harness
// ----------------------------------------------------------------------------

type testEnv struct {
	svc    *Service
	store  *fakeStore
	runner *fakeRunner
}

func newTestEnv() *testEnv {
	store := newFakeStore()
	runner := &fakeRunner{}
	svc := NewService(Deps{Store: store, Tenant: runner})
	return &testEnv{svc: svc, store: store, runner: runner}
}

func mustEnvelope(t *testing.T, eventType, orgID, actorID, resourceType, resourceID string) events.Envelope {
	t.Helper()
	e, err := events.New(eventType, 1, orgID, map[string]any{"k": "v"},
		events.WithActor(events.Actor{Type: "user", ID: actorID}),
		events.WithResource(events.Resource{Type: resourceType, ID: resourceID}))
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	return e
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestRecordEvent_SupportedDomain(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()
	org := uuid.NewString()

	env := mustEnvelope(t, "tenant.organization.created", org, "user-1", "organization", org)
	inserted, err := e.svc.RecordEvent(ctx, env)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if !inserted {
		t.Fatal("expected event to be recorded")
	}
	if e.runner.lastOrg != org {
		t.Errorf("expected write scoped to org %s, got %s", org, e.runner.lastOrg)
	}
	rec := e.store.byEvent[env.EventID]
	if rec.EventType != "tenant.organization.created" || deref(rec.ActorID) != "user-1" {
		t.Errorf("unexpected stored record: %+v", rec)
	}
}

func TestRecordEvent_UnsupportedDomainSkipped(t *testing.T) {
	e := newTestEnv()
	env := mustEnvelope(t, "billing.invoice.issued", uuid.NewString(), "user-1", "invoice", "inv-1")
	inserted, err := e.svc.RecordEvent(context.Background(), env)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if inserted {
		t.Error("expected unsupported-domain event to be skipped")
	}
	if len(e.store.byEvent) != 0 {
		t.Error("expected nothing recorded for unsupported domain")
	}
}

func TestRecordEvent_Idempotent(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()
	env := mustEnvelope(t, "auth.user.created", "platform", "user-9", "user", "user-9")

	first, err := e.svc.RecordEvent(ctx, env)
	if err != nil || !first {
		t.Fatalf("first record: inserted=%v err=%v", first, err)
	}
	second, err := e.svc.RecordEvent(ctx, env)
	if err != nil {
		t.Fatalf("second record: %v", err)
	}
	if second {
		t.Error("expected redelivered event to be a no-op")
	}
	if len(e.store.byEvent) != 1 {
		t.Errorf("expected exactly one stored record, got %d", len(e.store.byEvent))
	}
}

func TestRecordEvent_PlatformOrgFromAuthEvents(t *testing.T) {
	e := newTestEnv()
	env := mustEnvelope(t, "auth.user.created", "platform", "user-1", "user", "user-1")
	if _, err := e.svc.RecordEvent(context.Background(), env); err != nil {
		t.Fatalf("record: %v", err)
	}
	if e.runner.lastOrg != "platform" {
		t.Errorf("expected platform-scoped write, got %s", e.runner.lastOrg)
	}
}

func TestListLogs_FiltersByDomain(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()
	org := uuid.NewString()
	for _, et := range []string{"tenant.organization.created", "auth.user.created", "tenant.member.invited"} {
		if _, err := e.svc.RecordEvent(ctx, mustEnvelope(t, et, org, "a", "r", "1")); err != nil {
			t.Fatalf("seed %s: %v", et, err)
		}
	}

	page, err := e.svc.ListLogs(ctx, org, AuditFilter{Domain: "tenant"}, database.PageRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 tenant events, got %d", len(page.Items))
	}
	for _, it := range page.Items {
		if !strings.HasPrefix(it.EventType, "tenant.") {
			t.Errorf("unexpected event type in tenant filter: %s", it.EventType)
		}
	}
}

func TestSupportedDomains(t *testing.T) {
	got := map[string]bool{}
	for _, d := range SupportedDomains() {
		got[d] = true
	}
	for _, want := range []string{"auth", "tenant", "project", "cluster", "deployment", "secret"} {
		if !got[want] {
			t.Errorf("expected %q to be a supported domain", want)
		}
	}
}
