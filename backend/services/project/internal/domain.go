package project

import (
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
)

// Role is a project membership role. ProjectAdmin manages the project and its
// members; Developer can deploy workloads; Viewer is read-only.
type Role string

const (
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
)

// Valid reports whether r is a known project role.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleDeveloper, RoleViewer:
		return true
	default:
		return false
	}
}

// toAuthzRole maps a project membership role to the platform RBAC project role.
func (r Role) toAuthzRole() authz.ProjectRole {
	switch r {
	case RoleAdmin:
		return authz.ProjectAdmin
	case RoleDeveloper:
		return authz.ProjectDeveloper
	default:
		return authz.ProjectViewer
	}
}

// Project statuses.
const (
	ProjectStatusActive   = "active"
	ProjectStatusArchived = "archived"
)

// Project is a development workspace within an organization.
type Project struct {
	database.TenantModel
	Name        string  `db:"name"`
	Slug        string  `db:"slug"`
	Description *string `db:"description"`
	Status      string  `db:"status"`
	CreatedBy   *string `db:"created_by"`
}

// Member is a user's membership in a project.
type Member struct {
	database.TenantModel
	ProjectID string  `db:"project_id"`
	UserID    string  `db:"user_id"`
	Role      Role    `db:"role"`
	AddedBy   *string `db:"added_by"`
}

// ----------------------------------------------------------------------------
// Request / response DTOs
// ----------------------------------------------------------------------------

type CreateProjectRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description,omitempty"`
}

type UpdateProjectRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type AddMemberRequest struct {
	UserID string `json:"userId"`
	Role   Role   `json:"role"`
}

type ChangeRoleRequest struct {
	Role Role `json:"role"`
}

// ProjectView is the public projection of a project.
type ProjectView struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"organizationId"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toProjectView(p *Project) ProjectView {
	return ProjectView{
		ID:          p.ID,
		OrgID:       p.OrgID,
		Name:        p.Name,
		Slug:        p.Slug,
		Description: deref(p.Description),
		Status:      p.Status,
		CreatedAt:   p.CreatedAt,
	}
}

// MemberView is the public projection of a project membership.
type MemberView struct {
	UserID    string    `json:"userId"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

func toMemberView(m Member) MemberView {
	return MemberView{UserID: m.UserID, Role: m.Role, CreatedAt: m.CreatedAt}
}
