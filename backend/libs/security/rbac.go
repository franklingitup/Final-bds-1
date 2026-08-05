package security

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Permission represents a fine-grained permission.
type Permission string

// Resource types
const (
	ResourceOrg        = "organization"
	ResourceProject    = "project"
	ResourceCluster    = "cluster"
	ResourceDeployment = "deployment"
	ResourceSecret     = "secret"
	ResourceDomain     = "domain"
	ResourceUser       = "user"
	ResourceAuditLog   = "audit_log"
	ResourceBilling    = "billing"
	ResourceSSO        = "sso"
	ResourceSCIM       = "scim"
)

// Actions
const (
	ActionCreate = "create"
	ActionRead   = "read"
	ActionUpdate = "update"
	ActionDelete = "delete"
	ActionList   = "list"
	ActionManage = "manage"
	ActionInvite = "invite"
	ActionDeploy = "deploy"
	ActionRollback = "rollback"
)

// BuildPermission creates a permission string.
func BuildPermission(resource, action string) Permission {
	return Permission(resource + ":" + action)
}

// Predefined permissions
var (
	// Organization permissions
	PermOrgRead       = BuildPermission(ResourceOrg, ActionRead)
	PermOrgUpdate     = BuildPermission(ResourceOrg, ActionUpdate)
	PermOrgDelete     = BuildPermission(ResourceOrg, ActionDelete)
	PermOrgManage     = BuildPermission(ResourceOrg, ActionManage)
	PermOrgInvite     = BuildPermission(ResourceOrg, ActionInvite)

	// Project permissions
	PermProjectCreate = BuildPermission(ResourceProject, ActionCreate)
	PermProjectRead   = BuildPermission(ResourceProject, ActionRead)
	PermProjectUpdate = BuildPermission(ResourceProject, ActionUpdate)
	PermProjectDelete = BuildPermission(ResourceProject, ActionDelete)
	PermProjectManage = BuildPermission(ResourceProject, ActionManage)

	// Cluster permissions
	PermClusterCreate = BuildPermission(ResourceCluster, ActionCreate)
	PermClusterRead   = BuildPermission(ResourceCluster, ActionRead)
	PermClusterUpdate = BuildPermission(ResourceCluster, ActionUpdate)
	PermClusterDelete = BuildPermission(ResourceCluster, ActionDelete)
	PermClusterManage = BuildPermission(ResourceCluster, ActionManage)

	// Deployment permissions
	PermDeploymentCreate   = BuildPermission(ResourceDeployment, ActionCreate)
	PermDeploymentRead     = BuildPermission(ResourceDeployment, ActionRead)
	PermDeploymentDeploy   = BuildPermission(ResourceDeployment, ActionDeploy)
	PermDeploymentRollback = BuildPermission(ResourceDeployment, ActionRollback)
	PermDeploymentDelete   = BuildPermission(ResourceDeployment, ActionDelete)
	PermDeploymentManage   = BuildPermission(ResourceDeployment, ActionManage)

	// Secret permissions
	PermSecretCreate = BuildPermission(ResourceSecret, ActionCreate)
	PermSecretRead   = BuildPermission(ResourceSecret, ActionRead)
	PermSecretUpdate = BuildPermission(ResourceSecret, ActionUpdate)
	PermSecretDelete = BuildPermission(ResourceSecret, ActionDelete)
	PermSecretManage = BuildPermission(ResourceSecret, ActionManage)

	// Domain permissions
	PermDomainCreate = BuildPermission(ResourceDomain, ActionCreate)
	PermDomainRead   = BuildPermission(ResourceDomain, ActionRead)
	PermDomainUpdate = BuildPermission(ResourceDomain, ActionUpdate)
	PermDomainDelete = BuildPermission(ResourceDomain, ActionDelete)
	PermDomainManage = BuildPermission(ResourceDomain, ActionManage)

	// User management permissions
	PermUserList   = BuildPermission(ResourceUser, ActionList)
	PermUserRead   = BuildPermission(ResourceUser, ActionRead)
	PermUserUpdate = BuildPermission(ResourceUser, ActionUpdate)
	PermUserDelete = BuildPermission(ResourceUser, ActionDelete)
	PermUserManage = BuildPermission(ResourceUser, ActionManage)

	// Audit permissions
	PermAuditRead = BuildPermission(ResourceAuditLog, ActionRead)

	// Billing permissions
	PermBillingRead   = BuildPermission(ResourceBilling, ActionRead)
	PermBillingManage = BuildPermission(ResourceBilling, ActionManage)

	// SSO permissions
	PermSSOManage = BuildPermission(ResourceSSO, ActionManage)

	// SCIM permissions
	PermSCIMManage = BuildPermission(ResourceSCIM, ActionManage)
)

// Role represents a named set of permissions.
type Role struct {
	ID          string
	Name        string
	Description string
	Permissions []Permission
	IsSystem    bool // System roles cannot be modified
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// HasPermission checks if a role has a specific permission.
func (r *Role) HasPermission(perm Permission) bool {
	for _, p := range r.Permissions {
		// Universal wildcard
		if p == "*:*" {
			return true
		}
		// Direct match
		if p == perm {
			return true
		}
		// Check for resource-level wildcard permissions (e.g., "organization:*")
		parts := strings.SplitN(string(perm), ":", 2)
		if len(parts) == 2 {
			wildcard := Permission(parts[0] + ":*")
			if p == wildcard {
				return true
			}
		}
	}
	return false
}

// SystemRoles returns the default system roles.
func SystemRoles() map[string]*Role {
	return map[string]*Role{
		"owner": {
			ID:          "owner",
			Name:        "Owner",
			Description: "Full access to organization and all resources",
			IsSystem:    true,
			Permissions: []Permission{
				Permission("*:*"), // Super admin
			},
		},
		"admin": {
			ID:          "admin",
			Name:        "Administrator",
			Description: "Manage organization settings, users, and resources",
			IsSystem:    true,
			Permissions: []Permission{
				PermOrgRead, PermOrgUpdate, PermOrgInvite,
				PermProjectCreate, PermProjectRead, PermProjectUpdate, PermProjectDelete, PermProjectManage,
				PermClusterCreate, PermClusterRead, PermClusterUpdate, PermClusterDelete, PermClusterManage,
				PermDeploymentCreate, PermDeploymentRead, PermDeploymentDeploy, PermDeploymentRollback, PermDeploymentDelete, PermDeploymentManage,
				PermSecretCreate, PermSecretRead, PermSecretUpdate, PermSecretDelete, PermSecretManage,
				PermDomainCreate, PermDomainRead, PermDomainUpdate, PermDomainDelete, PermDomainManage,
				PermUserList, PermUserRead, PermUserUpdate, PermUserManage,
				PermAuditRead,
				PermBillingRead,
				PermSSOManage,
				PermSCIMManage,
			},
		},
		"developer": {
			ID:          "developer",
			Name:        "Developer",
			Description: "Deploy applications and manage project resources",
			IsSystem:    true,
			Permissions: []Permission{
				PermOrgRead,
				PermProjectRead,
				PermClusterRead,
				PermDeploymentCreate, PermDeploymentRead, PermDeploymentDeploy, PermDeploymentRollback,
				PermSecretCreate, PermSecretRead, PermSecretUpdate,
				PermDomainRead,
				PermUserList, PermUserRead,
			},
		},
		"viewer": {
			ID:          "viewer",
			Name:        "Viewer",
			Description: "Read-only access to organization resources",
			IsSystem:    true,
			Permissions: []Permission{
				PermOrgRead,
				PermProjectRead,
				PermClusterRead,
				PermDeploymentRead,
				PermSecretRead,
				PermDomainRead,
				PermUserList, PermUserRead,
			},
		},
		"auditor": {
			ID:          "auditor",
			Name:        "Auditor",
			Description: "Access to audit logs and compliance reports",
			IsSystem:    true,
			Permissions: []Permission{
				PermOrgRead,
				PermProjectRead,
				PermClusterRead,
				PermDeploymentRead,
				PermAuditRead,
			},
		},
		"billing": {
			ID:          "billing",
			Name:        "Billing Admin",
			Description: "Manage billing and subscription settings",
			IsSystem:    true,
			Permissions: []Permission{
				PermOrgRead,
				PermBillingRead, PermBillingManage,
			},
		},
	}
}

// RBACEngine provides permission checking with caching.
type RBACEngine struct {
	roleStore       RoleStore
	assignmentStore RoleAssignmentStore
	cache           *permissionCache
	systemRoles     map[string]*Role
}

// RoleStore persists roles.
type RoleStore interface {
	Get(ctx context.Context, roleID string) (*Role, error)
	List(ctx context.Context, orgID string) ([]Role, error)
	Create(ctx context.Context, orgID string, role *Role) error
	Update(ctx context.Context, role *Role) error
	Delete(ctx context.Context, roleID string) error
}

// RoleAssignment represents a user's role in a scope.
type RoleAssignment struct {
	ID        string
	UserID    string
	RoleID    string
	OrgID     string
	ProjectID string // Empty for org-level assignments
	CreatedAt time.Time
	CreatedBy string
}

// RoleAssignmentStore persists role assignments.
type RoleAssignmentStore interface {
	Assign(ctx context.Context, assignment *RoleAssignment) error
	Revoke(ctx context.Context, userID, roleID, orgID, projectID string) error
	GetUserRoles(ctx context.Context, userID, orgID string) ([]RoleAssignment, error)
	GetUserProjectRoles(ctx context.Context, userID, orgID, projectID string) ([]RoleAssignment, error)
	ListByOrg(ctx context.Context, orgID string) ([]RoleAssignment, error)
	ListByProject(ctx context.Context, projectID string) ([]RoleAssignment, error)
}

// permissionCache caches permission check results.
type permissionCache struct {
	entries map[string]*cacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
	maxSize int
}

type cacheEntry struct {
	allowed   bool
	expiresAt time.Time
}

func newPermissionCache(ttl time.Duration, maxSize int) *permissionCache {
	return &permissionCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

func (c *permissionCache) get(key string) (bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return false, false
	}
	return entry.allowed, true
}

func (c *permissionCache) set(key string, allowed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[key] = &cacheEntry{
		allowed:   allowed,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *permissionCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for k, v := range c.entries {
		if oldestKey == "" || v.expiresAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.expiresAt
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

func (c *permissionCache) invalidateUser(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key := range c.entries {
		if strings.Contains(key, userID) {
			delete(c.entries, key)
		}
	}
}

// NewRBACEngine creates a new RBAC engine.
func NewRBACEngine(roleStore RoleStore, assignmentStore RoleAssignmentStore) *RBACEngine {
	return &RBACEngine{
		roleStore:       roleStore,
		assignmentStore: assignmentStore,
		cache:           newPermissionCache(5*time.Minute, 10000),
		systemRoles:     SystemRoles(),
	}
}

// CheckPermission checks if a user has a permission in a context.
func (e *RBACEngine) CheckPermission(ctx context.Context, userID, orgID, projectID string, perm Permission) error {
	// Build cache key
	cacheKey := fmt.Sprintf("%s:%s:%s:%s", userID, orgID, projectID, perm)

	// Check cache
	if allowed, found := e.cache.get(cacheKey); found {
		if allowed {
			return nil
		}
		return fmt.Errorf("permission denied: %s", perm)
	}

	// Get user's roles
	assignments, err := e.assignmentStore.GetUserRoles(ctx, userID, orgID)
	if err != nil {
		return err
	}

	// Include project roles if applicable
	if projectID != "" {
		projectAssignments, err := e.assignmentStore.GetUserProjectRoles(ctx, userID, orgID, projectID)
		if err != nil {
			return err
		}
		assignments = append(assignments, projectAssignments...)
	}

	// Check each role
	for _, assignment := range assignments {
		role := e.getRole(ctx, assignment.RoleID)
		if role == nil {
			continue
		}

		if role.HasPermission(perm) {
			e.cache.set(cacheKey, true)
			return nil
		}
	}

	e.cache.set(cacheKey, false)
	return fmt.Errorf("permission denied: %s", perm)
}

func (e *RBACEngine) getRole(ctx context.Context, roleID string) *Role {
	// Check system roles first
	if role, ok := e.systemRoles[roleID]; ok {
		return role
	}

	// Check custom roles
	role, err := e.roleStore.Get(ctx, roleID)
	if err != nil {
		return nil
	}
	return role
}

// GetUserPermissions returns all permissions for a user in a context.
func (e *RBACEngine) GetUserPermissions(ctx context.Context, userID, orgID, projectID string) ([]Permission, error) {
	assignments, err := e.assignmentStore.GetUserRoles(ctx, userID, orgID)
	if err != nil {
		return nil, err
	}

	if projectID != "" {
		projectAssignments, err := e.assignmentStore.GetUserProjectRoles(ctx, userID, orgID, projectID)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, projectAssignments...)
	}

	permSet := make(map[Permission]bool)
	for _, assignment := range assignments {
		role := e.getRole(ctx, assignment.RoleID)
		if role == nil {
			continue
		}
		for _, perm := range role.Permissions {
			permSet[perm] = true
		}
	}

	perms := make([]Permission, 0, len(permSet))
	for perm := range permSet {
		perms = append(perms, perm)
	}
	return perms, nil
}

// AssignRole assigns a role to a user.
func (e *RBACEngine) AssignRole(ctx context.Context, assignment *RoleAssignment) error {
	if err := e.assignmentStore.Assign(ctx, assignment); err != nil {
		return err
	}
	e.cache.invalidateUser(assignment.UserID)
	return nil
}

// RevokeRole revokes a role from a user.
func (e *RBACEngine) RevokeRole(ctx context.Context, userID, roleID, orgID, projectID string) error {
	if err := e.assignmentStore.Revoke(ctx, userID, roleID, orgID, projectID); err != nil {
		return err
	}
	e.cache.invalidateUser(userID)
	return nil
}

// CreateCustomRole creates a custom role for an organization.
func (e *RBACEngine) CreateCustomRole(ctx context.Context, orgID string, role *Role) error {
	role.IsSystem = false
	role.CreatedAt = time.Now()
	role.UpdatedAt = time.Now()
	return e.roleStore.Create(ctx, orgID, role)
}

// UpdateCustomRole updates a custom role.
func (e *RBACEngine) UpdateCustomRole(ctx context.Context, role *Role) error {
	if role.IsSystem {
		return fmt.Errorf("cannot modify system role")
	}
	role.UpdatedAt = time.Now()
	return e.roleStore.Update(ctx, role)
}

// DeleteCustomRole deletes a custom role.
func (e *RBACEngine) DeleteCustomRole(ctx context.Context, roleID string) error {
	role, err := e.roleStore.Get(ctx, roleID)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return fmt.Errorf("cannot delete system role")
	}
	return e.roleStore.Delete(ctx, roleID)
}

// ListRoles lists all roles available in an organization.
func (e *RBACEngine) ListRoles(ctx context.Context, orgID string) ([]Role, error) {
	// Start with system roles
	roles := make([]Role, 0, len(e.systemRoles))
	for _, role := range e.systemRoles {
		roles = append(roles, *role)
	}

	// Add custom roles
	customRoles, err := e.roleStore.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	roles = append(roles, customRoles...)

	return roles, nil
}

// PermissionGroup represents a group of related permissions for UI display.
type PermissionGroup struct {
	Name        string
	Description string
	Permissions []PermissionInfo
}

// PermissionInfo describes a permission.
type PermissionInfo struct {
	Permission  Permission
	Name        string
	Description string
}

// GetPermissionGroups returns all permissions organized by group.
func GetPermissionGroups() []PermissionGroup {
	return []PermissionGroup{
		{
			Name:        "Organization",
			Description: "Organization-level permissions",
			Permissions: []PermissionInfo{
				{PermOrgRead, "View Organization", "View organization details and settings"},
				{PermOrgUpdate, "Update Organization", "Modify organization settings"},
				{PermOrgDelete, "Delete Organization", "Delete the organization"},
				{PermOrgManage, "Manage Organization", "Full organization management"},
				{PermOrgInvite, "Invite Members", "Invite new members to the organization"},
			},
		},
		{
			Name:        "Projects",
			Description: "Project management permissions",
			Permissions: []PermissionInfo{
				{PermProjectCreate, "Create Projects", "Create new projects"},
				{PermProjectRead, "View Projects", "View project details"},
				{PermProjectUpdate, "Update Projects", "Modify project settings"},
				{PermProjectDelete, "Delete Projects", "Delete projects"},
				{PermProjectManage, "Manage Projects", "Full project management"},
			},
		},
		{
			Name:        "Clusters",
			Description: "Kubernetes cluster permissions",
			Permissions: []PermissionInfo{
				{PermClusterCreate, "Create Clusters", "Register new clusters"},
				{PermClusterRead, "View Clusters", "View cluster details"},
				{PermClusterUpdate, "Update Clusters", "Modify cluster settings"},
				{PermClusterDelete, "Delete Clusters", "Remove clusters"},
				{PermClusterManage, "Manage Clusters", "Full cluster management"},
			},
		},
		{
			Name:        "Deployments",
			Description: "Application deployment permissions",
			Permissions: []PermissionInfo{
				{PermDeploymentCreate, "Create Deployments", "Create new deployments"},
				{PermDeploymentRead, "View Deployments", "View deployment details"},
				{PermDeploymentDeploy, "Deploy Applications", "Trigger deployments"},
				{PermDeploymentRollback, "Rollback Deployments", "Rollback to previous versions"},
				{PermDeploymentDelete, "Delete Deployments", "Remove deployments"},
				{PermDeploymentManage, "Manage Deployments", "Full deployment management"},
			},
		},
		{
			Name:        "Secrets",
			Description: "Secret management permissions",
			Permissions: []PermissionInfo{
				{PermSecretCreate, "Create Secrets", "Create new secrets"},
				{PermSecretRead, "View Secrets", "View secret metadata"},
				{PermSecretUpdate, "Update Secrets", "Modify secret values"},
				{PermSecretDelete, "Delete Secrets", "Remove secrets"},
				{PermSecretManage, "Manage Secrets", "Full secret management"},
			},
		},
		{
			Name:        "Domains",
			Description: "Domain and TLS management",
			Permissions: []PermissionInfo{
				{PermDomainCreate, "Create Domains", "Add custom domains"},
				{PermDomainRead, "View Domains", "View domain details"},
				{PermDomainUpdate, "Update Domains", "Modify domain settings"},
				{PermDomainDelete, "Delete Domains", "Remove domains"},
				{PermDomainManage, "Manage Domains", "Full domain management"},
			},
		},
		{
			Name:        "Users",
			Description: "User management permissions",
			Permissions: []PermissionInfo{
				{PermUserList, "List Users", "View member list"},
				{PermUserRead, "View Users", "View user details"},
				{PermUserUpdate, "Update Users", "Modify user roles"},
				{PermUserDelete, "Remove Users", "Remove users from organization"},
				{PermUserManage, "Manage Users", "Full user management"},
			},
		},
		{
			Name:        "Administration",
			Description: "Administrative permissions",
			Permissions: []PermissionInfo{
				{PermAuditRead, "View Audit Logs", "Access audit logs"},
				{PermBillingRead, "View Billing", "View billing information"},
				{PermBillingManage, "Manage Billing", "Modify billing settings"},
				{PermSSOManage, "Manage SSO", "Configure SSO providers"},
				{PermSCIMManage, "Manage SCIM", "Configure SCIM provisioning"},
			},
		},
	}
}
