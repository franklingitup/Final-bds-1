package tenant

import (
	"net/http"

	"github.com/bdsplatform/platform/backend/libs/errors"
)

// Tenant-specific error codes and statuses (see docs/04-api-spec.md section 2).
var (
	errInvalidToken    = errors.NewWithStatus("INVALID_TOKEN", http.StatusUnauthorized, "invalid or expired token")
	errSlugTaken       = errors.NewWithStatus("SLUG_TAKEN", http.StatusConflict, "organization slug is already taken")
	errMemberExists    = errors.NewWithStatus("MEMBER_EXISTS", http.StatusConflict, "user is already a member of this organization")
	errInviteExists    = errors.NewWithStatus("INVITE_PENDING", http.StatusConflict, "a pending invitation already exists for this email")
	errNotMember       = errors.NewWithStatus("NOT_A_MEMBER", http.StatusForbidden, "caller is not a member of this organization")
	errLastOwner       = errors.NewWithStatus("LAST_OWNER", http.StatusConflict, "the last owner cannot be removed or demoted")
	errOwnerOnly       = errors.NewWithStatus("OWNER_ONLY", http.StatusForbidden, "only an owner may grant or modify the owner role")
	errInviteEmail     = errors.NewWithStatus("INVITE_EMAIL_MISMATCH", http.StatusForbidden, "this invitation was issued to a different email address")
	errInviteNotUsable = errors.NewWithStatus("INVITE_NOT_USABLE", http.StatusGone, "invitation is no longer valid")
)
