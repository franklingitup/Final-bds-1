package auth

import (
	"net/http"

	"github.com/bdsplatform/platform/backend/libs/errors"
)

// Auth-specific error codes and statuses (docs/04-api-spec.md section 1). These
// use explicit HTTP statuses via errors.NewWithStatus so they render with the
// documented codes while keeping the platform's standard error envelope.
var (
	errInvalidCredentials = errors.NewWithStatus("INVALID_CREDENTIALS", http.StatusUnauthorized, "invalid email or password")
	errMFARequired        = errors.NewWithStatus("MFA_REQUIRED", http.StatusUnauthorized, "multi-factor authentication code required")
	errMFAInvalid         = errors.NewWithStatus("MFA_INVALID", http.StatusUnauthorized, "invalid multi-factor authentication code")
	errAccountLocked      = errors.NewWithStatus("ACCOUNT_LOCKED", http.StatusLocked, "account temporarily locked due to failed login attempts")
	errInvalidToken       = errors.NewWithStatus("INVALID_TOKEN", http.StatusUnauthorized, "invalid or expired token")
	errTokenRevoked       = errors.NewWithStatus("TOKEN_REVOKED", http.StatusUnauthorized, "token has been revoked")
	errEmailTaken         = errors.NewWithStatus("EMAIL_TAKEN", http.StatusConflict, "an account with this email already exists")
	errEmailNotVerified   = errors.NewWithStatus("EMAIL_NOT_VERIFIED", http.StatusForbidden, "email address is not verified")
	errNameTaken          = errors.NewWithStatus("NAME_TAKEN", http.StatusConflict, "name already in use")
)
