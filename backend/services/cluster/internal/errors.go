package cluster

import apperrors "github.com/bdsplatform/platform/backend/libs/errors"

var (
	// 401 token failures share one client-facing message so callers cannot
	// distinguish unknown vs expired vs revoked tokens. Keep distinct variables
	// so internal code can still use errors.Is against each sentinel.
	errInvalidToken = apperrors.Unauthorized("invalid or expired token")
	errOrgRequired  = apperrors.Validation("organization ID required")
	errSlugTaken    = apperrors.Conflict("cluster slug already taken")
	errTokenExpired = apperrors.Unauthorized("invalid or expired token")
	// 409: deliberate idempotent-recovery signal — do not unify with 401 messages.
	errTokenUsed         = apperrors.Conflict("registration token already used")
	errTokenRevoked      = apperrors.Unauthorized("invalid or expired token")
	errClusterNotPending = apperrors.Conflict("cluster is not in pending status")
	errAgentMismatch     = apperrors.Forbidden("agent ID mismatch")
	errClusterNotFound   = apperrors.NotFound("cluster not found")
)
