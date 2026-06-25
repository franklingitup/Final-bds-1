package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/authz"
)

// AuditEntry is an append-only audit record describing a security- or
// compliance-relevant action. It maps to the `audit_logs` table
// (docs/05-database-design.md). audit_logs is insert-only and trigger-protected
// against UPDATE/DELETE.
type AuditEntry struct {
	OrgID        string
	ActorID      string // empty for system-originated actions (stored as NULL)
	Action       string // e.g. "deployment.created"
	ResourceType string // e.g. "deployment"
	ResourceID   string
	Metadata     map[string]any
}

// Auditor records audit entries. When called inside a transaction (via the
// ambient context), the audit row commits atomically with the state change it
// describes, guaranteeing the trail matches persisted data.
type Auditor struct {
	db *DB
}

// NewAuditor constructs an Auditor.
func NewAuditor(db *DB) *Auditor { return &Auditor{db: db} }

// insertAuditSQL writes one immutable row into the event-sourced audit_logs
// table. event_id is unique; ON CONFLICT DO NOTHING makes writes idempotent so a
// retried action does not duplicate the trail. The Action maps to event_type and
// Metadata to payload.
const insertAuditSQL = `
INSERT INTO audit_logs (event_id, event_type, org_id, actor_id, resource_type, resource_id, occurred_at, payload)
VALUES ($1, $2, $3, $4, $5, $6, now(), $7)
ON CONFLICT (event_id) DO NOTHING`

// Record writes an audit entry. OrgID and ActorID fall back to the authenticated
// principal in ctx when unset. The entry is written via the ambient transaction
// if one exists, otherwise directly against the pool.
func (a *Auditor) Record(ctx context.Context, e AuditEntry) error {
	if e.OrgID == "" {
		e.OrgID = authz.OrgFromContext(ctx)
	}
	if e.ActorID == "" {
		if p, ok := authz.PrincipalFromContext(ctx); ok {
			e.ActorID = p.UserID
		}
	}
	if e.OrgID == "" {
		return fmt.Errorf("database: audit entry requires an orgID")
	}
	if e.Action == "" {
		return fmt.Errorf("database: audit entry requires an action")
	}

	meta := e.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("database: marshal audit metadata: %w", err)
	}

	_, err = a.db.Conn(ctx).Exec(ctx, insertAuditSQL,
		uuid.NewString(),
		e.Action,
		e.OrgID,
		nullableString(e.ActorID),
		nullableString(e.ResourceType),
		nullableString(e.ResourceID),
		metaJSON,
	)
	return MapError(err)
}

// nullableString returns nil for empty strings so they persist as SQL NULL,
// keeping nullable UUID/text columns clean.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
