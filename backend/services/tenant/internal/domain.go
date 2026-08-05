package tenant

import (
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
)

// Role is an organization membership role. Owner is the superuser of an org;
// Admin manages members and resources; Developer has write access to workloads;
// Viewer is read-only.
type Role string

const (
	RoleOwner     Role = "owner"
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleDeveloper, RoleViewer:
		return true
	default:
		return false
	}
}

// AssignableByInvite reports whether the role may be granted via an invitation.
// Owner is excluded: ownership is established at org creation and changed only
// by an explicit role change performed by an existing owner.
func (r Role) AssignableByInvite() bool {
	switch r {
	case RoleAdmin, RoleDeveloper, RoleViewer:
		return true
	default:
		return false
	}
}

// toAuthzRole maps a membership role to the platform RBAC org role used by
// libs/authz. Developer/Viewer have no org-level write capability and map to the
// read-oriented Member/Auditor roles; project-level write is granted separately
// via project roles.
func (r Role) toAuthzRole() authz.OrgRole {
	switch r {
	case RoleOwner:
		return authz.OrgOwner
	case RoleAdmin:
		return authz.OrgAdmin
	case RoleDeveloper:
		return authz.RoleMember
	default:
		return authz.OrgAuditor
	}
}

// Membership statuses.
const (
	MemberStatusActive    = "active"
	MemberStatusSuspended = "suspended"
)

// Invitation statuses.
const (
	InviteStatusPending  = "pending"
	InviteStatusAccepted = "accepted"
	InviteStatusRevoked  = "revoked"
	InviteStatusExpired  = "expired"
)

// Organization statuses.
const (
	OrgStatusActive   = "active"
	OrgStatusDisabled = "disabled"
)

// Organization is the tenant root.
type Organization struct {
	database.Model
	Name   string `db:"name"`
	Slug   string `db:"slug"`
	Plan   string `db:"plan"`
	Status string `db:"status"`
}

// Member is a user's membership in an organization.
type Member struct {
	database.TenantModel
	UserID    string  `db:"user_id"`
	Role      Role    `db:"role"`
	Status    string  `db:"status"`
	InvitedBy *string `db:"invited_by"`
}

// Invitation is a pending offer to join an organization with a given role.
type Invitation struct {
	database.TenantModel
	Email      string     `db:"email"`
	Role       Role       `db:"role"`
	TokenHash  string     `db:"token_hash"`
	Status     string     `db:"status"`
	InvitedBy  *string    `db:"invited_by"`
	ExpiresAt  time.Time  `db:"expires_at"`
	AcceptedBy *string    `db:"accepted_by"`
	AcceptedAt *time.Time `db:"accepted_at"`
}

// ----------------------------------------------------------------------------
// Request / response DTOs (see docs/04-api-spec.md section 2).
// ----------------------------------------------------------------------------

type CreateOrganizationRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type UpdateOrganizationRequest struct {
	Name *string `json:"name,omitempty"`
	Plan *string `json:"plan,omitempty"`
}

type InviteMemberRequest struct {
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

type AcceptInviteRequest struct {
	Token string `json:"token"`
}

type ChangeRoleRequest struct {
	Role Role `json:"role"`
}

// OrganizationView is the public projection of an organization.
type OrganizationView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Plan      string    `json:"plan"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

func toOrganizationView(o *Organization) OrganizationView {
	return OrganizationView{
		ID: o.ID, Name: o.Name, Slug: o.Slug, Plan: o.Plan, Status: o.Status, CreatedAt: o.CreatedAt,
	}
}

// MemberView is the public projection of a membership.
type MemberView struct {
	UserID    string    `json:"userId"`
	Role      Role      `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

func toMemberView(m Member) MemberView {
	return MemberView{UserID: m.UserID, Role: m.Role, Status: m.Status, CreatedAt: m.CreatedAt}
}

// InvitationView is the public projection of an invitation (no token).
type InvitationView struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

func toInvitationView(i Invitation) InvitationView {
	return InvitationView{
		ID: i.ID, Email: i.Email, Role: i.Role, Status: i.Status,
		ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt,
	}
}
