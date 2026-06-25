package audit

import (
	"context"
	"fmt"
	"strings"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// TenantRunner runs a function within a tenant-scoped transaction so RLS applies
// to audit_logs. *database.DB satisfies it.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// AuditLogStore persists and queries immutable audit records. Callers run these
// within a tenant-scoped context (TenantRunner.WithTenant) so row-level security
// isolates the org.
type AuditLogStore interface {
	// Insert records one event. It is idempotent on event_id: a redelivered
	// event is a no-op. Returns whether a new row was written.
	Insert(ctx context.Context, rec AuditLog) (bool, error)
	// List returns a filtered, cursor-paginated page ordered by
	// (created_at DESC, id DESC).
	List(ctx context.Context, f AuditFilter, page database.PageRequest) (database.Page[AuditLog], error)
	// GetByEventID returns the record for a source event id, or NOT_FOUND.
	GetByEventID(ctx context.Context, eventID string) (AuditLog, error)
}

type auditLogRepo struct{ db *database.DB }

// NewAuditLogStore returns a Postgres-backed AuditLogStore.
func NewAuditLogStore(db *database.DB) AuditLogStore { return &auditLogRepo{db: db} }

const insertSQL = `
INSERT INTO audit_logs (event_id, event_type, org_id, actor_id, resource_type, resource_id, occurred_at, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (event_id) DO NOTHING`

func (r *auditLogRepo) Insert(ctx context.Context, rec AuditLog) (bool, error) {
	// The payload column is NOT NULL with a {} default; pgx binds the JSON bytes
	// into the jsonb column directly, so an empty payload becomes an empty object.
	payload := []byte(rec.Payload)
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	tag, err := r.db.Conn(ctx).Exec(ctx, insertSQL,
		rec.EventID,
		rec.EventType,
		rec.OrgID,
		rec.ActorID,
		rec.ResourceType,
		rec.ResourceID,
		rec.OccurredAt,
		payload,
	)
	if err != nil {
		return false, database.MapError(err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *auditLogRepo) GetByEventID(ctx context.Context, eventID string) (AuditLog, error) {
	return database.QueryOne[AuditLog](ctx, r.db.Conn(ctx),
		"SELECT * FROM audit_logs WHERE event_id = $1", eventID)
}

func (r *auditLogRepo) List(ctx context.Context, f AuditFilter, page database.PageRequest) (database.Page[AuditLog], error) {
	page = page.Normalize()
	cur, err := database.DecodeCursor(page.Cursor)
	if err != nil {
		return database.Page[AuditLog]{}, err
	}

	var (
		conds []string
		args  []any
	)
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	if f.EventType != "" {
		add("event_type = $%d", f.EventType)
	}
	if f.Domain != "" {
		add("event_type LIKE $%d", f.Domain+".%")
	}
	if f.ActorID != "" {
		add("actor_id = $%d", f.ActorID)
	}
	if f.ResourceType != "" {
		add("resource_type = $%d", f.ResourceType)
	}
	if f.ResourceID != "" {
		add("resource_id = $%d", f.ResourceID)
	}
	if f.From != nil {
		add("occurred_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("occurred_at <= $%d", *f.To)
	}
	// Keyset predicate over the composite sort key (created_at, id).
	if !cur.IsZero() {
		args = append(args, cur.CreatedAt, cur.ID)
		conds = append(conds, fmt.Sprintf("(created_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, page.Limit+1)
	sql := fmt.Sprintf(
		"SELECT * FROM audit_logs %s ORDER BY created_at DESC, id DESC LIMIT $%d",
		where, len(args),
	)

	items, err := database.QueryAll[AuditLog](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[AuditLog]{}, err
	}
	return database.BuildPage(items, page.Limit, func(a AuditLog) database.Cursor { return a.Cursor() }), nil
}
