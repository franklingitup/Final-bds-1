// Package authz provides role-based access control with tenant isolation.
// This file provides shared organization membership authorization helpers.

package authz

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/errors"
)

// OrgMember represents an organization membership.
type OrgMember struct {
	OrgID  string
	UserID string
	Role   OrgRole
	Status string
}

// ProjectMember represents a project membership.
type ProjectMember struct {
	OrgID     string
	ProjectID string
	UserID    string
	Role      ProjectRole
}

// OrgMemberStore retrieves organization membership. Used by authorization helpers.
type OrgMemberStore interface {
	GetOrgMember(ctx context.Context, userID string) (*OrgMember, error)
}

// ProjectMemberStore retrieves project membership. Used by authorization helpers.
type ProjectMemberStore interface {
	GetProjectMember(ctx context.Context, projectID, userID string) (*ProjectMember, error)
}

// TenantRunner runs functions within a tenant-scoped transaction.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// AuthorizationService provides reusable authorization methods.
type AuthorizationService struct {
	tenant     TenantRunner
	orgMembers OrgMemberStore
	authorizer Authorizer
}

// NewAuthorizationService creates a new authorization service.
func NewAuthorizationService(tenant TenantRunner, orgMembers OrgMemberStore, authorizer Authorizer) *AuthorizationService {
	if authorizer == nil {
		authorizer = NewPolicyAuthorizer()
	}
	return &AuthorizationService{
		tenant:     tenant,
		orgMembers: orgMembers,
		authorizer: authorizer,
	}
}

// AuthorizeOrgMember verifies the caller is a member of the organization and has
// the required permission for the action. Returns the member's role on success.
//
// This is the primary authorization entry point for org-scoped operations.
// It enforces:
//  1. Caller is an org member (membership check within tenant context)
//  2. Caller has sufficient role for the action (RBAC check)
//  3. Tenant isolation (orgID matches throughout)
func (s *AuthorizationService) AuthorizeOrgMember(ctx context.Context, orgID, userID string, action Action) (OrgRole, error) {
	if orgID == "" {
		return "", errors.Validation("organization ID is required")
	}
	if userID == "" {
		return "", errors.Validation("user ID is required")
	}

	var member *OrgMember
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		m, err := s.orgMembers.GetOrgMember(ctx, userID)
		if err != nil {
			if database.IsNotFound(err) {
				return errors.Forbidden("not a member of this organization")
			}
			return err
		}
		member = m
		return nil
	})
	if err != nil {
		return "", err
	}

	// Verify member status is active
	if member.Status != "active" {
		return "", errors.Forbidden("membership is suspended")
	}

	// Build principal and check authorization
	principal := Principal{
		UserID:   userID,
		OrgID:    orgID,
		OrgRoles: []OrgRole{member.Role},
	}
	if err := s.authorizer.Authorize(ctx, principal, AccessRequest{Action: action, OrgID: orgID}); err != nil {
		return member.Role, err
	}

	return member.Role, nil
}

// AuthorizeOrgRead is a convenience wrapper for read-only org operations.
// It verifies org membership but does not require a specific action - any member can read.
func (s *AuthorizationService) AuthorizeOrgRead(ctx context.Context, orgID, userID string) (OrgRole, error) {
	if orgID == "" {
		return "", errors.Validation("organization ID is required")
	}
	if userID == "" {
		return "", errors.Validation("user ID is required")
	}

	var member *OrgMember
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		m, err := s.orgMembers.GetOrgMember(ctx, userID)
		if err != nil {
			if database.IsNotFound(err) {
				return errors.Forbidden("not a member of this organization")
			}
			return err
		}
		member = m
		return nil
	})
	if err != nil {
		return "", err
	}

	// Verify member status is active
	if member.Status != "active" {
		return "", errors.Forbidden("membership is suspended")
	}

	return member.Role, nil
}

// OrgMemberRepo implements OrgMemberStore using PostgreSQL.
type OrgMemberRepo struct {
	db *database.DB
}

// NewOrgMemberRepo creates a new org member repository.
func NewOrgMemberRepo(db *database.DB) *OrgMemberRepo {
	return &OrgMemberRepo{db: db}
}

// GetOrgMember retrieves org membership by user ID (must run within tenant context).
func (r *OrgMemberRepo) GetOrgMember(ctx context.Context, userID string) (*OrgMember, error) {
	const sql = `
		SELECT org_id, user_id, role, status
		FROM organization_members
		WHERE user_id = $1`

	row := r.db.Conn(ctx).QueryRow(ctx, sql, userID)

	var m OrgMember
	err := row.Scan(&m.OrgID, &m.UserID, &m.Role, &m.Status)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &m, nil
}

// ProjectMemberRepo implements ProjectMemberStore using PostgreSQL.
type ProjectMemberRepo struct {
	db *database.DB
}

// NewProjectMemberRepo creates a new project member repository.
func NewProjectMemberRepo(db *database.DB) *ProjectMemberRepo {
	return &ProjectMemberRepo{db: db}
}

// GetProjectMember retrieves project membership (must run within tenant context).
func (r *ProjectMemberRepo) GetProjectMember(ctx context.Context, projectID, userID string) (*ProjectMember, error) {
	const sql = `
		SELECT org_id, project_id, user_id, role
		FROM project_members
		WHERE project_id = $1 AND user_id = $2`

	row := r.db.Conn(ctx).QueryRow(ctx, sql, projectID, userID)

	var m ProjectMember
	err := row.Scan(&m.OrgID, &m.ProjectID, &m.UserID, &m.Role)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &m, nil
}
