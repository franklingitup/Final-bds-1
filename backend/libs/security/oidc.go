package security

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OIDCProvider handles OpenID Connect authentication.
type OIDCProvider struct {
	config     OIDCConfig
	httpClient *http.Client
	jwks       *JWKS
	jwksMu     sync.RWMutex
}

// OIDCConfig configures an OIDC provider.
type OIDCConfig struct {
	// Provider identification
	ProviderID   string
	ProviderName string

	// Discovery
	Issuer       string // e.g., https://accounts.google.com
	DiscoveryURL string // Defaults to Issuer + /.well-known/openid-configuration

	// Client credentials
	ClientID     string
	ClientSecret string

	// Redirect configuration
	RedirectURI string

	// Scopes to request
	Scopes []string

	// Optional: override discovered endpoints
	AuthorizationEndpoint string
	TokenEndpoint         string
	UserInfoEndpoint      string
	JWKSEndpoint          string

	// Token verification
	SkipIssuerCheck   bool
	SkipExpiryCheck   bool
	InsecureSkipNonce bool
}

// OIDCDiscovery represents the OpenID Connect discovery document.
type OIDCDiscovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
	ResponseTypesSupported []string `json:"response_types_supported"`
	ClaimsSupported       []string `json:"claims_supported"`
}

// OIDCTokenResponse represents the token endpoint response.
type OIDCTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// OIDCClaims represents standard OIDC claims.
type OIDCClaims struct {
	// Standard claims
	Subject       string `json:"sub"`
	Issuer        string `json:"iss"`
	Audience      string `json:"aud"`
	Expiry        int64  `json:"exp"`
	IssuedAt      int64  `json:"iat"`
	Nonce         string `json:"nonce,omitempty"`
	AuthTime      int64  `json:"auth_time,omitempty"`

	// Profile claims
	Name              string `json:"name,omitempty"`
	GivenName         string `json:"given_name,omitempty"`
	FamilyName        string `json:"family_name,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Picture           string `json:"picture,omitempty"`
	Email             string `json:"email,omitempty"`
	EmailVerified     bool   `json:"email_verified,omitempty"`

	// Additional claims
	Groups []string `json:"groups,omitempty"`
	Roles  []string `json:"roles,omitempty"`

	// Raw claims for custom processing
	Raw map[string]interface{} `json:"-"`
}

// JWKS represents a JSON Web Key Set.
type JWKS struct {
	Keys      []JSONWebKey `json:"keys"`
	ExpiresAt time.Time
}

// JSONWebKey represents a JSON Web Key.
type JSONWebKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n,omitempty"`   // RSA modulus
	E   string `json:"e,omitempty"`   // RSA exponent
	Crv string `json:"crv,omitempty"` // EC curve
	X   string `json:"x,omitempty"`   // EC x coordinate
	Y   string `json:"y,omitempty"`   // EC y coordinate
}

// AuthorizationState represents the state for an auth request.
type AuthorizationState struct {
	State       string
	Nonce       string
	RedirectURI string
	ProviderID  string
	OrgID       string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// NewOIDCProvider creates a new OIDC provider.
func NewOIDCProvider(cfg OIDCConfig) *OIDCProvider {
	if cfg.DiscoveryURL == "" && cfg.Issuer != "" {
		cfg.DiscoveryURL = strings.TrimSuffix(cfg.Issuer, "/") + "/.well-known/openid-configuration"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "email", "profile"}
	}

	return &OIDCProvider{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Discover fetches the OIDC discovery document.
func (p *OIDCProvider) Discover(ctx context.Context) (*OIDCDiscovery, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.DiscoveryURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery failed with status: %d", resp.StatusCode)
	}

	var discovery OIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("decode discovery: %w", err)
	}

	// Update config with discovered endpoints
	if p.config.AuthorizationEndpoint == "" {
		p.config.AuthorizationEndpoint = discovery.AuthorizationEndpoint
	}
	if p.config.TokenEndpoint == "" {
		p.config.TokenEndpoint = discovery.TokenEndpoint
	}
	if p.config.UserInfoEndpoint == "" {
		p.config.UserInfoEndpoint = discovery.UserInfoEndpoint
	}
	if p.config.JWKSEndpoint == "" {
		p.config.JWKSEndpoint = discovery.JWKSURI
	}

	return &discovery, nil
}

// GenerateAuthorizationURL creates the authorization URL.
func (p *OIDCProvider) GenerateAuthorizationURL(state, nonce string) (string, error) {
	u, err := url.Parse(p.config.AuthorizationEndpoint)
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.config.ClientID)
	q.Set("redirect_uri", p.config.RedirectURI)
	q.Set("scope", strings.Join(p.config.Scopes, " "))
	q.Set("state", state)
	if nonce != "" {
		q.Set("nonce", nonce)
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// ExchangeCode exchanges an authorization code for tokens.
func (p *OIDCProvider) ExchangeCode(ctx context.Context, code string) (*OIDCTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", p.config.RedirectURI)
	data.Set("client_id", p.config.ClientID)
	data.Set("client_secret", p.config.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.TokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token request failed: %d - %s", resp.StatusCode, string(body))
	}

	var tokenResp OIDCTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	return &tokenResp, nil
}

// RefreshToken refreshes an access token.
func (p *OIDCProvider) RefreshToken(ctx context.Context, refreshToken string) (*OIDCTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", p.config.ClientID)
	data.Set("client_secret", p.config.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.TokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("refresh failed: %d - %s", resp.StatusCode, string(body))
	}

	var tokenResp OIDCTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}

	return &tokenResp, nil
}

// GetUserInfo fetches user information from the userinfo endpoint.
func (p *OIDCProvider) GetUserInfo(ctx context.Context, accessToken string) (*OIDCClaims, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.UserInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo failed: %d - %s", resp.StatusCode, string(body))
	}

	var claims OIDCClaims
	var raw map[string]interface{}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode raw userinfo: %w", err)
	}
	claims.Raw = raw

	return &claims, nil
}

// FetchJWKS fetches and caches the JSON Web Key Set.
func (p *OIDCProvider) FetchJWKS(ctx context.Context) (*JWKS, error) {
	p.jwksMu.RLock()
	if p.jwks != nil && time.Now().Before(p.jwks.ExpiresAt) {
		defer p.jwksMu.RUnlock()
		return p.jwks, nil
	}
	p.jwksMu.RUnlock()

	p.jwksMu.Lock()
	defer p.jwksMu.Unlock()

	// Double-check after acquiring write lock
	if p.jwks != nil && time.Now().Before(p.jwks.ExpiresAt) {
		return p.jwks, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.JWKSEndpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jwks request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks request failed: %d", resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}

	// Cache for 1 hour
	jwks.ExpiresAt = time.Now().Add(time.Hour)
	p.jwks = &jwks

	return &jwks, nil
}

// GenerateState generates a cryptographically secure state parameter.
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// GenerateNonce generates a cryptographically secure nonce.
func GenerateNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// OIDCProviderRegistry manages multiple OIDC providers.
type OIDCProviderRegistry struct {
	providers map[string]*OIDCProvider
	mu        sync.RWMutex
}

// NewOIDCProviderRegistry creates a new registry.
func NewOIDCProviderRegistry() *OIDCProviderRegistry {
	return &OIDCProviderRegistry{
		providers: make(map[string]*OIDCProvider),
	}
}

// Register adds a provider to the registry.
func (r *OIDCProviderRegistry) Register(provider *OIDCProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[provider.config.ProviderID] = provider
}

// Get retrieves a provider by ID.
func (r *OIDCProviderRegistry) Get(providerID string) (*OIDCProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	return p, ok
}

// List returns all registered provider IDs.
func (r *OIDCProviderRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

// CommonOIDCProviders returns configurations for common providers.
func CommonOIDCProviders() map[string]OIDCConfig {
	return map[string]OIDCConfig{
		"google": {
			ProviderID:   "google",
			ProviderName: "Google",
			Issuer:       "https://accounts.google.com",
			Scopes:       []string{"openid", "email", "profile"},
		},
		"microsoft": {
			ProviderID:   "microsoft",
			ProviderName: "Microsoft",
			Issuer:       "https://login.microsoftonline.com/common/v2.0",
			Scopes:       []string{"openid", "email", "profile"},
		},
		"okta": {
			ProviderID:   "okta",
			ProviderName: "Okta",
			// Issuer must be set per-tenant: https://{domain}.okta.com
			Scopes: []string{"openid", "email", "profile", "groups"},
		},
		"auth0": {
			ProviderID:   "auth0",
			ProviderName: "Auth0",
			// Issuer must be set per-tenant: https://{domain}.auth0.com
			Scopes: []string{"openid", "email", "profile"},
		},
	}
}
