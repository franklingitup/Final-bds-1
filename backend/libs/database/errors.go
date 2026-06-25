package database

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// PostgreSQL SQLSTATE codes handled by MapError.
const (
	sqlStateUniqueViolation     = "23505"
	sqlStateForeignKeyViolation = "23503"
	sqlStateNotNullViolation    = "23502"
	sqlStateCheckViolation      = "23514"
)

// MapError translates low-level pgx/PostgreSQL errors into the platform's typed
// application errors so handlers return consistent codes. Unrecognized errors
// are returned unchanged (callers may wrap them as INTERNAL via libs/errors).
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return apperrors.NotFound("resource not found")
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case sqlStateUniqueViolation:
			return apperrors.Conflict("resource already exists")
		case sqlStateForeignKeyViolation:
			return apperrors.Validation("referenced resource does not exist")
		case sqlStateNotNullViolation:
			return apperrors.Validation("a required field is missing")
		case sqlStateCheckViolation:
			return apperrors.Validation("a value violates a database constraint")
		}
	}
	return err
}

// IsNotFound reports whether err represents a missing row.
func IsNotFound(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	return apperrors.From(err).Code == apperrors.CodeNotFound
}
