package auth

import (
	"testing"
	"time"

	"github.com/bdsplatform/platform/backend/libs/config"
)

func testAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		JWTSigningKey: "test-signing-key-please-change",
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    720 * time.Hour,
	}
}

func TestJWTIssueAndVerify(t *testing.T) {
	issuer := NewJWTIssuer(testAuthConfig())
	token, jti, expiresIn, err := issuer.Issue("user-1", "a@b.com", "sess-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" || jti == "" {
		t.Fatal("expected token and jti")
	}
	if expiresIn != int((15 * time.Minute).Seconds()) {
		t.Errorf("expiresIn = %d", expiresIn)
	}

	claims, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "user-1" || claims.Email != "a@b.com" {
		t.Errorf("claims = %+v", claims)
	}
	if claims.ID != jti {
		t.Errorf("jti mismatch: %s != %s", claims.ID, jti)
	}
	if claims.SID != "sess-1" {
		t.Errorf("sid mismatch: got %q want %q", claims.SID, "sess-1")
	}
}

func TestJWTVerify_WrongKey(t *testing.T) {
	issuer := NewJWTIssuer(testAuthConfig())
	token, _, _, _ := issuer.Issue("user-1", "a@b.com", "sess-1")

	other := NewJWTIssuer(config.AuthConfig{JWTSigningKey: "different-key", AccessTTL: time.Minute})
	if _, err := other.Verify(token); err == nil {
		t.Error("expected verification with wrong key to fail")
	}
}

func TestJWTVerify_Expired(t *testing.T) {
	issuer := NewJWTIssuer(testAuthConfig())
	issuer.now = func() time.Time { return time.Now().Add(-time.Hour) } // issue in the past
	token, _, _, _ := issuer.Issue("user-1", "a@b.com", "sess-1")

	if _, err := issuer.Verify(token); err == nil {
		t.Error("expected expired token to fail verification")
	}
}
