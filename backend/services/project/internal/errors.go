package project

import apperrors "github.com/bdsplatform/platform/backend/libs/errors"

var (
	errInvalidToken = apperrors.Unauthorized("invalid or expired token")
	errOrgRequired  = apperrors.Validation("organization ID required")
	errSlugTaken    = apperrors.Conflict("project slug already taken")
	errMemberExists = apperrors.Conflict("user is already a member")
	errNotMember    = apperrors.Forbidden("not a project member")
	errLastAdmin    = apperrors.Validation("cannot remove or demote the last admin")
)
