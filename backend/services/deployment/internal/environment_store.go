package deployment

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// ----------------------------------------------------------------------------
// EnvironmentStore
// ----------------------------------------------------------------------------

// EnvironmentStore persists named deployment environments.
type EnvironmentStore interface {
	Create(ctx context.Context, e *Environment) error
	GetByID(ctx context.Context, id string) (*Environment, error)
	List(ctx context.Context, appID string) ([]Environment, error)
	Update(ctx context.Context, e *Environment) error
	Delete(ctx context.Context, id string) error
}

type environmentRepo struct{ db *database.DB }

// NewEnvironmentStore returns a Postgres-backed EnvironmentStore.
func NewEnvironmentStore(db *database.DB) EnvironmentStore { return &environmentRepo{db: db} }

func (r *environmentRepo) Create(ctx context.Context, e *Environment) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}

	const sql = `
INSERT INTO environments (id, org_id, application_id, name, namespace, cluster_id, order_index, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		e.ID, e.OrgID, e.ApplicationID, e.Name, e.Namespace, e.ClusterID, e.OrderIndex, e.CreatedBy)
	return database.MapError(row.Scan(&e.CreatedAt, &e.UpdatedAt))
}

func (r *environmentRepo) GetByID(ctx context.Context, id string) (*Environment, error) {
	e, err := database.QueryOne[Environment](ctx, r.db.Conn(ctx),
		"SELECT * FROM environments WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *environmentRepo) List(ctx context.Context, appID string) ([]Environment, error) {
	items, err := database.QueryAll[Environment](ctx, r.db.Conn(ctx),
		"SELECT * FROM environments WHERE application_id = $1 ORDER BY order_index ASC, created_at ASC", appID)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (r *environmentRepo) Update(ctx context.Context, e *Environment) error {
	const sql = `
UPDATE environments
SET name = $1, namespace = $2, order_index = $3, updated_at = now()
WHERE id = $4`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, e.Name, e.Namespace, e.OrderIndex, e.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("environment not found")
	}
	e.UpdatedAt = time.Now()
	return nil
}

func (r *environmentRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM environments WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("environment not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// PromotionStore
// ----------------------------------------------------------------------------

// PromotionStore persists promotion requests.
type PromotionStore interface {
	Create(ctx context.Context, p *PromotionRequest) error
	GetByID(ctx context.Context, id string) (*PromotionRequest, error)
	ListByApplication(ctx context.Context, appID string) ([]PromotionRequest, error)
	Update(ctx context.Context, p *PromotionRequest) error
	// UpdateIfPending atomically updates p only when the stored status is still
	// 'pending'. Returns (true, nil) on success, (false, nil) when another writer
	// has already transitioned the promotion away from pending (CAS miss).
	// Callers must treat a CAS miss as a conflict error and abort their transaction.
	UpdateIfPending(ctx context.Context, p *PromotionRequest) (bool, error)
}

type promotionRepo struct{ db *database.DB }

// NewPromotionStore returns a Postgres-backed PromotionStore.
func NewPromotionStore(db *database.DB) PromotionStore { return &promotionRepo{db: db} }

func (r *promotionRepo) Create(ctx context.Context, p *PromotionRequest) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Status == "" {
		p.Status = PromotionStatusPending
	}

	const sql = `
INSERT INTO promotion_requests
    (id, org_id, application_id, release_id, from_environment_id, to_environment_id, requested_by, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		p.ID, p.OrgID, p.ApplicationID, p.ReleaseID,
		p.FromEnvironmentID, p.ToEnvironmentID, p.RequestedBy, p.Status)
	return database.MapError(row.Scan(&p.CreatedAt, &p.UpdatedAt))
}

func (r *promotionRepo) GetByID(ctx context.Context, id string) (*PromotionRequest, error) {
	p, err := database.QueryOne[PromotionRequest](ctx, r.db.Conn(ctx),
		"SELECT * FROM promotion_requests WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *promotionRepo) ListByApplication(ctx context.Context, appID string) ([]PromotionRequest, error) {
	items, err := database.QueryAll[PromotionRequest](ctx, r.db.Conn(ctx),
		"SELECT * FROM promotion_requests WHERE application_id = $1 ORDER BY created_at DESC", appID)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (r *promotionRepo) Update(ctx context.Context, p *PromotionRequest) error {
	const sql = `
UPDATE promotion_requests
SET status = $1, approved_by = $2, approved_at = $3, applied_at = $4,
    rejection_reason = $5, updated_at = now()
WHERE id = $6`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		p.Status, p.ApprovedBy, p.ApprovedAt, p.AppliedAt, p.RejectionReason, p.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("promotion request not found")
	}
	return nil
}

// UpdateIfPending atomically transitions a promotion only when its current
// DB status is 'pending'. The extra WHERE status='pending' predicate acts as a
// compare-and-swap: if two concurrent approvals race, exactly one will update
// a row (RowsAffected=1) and the other will find zero rows (RowsAffected=0),
// returning applied=false so the caller can abort its transaction.
func (r *promotionRepo) UpdateIfPending(ctx context.Context, p *PromotionRequest) (bool, error) {
	const sql = `
UPDATE promotion_requests
SET status = $1, approved_by = $2, approved_at = $3, applied_at = $4,
    rejection_reason = $5, updated_at = now()
WHERE id = $6 AND status = 'pending'`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		p.Status, p.ApprovedBy, p.ApprovedAt, p.AppliedAt, p.RejectionReason, p.ID)
	if err != nil {
		return false, database.MapError(err)
	}
	return tag.RowsAffected() == 1, nil
}
