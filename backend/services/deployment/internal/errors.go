package deployment

import apperrors "github.com/bdsplatform/platform/backend/libs/errors"

var (
	errInvalidToken      = apperrors.Unauthorized("invalid or expired token")
	errOrgRequired       = apperrors.Validation("organization ID required")
	errProjectRequired   = apperrors.Validation("project ID required")
	errSlugTaken         = apperrors.Conflict("application slug already taken")
	errAppNotFound       = apperrors.NotFound("application not found")
	errDeploymentNotFound = apperrors.NotFound("deployment not found")
	errReleaseNotFound   = apperrors.NotFound("release not found")
	errClusterNotReady   = apperrors.Conflict("cluster is not ready for deployments")
	errNoRollbackTarget  = apperrors.Conflict("no previous successful release to rollback to")
	errInvalidImage      = apperrors.Validation("image is required")
	errInvalidReplicas   = apperrors.Validation("replicas must be at least 1")
	errArgoAppNotFound   = apperrors.NotFound("gitops application not found for deployment")
)
