package cluster

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	"github.com/bdsplatform/platform/backend/libs/config"
)

// jwtIssuer must match the issuer used by the auth service when minting tokens.
const jwtIssuer = "bdsplatform-auth"

// generateRegistrationToken returns a URL-safe random registration token.
func generateRegistrationToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("cluster: generate registration token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex-encoded SHA-256 of a token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// accessClaims mirrors the claims minted by the auth service.
type accessClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// TokenVerifier validates access tokens issued by the auth service.
type TokenVerifier struct {
	key []byte
}

// NewTokenVerifier builds a verifier from the shared signing key.
func NewTokenVerifier(cfg config.AuthConfig) *TokenVerifier {
	return &TokenVerifier{key: []byte(cfg.JWTSigningKey)}
}

// Identity is the authenticated caller extracted from an access token.
type Identity struct {
	UserID string
	Email  string
}

// Verify parses and validates an access token, returning the caller identity.
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
