package tenant

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// TenantRunner runs a function within a tenant-scoped transaction so RLS
// isolates org-owned tables. Because WithTenant opens a transaction, outbox
// enqueues performed inside it commit atomically with the state change.
// *database.DB satisfies it.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// OrganizationStore persists organizations. Create/Update/Delete run inside a
// tenant-scoped transaction (RLS WITH CHECK enforces id = current org).
type OrganizationStore interface {
	Create(ctx context.Context, o *Organization) error
	GetByID(ctx context.Context, id string) (*Organization, error)
	GetBySlug(ctx context.Context, slug string) (*Organization, error)
	ListByUser(ctx context.Context, userID string, page database.PageRequest) (database.Page[Organization], error)
	Update(ctx context.Context, o *Organization) error
	Delete(ctx context.Context, id string) error
}

// MemberStore persists organization memberships within a tenant scope.
type MemberStore interface {
	Create(ctx context.Context, m *Member) error
	GetByUser(ctx context.Context, userID string) (*Member, error)
	List(ctx context.Context, req database.PageRequest) (database.Page[Member], error)
	CountByRole(ctx context.Context, role Role) (int, error)
	UpdateRole(ctx context.Context, userID string, role Role) error
	Delete(ctx context.Context, userID string) error
}

// InvitationStore persists invitations. GetByTokenHash is intentionally
// unscoped: acceptance is a capability-based, cross-tenant flow keyed on the
// token (see migration 0002). All other methods are tenant-scoped.
type InvitationStore interface {
	Create(ctx context.Context, i *Invitation) error
	GetByTokenHash(ctx context.Context, hash string) (*Invitation, error)
	List(ctx context.Context, req database.PageRequest) (database.Page[Invitation], error)
	UpdateStatus(ctx context.Context, id, status string, acceptedBy *string) error
	GetByID(ctx context.Context, id string) (*Invitation, error)
}

// ----------------------------------------------------------------------------
// Postgres implementations
// ----------------------------------------------------------------------------

type orgRepo struct{ db *database.DB }

// NewOrganizationStore returns a Postgres-backed OrganizationStore.
func NewOrganizationStore(db *database.DB) OrganizationStore { return &orgRepo{db: db} }

func (r *orgRepo) Create(ctx context.Context, o *Organization) error {
	const sql = `
INSERT INTO organizations (id, name, slug, plan, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING created_at, updated_at, version`
	row := r.db.Conn(ctx).QueryRow(ctx, sql, o.ID, o.Name, o.Slug, o.Plan, o.Status)
	return database.MapError(row.Scan(&o.CreatedAt, &o.UpdatedAt, &o.Version))
}

func (r *orgRepo) GetByID(ctx context.Context, id string) (*Organization, error) {
	o, err := database.QueryOne[Organization](ctx, r.db.Conn(ctx), "SELECT * FROM organizations WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// GetBySlug returns an organization by slug. This bypasses RLS to allow lookup
// before tenant context is established.
func (r *orgRepo) GetBySlug(ctx context.Context, slug string) (*Organization, error) {
	o, err := database.QueryOne[Organization](ctx, r.db.Pool, "SELECT * FROM organizations WHERE slug = $1", slug)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// ListByUser returns organizations that a user is a member of.
// This query bypasses tenant-scoped RLS by joining through memberships.
func (r *orgRepo) ListByUser(ctx context.Context, userID string, page database.PageRequest) (database.Page[Organization], error) {
	page = page.Normalize()
	cur, err := database.DecodeCursor(page.Cursor)
	if err != nil {
		return database.Page[Organization]{}, err
	}

	var (
		sql  string
		args []any
	)
	if cur.IsZero() {
		sql = `
			SELECT o.* FROM organizations o
			INNER JOIN organization_members m ON o.id = m.org_id
			WHERE m.user_id = $1 AND m.status = 'active'
			ORDER BY o.created_at DESC, o.id DESC
			LIMIT $2`
		args = []any{userID, page.Limit + 1}
	} else {
		sql = `
			SELECT o.* FROM organizations o
			INNER JOIN organization_members m ON o.id = m.org_id
			WHERE m.user_id = $1 AND m.status = 'active'
			AND (o.created_at, o.id) < ($2, $3)
			ORDER BY o.created_at DESC, o.id DESC
			LIMIT $4`
		args = []any{userID, cur.CreatedAt, cur.ID, page.Limit + 1}
	}

	items, err := database.QueryAll[Organization](ctx, r.db.Pool, sql, args...)
	if err != nil {
		return database.Page[Organization]{}, err
	}
	return database.BuildPage(items, page.Limit, func(o Organization) database.Cursor { return o.Cursor() }), nil
}

func (r *orgRepo) Update(ctx context.Context, o *Organization) error {
	const sql = `
UPDATE organizations
SET name = $1, plan = $2, status = $3, version = version + 1, updated_at = now()
WHERE id = $4 AND version = $5`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, o.Name, o.Plan, o.Status, o.ID, o.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	o.Version++
	return nil
}

func (r *orgRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM organizations WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("organization not found")
	}
	return nil
}

type memberRepo struct{ db *database.DB }

// NewMemberStore returns a Postgres-backed MemberStore.
func NewMemberStore(db *database.DB) MemberStore { return &memberRepo{db: db} }

func (r *memberRepo) Create(ctx context.Context, m *Member) error {
	const sql = `
INSERT INTO organization_members (org_id, user_id, role, status, invited_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at, updated_at, version`
	row := r.db.Conn(ctx).QueryRow(ctx, sql, m.OrgID, m.UserID, m.Role, m.Status, m.InvitedBy)
	return database.MapError(row.Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt, &m.Version))
}

func (r *memberRepo) GetByUser(ctx context.Context, userID string) (*Member, error) {
	m, err := database.QueryOne[Member](ctx, r.db.Conn(ctx),
		"SELECT * FROM organization_members WHERE user_id = $1", userID)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *memberRepo) List(ctx context.Context, req database.PageRequest) (database.Page[Member], error) {
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
		sql = "SELECT * FROM organization_members ORDER BY created_at DESC, id DESC LIMIT $1"
		args = []any{req.Limit + 1}
	} else {
		sql = "SELECT * FROM organization_members WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3"
		args = []any{cur.CreatedAt, cur.ID, req.Limit + 1}
	}
	items, err := database.QueryAll[Member](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Member]{}, err
	}
	return database.BuildPage(items, req.Limit, func(m Member) database.Cursor { return m.Cursor() }), nil
}

func (r *memberRepo) CountByRole(ctx context.Context, role Role) (int, error) {
	var n int
	row := r.db.Conn(ctx).QueryRow(ctx,
		"SELECT count(*) FROM organization_members WHERE role = $1", role)
	if err := row.Scan(&n); err != nil {
		return 0, database.MapError(err)
	}
	return n, nil
}

func (r *memberRepo) UpdateRole(ctx context.Context, userID string, role Role) error {
	tag, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE organization_members SET role = $1, version = version + 1, updated_at = now() WHERE user_id = $2",
		role, userID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("member not found")
	}
	return nil
}

func (r *memberRepo) Delete(ctx context.Context, userID string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx,
		"DELETE FROM organization_members WHERE user_id = $1", userID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("member not found")
	}
	return nil
}

type invitationRepo struct{ db *database.DB }

// NewInvitationStore returns a Postgres-backed InvitationStore.
func NewInvitationStore(db *database.DB) InvitationStore { return &invitationRepo{db: db} }

func (r *invitationRepo) Create(ctx context.Context, i *Invitation) error {
	const sql = `
INSERT INTO organization_invitations (org_id, email, role, token_hash, status, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at, updated_at, version`
	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		i.OrgID, i.Email, i.Role, i.TokenHash, i.Status, i.InvitedBy, i.ExpiresAt)
	return database.MapError(row.Scan(&i.ID, &i.CreatedAt, &i.UpdatedAt, &i.Version))
}

// GetByTokenHash reads an invitation by token across tenants (unscoped); the
// token is the capability. Callers must validate status/expiry.
func (r *invitationRepo) GetByTokenHash(ctx context.Context, hash string) (*Invitation, error) {
	i, err := database.QueryOne[Invitation](ctx, r.db.Pool,
		"SELECT * FROM organization_invitations WHERE token_hash = $1", hash)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *invitationRepo) GetByID(ctx context.Context, id string) (*Invitation, error) {
	i, err := database.QueryOne[Invitation](ctx, r.db.Conn(ctx),
		"SELECT * FROM organization_invitations WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *invitationRepo) List(ctx context.Context, req database.PageRequest) (database.Page[Invitation], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Invitation]{}, err
	}
	var (
		sql  string
		args []any
	)
	if cur.IsZero() {
		sql = "SELECT * FROM organization_invitations ORDER BY created_at DESC, id DESC LIMIT $1"
		args = []any{req.Limit + 1}
	} else {
		sql = "SELECT * FROM organization_invitations WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3"
		args = []any{cur.CreatedAt, cur.ID, req.Limit + 1}
	}
	items, err := database.QueryAll[Invitation](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Invitation]{}, err
	}
	return database.BuildPage(items, req.Limit, func(i Invitation) database.Cursor { return i.Cursor() }), nil
}

func (r *invitationRepo) UpdateStatus(ctx context.Context, id, status string, acceptedBy *string) error {
	const sql = `
UPDATE organization_invitations
SET status = $1, accepted_by = $2,
    accepted_at = CASE WHEN $1 = 'accepted' THEN now() ELSE accepted_at END,
    version = version + 1, updated_at = now()
WHERE id = $3`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, acceptedBy, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("invitation not found")
	}
	return nil
}
