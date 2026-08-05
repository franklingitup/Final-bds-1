package build

import apperrors "github.com/bdsplatform/platform/backend/libs/errors"

var (
	errInvalidToken        = apperrors.Unauthorized("invalid or expired token")
	errOrgRequired         = apperrors.Validation("organization ID required")
	errProjectRequired     = apperrors.Validation("project ID required")
	errRepositoryNotFound  = apperrors.NotFound("repository not found")
	errBuildNotFound       = apperrors.NotFound("build not found")
	errInvalidSource       = apperrors.Validation("either repositoryId or gitUrl is required")
	errInvalidTargetImage  = apperrors.Validation("targetImage is required")
	errInvalidRegistry     = apperrors.Validation("targetRegistry is required")
	errInvalidGitURL       = apperrors.Validation("invalid git URL format")
	errInvalidDockerfile   = apperrors.Validation("dockerfile not found at specified path")
	errBuildNotCancellable = apperrors.Conflict("build cannot be cancelled in current state")
	errBuildNotRetryable   = apperrors.Conflict("build cannot be retried: max retries exceeded or not in failed state")
	errNameRequired        = apperrors.Validation("name is required")
	errURLRequired         = apperrors.Validation("url is required")
	errRepositoryNameTaken = apperrors.Conflict("repository name already taken in project")
)
