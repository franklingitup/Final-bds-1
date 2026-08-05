// Package auth provides JWT and service account token validation for the API Gateway.
package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/bdsplatform/platform/backend/libs/config"
)

// jwtIssuer must match the issuer used by the auth service when minting tokens.
const jwtIssuer = "bdsplatform-auth"

// TokenType distinguishes between user access tokens and service account tokens.
type TokenType string

const (
	TokenTypeUser           TokenType = "user"
	TokenTypeServiceAccount TokenType = "service_account"
)

// Identity represents an authenticated caller extracted from a token.
type Identity struct {
	Type      TokenType
	UserID    string
	Email     string
	OrgID     string   // Organization scope (for service accounts)
	Scopes    []string // Permission scopes (for service accounts)
	IssuedAt  int64
	JTI       string // Token ID (RFC 7519 "jti"); used for revocation checks.
	SessionID string // Owning refresh session ("sid"); empty for service accounts.
}

// HasScope checks if the identity has a specific permission scope.
func (i Identity) HasScope(scope string) bool {
	for _, s := range i.Scopes {
		if s == scope || s == "*" {
			return true
		}
	}
	return false
}

// accessClaims mirrors the claims minted by the auth service for user tokens.
type accessClaims struct {
	Email string `json:"email"`
	// SID is the owning refresh session; the gateway checks it against the
	// revocation store so logout/refresh invalidate this token before expiry.
	SID string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

// serviceAccountClaims are claims for service account tokens.
type serviceAccountClaims struct {
	OrgID  string   `json:"org_id"`
	Scopes []string `json:"scopes"`
	jwt.RegisteredClaims
}

// Validator validates access tokens and service account tokens.
type Validator struct {
	key []byte
}

// NewValidator creates a token validator with the shared signing key.
func NewValidator(cfg config.AuthConfig) *Validator {
	return &Validator{key: []byte(cfg.JWTSigningKey)}
}

// ValidateUserToken parses and validates a user access token.
func (v *Validator) ValidateUserToken(token string) (Identity, error) {
	claims := &accessClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, v.keyFunc,
		jwt.WithIssuer(jwtIssuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return Identity{}, ErrInvalidToken
	}

	iat, _ := claims.GetIssuedAt()
	issuedAt := int64(0)
	if iat != nil {
		issuedAt = iat.Unix()
	}

	return Identity{
		Type:      TokenTypeUser,
		UserID:    claims.Subject,
		Email:     claims.Email,
		IssuedAt:  issuedAt,
		JTI:       claims.ID,
		SessionID: claims.SID,
	}, nil
}

// ValidateServiceAccountToken parses and validates a service account token.
func (v *Validator) ValidateServiceAccountToken(token string) (Identity, error) {
	claims := &serviceAccountClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, v.keyFunc,
		jwt.WithIssuer(jwtIssuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return Identity{}, ErrInvalidToken
	}

	iat, _ := claims.GetIssuedAt()
	issuedAt := int64(0)
	if iat != nil {
		issuedAt = iat.Unix()
	}

	return Identity{
		Type:     TokenTypeServiceAccount,
		UserID:   claims.Subject,
		OrgID:    claims.OrgID,
		Scopes:   claims.Scopes,
		IssuedAt: issuedAt,
		JTI:      claims.ID,
	}, nil
}

// ValidateToken attempts to validate a token, trying user token first then service account.
func (v *Validator) ValidateToken(token string) (Identity, error) {
	// Try user token first.
	if id, err := v.ValidateUserToken(token); err == nil {
		return id, nil
	}

	// Try service account token.
	if id, err := v.ValidateServiceAccountToken(token); err == nil {
		return id, nil
	}

	return Identity{}, ErrInvalidToken
}

func (v *Validator) keyFunc(t *jwt.Token) (any, error) {
	if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
		return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
	}
	return v.key, nil
}

// ExtractBearerToken extracts the token from an Authorization header.
func ExtractBearerToken(authHeader string) string {
	const prefix = "Bearer "
	if len(authHeader) <= len(prefix) || !strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(authHeader[len(prefix):])
}

// ContextKey is the type for context keys used by auth.
type ContextKey string

const (
	// IdentityKey is the context key for the authenticated identity.
	IdentityKey ContextKey = "gateway_identity"
)

// WithIdentity stores the identity in the context.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, IdentityKey, id)
}

// IdentityFromContext retrieves the identity from the context.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(IdentityKey).(Identity)
	return id, ok
}
