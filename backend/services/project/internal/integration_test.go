//go:build integration

// Integration tests exercise the project service against a real PostgreSQL
// instance (RLS + transactional outbox). They are skipped unless a database is
// reachable. Run with:
//
//	DATABASE_URL=postgres://... go test -tags=integration ./services/project/...
package project

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/migrations"
)

func setupIntegration(t *testing.T) (*Service, *database.DB, string) {
	t.Helper()
	cfg, err := config.Load("project")
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
		{"project", "schema_migrations_project"},
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

	// Create an organization for tests.
	orgID := uuid.NewString()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, plan, status)
		VALUES ($1, 'Integration Test Org', $2, 'free', 'active')`,
		orgID, "int-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", orgID)
	})

	svc := NewService(Deps{
		Projects: NewProjectStore(db),
		Members:  NewMemberStore(db),
		Outbox:   events.NewPostgresOutbox(db, "outbox"),
		Tenant:   db,
	})
	return svc, db, orgID
}

func TestIntegration_ProjectLifecycle(t *testing.T) {
	svc, db, orgID := setupIntegration(t)
	ctx := context.Background()
	creator := uuid.NewString()

	// Create project.
	p, err := svc.CreateProject(ctx, orgID, creator, CreateProjectRequest{
		Name: "Integration Project",
		Slug: "int-" + uuid.NewString()[:8],
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithTenant(ctx, orgID, func(ctx context.Context) error {
			_, _ = db.Conn(ctx).Exec(ctx, "DELETE FROM project_members WHERE project_id = $1", p.ID)
			_, err := db.Conn(ctx).Exec(ctx, "DELETE FROM projects WHERE id = $1", p.ID)
			return err
		})
	})

	// Creator membership persisted under RLS scope.
	assertOutboxHas(t, db, orgID, EventProjectCreated)

	// Get project.
	got, err := svc.GetProject(ctx, orgID, p.ID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if got.Name != "Integration Project" {
		t.Errorf("name = %s", got.Name)
	}

	// List projects.
	page, err := svc.ListProjects(ctx, orgID, database.PageRequest{Limit: 10})
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	found := false
	for _, prj := range page.Items {
		if prj.ID == p.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected project in list")
	}

	// Update project.
	newName := "Updated Project"
	updated, err := svc.UpdateProject(ctx, orgID, creator, p.ID, UpdateProjectRequest{Name: &newName})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Updated Project" {
		t.Errorf("name = %s", updated.Name)
	}
	assertOutboxHas(t, db, orgID, EventProjectUpdated)
}

func TestIntegration_MembershipLifecycle(t *testing.T) {
	svc, db, orgID := setupIntegration(t)
	ctx := context.Background()
	creator := uuid.NewString()

	p, err := svc.CreateProject(ctx, orgID, creator, CreateProjectRequest{
		Name: "Membership Test",
		Slug: "mem-" + uuid.NewString()[:8],
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithTenant(ctx, orgID, func(ctx context.Context) error {
			_, _ = db.Conn(ctx).Exec(ctx, "DELETE FROM project_members WHERE project_id = $1", p.ID)
			_, err := db.Conn(ctx).Exec(ctx, "DELETE FROM projects WHERE id = $1", p.ID)
			return err
		})
	})

	// Add member.
	newUser := uuid.NewString()
	m, err := svc.AddMember(ctx, orgID, creator, p.ID, AddMemberRequest{
		UserID: newUser,
		Role:   RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	if m.Role != RoleDeveloper {
		t.Errorf("role = %s", m.Role)
	}
	assertOutboxHas(t, db, orgID, EventMemberAdded)

	// List members.
	members, err := svc.ListMembers(ctx, orgID, p.ID, database.PageRequest{Limit: 10})
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members.Items) != 2 { // creator + new user
		t.Errorf("expected 2 members, got %d", len(members.Items))
	}

	// Change role.
	changed, err := svc.ChangeRole(ctx, orgID, creator, p.ID, newUser, RoleViewer)
	if err != nil {
		t.Fatalf("change role: %v", err)
	}
	if changed.Role != RoleViewer {
		t.Errorf("role = %s", changed.Role)
	}
	assertOutboxHas(t, db, orgID, EventRoleChanged)

	// Remove member.
	if err := svc.RemoveMember(ctx, orgID, creator, p.ID, newUser); err != nil {
		t.Fatalf("remove: %v", err)
	}
	assertOutboxHas(t, db, orgID, EventMemberRemoved)
}

func TestIntegration_DeleteProject(t *testing.T) {
	svc, db, orgID := setupIntegration(t)
	ctx := context.Background()
	creator := uuid.NewString()

	p, err := svc.CreateProject(ctx, orgID, creator, CreateProjectRequest{
		Name: "Delete Test",
		Slug: "del-" + uuid.NewString()[:8],
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	if err := svc.DeleteProject(ctx, orgID, creator, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	assertOutboxHas(t, db, orgID, EventProjectDeleted)

	// Project should not be retrievable.
	if _, err := svc.GetProject(ctx, orgID, p.ID); !database.IsNotFound(err) {
		t.Errorf("expected not found after delete, got %v", err)
	}
}

func TestIntegration_CrossTenantIsolation(t *testing.T) {
	svc, db, orgA := setupIntegration(t)
	ctx := context.Background()

	// Create another org.
	orgB := uuid.NewString()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, plan, status)
		VALUES ($1, 'Org B', $2, 'free', 'active')`,
		orgB, "orgb-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create org B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", orgB)
	})

	// Create a project in org A.
	creator := uuid.NewString()
	p, err := svc.CreateProject(ctx, orgA, creator, CreateProjectRequest{
		Name: "Org A Project",
		Slug: "orga-" + uuid.NewString()[:8],
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithTenant(ctx, orgA, func(ctx context.Context) error {
			_, _ = db.Conn(ctx).Exec(ctx, "DELETE FROM project_members WHERE project_id = $1", p.ID)
			_, err := db.Conn(ctx).Exec(ctx, "DELETE FROM projects WHERE id = $1", p.ID)
			return err
		})
	})

	// Trying to access project from org B should not find it.
	_, err = svc.GetProject(ctx, orgB, p.ID)
	if !database.IsNotFound(err) {
		t.Errorf("expected not found for cross-tenant access, got %v", err)
	}
}

func TestIntegration_LastAdminProtection(t *testing.T) {
	svc, db, orgID := setupIntegration(t)
	ctx := context.Background()
	creator := uuid.NewString()

	p, err := svc.CreateProject(ctx, orgID, creator, CreateProjectRequest{
		Name: "Last Admin Test",
		Slug: "adm-" + uuid.NewString()[:8],
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithTenant(ctx, orgID, func(ctx context.Context) error {
			_, _ = db.Conn(ctx).Exec(ctx, "DELETE FROM project_members WHERE project_id = $1", p.ID)
			_, err := db.Conn(ctx).Exec(ctx, "DELETE FROM projects WHERE id = $1", p.ID)
			return err
		})
	})

	// Cannot demote the last admin.
	if _, err := svc.ChangeRole(ctx, orgID, creator, p.ID, creator, RoleDeveloper); err != errLastAdmin {
		t.Errorf("expected errLastAdmin, got %v", err)
	}

	// Cannot remove the last admin.
	if err := svc.RemoveMember(ctx, orgID, creator, p.ID, creator); err != errLastAdmin {
		t.Errorf("expected errLastAdmin, got %v", err)
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
