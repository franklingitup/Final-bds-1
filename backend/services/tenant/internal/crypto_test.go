package tenant

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/bdsplatform/platform/backend/libs/config"
)

func TestHashToken_Stable(t *testing.T) {
	if hashToken("abc") != hashToken("abc") {
		t.Error("hashToken must be deterministic")
	}
	if hashToken("abc") == hashToken("abd") {
		t.Error("distinct inputs must hash differently")
	}
	if len(hashToken("abc")) != 64 {
		t.Error("expected 64-char hex sha256")
	}
}

func TestGenerateInviteToken_Unique(t *testing.T) {
	a, _ := generateInviteToken()
	b, _ := generateInviteToken()
	if a == "" || a == b {
		t.Error("expected unique non-empty invite tokens")
	}
}

func signTestToken(t *testing.T, key, issuer, sub, email string, exp time.Time) string {
	t.Helper()
	claims := accessClaims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   sub,
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func TestTokenVerifier_Verify(t *testing.T) {
	v := NewTokenVerifier(config.AuthConfig{JWTSigningKey: "secret-key"})
	tok := signTestToken(t, "secret-key", jwtIssuer, "user-1", "u@example.com", time.Now().Add(time.Hour))

	id, err := v.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.UserID != "user-1" || id.Email != "u@example.com" {
		t.Errorf("unexpected identity %+v", id)
	}
}

func TestTokenVerifier_Rejects(t *testing.T) {
	v := NewTokenVerifier(config.AuthConfig{JWTSigningKey: "secret-key"})

	// Wrong signing key.
	if _, err := v.Verify(signTestToken(t, "other-key", jwtIssuer, "u", "e@x.com", time.Now().Add(time.Hour))); err == nil {
		t.Error("expected wrong-key token to be rejected")
	}
	// Wrong issuer.
	if _, err := v.Verify(signTestToken(t, "secret-key", "evil", "u", "e@x.com", time.Now().Add(time.Hour))); err == nil {
		t.Error("expected wrong-issuer token to be rejected")
	}
	// Expired.
	if _, err := v.Verify(signTestToken(t, "secret-key", jwtIssuer, "u", "e@x.com", time.Now().Add(-time.Hour))); err == nil {
		t.Error("expected expired token to be rejected")
	}
}
