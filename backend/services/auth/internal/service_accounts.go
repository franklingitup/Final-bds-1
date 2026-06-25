package auth

import (
	"context"
	"strings"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// APITokenResult is returned when an API token is created. Token is the
// plaintext credential and is shown exactly once.
type APITokenResult struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	Token     string     `json:"token"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// CreateServiceAccount creates an org-scoped machine identity.
// SECURITY: Requires org membership with admin privileges.
func (s *Service) CreateServiceAccount(ctx context.Context, orgID, creatorUserID string, req CreateServiceAccountRequest) (*ServiceAccount, error) {
	// SECURITY: Verify caller has org admin privileges
	if s.authSvc != nil {
		if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, creatorUserID, authz.ActionManageOrg); err != nil {
			return nil, err
		}
	}

	if strings.TrimSpace(req.Name) == "" {
		return nil, apperrors.Validation("name is required")
	}
	sa := &ServiceAccount{
		Name:      strings.TrimSpace(req.Name),
		Status:    "active",
		CreatedBy: optionalString(creatorUserID),
	}
	sa.OrgID = orgID
	if d := strings.TrimSpace(req.Description); d != "" {
		sa.Description = &d
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.serviceAccounts.Create(ctx, sa); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errNameTaken
			}
			return err
		}
		return s.enqueue(ctx, EventServiceAccountCreated, orgID, serviceAccountCreatedPayload{
			ServiceAccountID: sa.ID, Name: sa.Name, CreatedBy: creatorUserID,
		}, events.WithActor(events.Actor{Type: "user", ID: creatorUserID}),
			events.WithResource(events.Resource{Type: "service_account", ID: sa.ID}))
	})
	if err != nil {
		return nil, err
	}
	return sa, nil
}

// ListServiceAccounts returns a page of the org's service accounts.
// SECURITY: Requires org membership to list service accounts.
func (s *Service) ListServiceAccounts(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[ServiceAccount], error) {
	// SECURITY: Verify caller is org member
	if s.authSvc != nil {
		if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
			return database.Page[ServiceAccount]{}, err
		}
	}

	var out database.Page[ServiceAccount]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.serviceAccounts.List(ctx, page)
		return err
	})
	return out, err
}

// DeleteServiceAccount removes a service account (and cascades its API tokens).
// SECURITY: Requires org membership with admin privileges.
func (s *Service) DeleteServiceAccount(ctx context.Context, orgID, userID, id string) error {
	// SECURITY: Verify caller has org admin privileges
	if s.authSvc != nil {
		if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
			return err
		}
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.serviceAccounts.Delete(ctx, id); err != nil {
			return err
		}
		return s.enqueue(ctx, EventServiceAccountDeleted, orgID, serviceAccountDeletedPayload{
			ServiceAccountID: id,
		}, events.WithResource(events.Resource{Type: "service_account", ID: id}))
	})
}

// CreateAPIToken issues a scoped API token for a service account.
// The token is a signed JWT that can be validated by the Gateway without
// requiring a database lookup.
// SECURITY: Requires org membership with admin privileges.
func (s *Service) CreateAPIToken(ctx context.Context, orgID, userID, serviceAccountID string, req CreateAPITokenRequest) (*APITokenResult, error) {
	// SECURITY: Verify caller has org admin privileges
	if s.authSvc != nil {
		if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
			return nil, err
		}
	}

	if strings.TrimSpace(req.Name) == "" {
		return nil, apperrors.Validation("name is required")
	}

	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		t := s.now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}

	scopes := scopesOrEmpty(req.Scopes)

	// Issue a JWT token instead of an opaque token
	jwtToken, jti, err := s.jwt.IssueServiceAccountToken(serviceAccountID, orgID, scopes, expiresAt)
	if err != nil {
		return nil, err
	}

	// Store only the JTI (for revocation) and metadata
	prefix := "jwt_" + jti[:8]
	token := &APIToken{
		ServiceAccountID: serviceAccountID,
		Name:             strings.TrimSpace(req.Name),
		Prefix:           prefix,
		TokenHash:        jti, // Store JTI for revocation tracking
		Scopes:           scopes,
		ExpiresAt:        expiresAt,
	}
	token.OrgID = orgID

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Ensure the service account exists within this org (RLS-scoped).
		if _, err := s.serviceAccounts.GetByID(ctx, serviceAccountID); err != nil {
			if database.IsNotFound(err) {
				return apperrors.NotFound("service account not found")
			}
			return err
		}
		if err := s.apiTokens.Create(ctx, token); err != nil {
			return err
		}
		return s.enqueue(ctx, EventAPITokenCreated, orgID, apiTokenCreatedPayload{
			APITokenID: token.ID, ServiceAccountID: serviceAccountID, Name: token.Name,
		}, events.WithResource(events.Resource{Type: "api_token", ID: token.ID}))
	})
	if err != nil {
		return nil, err
	}

	return &APITokenResult{
		ID:        token.ID,
		Name:      token.Name,
		Prefix:    token.Prefix,
		Token:     jwtToken, // Return the JWT
		Scopes:    token.Scopes,
		ExpiresAt: token.ExpiresAt,
		CreatedAt: token.CreatedAt,
	}, nil
}

// ListAPITokens returns a page of the org's API tokens (no secrets).
// SECURITY: Requires org membership to list API tokens.
func (s *Service) ListAPITokens(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[APIToken], error) {
	// SECURITY: Verify caller is org member
	if s.authSvc != nil {
		if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
			return database.Page[APIToken]{}, err
		}
	}

	var out database.Page[APIToken]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.apiTokens.ListByOrg(ctx, page)
		return err
	})
	return out, err
}

// RevokeAPIToken revokes an API token.
// SECURITY: Requires org membership with admin privileges.
func (s *Service) RevokeAPIToken(ctx context.Context, orgID, userID, id string) error {
	// SECURITY: Verify caller has org admin privileges
	if s.authSvc != nil {
		if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
			return err
		}
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.apiTokens.Revoke(ctx, id); err != nil {
			return err
		}
		return s.enqueue(ctx, EventAPITokenRevoked, orgID, apiTokenRevokedPayload{APITokenID: id},
			events.WithResource(events.Resource{Type: "api_token", ID: id}))
	})
}
