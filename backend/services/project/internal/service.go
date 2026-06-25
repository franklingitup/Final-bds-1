package project

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// Deps are the project service dependencies.
type Deps struct {
	Projects   ProjectStore
	Members    MemberStore
	OrgMembers authz.OrgMemberStore // For org membership authorization
	Outbox     events.Outbox
	Tenant     TenantRunner
	Authorizer authz.Authorizer
	Logger     *slog.Logger
	Now        func() time.Time
}

// Service implements the project domain logic.
type Service struct {
	projects   ProjectStore
	members    MemberStore
	orgMembers authz.OrgMemberStore
	outbox     events.Outbox
	tenant     TenantRunner
	authorizer authz.Authorizer
	authSvc    *authz.AuthorizationService
	log        *slog.Logger
	now        func() time.Time
}

// NewService wires a project Service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Authorizer == nil {
		d.Authorizer = authz.NewPolicyAuthorizer()
	}
	return &Service{
		projects:   d.Projects,
		members:    d.Members,
		orgMembers: d.OrgMembers,
		outbox:     d.Outbox,
		tenant:     d.Tenant,
		authorizer: d.Authorizer,
		authSvc:    authz.NewAuthorizationService(d.Tenant, d.OrgMembers, d.Authorizer),
		log:        d.Logger,
		now:        d.Now,
	}
}

// ----------------------------------------------------------------------------
// Projects
// ----------------------------------------------------------------------------

// CreateProject creates a project and makes the caller its admin.
// SECURITY: Requires org admin role to create projects.
func (s *Service) CreateProject(ctx context.Context, orgID, userID string, req CreateProjectRequest) (*Project, error) {
	// SECURITY: Verify caller is org member with admin privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageProject); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	if err := validateName(name); err != nil {
		return nil, err
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if !slugPattern.MatchString(slug) {
		return nil, apperrors.Validation("slug must be 2-64 chars of lowercase letters, digits, or hyphens")
	}

	p := &Project{
		Name:      name,
		Slug:      slug,
		Status:    ProjectStatusActive,
		CreatedBy: &userID,
	}
	p.OrgID = orgID
	if req.Description != nil {
		d := strings.TrimSpace(*req.Description)
		p.Description = &d
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.projects.Create(ctx, p); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errSlugTaken
			}
			return err
		}
		// Creator becomes project admin.
		member := &Member{ProjectID: p.ID, UserID: userID, Role: RoleAdmin, AddedBy: &userID}
		member.OrgID = orgID
		if err := s.members.Create(ctx, member); err != nil {
			return err
		}
		return s.enqueue(ctx, EventProjectCreated, orgID, projectCreatedPayload{
			ProjectID: p.ID, Name: p.Name, Slug: p.Slug, Description: deref(p.Description), CreatedBy: userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "project", ID: p.ID}))
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// GetProject returns a project.
// SECURITY: Requires org membership to read projects.
func (s *Service) GetProject(ctx context.Context, orgID, userID, projectID string) (*Project, error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var p *Project
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		p, err = s.projects.GetByID(ctx, projectID)
		return err
	})
	return p, err
}

// ListProjects returns a paginated list of projects within an org.
// SECURITY: Requires org membership to list projects.
func (s *Service) ListProjects(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[Project], error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Project]{}, err
	}

	var out database.Page[Project]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.projects.List(ctx, page)
		return err
	})
	return out, err
}

// UpdateProject updates mutable project fields. Requires project:manage.
func (s *Service) UpdateProject(ctx context.Context, orgID, userID, projectID string, req UpdateProjectRequest) (*Project, error) {
	if _, err := s.authorize(ctx, orgID, userID, projectID, authz.ActionManageProject); err != nil {
		return nil, err
	}

	var p *Project
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		p, err = s.projects.GetByID(ctx, projectID)
		if err != nil {
			return err
		}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if err := validateName(name); err != nil {
				return err
			}
			p.Name = name
		}
		if req.Description != nil {
			d := strings.TrimSpace(*req.Description)
			p.Description = &d
		}
		if err := s.projects.Update(ctx, p); err != nil {
			return err
		}
		return s.enqueue(ctx, EventProjectUpdated, orgID, projectUpdatedPayload{
			ProjectID: p.ID, Name: p.Name, Description: deref(p.Description),
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "project", ID: p.ID}))
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// DeleteProject deletes a project. Requires project:manage.
func (s *Service) DeleteProject(ctx context.Context, orgID, userID, projectID string) error {
	if _, err := s.authorize(ctx, orgID, userID, projectID, authz.ActionManageProject); err != nil {
		return err
	}
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.projects.Delete(ctx, projectID); err != nil {
			return err
		}
		return s.enqueue(ctx, EventProjectDeleted, orgID, projectDeletedPayload{
			ProjectID: projectID, DeletedBy: userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "project", ID: projectID}))
	})
}

// ----------------------------------------------------------------------------
// Memberships
// ----------------------------------------------------------------------------

// AddMember adds a user to the project. Requires project:manage.
func (s *Service) AddMember(ctx context.Context, orgID, callerID, projectID string, req AddMemberRequest) (*Member, error) {
	if _, err := s.authorize(ctx, orgID, callerID, projectID, authz.ActionManageProject); err != nil {
		return nil, err
	}
	if !req.Role.Valid() {
		return nil, apperrors.Validation("invalid role")
	}

	m := &Member{ProjectID: projectID, UserID: req.UserID, Role: req.Role, AddedBy: &callerID}
	m.OrgID = orgID

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.members.Create(ctx, m); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errMemberExists
			}
			return err
		}
		return s.enqueue(ctx, EventMemberAdded, orgID, memberAddedPayload{
			ProjectID: projectID, UserID: req.UserID, Role: req.Role, AddedBy: callerID,
		}, events.WithActor(events.Actor{Type: "user", ID: callerID}),
			events.WithResource(events.Resource{Type: "project_member", ID: m.ID}))
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// RemoveMember removes a user from the project. Requires project:manage.
// The last admin cannot be removed.
func (s *Service) RemoveMember(ctx context.Context, orgID, callerID, projectID, targetUserID string) error {
	if _, err := s.authorize(ctx, orgID, callerID, projectID, authz.ActionManageProject); err != nil {
		return err
	}
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		target, err := s.members.GetByUser(ctx, projectID, targetUserID)
		if err != nil {
			if database.IsNotFound(err) {
				return apperrors.NotFound("member not found")
			}
			return err
		}
		// Prevent removing the last admin.
		if target.Role == RoleAdmin {
			page, err := s.members.List(ctx, projectID, database.PageRequest{Limit: 100})
			if err != nil {
				return err
			}
			adminCount := 0
			for _, m := range page.Items {
				if m.Role == RoleAdmin {
					adminCount++
				}
			}
			if adminCount <= 1 {
				return errLastAdmin
			}
		}
		if err := s.members.Delete(ctx, projectID, targetUserID); err != nil {
			return err
		}
		return s.enqueue(ctx, EventMemberRemoved, orgID, memberRemovedPayload{
			ProjectID: projectID, UserID: targetUserID, RemovedBy: callerID,
		}, events.WithActor(events.Actor{Type: "user", ID: callerID}),
			events.WithResource(events.Resource{Type: "project_member", ID: target.ID}))
	})
}

// ChangeRole changes a member's role. Requires project:manage.
// The last admin cannot be demoted.
func (s *Service) ChangeRole(ctx context.Context, orgID, callerID, projectID, targetUserID string, newRole Role) (*Member, error) {
	if !newRole.Valid() {
		return nil, apperrors.Validation("invalid role")
	}
	if _, err := s.authorize(ctx, orgID, callerID, projectID, authz.ActionManageProject); err != nil {
		return nil, err
	}

	var updated *Member
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		target, err := s.members.GetByUser(ctx, projectID, targetUserID)
		if err != nil {
			if database.IsNotFound(err) {
				return apperrors.NotFound("member not found")
			}
			return err
		}
		oldRole := target.Role
		if oldRole == newRole {
			updated = target
			return nil
		}
		// Prevent demoting the last admin.
		if oldRole == RoleAdmin && newRole != RoleAdmin {
			page, err := s.members.List(ctx, projectID, database.PageRequest{Limit: 100})
			if err != nil {
				return err
			}
			adminCount := 0
			for _, m := range page.Items {
				if m.Role == RoleAdmin {
					adminCount++
				}
			}
			if adminCount <= 1 {
				return errLastAdmin
			}
		}
		if err := s.members.UpdateRole(ctx, projectID, targetUserID, newRole); err != nil {
			return err
		}
		target.Role = newRole
		updated = target
		return s.enqueue(ctx, EventRoleChanged, orgID, roleChangedPayload{
			ProjectID: projectID, UserID: targetUserID, OldRole: oldRole, NewRole: newRole, ChangedBy: callerID,
		}, events.WithActor(events.Actor{Type: "user", ID: callerID}),
			events.WithResource(events.Resource{Type: "project_member", ID: target.ID}))
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ListMembers returns a paginated list of project members.
// SECURITY: Requires org membership to list project members.
func (s *Service) ListMembers(ctx context.Context, orgID, userID, projectID string, page database.PageRequest) (database.Page[Member], error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Member]{}, err
	}

	var out database.Page[Member]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.members.List(ctx, projectID, page)
		return err
	})
	return out, err
}

// ----------------------------------------------------------------------------
// Authorization helpers
// ----------------------------------------------------------------------------

func (s *Service) authorize(ctx context.Context, orgID, userID, projectID string, action authz.Action) (Role, error) {
	var projectRole Role
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		m, err := s.members.GetByUser(ctx, projectID, userID)
		if err != nil {
			if database.IsNotFound(err) {
				return errNotMember
			}
			return err
		}
		projectRole = m.Role
		return nil
	})
	if err != nil {
		return "", err
	}

	principal := authz.Principal{
		UserID:       userID,
		OrgID:        orgID,
		ProjectRoles: map[string]authz.ProjectRole{projectID: projectRole.toAuthzRole()},
	}
	if err := s.authorizer.Authorize(ctx, principal, authz.AccessRequest{
		Action:    action,
		OrgID:     orgID,
		ProjectID: projectID,
	}); err != nil {
		return "", err
	}
	return projectRole, nil
}

func validateName(name string) error {
	if l := len(name); l < 2 || l > 64 {
		return apperrors.Validation("name must be between 2 and 64 characters")
	}
	return nil
}
