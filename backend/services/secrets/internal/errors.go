package secrets

import apperrors "github.com/bdsplatform/platform/backend/libs/errors"

// Domain errors.
var (
	errSecretNotFound = apperrors.NotFound("secret not found")
	errSecretExists   = apperrors.Conflict("secret with this name already exists")

	// ErrInvalidOrgID is returned when orgID is empty in security-critical operations.
	// This is a programming error - callers must always provide authenticated org ID.
	ErrInvalidOrgID = apperrors.Validation("organization ID is required for security validation")
)
