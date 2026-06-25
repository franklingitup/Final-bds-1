package auth

import apperrors "github.com/bdsplatform/platform/backend/libs/errors"

var (
	// ErrInvalidToken is returned when the token is malformed or invalid.
	ErrInvalidToken = apperrors.Unauthorized("invalid or expired token")

	// ErrNoToken is returned when no token is provided.
	ErrNoToken = apperrors.Unauthorized("authentication required")

	// ErrInsufficientScope is returned when the token lacks required scopes.
	ErrInsufficientScope = apperrors.Forbidden("insufficient scope")

	// ErrOrgMismatch is returned when the request org doesn't match token org.
	ErrOrgMismatch = apperrors.Forbidden("organization mismatch")
)
