package build

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	"github.com/bdsplatform/platform/backend/libs/config"
)

const jwtIssuer = "bdsplatform-auth"

type accessClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// TokenVerifier validates access tokens.
type TokenVerifier struct {
	key []byte
}

// NewTokenVerifier builds a verifier from the shared signing key.
func NewTokenVerifier(cfg config.AuthConfig) *TokenVerifier {
	return &TokenVerifier{key: []byte(cfg.JWTSigningKey)}
}

// Identity is the authenticated caller.
type Identity struct {
	UserID string
	Email  string
}

// Verify parses and validates an access token.
func (v *TokenVerifier) Verify(token string) (Identity, error) {
	claims := &accessClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return v.key, nil
	}, jwt.WithIssuer(jwtIssuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return Identity{}, errInvalidToken
	}
	return Identity{UserID: claims.Subject, Email: claims.Email}, nil
}
