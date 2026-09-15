package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

const (
	defaultSSOEmailAttr     = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"
	defaultSSOFirstNameAttr = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname"
	defaultSSOLastNameAttr  = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname"
)

func (s *Service) requireManageOrg(ctx context.Context, orgID, userID string) error {
	if s.authSvc != nil {
		if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
			return err
		}
	}
	return nil
}

// UpsertSSOConfig validates and saves org SAML SSO settings.
func (s *Service) UpsertSSOConfig(ctx context.Context, orgID, userID string, req UpsertSSOConfigRequest) (*SSOConfigResponse, error) {
	if err := s.requireManageOrg(ctx, orgID, userID); err != nil {
		return nil, err
	}
	if s.ssoConfigs == nil || s.ssoProviders == nil {
		return nil, apperrors.Internal("SSO is not configured")
	}

	roleValue := strings.TrimSpace(req.DefaultRole)
	if roleValue == "" {
		roleValue = string(authz.RoleMember)
	}
	role, err := ssoOrgRole(roleValue)
	if err != nil {
		return nil, apperrors.Validation("invalid defaultRole")
	}

	urlSet := req.IDPMetadataURL != nil && strings.TrimSpace(*req.IDPMetadataURL) != ""
	xmlSet := req.IDPMetadataXML != nil && strings.TrimSpace(*req.IDPMetadataXML) != ""
	if urlSet == xmlSet {
		return nil, apperrors.Validation("exactly one of idpMetadataUrl or idpMetadataXml must be provided")
	}

	draft := &OrgSSOConfig{TenantModel: database.TenantModel{OrgID: orgID}}
	if urlSet {
		u := strings.TrimSpace(*req.IDPMetadataURL)
		draft.IDPMetadataURL = &u
	} else {
		x := strings.TrimSpace(*req.IDPMetadataXML)
		draft.IDPMetadataXML = &x
	}

	meta, err := s.ssoProviders.LoadIDPMetadata(ctx, draft)
	if err != nil {
		if urlSet {
			return nil, apperrors.Validation(fmt.Sprintf("could not fetch IdP metadata from the provided URL: %s", err))
		}
		return nil, apperrors.Validation(fmt.Sprintf("could not parse IdP metadata XML: %s", err))
	}

	cfg := &OrgSSOConfig{
		TenantModel:        database.TenantModel{OrgID: orgID},
		IDPMetadataURL:     draft.IDPMetadataURL,
		IDPMetadataXML:     draft.IDPMetadataXML,
		IDPEntityID:        strings.TrimSpace(meta.EntityID),
		SPEntityID:         metadataURLString(s.ssoProviders, orgID),
		AttributeEmail:     firstNonEmpty(strings.TrimSpace(req.AttributeEmail), defaultSSOEmailAttr),
		AttributeFirstName: firstNonEmpty(strings.TrimSpace(req.AttributeFirstName), defaultSSOFirstNameAttr),
		AttributeLastName:  firstNonEmpty(strings.TrimSpace(req.AttributeLastName), defaultSSOLastNameAttr),
		DefaultRole:        string(role),
		Enabled:            req.Enabled,
	}

	if err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.ssoConfigs.Upsert(ctx, cfg)
	}); err != nil {
		return nil, err
	}

	s.ssoProviders.Invalidate(orgID)
	return s.toSSOConfigResponse(cfg), nil
}

// GetSSOConfig returns the stored org SSO config without live-fetching IdP metadata.
func (s *Service) GetSSOConfig(ctx context.Context, orgID, userID string) (*SSOConfigResponse, error) {
	if err := s.requireManageOrg(ctx, orgID, userID); err != nil {
		return nil, err
	}
	if s.ssoConfigs == nil || s.ssoProviders == nil {
		return nil, apperrors.Internal("SSO is not configured")
	}

	var cfg *OrgSSOConfig
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var getErr error
		cfg, getErr = s.ssoConfigs.GetByOrgID(ctx, orgID)
		return getErr
	})
	if err != nil {
		if database.IsNotFound(err) {
			return nil, apperrors.NotFound("SSO configuration not found")
		}
		return nil, err
	}
	return s.toSSOConfigResponse(cfg), nil
}

// DeleteSSOConfig removes the org SSO config and drops the cached ServiceProvider.
func (s *Service) DeleteSSOConfig(ctx context.Context, orgID, userID string) error {
	if err := s.requireManageOrg(ctx, orgID, userID); err != nil {
		return err
	}
	if s.ssoConfigs == nil || s.ssoProviders == nil {
		return apperrors.Internal("SSO is not configured")
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.ssoConfigs.DeleteByOrgID(ctx, orgID)
	})
	if err != nil {
		return err
	}
	s.ssoProviders.Invalidate(orgID)
	return nil
}

func (s *Service) toSSOConfigResponse(cfg *OrgSSOConfig) *SSOConfigResponse {
	spMeta := ""
	if s.ssoProviders != nil && cfg != nil {
		spMeta = metadataURLString(s.ssoProviders, cfg.OrgID)
	}
	return &SSOConfigResponse{
		OrgID:              cfg.OrgID,
		IDPMetadataURL:     cfg.IDPMetadataURL,
		IDPMetadataXML:     cfg.IDPMetadataXML,
		IDPEntityID:        cfg.IDPEntityID,
		SPEntityID:         cfg.SPEntityID,
		SPMetadataURL:      spMeta,
		AttributeEmail:     cfg.AttributeEmail,
		AttributeFirstName: cfg.AttributeFirstName,
		AttributeLastName:  cfg.AttributeLastName,
		DefaultRole:        cfg.DefaultRole,
		Enabled:            cfg.Enabled,
	}
}

func metadataURLString(providers ssoProviderRuntime, orgID string) string {
	u := providers.MetadataURL(orgID)
	return u.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
