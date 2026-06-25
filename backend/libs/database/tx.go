package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// maxTxAttempts bounds automatic retries of transactions that fail with a
// transient serialization or deadlock error.
const maxTxAttempts = 3

// setTenantSQL sets the per-transaction RLS scope. The third argument (true)
// makes the setting transaction-local so it is reset on commit/rollback.
const setTenantSQL = "SELECT set_config('app.current_org_id', $1, true)"

// TxOptions configures a transaction.
type TxOptions struct {
	IsoLevel   pgx.TxIsoLevel
	AccessMode pgx.TxAccessMode
	// OrgID, when set, configures app.current_org_id so RLS isolates the tenant.
	OrgID string
}

// TxFunc is the unit of work executed within a transaction. The provided ctx
// carries the active transaction; pass it to repositories so their queries run
// inside the transaction.
type TxFunc func(ctx context.Context) error

// Tx runs fn inside a transaction. If ctx already carries a transaction, fn runs
// in a nested savepoint so callers compose safely without double-committing.
// Transient serialization/deadlock failures are retried on a fresh transaction.
func (db *DB) Tx(ctx context.Context, fn TxFunc) error {
	return db.TxWithOptions(ctx, TxOptions{}, fn)
}

// WithTenant runs fn in a transaction scoped to orgID so row-level security
// isolates the tenant. An empty orgID is rejected to prevent accidental
// cross-tenant access.
func (db *DB) WithTenant(ctx context.Context, orgID string, fn TxFunc) error {
	if orgID == "" {
		return fmt.Errorf("database: tenant scope (orgID) is required")
	}
	return db.TxWithOptions(ctx, TxOptions{OrgID: orgID}, fn)
}

// TxWithOptions runs fn within a transaction configured by opts.
func (db *DB) TxWithOptions(ctx context.Context, opts TxOptions, fn TxFunc) error {
	// Nested transaction: reuse the ambient transaction via a savepoint. No
	// retry here because retrying a savepoint without replaying the parent is
	// unsafe; the outermost Tx owns retry semantics.
	if parent, ok := TxFromContext(ctx); ok {
		return runInSavepoint(ctx, parent, opts, fn)
	}

	var lastErr error
	for attempt := 1; attempt <= maxTxAttempts; attempt++ {
		err := db.runTx(ctx, opts, fn)
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("database: transaction failed after %d attempts: %w", maxTxAttempts, lastErr)
}

func (db *DB) runTx(ctx context.Context, opts TxOptions, fn TxFunc) error {
	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   opts.IsoLevel,
		AccessMode: opts.AccessMode,
	})
	if err != nil {
		return fmt.Errorf("database: begin tx: %w", err)
	}
	// Rollback is a no-op once the tx has committed, so deferring is safe.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := applyTenant(ctx, tx, opts.OrgID); err != nil {
		return err
	}
	if err := fn(withTx(ctx, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database: commit: %w", err)
	}
	return nil
}

func runInSavepoint(ctx context.Context, parent pgx.Tx, opts TxOptions, fn TxFunc) error {
	sp, err := parent.Begin(ctx) // pgx implements nested Begin via SAVEPOINT.
	if err != nil {
		return fmt.Errorf("database: begin savepoint: %w", err)
	}
	defer func() { _ = sp.Rollback(ctx) }()

	if err := applyTenant(ctx, sp, opts.OrgID); err != nil {
		return err
	}
	if err := fn(withTx(ctx, sp)); err != nil {
		return err
	}
	return sp.Commit(ctx)
}

func applyTenant(ctx context.Context, tx pgx.Tx, orgID string) error {
	if orgID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, setTenantSQL, orgID); err != nil {
		return fmt.Errorf("database: set tenant scope: %w", err)
	}
	return nil
}

// isRetryable reports whether err is a transient transaction failure that is
// safe to retry: serialization_failure (40001) or deadlock_detected (40P01).
func isRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	return false
}
