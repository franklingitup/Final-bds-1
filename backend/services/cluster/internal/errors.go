package cluster

import apperrors "github.com/bdsplatform/platform/backend/libs/errors"

var (
	errInvalidToken     = apperrors.Unauthorized("invalid or expired token")
	errOrgRequired      = apperrors.Validation("organization ID required")
	errSlugTaken        = apperrors.Conflict("cluster slug already taken")
	errTokenExpired     = apperrors.Unauthorized("registration token expired")
	errTokenUsed        = apperrors.Conflict("registration token already used")
	errTokenRevoked     = apperrors.Unauthorized("registration token revoked")
	errClusterNotPending = apperrors.Conflict("cluster is not in pending status")
	errAgentMismatch    = apperrors.Forbidden("agent ID mismatch")
	errClusterNotFound  = apperrors.NotFound("cluster not found")
)
