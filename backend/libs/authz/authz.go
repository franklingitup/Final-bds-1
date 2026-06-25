// Package authz provides role-based access control with tenant isolation.
//
// Authorization is deny-by-default. Every decision first verifies the principal
// belongs to the organization named in the request (tenant isolation), then
// checks whether any of the principal's organization or project roles grant the
// requested action. See docs/06-security-design.md.
package authz

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/errors"
)

// OrgRole enumerates organization-level roles.
type OrgRole string

const (
	OrgOwner   OrgRole = "owner"
	OrgAdmin   OrgRole = "admin"
	OrgMember  OrgRole = "member"
	OrgAuditor OrgRole = "auditor"
)

// ProjectRole enumerates project-level roles.
type ProjectRole string

const (
	ProjectAdmin     ProjectRole = "admin"
	ProjectDeveloper ProjectRole = "developer"
	ProjectViewer    ProjectRole = "viewer"
)

// Action is a capability that can be authorized.
type Action string

const (
	ActionManageOrg      Action = "org:manage"
	ActionManageMembers  Action = "members:manage"
	ActionManageClusters Action = "clusters:manage"
	ActionReadClusters   Action = "clusters:read"
	ActionManageProject  Action = "project:manage"
	ActionDeploy         Action = "deployment:write"
	ActionReadDeployment Action = "deployment:read"
	ActionReadSecrets    Action = "secrets:read"   // Read secret metadata
	ActionWriteSecrets   Action = "secrets:write"  // Create/update secrets
	ActionManageSecrets  Action = "secrets:manage" // Full access including delete
	ActionManageDomains  Action = "domains:manage"
	ActionReadLogs       Action = "logs:read"
	ActionReadAudit      Action = "audit:read"
)

// Principal is the authenticated caller.
type Principal struct {
	UserID       string
	OrgID        string
	OrgRoles     []OrgRole
	ProjectRoles map[string]ProjectRole // projectID -> role
}

// AccessRequest describes an authorization query.
type AccessRequest struct {
	Action    Action
	OrgID     string
	ProjectID string // optional; required for project-scoped actions
}

// Authorizer resolves whether a principal may perform an action.
type Authorizer interface {
	Authorize(ctx context.Context, p Principal, req AccessRequest) error
}

// PolicyAuthorizer is a static RBAC policy evaluator.
type PolicyAuthorizer struct {
	org     map[OrgRole]map[Action]bool
	project map[ProjectRole]map[Action]bool
}

// NewPolicyAuthorizer returns an authorizer with the platform's default matrix.
func NewPolicyAuthorizer() *PolicyAuthorizer {
	return &PolicyAuthorizer{
		org: map[OrgRole]map[Action]bool{
			OrgOwner: allActions(),
			OrgAdmin: set(
				ActionManageMembers, ActionManageClusters, ActionReadClusters,
				ActionManageProject, ActionDeploy, ActionReadDeployment,
				ActionReadSecrets, ActionWriteSecrets, ActionManageSecrets,
				ActionManageDomains, ActionReadLogs, ActionReadAudit,
			),
			OrgMember: set(
				ActionReadClusters, ActionReadDeployment, ActionReadLogs,
			),
			OrgAuditor: set(
				ActionReadAudit, ActionReadClusters, ActionReadDeployment, ActionReadLogs,
			),
		},
		project: map[ProjectRole]map[Action]bool{
			ProjectAdmin: set(
				ActionManageProject, ActionDeploy, ActionReadDeployment,
				ActionReadSecrets, ActionWriteSecrets, ActionManageSecrets,
				ActionManageDomains, ActionReadLogs,
			),
			ProjectDeveloper: set(
				ActionDeploy, ActionReadDeployment,
				ActionReadSecrets, ActionWriteSecrets,
				ActionReadLogs,
			),
			ProjectViewer: set(
				ActionReadDeployment, ActionReadSecrets, ActionReadLogs,
			),
		},
	}
}

// Authorize implements Authorizer. It returns nil when allowed, or a FORBIDDEN
// error otherwise.
func (a *PolicyAuthorizer) Authorize(_ context.Context, p Principal, req AccessRequest) error {
	// Tenant isolation: principal and request must reference the same org.
	if req.OrgID == "" || p.OrgID != req.OrgID {
		return errors.Forbidden("cross-tenant access denied")
	}

	// Organization-role grants.
	for _, role := range p.OrgRoles {
		if a.org[role][req.Action] {
			return nil
		}
	}

	// Project-role grants (only when the request is project-scoped).
	if req.ProjectID != "" {
		if role, ok := p.ProjectRoles[req.ProjectID]; ok {
			if a.project[role][req.Action] {
				return nil
			}
		}
	}

	return errors.Forbidden("insufficient permissions")
}

func set(actions ...Action) map[Action]bool {
	m := make(map[Action]bool, len(actions))
	for _, a := range actions {
		m[a] = true
	}
	return m
}

func allActions() map[Action]bool {
	return set(
		ActionManageOrg, ActionManageMembers, ActionManageClusters, ActionReadClusters,
		ActionManageProject, ActionDeploy, ActionReadDeployment,
		ActionReadSecrets, ActionWriteSecrets, ActionManageSecrets,
		ActionManageDomains, ActionReadLogs, ActionReadAudit,
	)
}

var _ Authorizer = (*PolicyAuthorizer)(nil)
