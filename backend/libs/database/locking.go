package database

import (
	"github.com/jackc/pgx/v5/pgconn"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// ErrOptimisticLock is returned when a versioned write matches no rows, meaning
// the row was modified concurrently (its version advanced) or no longer exists.
// Callers should reload the entity and retry.
var ErrOptimisticLock = apperrors.Conflict("resource was modified concurrently; reload and retry")

// expectAffected returns ErrOptimisticLock when an UPDATE/DELETE guarded by a
// version predicate affected no rows, and ErrNotFound-style behavior is handled
// by the caller's context. It is the building block for optimistic concurrency.
func expectAffected(tag pgconn.CommandTag) error {
	if tag.RowsAffected() == 0 {
		return ErrOptimisticLock
	}
	return nil
}

// IsOptimisticLock reports whether err is an optimistic-lock conflict.
func IsOptimisticLock(err error) bool {
	return err == ErrOptimisticLock //nolint:errorlint // sentinel comparison is intentional
}
