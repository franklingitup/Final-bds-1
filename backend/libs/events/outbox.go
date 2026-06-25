package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// Outbox is the transactional outbox store. Enqueue runs within the caller's
// database transaction (resolved from context) so an event is persisted
// atomically with the state change that produced it; a relay later publishes it
// to the broker. This guarantees no event is lost if the broker is briefly
// unavailable and no event is published for a change that rolled back.
type Outbox interface {
	// Enqueue persists an event for later publication. Call inside the same
	// transaction (database.WithTenant / database.Tx) as the state change.
	Enqueue(ctx context.Context, e Envelope) error
	// FetchUnpublished locks and returns up to limit pending events. Must run
	// inside a transaction; rows are locked FOR UPDATE SKIP LOCKED so concurrent
	// relays do not double-publish.
	FetchUnpublished(ctx context.Context, limit int) ([]OutboxRecord, error)
	// MarkPublished marks the given outbox rows as published.
	MarkPublished(ctx context.Context, ids []string) error
}

// OutboxRecord is a stored, not-yet-published event.
type OutboxRecord struct {
	ID        string
	Envelope  Envelope
	CreatedAt time.Time
}

// PostgresOutbox is a Postgres-backed Outbox using libs/database. It honors the
// ambient transaction via DB.Conn so Enqueue participates in the caller's tx.
type PostgresOutbox struct {
	db    *database.DB
	table string
}

// NewPostgresOutbox builds an outbox over the given table (default "outbox" when
// empty). The table name must be a trusted, static identifier.
func NewPostgresOutbox(db *database.DB, table string) *PostgresOutbox {
	if table == "" {
		table = "outbox"
	}
	return &PostgresOutbox{db: db, table: table}
}

// Enqueue inserts the event into the outbox within the ambient transaction.
func (o *PostgresOutbox) Enqueue(ctx context.Context, e Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("events: marshal outbox envelope: %w", err)
	}
	sql := fmt.Sprintf(`
INSERT INTO %s (id, event_id, event_type, version, org_id, envelope, created_at)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, now())`, o.table)

	_, err = o.db.Conn(ctx).Exec(ctx, sql, e.EventID, e.Type, e.Version, e.OrgID, body)
	return database.MapError(err)
}

// FetchUnpublished returns pending events oldest-first, locking them so a
// concurrent relay skips them.
func (o *PostgresOutbox) FetchUnpublished(ctx context.Context, limit int) ([]OutboxRecord, error) {
	sql := fmt.Sprintf(`
SELECT id, envelope, created_at
FROM %s
WHERE published_at IS NULL
ORDER BY created_at
LIMIT $1
FOR UPDATE SKIP LOCKED`, o.table)

	rows, err := o.db.Conn(ctx).Query(ctx, sql, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	var out []OutboxRecord
	for rows.Next() {
		var (
			id   string
			body []byte
			ts   time.Time
		)
		if err := rows.Scan(&id, &body, &ts); err != nil {
			return nil, database.MapError(err)
		}
		var env Envelope
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("events: decode outbox row %s: %w", id, err)
		}
		out = append(out, OutboxRecord{ID: id, Envelope: env, CreatedAt: ts})
	}
	return out, rows.Err()
}

// MarkPublished stamps published_at on the given rows.
func (o *PostgresOutbox) MarkPublished(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	sql := fmt.Sprintf("UPDATE %s SET published_at = now() WHERE id = ANY($1)", o.table)
	_, err := o.db.Conn(ctx).Exec(ctx, sql, ids)
	return database.MapError(err)
}

var _ Outbox = (*PostgresOutbox)(nil)
