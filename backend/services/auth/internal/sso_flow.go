package auth

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

const (
	ssoLoginStateTTL   = 5 * time.Minute
	ssoExchangeCodeTTL = time.Minute
)

// SSOExchangeRequest is submitted by the frontend callback after the ACS
// redirects it with a short-lived one-time code.
type SSOExchangeRequest struct {
	OrgID string `json:"orgId"`
	Code  string `json:"code"`
}

// StartSSOLogin builds a signed HTTP-Redirect AuthnRequest for an enabled
// organization and persists its request ID behind one-time RelayState.
func (s *Service) StartSSOLogin(ctx context.Context, orgSlug string) (*url.URL, error) {
	if s.ssoOrganizations == nil || s.ssoConfigs == nil || s.ssoProviders == nil || s.ssoHandoffs == nil {
		return nil, apperrors.Internal("SSO is not configured")
	}
	orgSlug = strings.TrimSpace(orgSlug)
	if orgSlug == "" {
		return nil, apperrors.Validation("organization slug is required")
	}

	orgID, err := s.ssoOrganizations.GetIDBySlug(ctx, orgSlug)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, apperrors.NotFound("SSO configuration not found")
		}
		return nil, err
	}
	cfg, err := s.getEnabledSSOConfig(ctx, orgID)
	if err != nil {
		return nil, err
	}
	sp, err := s.ssoProviders.Get(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build SSO provider: %w", err)
	}

	idpURL := sp.GetSSOBindingLocation(saml.HTTPRedirectBinding)
	if idpURL == "" {
		return nil, apperrors.Internal("identity provider does not support SAML HTTP-Redirect binding")
	}
	authnRequest, err := sp.MakeAuthenticationRequest(idpURL, saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return nil, fmt.Errorf("build SAML authentication request: %w", err)
	}
	relayState, err := generateSecret(32)
	if err != nil {
		return nil, err
	}
	state := &SSOLoginState{
		OrgID:     orgID,
		StateHash: hashToken(relayState),
		RequestID: authnRequest.ID,
		ExpiresAt: s.now().Add(ssoLoginStateTTL),
	}
	if err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.ssoHandoffs.CreateLoginState(ctx, state)
	}); err != nil {
		return nil, err
	}

	redirectURL, err := authnRequest.Redirect(relayState, sp)
	if err != nil {
		return nil, fmt.Errorf("sign SAML authentication request: %w", err)
	}
	return redirectURL, nil
}

// CompleteSSOLogin validates a SAML response, JIT-provisions the user and
// organization membership, then stores the resolved user ID behind a short-lived
// one-time browser exchange code. Tokens are minted later at exchange.
func (s *Service) CompleteSSOLogin(
	ctx context.Context,
	orgID, samlResponse, relayState string,
) (*url.URL, error) {
	if s.ssoMembers == nil || s.ssoHandoffs == nil {
		return nil, apperrors.Internal("SSO is not configured")
	}
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(samlResponse) == "" || strings.TrimSpace(relayState) == "" {
		return nil, apperrors.Unauthorized("authentication failed")
	}

	cfg, err := s.getEnabledSSOConfig(ctx, orgID)
	if err != nil {
		return nil, err
	}
	sp, err := s.ssoProviders.Get(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build SSO provider: %w", err)
	}

	var loginState *SSOLoginState
	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var consumeErr error
		loginState, consumeErr = s.ssoHandoffs.ConsumeLoginState(ctx, orgID, hashToken(relayState), s.now())
		return consumeErr
	})
	if err != nil {
		return nil, err
	}

	form := url.Values{
		"SAMLResponse": {samlResponse},
		"RelayState":   {relayState},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sp.AcsURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		return nil, apperrors.Unauthorized("authentication failed")
	}

	// ParseResponse performs signature verification, request correlation,
	// destination/recipient checks, assertion validity-window checks, and
	// audience restriction validation using the org-specific ServiceProvider.
	assertion, err := sp.ParseResponse(req, []string{loginState.RequestID})
	if err != nil {
		s.log.WarnContext(ctx, "SAML response validation failed",
			"org_id", orgID,
			"error", err)
		return nil, apperrors.Unauthorized("authentication failed")
	}

	email := normalizeEmail(samlAttribute(assertion, cfg.AttributeEmail))
	if err := validateEmail(email); err != nil {
		return nil, apperrors.Unauthorized("SAML assertion did not contain a valid email")
	}
	name := strings.TrimSpace(strings.Join([]string{
		samlAttribute(assertion, cfg.AttributeFirstName),
		samlAttribute(assertion, cfg.AttributeLastName),
	}, " "))
	if name == "" {
		name = email
	}

	user, err := s.findOrCreateSSOUser(ctx, email, name)
	if err != nil {
		return nil, err
	}
	if user.Status == UserStatusDisabled {
		return nil, apperrors.Unauthorized("authentication failed")
	}

	role, err := ssoOrgRole(cfg.DefaultRole)
	if err != nil {
		return nil, err
	}
	if err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.ssoMembers.Ensure(ctx, user.ID, role)
	}); err != nil {
		return nil, err
	}

	code, err := generateSecret(32)
	if err != nil {
		return nil, err
	}
	exchange := &SSOExchangeCode{
		OrgID:     orgID,
		UserID:    user.ID,
		CodeHash:  hashToken(code),
		ExpiresAt: s.now().Add(ssoExchangeCodeTTL),
	}
	if err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.ssoHandoffs.CreateExchangeCode(ctx, exchange)
	}); err != nil {
		return nil, err
	}

	if strings.TrimSpace(s.ssoRedirectURL) == "" {
		return nil, apperrors.Internal("SSO browser redirect is not configured")
	}
	redirectURL, err := url.Parse(s.ssoRedirectURL)
	if err != nil {
		return nil, apperrors.Internal("SSO browser redirect is invalid")
	}
	query := redirectURL.Query()
	query.Set("code", code)
	query.Set("orgId", orgID)
	redirectURL.RawQuery = query.Encode()
	return redirectURL, nil
}

// ExchangeSSOCode atomically consumes a one-time code and mints a fresh token
// pair for the bound user, the same way Login does.
func (s *Service) ExchangeSSOCode(ctx context.Context, req SSOExchangeRequest, meta RequestMeta) (*TokenPair, error) {
	if s.ssoHandoffs == nil {
		return nil, apperrors.Internal("SSO is not configured")
	}
	if strings.TrimSpace(req.OrgID) == "" || strings.TrimSpace(req.Code) == "" {
		return nil, apperrors.Validation("orgId and code are required")
	}

	var exchange *SSOExchangeCode
	err := s.tenant.WithTenant(ctx, req.OrgID, func(ctx context.Context) error {
		var consumeErr error
		exchange, consumeErr = s.ssoHandoffs.ConsumeExchangeCode(
			ctx, req.OrgID, hashToken(req.Code), s.now())
		return consumeErr
	})
	if err != nil {
		return nil, err
	}

	user, err := s.users.GetByID(ctx, exchange.UserID)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, apperrors.Unauthorized("invalid or expired SSO exchange code")
		}
		return nil, err
	}
	now := s.now()
	if user.IsLocked(now) {
		return nil, errAccountLocked
	}
	if user.Status == UserStatusDisabled {
		return nil, apperrors.Unauthorized("authentication failed")
	}

	var pair *TokenPair
	err = s.tx.Tx(ctx, func(ctx context.Context) error {
		var issueErr error
		pair, issueErr = s.issueTokenPair(ctx, user, true, meta)
		if issueErr != nil {
			return issueErr
		}
		return s.enqueue(ctx, EventLoginSucceeded, systemOrg, loginSucceededPayload{
			UserID: user.ID, Email: user.Email,
		}, events.WithActor(events.Actor{Type: "user", ID: user.ID}))
	})
	if err != nil {
		return nil, err
	}
	return pair, nil
}

// SSOMetadata returns this org's Service Provider metadata generated by crewjam.
func (s *Service) SSOMetadata(ctx context.Context, orgID string) ([]byte, error) {
	cfg, err := s.getEnabledSSOConfig(ctx, orgID)
	if err != nil {
		return nil, err
	}
	sp, err := s.ssoProviders.Get(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build SSO provider: %w", err)
	}
	body, err := xml.MarshalIndent(sp.Metadata(), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal SSO metadata: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

func (s *Service) getEnabledSSOConfig(ctx context.Context, orgID string) (*OrgSSOConfig, error) {
	if s.ssoConfigs == nil || s.ssoProviders == nil || strings.TrimSpace(orgID) == "" {
		return nil, apperrors.NotFound("SSO configuration not found")
	}
	var cfg *OrgSSOConfig
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var getErr error
		cfg, getErr = s.ssoConfigs.GetByOrgID(ctx, orgID)
		return getErr
	})
	if err != nil || cfg == nil || !cfg.Enabled || cfg.OrgID != orgID {
		if err != nil && !database.IsNotFound(err) {
			return nil, err
		}
		return nil, apperrors.NotFound("SSO configuration not found")
	}
	return cfg, nil
}

func (s *Service) findOrCreateSSOUser(ctx context.Context, email, name string) (*User, error) {
	user, err := s.users.GetByEmail(ctx, email)
	if err == nil {
		return user, nil
	}
	if !database.IsNotFound(err) {
		return nil, err
	}

	randomPassword, err := generateSecret(32)
	if err != nil {
		return nil, err
	}
	passwordHash, err := HashPassword(randomPassword)
	if err != nil {
		return nil, err
	}
	user = &User{
		Email:         email,
		Name:          name,
		PasswordHash:  passwordHash,
		Status:        UserStatusActive,
		EmailVerified: true,
	}
	if err := s.users.Create(ctx, user); err != nil {
		if apperrors.From(err).Code == apperrors.CodeConflict {
			return s.users.GetByEmail(ctx, email)
		}
		return nil, err
	}
	return user, nil
}

func samlAttribute(assertion *saml.Assertion, name string) string {
	name = strings.TrimSpace(name)
	if assertion == nil || name == "" {
		return ""
	}
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			if attribute.Name != name && attribute.FriendlyName != name {
				continue
			}
			for _, value := range attribute.Values {
				if v := strings.TrimSpace(value.Value); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

func ssoOrgRole(value string) (authz.OrgRole, error) {
	role := authz.OrgRole(strings.TrimSpace(value))
	switch role {
	case authz.OrgOwner, authz.OrgAdmin, authz.RoleMember, authz.OrgAuditor:
		return role, nil
	default:
		return "", apperrors.Internal("SSO default role is invalid")
	}
}
