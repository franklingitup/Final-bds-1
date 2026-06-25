package database

import (
	stderrors "errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

func TestMapError_NoRows(t *testing.T) {
	err := MapError(pgx.ErrNoRows)
	if apperrors.From(err).Code != apperrors.CodeNotFound {
		t.Errorf("expected NOT_FOUND, got %v", err)
	}
}

func TestMapError_PgErrors(t *testing.T) {
	cases := map[string]apperrors.Code{
		sqlStateUniqueViolation:     apperrors.CodeConflict,
		sqlStateForeignKeyViolation: apperrors.CodeValidationFailed,
		sqlStateNotNullViolation:    apperrors.CodeValidationFailed,
		sqlStateCheckViolation:      apperrors.CodeValidationFailed,
	}
	for code, want := range cases {
		err := MapError(&pgconn.PgError{Code: code})
		if got := apperrors.From(err).Code; got != want {
			t.Errorf("code %s mapped to %s, want %s", code, got, want)
		}
	}
}

func TestMapError_Passthrough(t *testing.T) {
	sentinel := stderrors.New("boom")
	if got := MapError(sentinel); !stderrors.Is(got, sentinel) {
		t.Errorf("unrecognized error should pass through, got %v", got)
	}
	if MapError(nil) != nil {
		t.Error("nil should map to nil")
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(pgx.ErrNoRows) {
		t.Error("pgx.ErrNoRows should be NotFound")
	}
	if !IsNotFound(apperrors.NotFound("x")) {
		t.Error("apperrors.NotFound should be NotFound")
	}
	if IsNotFound(apperrors.Conflict("x")) {
		t.Error("conflict is not NotFound")
	}
}
