package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/config"
)

const jwtIssuer = "bdsplatform-auth"

// AccessClaims are the claims embedded in a short-lived access token.
type AccessClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// ServiceAccountClaims are the claims embedded in a service account token.
type ServiceAccountClaims struct {
	OrgID  string   `json:"org_id"`
	Scopes []string `json:"scopes,omitempty"`
	jwt.RegisteredClaims
}

// JWTIssuer issues and verifies HS256 access tokens.
type JWTIssuer struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

// NewJWTIssuer builds an issuer from auth configuration.
func NewJWTIssuer(cfg config.AuthConfig) *JWTIssuer {
	return &JWTIssuer{key: []byte(cfg.JWTSigningKey), ttl: cfg.AccessTTL, now: time.Now}
}

// Issue mints an access token for the user, returning the signed token, its JTI
// (for revocation tracking), and its lifetime in seconds.
func (i *JWTIssuer) Issue(userID, email string) (token, jti string, expiresIn int, err error) {
	now := i.now()
	jti = uuid.NewString()
	claims := AccessClaims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			Subject:   userID,
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.key)
	if err != nil {
		return "", "", 0, fmt.Errorf("auth: sign access token: %w", err)
	}
	return signed, jti, int(i.ttl.Seconds()), nil
}

// Verify parses and validates an access token, returning its claims. It enforces
// the HS256 signing method to prevent algorithm-confusion attacks.
func (i *JWTIssuer) Verify(token string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return i.key, nil
	}, jwt.WithIssuer(jwtIssuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return nil, errInvalidToken
	}
	return claims, nil
}

// IssueServiceAccountToken mints a JWT for a service account.
// The token encodes the service account ID, organization, and scopes.
func (i *JWTIssuer) IssueServiceAccountToken(serviceAccountID, orgID string, scopes []string, expiresAt *time.Time) (string, string, error) {
	now := i.now()
	jti := uuid.NewString()

	exp := now.Add(365 * 24 * time.Hour) // Default to 1 year
	if expiresAt != nil {
		exp = *expiresAt
	}

	claims := ServiceAccountClaims{
		OrgID:  orgID,
		Scopes: scopes,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			Subject:   serviceAccountID,
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.key)
	if err != nil {
		return "", "", fmt.Errorf("auth: sign service account token: %w", err)
	}
	return signed, jti, nil
}
