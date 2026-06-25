package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// QueryOne runs sql and scans a single row into T by column name (lax: struct
// fields without a matching column are left zero). Returns a NOT_FOUND error
// when no row matches.
func QueryOne[T any](ctx context.Context, q Querier, sql string, args ...any) (T, error) {
	var zero T
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return zero, MapError(err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return zero, MapError(err)
	}
	return v, nil
}

// QueryAll runs sql and scans every row into a []T by column name.
func QueryAll[T any](ctx context.Context, q Querier, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, MapError(err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return nil, MapError(err)
	}
	return items, nil
}

// Repository is a generic, table-scoped base providing the CRUD operations
// common to every entity. Concrete repositories embed it and add domain queries
// using the Querier returned by Conn so they share transaction and tenant scope.
//
// T must be a struct using `db` tags (typically embedding Model/TenantModel).
type Repository[T any] struct {
	db    *DB
	table string
}

// NewRepository constructs a Repository for the given table. The table name is
// interpolated into SQL, so callers must pass a trusted, static identifier.
func NewRepository[T any](db *DB, table string) *Repository[T] {
	return &Repository[T]{db: db, table: table}
}

// Table returns the backing table name.
func (r *Repository[T]) Table() string { return r.table }

// Conn returns the active Querier (ambient transaction or pool) for ctx, so
// concrete repositories can issue custom SQL that honors the surrounding
// transaction and tenant scope.
func (r *Repository[T]) Conn(ctx context.Context) Querier { return r.db.Conn(ctx) }

// GetByID fetches a single row by primary key. Tenant rows are additionally
// constrained by RLS when called within WithTenant.
func (r *Repository[T]) GetByID(ctx context.Context, id string) (T, error) {
	sql := fmt.Sprintf("SELECT * FROM %s WHERE id = $1", r.table)
	return QueryOne[T](ctx, r.Conn(ctx), sql, id)
}

// Exists reports whether a row with the given id is visible in the current scope.
func (r *Repository[T]) Exists(ctx context.Context, id string) (bool, error) {
	sql := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE id = $1)", r.table)
	var exists bool
	if err := r.Conn(ctx).QueryRow(ctx, sql, id).Scan(&exists); err != nil {
		return false, MapError(err)
	}
	return exists, nil
}

// Count returns the number of rows visible in the current scope.
func (r *Repository[T]) Count(ctx context.Context) (int64, error) {
	sql := fmt.Sprintf("SELECT count(*) FROM %s", r.table)
	var n int64
	if err := r.Conn(ctx).QueryRow(ctx, sql).Scan(&n); err != nil {
		return 0, MapError(err)
	}
	return n, nil
}

// List returns a cursor-paginated page ordered by (created_at DESC, id DESC),
// the platform's canonical list ordering. key extracts the keyset position from
// an item (typically Model.Cursor).
func (r *Repository[T]) List(ctx context.Context, req PageRequest, key func(T) Cursor) (Page[T], error) {
	req = req.Normalize()
	cur, err := DecodeCursor(req.Cursor)
	if err != nil {
		return Page[T]{}, err
	}

	// Fetch one extra row to determine whether a further page exists.
	fetch := req.Limit + 1

	var (
		sql  string
		args []any
	)
	if cur.IsZero() {
		sql = fmt.Sprintf(
			"SELECT * FROM %s ORDER BY created_at DESC, id DESC LIMIT $1",
			r.table,
		)
		args = []any{fetch}
	} else {
		// Keyset predicate over the composite sort key.
		sql = fmt.Sprintf(
			"SELECT * FROM %s WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3",
			r.table,
		)
		args = []any{cur.CreatedAt, cur.ID, fetch}
	}

	items, err := QueryAll[T](ctx, r.Conn(ctx), sql, args...)
	if err != nil {
		return Page[T]{}, err
	}
	return BuildPage(items, req.Limit, key), nil
}

// DeleteByID removes a row by primary key. Returns NOT_FOUND when no row matched.
func (r *Repository[T]) DeleteByID(ctx context.Context, id string) error {
	sql := fmt.Sprintf("DELETE FROM %s WHERE id = $1", r.table)
	tag, err := r.Conn(ctx).Exec(ctx, sql, id)
	if err != nil {
		return MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return MapError(pgx.ErrNoRows)
	}
	return nil
}

// UpdateVersioned runs an optimistic-locked UPDATE. The provided assignments are
// applied with `WHERE id = $id AND version = $expectedVersion` and the version
// column is bumped automatically. It returns ErrOptimisticLock when no row
// matched (concurrent modification or missing row).
//
// assignments is a SQL fragment of comma-separated `col = $n` clauses; its
// placeholders start at $1 and args supplies their values in order. The id and
// expectedVersion are appended as the final two placeholders.
//
// Example:
//
//	repo.UpdateVersioned(ctx, id, version,
//	    "name = $1, status = $2", name, status)
func (r *Repository[T]) UpdateVersioned(ctx context.Context, id string, expectedVersion int64, assignments string, args ...any) error {
	idPos := len(args) + 1
	verPos := len(args) + 2
	sql := fmt.Sprintf(
		"UPDATE %s SET %s, version = version + 1, updated_at = now() WHERE id = $%d AND version = $%d",
		r.table, assignments, idPos, verPos,
	)
	allArgs := append(append([]any{}, args...), id, expectedVersion)

	tag, err := r.Conn(ctx).Exec(ctx, sql, allArgs...)
	if err != nil {
		return MapError(err)
	}
	return expectAffected(tag)
}
