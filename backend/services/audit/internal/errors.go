package audit

import (
	"net/http"

	"github.com/bdsplatform/platform/backend/libs/errors"
)

// Audit-specific error codes (see docs/04-api-spec.md section 2).
var (
	errInvalidToken = errors.NewWithStatus("INVALID_TOKEN", http.StatusUnauthorized, "invalid or expired token")
	errOrgRequired  = errors.NewWithStatus("ORG_REQUIRED", http.StatusBadRequest, "organization id is required")
)
