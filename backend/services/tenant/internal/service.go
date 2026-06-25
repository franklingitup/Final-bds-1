package tenant

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

const defaultInviteTTL = 7 * 24 * time.Hour

var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// Deps are the tenant service dependencies. Stores and collaborators are
// interfaces so the service is unit-testable with in-memory fakes.
type Deps struct {
	Orgs        OrganizationStore
	Members     MemberStore
	Invitations InvitationStore
	Outbox      events.Outbox
	Tenant      TenantRunner
	Authorizer  authz.Authorizer
	// Notifier delivers invitation tokens over a secure channel (optional).
	Notifier  Notifier
	Logger    *slog.Logger
	Now       func() time.Time
	InviteTTL time.Duration
}

// Service implements the tenant domain logic.
type Service struct {
	orgs       OrganizationStore
	members    MemberStore
	invites    InvitationStore
	outbox     events.Outbox
	tenant     TenantRunner
	authorizer authz.Authorizer
	notifier   Notifier
	log        *slog.Logger
	now        func() time.Time
	inviteTTL  time.Duration
}

// NewService wires a tenant Service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.InviteTTL <= 0 {
		d.InviteTTL = defaultInviteTTL
	}
	if d.Authorizer == nil {
		d.Authorizer = authz.NewPolicyAuthorizer()
	}
	return &Service{
		orgs:       d.Orgs,
		members:    d.Members,
		invites:    d.Invitations,
		outbox:     d.Outbox,
		tenant:     d.Tenant,
		authorizer: d.Authorizer,
		notifier:   d.Notifier,
		log:        d.Logger,
		now:        d.Now,
		inviteTTL:  d.InviteTTL,
	}
}

// ----------------------------------------------------------------------------
// Organizations
// ----------------------------------------------------------------------------

// CreateOrganization creates an organization and makes the caller its owner.
// The org row, the owner membership, and the created event are written in one
// transaction scoped to the new org id so RLS WITH CHECK is satisfied.
func (s *Service) CreateOrganization(ctx context.Context, userID string, req CreateOrganizationRequest) (*Organization, error) {
	name := strings.TrimSpace(req.Name)
	if err := validateOrgName(name); err != nil {
		return nil, err
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if !slugPattern.MatchString(slug) {
		return nil, apperrors.Validation("slug must be 2-64 chars of lowercase letters, digits, or hyphens")
	}

	org := &Organization{Name: name, Slug: slug, Plan: "free", Status: OrgStatusActive}
	org.ID = uuid.NewString()

	err := s.tenant.WithTenant(ctx, org.ID, func(ctx context.Context) error {
		if err := s.orgs.Create(ctx, org); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errSlugTaken
			}
			return err
		}
		owner := &Member{UserID: userID, Role: RoleOwner, Status: MemberStatusActive}
		owner.OrgID = org.ID
		if err := s.members.Create(ctx, owner); err != nil {
			return err
		}
		return s.enqueue(ctx, EventOrganizationCreated, org.ID, orgCreatedPayload{
			Name: org.Name, Slug: org.Slug, OwnerID: userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "organization", ID: org.ID}))
	})
	if err != nil {
		return nil, err
	}
	return org, nil
}

// ListOrganizations returns organizations the caller belongs to.
func (s *Service) ListOrganizations(ctx context.Context, userID string, page database.PageRequest) (database.Page[Organization], error) {
	return s.orgs.ListByUser(ctx, userID, page)
}

// GetOrganization returns an organization the caller belongs to.
func (s *Service) GetOrganization(ctx context.Context, userID, orgID string) (*Organization, error) {
	if _, err := s.loadMember(ctx, orgID, userID); err != nil {
		return nil, err
	}
	var org *Organization
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		o, err := s.orgs.GetByID(ctx, orgID)
		if err != nil {
			return err
		}
		org = o
		return nil
	})
	return org, err
}

// GetOrganizationBySlug returns an organization by slug if the caller belongs to it.
func (s *Service) GetOrganizationBySlug(ctx context.Context, userID, slug string) (*Organization, error) {
	org, err := s.orgs.GetBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	// Verify caller is a member
	if _, err := s.loadMember(ctx, org.ID, userID); err != nil {
		return nil, err
	}
	return org, nil
}

// UpdateOrganization updates mutable organization fields. Requires owner.
func (s *Service) UpdateOrganization(ctx context.Context, userID, orgID string, req UpdateOrganizationRequest) (*Organization, error) {
	if _, err := s.authorize(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return nil, err
	}

	var org *Organization
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		o, err := s.orgs.GetByID(ctx, orgID)
		if err != nil {
			return err
		}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if err := validateOrgName(name); err != nil {
				return err
			}
			o.Name = name
		}
		if req.Plan != nil {
			o.Plan = strings.TrimSpace(*req.Plan)
		}
		if err := s.orgs.Update(ctx, o); err != nil {
			return err
		}
		org = o
		return s.enqueue(ctx, EventOrganizationUpdated, orgID, orgUpdatedPayload{
			Name: o.Name, Plan: o.Plan,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "organization", ID: orgID}))
	})
	if err != nil {
		return nil, err
	}
	return org, nil
}

// DeleteOrganization deletes an organization. Requires owner. Fails if the org
// still has projects (FK restrict), surfaced as a conflict.
func (s *Service) DeleteOrganization(ctx context.Context, userID, orgID string) error {
	if _, err := s.authorize(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return err
	}
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.orgs.Delete(ctx, orgID); err != nil {
			return err
		}
		return s.enqueue(ctx, EventOrganizationDeleted, orgID, orgDeletedPayload{
			DeletedBy: userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "organization", ID: orgID}))
	})
}

// ----------------------------------------------------------------------------
// Memberships
// ----------------------------------------------------------------------------

// ListMembers returns a page of the organization's members.
func (s *Service) ListMembers(ctx context.Context, userID, orgID string, page database.PageRequest) (database.Page[Member], error) {
	if _, err := s.loadMember(ctx, orgID, userID); err != nil {
		return database.Page[Member]{}, err
	}
	var out database.Page[Member]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.members.List(ctx, page)
		return err
	})
	return out, err
}

// InviteMember creates a pending invitation and emits an invited event carrying
// the plaintext token for out-of-band delivery. Requires members:manage.
func (s *Service) InviteMember(ctx context.Context, userID, orgID string, req InviteMemberRequest) (*Invitation, error) {
	if _, err := s.authorize(ctx, orgID, userID, authz.ActionManageMembers); err != nil {
		return nil, err
	}
	email := normalizeEmail(req.Email)
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if !req.Role.AssignableByInvite() {
		return nil, apperrors.Validation("role must be one of admin, developer, viewer")
	}

	token, err := generateInviteToken()
	if err != nil {
		return nil, err
	}
	inv := &Invitation{
		Email:     email,
		Role:      req.Role,
		TokenHash: hashToken(token),
		Status:    InviteStatusPending,
		InvitedBy: &userID,
		ExpiresAt: s.now().Add(s.inviteTTL),
	}
	inv.OrgID = orgID

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.invites.Create(ctx, inv); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errInviteExists
			}
			return err
		}
		// deliveryRef is the invitation id: a non-secret reference the
		// Notification Service exchanges for the sealed token via internal API.
		return s.enqueue(ctx, EventMemberInvited, orgID, memberInvitedPayload{
			InvitationID: inv.ID, Email: inv.Email, Role: inv.Role,
			InvitedBy: userID, ExpiresAt: inv.ExpiresAt, DeliveryRef: inv.ID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "invitation", ID: inv.ID}))
	})
	if err != nil {
		return nil, err
	}

	s.notifyInvite(ctx, InviteDelivery{
		OrgID: orgID, InvitationID: inv.ID, Email: inv.Email, Role: inv.Role,
		Token: token, DeliveryRef: inv.ID,
	})
	return inv, nil
}

// AcceptInvite consumes an invitation token and creates the caller's membership.
// The token is the capability; the lookup is cross-tenant by design, after which
// all writes are scoped to the invitation's organization.
func (s *Service) AcceptInvite(ctx context.Context, caller Identity, token string) (*Member, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errInvalidToken
	}
	inv, err := s.invites.GetByTokenHash(ctx, hashToken(token))
	if err != nil {
		if database.IsNotFound(err) {
			return nil, errInvalidToken
		}
		return nil, err
	}
	if inv.Status != InviteStatusPending || !inv.ExpiresAt.After(s.now()) {
		return nil, errInviteNotUsable
	}
	if normalizeEmail(inv.Email) != normalizeEmail(caller.Email) {
		return nil, errInviteEmail
	}

	member := &Member{UserID: caller.UserID, Role: inv.Role, Status: MemberStatusActive, InvitedBy: inv.InvitedBy}
	member.OrgID = inv.OrgID

	err = s.tenant.WithTenant(ctx, inv.OrgID, func(ctx context.Context) error {
		if _, err := s.members.GetByUser(ctx, caller.UserID); err == nil {
			return errMemberExists
		} else if !database.IsNotFound(err) {
			return err
		}
		if err := s.members.Create(ctx, member); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errMemberExists
			}
			return err
		}
		if err := s.invites.UpdateStatus(ctx, inv.ID, InviteStatusAccepted, &caller.UserID); err != nil {
			return err
		}
		return s.enqueue(ctx, EventInvitationAccepted, inv.OrgID, invitationAcceptedPayload{
			InvitationID: inv.ID, UserID: caller.UserID, Role: inv.Role,
		}, events.WithActor(events.Actor{Type: "user", ID: caller.UserID}),
			events.WithResource(events.Resource{Type: "invitation", ID: inv.ID}))
	})
	if err != nil {
		return nil, err
	}
	return member, nil
}

// ListInvitations returns a page of the organization's invitations.
func (s *Service) ListInvitations(ctx context.Context, userID, orgID string, page database.PageRequest) (database.Page[Invitation], error) {
	if _, err := s.authorize(ctx, orgID, userID, authz.ActionManageMembers); err != nil {
		return database.Page[Invitation]{}, err
	}
	var out database.Page[Invitation]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.invites.List(ctx, page)
		return err
	})
	return out, err
}

// RevokeInvitation marks a pending invitation revoked. Requires members:manage.
func (s *Service) RevokeInvitation(ctx context.Context, userID, orgID, invitationID string) error {
	if _, err := s.authorize(ctx, orgID, userID, authz.ActionManageMembers); err != nil {
		return err
	}
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		inv, err := s.invites.GetByID(ctx, invitationID)
		if err != nil {
			return err
		}
		if inv.Status != InviteStatusPending {
			return nil // already terminal; revoke is idempotent
		}
		if err := s.invites.UpdateStatus(ctx, invitationID, InviteStatusRevoked, nil); err != nil {
			return err
		}
		return s.enqueue(ctx, EventInvitationRevoked, orgID, invitationRevokedPayload{
			InvitationID: invitationID, RevokedBy: userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "invitation", ID: invitationID}))
	})
}

// RemoveMember removes a member from the organization. Requires members:manage.
// Owners may only be removed by an owner, and the last owner cannot be removed.
func (s *Service) RemoveMember(ctx context.Context, userID, orgID, targetUserID string) error {
	callerRole, err := s.authorize(ctx, orgID, userID, authz.ActionManageMembers)
	if err != nil {
		return err
	}
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		target, err := s.members.GetByUser(ctx, targetUserID)
		if err != nil {
			if database.IsNotFound(err) {
				return apperrors.NotFound("member not found")
			}
			return err
		}
		if target.Role == RoleOwner {
			if callerRole != RoleOwner {
				return errOwnerOnly
			}
			owners, err := s.members.CountByRole(ctx, RoleOwner)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return errLastOwner
			}
		}
		if err := s.members.Delete(ctx, targetUserID); err != nil {
			return err
		}
		return s.enqueue(ctx, EventMemberRemoved, orgID, memberRemovedPayload{
			UserID: targetUserID, RemovedBy: userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "member", ID: targetUserID}))
	})
}

// ChangeRole changes a member's role. Requires members:manage. Only an owner may
// grant or modify the owner role, and the last owner cannot be demoted.
func (s *Service) ChangeRole(ctx context.Context, userID, orgID, targetUserID string, newRole Role) (*Member, error) {
	if !newRole.Valid() {
		return nil, apperrors.Validation("invalid role")
	}
	callerRole, err := s.authorize(ctx, orgID, userID, authz.ActionManageMembers)
	if err != nil {
		return nil, err
	}

	var updated *Member
	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		target, err := s.members.GetByUser(ctx, targetUserID)
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
		if (newRole == RoleOwner || oldRole == RoleOwner) && callerRole != RoleOwner {
			return errOwnerOnly
		}
		if oldRole == RoleOwner && newRole != RoleOwner {
			owners, err := s.members.CountByRole(ctx, RoleOwner)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return errLastOwner
			}
		}
		if err := s.members.UpdateRole(ctx, targetUserID, newRole); err != nil {
			return err
		}
		target.Role = newRole
		updated = target
		return s.enqueue(ctx, EventRoleChanged, orgID, roleChangedPayload{
			UserID: targetUserID, OldRole: oldRole, NewRole: newRole, ChangedBy: userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "member", ID: targetUserID}))
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ----------------------------------------------------------------------------
// Validation helpers
// ----------------------------------------------------------------------------

func validateOrgName(name string) error {
	if l := len(name); l < 2 || l > 64 {
		return apperrors.Validation("name must be between 2 and 64 characters")
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateEmail(email string) error {
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || !strings.Contains(email[at+1:], ".") {
		return apperrors.Validation("a valid email address is required")
	}
	return nil
}
