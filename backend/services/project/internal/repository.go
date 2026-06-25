package project

import (
	"context"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// TenantRunner runs a function within a tenant-scoped transaction so RLS
// isolates org-owned tables. *database.DB satisfies it.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// ProjectStore persists projects within a tenant scope.
type ProjectStore interface {
	Create(ctx context.Context, p *Project) error
	GetByID(ctx context.Context, id string) (*Project, error)
	GetBySlug(ctx context.Context, slug string) (*Project, error)
	List(ctx context.Context, req database.PageRequest) (database.Page[Project], error)
	Update(ctx context.Context, p *Project) error
	Delete(ctx context.Context, id string) error
}

// MemberStore persists project memberships within a tenant scope.
type MemberStore interface {
	Create(ctx context.Context, m *Member) error
	GetByUser(ctx context.Context, projectID, userID string) (*Member, error)
	List(ctx context.Context, projectID string, req database.PageRequest) (database.Page[Member], error)
	UpdateRole(ctx context.Context, projectID, userID string, role Role) error
	Delete(ctx context.Context, projectID, userID string) error
	ListByUser(ctx context.Context, userID string) ([]Member, error)
}

// ----------------------------------------------------------------------------
// Postgres implementations
// ----------------------------------------------------------------------------

type projectRepo struct{ db *database.DB }

// NewProjectStore returns a Postgres-backed ProjectStore.
func NewProjectStore(db *database.DB) ProjectStore { return &projectRepo{db: db} }

func (r *projectRepo) Create(ctx context.Context, p *Project) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Status == "" {
		p.Status = ProjectStatusActive
	}
	const sql = `
INSERT INTO projects (id, org_id, name, slug, description, status, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING created_at, updated_at, version`
	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		p.ID, p.OrgID, p.Name, p.Slug, p.Description, p.Status, p.CreatedBy)
	return database.MapError(row.Scan(&p.CreatedAt, &p.UpdatedAt, &p.Version))
}

func (r *projectRepo) GetByID(ctx context.Context, id string) (*Project, error) {
	p, err := database.QueryOne[Project](ctx, r.db.Conn(ctx),
		"SELECT * FROM projects WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *projectRepo) GetBySlug(ctx context.Context, slug string) (*Project, error) {
	p, err := database.QueryOne[Project](ctx, r.db.Conn(ctx),
		"SELECT * FROM projects WHERE slug = $1", slug)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *projectRepo) List(ctx context.Context, req database.PageRequest) (database.Page[Project], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Project]{}, err
	}
	var (
		sql  string
		args []any
	)
	if cur.IsZero() {
		sql = "SELECT * FROM projects ORDER BY created_at DESC, id DESC LIMIT $1"
		args = []any{req.Limit + 1}
	} else {
		sql = "SELECT * FROM projects WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3"
		args = []any{cur.CreatedAt, cur.ID, req.Limit + 1}
	}
	items, err := database.QueryAll[Project](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Project]{}, err
	}
	return database.BuildPage(items, req.Limit, func(p Project) database.Cursor { return p.Cursor() }), nil
}

func (r *projectRepo) Update(ctx context.Context, p *Project) error {
	const sql = `
UPDATE projects
SET name = $1, description = $2, status = $3, version = version + 1, updated_at = now()
WHERE id = $4 AND version = $5`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, p.Name, p.Description, p.Status, p.ID, p.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	p.Version++
	return nil
}

func (r *projectRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM projects WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("project not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Project members
// ----------------------------------------------------------------------------

type memberRepo struct{ db *database.DB }

// NewMemberStore returns a Postgres-backed MemberStore.
func NewMemberStore(db *database.DB) MemberStore { return &memberRepo{db: db} }

func (r *memberRepo) Create(ctx context.Context, m *Member) error {
	const sql = `
INSERT INTO project_members (org_id, project_id, user_id, role, added_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at, updated_at, version`
	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		m.OrgID, m.ProjectID, m.UserID, m.Role, m.AddedBy)
	return database.MapError(row.Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt, &m.Version))
}

func (r *memberRepo) GetByUser(ctx context.Context, projectID, userID string) (*Member, error) {
	m, err := database.QueryOne[Member](ctx, r.db.Conn(ctx),
		"SELECT * FROM project_members WHERE project_id = $1 AND user_id = $2",
		projectID, userID)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *memberRepo) List(ctx context.Context, projectID string, req database.PageRequest) (database.Page[Member], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Member]{}, err
	}
	var (
		sql  string
		args []any
	)
	if cur.IsZero() {
		sql = "SELECT * FROM project_members WHERE project_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2"
		args = []any{projectID, req.Limit + 1}
	} else {
		sql = "SELECT * FROM project_members WHERE project_id = $1 AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4"
		args = []any{projectID, cur.CreatedAt, cur.ID, req.Limit + 1}
	}
	items, err := database.QueryAll[Member](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Member]{}, err
	}
	return database.BuildPage(items, req.Limit, func(m Member) database.Cursor { return m.Cursor() }), nil
}

func (r *memberRepo) UpdateRole(ctx context.Context, projectID, userID string, role Role) error {
	tag, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE project_members SET role = $1, version = version + 1, updated_at = now() WHERE project_id = $2 AND user_id = $3",
		role, projectID, userID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("member not found")
	}
	return nil
}

func (r *memberRepo) Delete(ctx context.Context, projectID, userID string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx,
		"DELETE FROM project_members WHERE project_id = $1 AND user_id = $2",
		projectID, userID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("member not found")
	}
	return nil
}

func (r *memberRepo) ListByUser(ctx context.Context, userID string) ([]Member, error) {
	return database.QueryAll[Member](ctx, r.db.Conn(ctx),
		"SELECT * FROM project_members WHERE user_id = $1", userID)
}
