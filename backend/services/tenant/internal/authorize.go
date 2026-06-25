package tenant

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
)

// loadMember loads the caller's membership in orgID under the tenant scope so
// RLS confirms the row belongs to that org. A non-member is rejected.
func (s *Service) loadMember(ctx context.Context, orgID, userID string) (*Member, error) {
	var member *Member
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		m, err := s.members.GetByUser(ctx, userID)
		if err != nil {
			if database.IsNotFound(err) {
				return errNotMember
			}
			return err
		}
		member = m
		return nil
	})
	if err != nil {
		return nil, err
	}
	return member, nil
}

// authorize confirms the caller is a member of orgID and that their role grants
// the requested action, delegating the capability decision to libs/authz. It
// returns the caller's role so callers can apply additional business rules
// (e.g. owner-only operations).
func (s *Service) authorize(ctx context.Context, orgID, userID string, action authz.Action) (Role, error) {
	member, err := s.loadMember(ctx, orgID, userID)
	if err != nil {
		return "", err
	}
	principal := authz.Principal{
		UserID:   userID,
		OrgID:    orgID,
		OrgRoles: []authz.OrgRole{member.Role.toAuthzRole()},
	}
	if err := s.authorizer.Authorize(ctx, principal, authz.AccessRequest{Action: action, OrgID: orgID}); err != nil {
		return member.Role, err
	}
	return member.Role, nil
}
