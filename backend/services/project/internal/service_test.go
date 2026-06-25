package project

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// ----------------------------------------------------------------------------
// Fakes
// ----------------------------------------------------------------------------

type orgCtxKey struct{}

func withOrg(ctx context.Context, org string) context.Context {
	return context.WithValue(ctx, orgCtxKey{}, org)
}

func orgOf(ctx context.Context) string {
	v, _ := ctx.Value(orgCtxKey{}).(string)
	return v
}

// fakeRunner mimics WithTenant by stashing the org in context.
type fakeRunner struct{}

func (fakeRunner) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	return fn(withOrg(ctx, orgID))
}

type fakeOutbox struct{ records []events.Envelope }

func (o *fakeOutbox) Enqueue(_ context.Context, e events.Envelope) error {
	o.records = append(o.records, e)
	return nil
}
func (o *fakeOutbox) FetchUnpublished(context.Context, int) ([]events.OutboxRecord, error) {
	return nil, nil
}
func (o *fakeOutbox) MarkPublished(context.Context, []string) error { return nil }

func (o *fakeOutbox) find(eventType string) (events.Envelope, bool) {
	for _, e := range o.records {
		if e.Type == eventType {
			return e, true
		}
	}
	return events.Envelope{}, false
}

type fakeProjectStore struct {
	byID  map[string]*Project
	slugs map[string]bool // org/slug
}

func newFakeProjectStore() *fakeProjectStore {
	return &fakeProjectStore{byID: map[string]*Project{}, slugs: map[string]bool{}}
}

func slugKey(org, slug string) string { return org + "/" + slug }

func (s *fakeProjectStore) Create(ctx context.Context, p *Project) error {
	org := orgOf(ctx)
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	k := slugKey(org, p.Slug)
	if s.slugs[k] {
		return apperrors.Conflict("slug taken")
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	p.Version = 1
	s.byID[p.ID] = p
	s.slugs[k] = true
	return nil
}

func (s *fakeProjectStore) GetByID(ctx context.Context, id string) (*Project, error) {
	p, ok := s.byID[id]
	if !ok || p.OrgID != orgOf(ctx) {
		return nil, apperrors.NotFound("project not found")
	}
	return p, nil
}

func (s *fakeProjectStore) GetBySlug(ctx context.Context, slug string) (*Project, error) {
	org := orgOf(ctx)
	for _, p := range s.byID {
		if p.Slug == slug && p.OrgID == org {
			return p, nil
		}
	}
	return nil, apperrors.NotFound("project not found")
}

func (s *fakeProjectStore) List(ctx context.Context, req database.PageRequest) (database.Page[Project], error) {
	req = req.Normalize()
	org := orgOf(ctx)
	var items []Project
	for _, p := range s.byID {
		if p.OrgID == org {
			items = append(items, *p)
		}
	}
	return database.BuildPage(items, req.Limit, func(p Project) database.Cursor { return p.Cursor() }), nil
}

func (s *fakeProjectStore) Update(ctx context.Context, p *Project) error {
	if _, ok := s.byID[p.ID]; !ok {
		return apperrors.NotFound("project not found")
	}
	p.Version++
	s.byID[p.ID] = p
	return nil
}

func (s *fakeProjectStore) Delete(ctx context.Context, id string) error {
	p, ok := s.byID[id]
	if !ok || p.OrgID != orgOf(ctx) {
		return apperrors.NotFound("project not found")
	}
	delete(s.slugs, slugKey(p.OrgID, p.Slug))
	delete(s.byID, id)
	return nil
}

type fakeMemberStore struct {
	byKey map[string]*Member // project/user
}

func newFakeMemberStore() *fakeMemberStore {
	return &fakeMemberStore{byKey: map[string]*Member{}}
}

func mkey(project, user string) string { return project + "/" + user }

func (s *fakeMemberStore) Create(ctx context.Context, m *Member) error {
	k := mkey(m.ProjectID, m.UserID)
	if _, ok := s.byKey[k]; ok {
		return apperrors.Conflict("member exists")
	}
	m.ID = uuid.NewString()
	m.CreatedAt = time.Now()
	m.UpdatedAt = m.CreatedAt
	m.Version = 1
	s.byKey[k] = m
	return nil
}

func (s *fakeMemberStore) GetByUser(_ context.Context, projectID, userID string) (*Member, error) {
	if m, ok := s.byKey[mkey(projectID, userID)]; ok {
		return m, nil
	}
	return nil, apperrors.NotFound("member not found")
}

func (s *fakeMemberStore) List(_ context.Context, projectID string, req database.PageRequest) (database.Page[Member], error) {
	req = req.Normalize()
	var items []Member
	for _, m := range s.byKey {
		if m.ProjectID == projectID {
			items = append(items, *m)
		}
	}
	return database.BuildPage(items, req.Limit, func(m Member) database.Cursor { return m.Cursor() }), nil
}

func (s *fakeMemberStore) UpdateRole(_ context.Context, projectID, userID string, role Role) error {
	m, ok := s.byKey[mkey(projectID, userID)]
	if !ok {
		return apperrors.NotFound("member not found")
	}
	m.Role = role
	return nil
}

func (s *fakeMemberStore) Delete(_ context.Context, projectID, userID string) error {
	k := mkey(projectID, userID)
	if _, ok := s.byKey[k]; !ok {
		return apperrors.NotFound("member not found")
	}
	delete(s.byKey, k)
	return nil
}

func (s *fakeMemberStore) ListByUser(_ context.Context, userID string) ([]Member, error) {
	var out []Member
	for _, m := range s.byKey {
		if m.UserID == userID {
			out = append(out, *m)
		}
	}
	return out, nil
}

// ----------------------------------------------------------------------------
// Harness
// ----------------------------------------------------------------------------

type testEnv struct {
	svc      *Service
	projects *fakeProjectStore
	members  *fakeMemberStore
	outbox   *fakeOutbox
	now      time.Time
}

func newTestEnv() *testEnv {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	projects := newFakeProjectStore()
	members := newFakeMemberStore()
	outbox := &fakeOutbox{}
	svc := NewService(Deps{
		Projects:   projects,
		Members:    members,
		Outbox:     outbox,
		Tenant:     fakeRunner{},
		Authorizer: authz.NewPolicyAuthorizer(),
		Now:        func() time.Time { return now },
	})
	return &testEnv{svc: svc, projects: projects, members: members, outbox: outbox, now: now}
}

const testOrgID = "org-1"

// addMember directly seeds a membership for setup.
func (e *testEnv) addMember(projectID, userID string, role Role) {
	m := &Member{ProjectID: projectID, UserID: userID, Role: role}
	m.OrgID = testOrgID
	m.ID = uuid.NewString()
	m.CreatedAt = e.now
	e.members.byKey[mkey(projectID, userID)] = m
}

func mustCreateProject(t *testing.T, e *testEnv, userID string) *Project {
	t.Helper()
	p, err := e.svc.CreateProject(context.Background(), testOrgID, userID, CreateProjectRequest{
		Name: "My Project",
		Slug: "my-project",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return p
}

func isForbidden(err error) bool { return apperrors.From(err).Code == apperrors.CodeForbidden }

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestCreateProject(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "user-1")

	if p.ID == "" || p.Slug != "my-project" || p.Status != ProjectStatusActive {
		t.Fatalf("unexpected project %+v", p)
	}
	// Creator becomes admin.
	m, err := e.members.GetByUser(context.Background(), p.ID, "user-1")
	if err != nil || m.Role != RoleAdmin {
		t.Fatalf("expected admin membership, got %+v err=%v", m, err)
	}
	if _, ok := e.outbox.find(EventProjectCreated); !ok {
		t.Error("expected project.created event in outbox")
	}
}

func TestCreateProject_DuplicateSlug(t *testing.T) {
	e := newTestEnv()
	mustCreateProject(t, e, "user-1")
	_, err := e.svc.CreateProject(context.Background(), testOrgID, "user-2", CreateProjectRequest{
		Name: "Another", Slug: "my-project",
	})
	if err != errSlugTaken {
		t.Errorf("expected errSlugTaken, got %v", err)
	}
}

func TestCreateProject_InvalidSlug(t *testing.T) {
	e := newTestEnv()
	_, err := e.svc.CreateProject(context.Background(), testOrgID, "user-1", CreateProjectRequest{
		Name: "Test", Slug: "Bad Slug!",
	})
	if err == nil {
		t.Error("expected validation error for bad slug")
	}
}

func TestGetProject(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "user-1")

	got, err := e.svc.GetProject(context.Background(), testOrgID, p.ID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("got %s, want %s", got.ID, p.ID)
	}
}

func TestListProjects(t *testing.T) {
	e := newTestEnv()
	mustCreateProject(t, e, "user-1")
	// Change slug for second project.
	_, err := e.svc.CreateProject(context.Background(), testOrgID, "user-1", CreateProjectRequest{
		Name: "Second", Slug: "second",
	})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	page, err := e.svc.ListProjects(context.Background(), testOrgID, database.PageRequest{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(page.Items))
	}
}

func TestUpdateProject(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "admin-1")
	e.addMember(p.ID, "dev-1", RoleDeveloper)
	e.addMember(p.ID, "viewer-1", RoleViewer)

	// Viewer cannot update.
	if _, err := e.svc.UpdateProject(context.Background(), testOrgID, "viewer-1", p.ID, UpdateProjectRequest{}); !isForbidden(err) {
		t.Errorf("expected forbidden for viewer, got %v", err)
	}
	// Developer cannot update (project:manage required).
	if _, err := e.svc.UpdateProject(context.Background(), testOrgID, "dev-1", p.ID, UpdateProjectRequest{}); !isForbidden(err) {
		t.Errorf("expected forbidden for developer, got %v", err)
	}

	newName := "Updated"
	updated, err := e.svc.UpdateProject(context.Background(), testOrgID, "admin-1", p.ID, UpdateProjectRequest{Name: &newName})
	if err != nil {
		t.Fatalf("admin update: %v", err)
	}
	if updated.Name != "Updated" {
		t.Errorf("name = %s", updated.Name)
	}
	if _, ok := e.outbox.find(EventProjectUpdated); !ok {
		t.Error("expected project.updated event")
	}
}

func TestDeleteProject(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "admin-1")
	e.addMember(p.ID, "dev-1", RoleDeveloper)

	// Developer cannot delete.
	if err := e.svc.DeleteProject(context.Background(), testOrgID, "dev-1", p.ID); !isForbidden(err) {
		t.Errorf("expected forbidden for developer, got %v", err)
	}

	if err := e.svc.DeleteProject(context.Background(), testOrgID, "admin-1", p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := e.projects.byID[p.ID]; ok {
		t.Error("expected project to be deleted")
	}
	if _, ok := e.outbox.find(EventProjectDeleted); !ok {
		t.Error("expected project.deleted event")
	}
}

func TestAddMember(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "admin-1")
	e.addMember(p.ID, "dev-1", RoleDeveloper)

	// Developer cannot add members.
	if _, err := e.svc.AddMember(context.Background(), testOrgID, "dev-1", p.ID, AddMemberRequest{
		UserID: "new-1", Role: RoleViewer,
	}); !isForbidden(err) {
		t.Errorf("expected forbidden for developer, got %v", err)
	}

	m, err := e.svc.AddMember(context.Background(), testOrgID, "admin-1", p.ID, AddMemberRequest{
		UserID: "new-1", Role: RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	if m.Role != RoleDeveloper {
		t.Errorf("role = %s", m.Role)
	}
	if _, ok := e.outbox.find(EventMemberAdded); !ok {
		t.Error("expected member.added event")
	}
}

func TestAddMember_Duplicate(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "admin-1")

	_, err := e.svc.AddMember(context.Background(), testOrgID, "admin-1", p.ID, AddMemberRequest{
		UserID: "admin-1", Role: RoleDeveloper,
	})
	if err != errMemberExists {
		t.Errorf("expected errMemberExists, got %v", err)
	}
}

func TestRemoveMember(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "admin-1")
	e.addMember(p.ID, "admin-2", RoleAdmin)
	e.addMember(p.ID, "dev-1", RoleDeveloper)

	// Developer cannot remove.
	if err := e.svc.RemoveMember(context.Background(), testOrgID, "dev-1", p.ID, "admin-2"); !isForbidden(err) {
		t.Errorf("expected forbidden for developer, got %v", err)
	}

	// Admin removes developer.
	if err := e.svc.RemoveMember(context.Background(), testOrgID, "admin-1", p.ID, "dev-1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := e.outbox.find(EventMemberRemoved); !ok {
		t.Error("expected member.removed event")
	}

	// Cannot remove the last admin.
	if err := e.svc.RemoveMember(context.Background(), testOrgID, "admin-1", p.ID, "admin-2"); err != nil {
		t.Fatalf("remove admin-2: %v", err)
	}
	if err := e.svc.RemoveMember(context.Background(), testOrgID, "admin-1", p.ID, "admin-1"); err != errLastAdmin {
		t.Errorf("expected errLastAdmin, got %v", err)
	}
}

func TestChangeRole(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "admin-1")
	e.addMember(p.ID, "admin-2", RoleAdmin)
	e.addMember(p.ID, "dev-1", RoleDeveloper)

	// Developer cannot change roles.
	if _, err := e.svc.ChangeRole(context.Background(), testOrgID, "dev-1", p.ID, "admin-2", RoleViewer); !isForbidden(err) {
		t.Errorf("expected forbidden for developer, got %v", err)
	}

	// Admin promotes developer to admin.
	m, err := e.svc.ChangeRole(context.Background(), testOrgID, "admin-1", p.ID, "dev-1", RoleAdmin)
	if err != nil {
		t.Fatalf("change role: %v", err)
	}
	if m.Role != RoleAdmin {
		t.Errorf("role = %s", m.Role)
	}
	if _, ok := e.outbox.find(EventRoleChanged); !ok {
		t.Error("expected role.changed event")
	}
}

func TestChangeRole_LastAdmin(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "admin-1")

	// Cannot demote the last admin.
	if _, err := e.svc.ChangeRole(context.Background(), testOrgID, "admin-1", p.ID, "admin-1", RoleDeveloper); err != errLastAdmin {
		t.Errorf("expected errLastAdmin, got %v", err)
	}
}

func TestChangeRole_Idempotent(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "admin-1")
	e.addMember(p.ID, "dev-1", RoleDeveloper)

	before := len(e.outbox.records)
	m, err := e.svc.ChangeRole(context.Background(), testOrgID, "admin-1", p.ID, "dev-1", RoleDeveloper)
	if err != nil {
		t.Fatalf("change role: %v", err)
	}
	if m.Role != RoleDeveloper {
		t.Errorf("role = %s", m.Role)
	}
	// No new event for idempotent change.
	if len(e.outbox.records) != before {
		t.Errorf("expected no new event for same role, got %d new", len(e.outbox.records)-before)
	}
}

func TestListMembers(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "admin-1")
	e.addMember(p.ID, "dev-1", RoleDeveloper)
	e.addMember(p.ID, "viewer-1", RoleViewer)

	page, err := e.svc.ListMembers(context.Background(), testOrgID, p.ID, database.PageRequest{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// admin-1 + dev-1 + viewer-1
	if len(page.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(page.Items))
	}
}

func TestNotMember(t *testing.T) {
	e := newTestEnv()
	p := mustCreateProject(t, e, "admin-1")

	// Non-member cannot update.
	if _, err := e.svc.UpdateProject(context.Background(), testOrgID, "stranger", p.ID, UpdateProjectRequest{}); err != errNotMember {
		t.Errorf("expected errNotMember, got %v", err)
	}
}
