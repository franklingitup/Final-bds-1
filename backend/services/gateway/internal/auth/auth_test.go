package auth

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/bdsplatform/platform/backend/libs/config"
)

const testSigningKey = "test-signing-key-12345"

func testConfig() config.AuthConfig {
	return config.AuthConfig{
		JWTSigningKey: testSigningKey,
		AccessTTL:     15 * time.Minute,
	}
}

func signToken(claims jwt.Claims) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte(testSigningKey))
	return signed
}

func TestValidateUserToken(t *testing.T) {
	validator := NewValidator(testConfig())

	// Valid user token.
	claims := &accessClaims{
		Email: "user@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    jwtIssuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	token := signToken(claims)

	identity, err := validator.ValidateUserToken(token)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if identity.UserID != "user-123" {
		t.Errorf("expected UserID user-123, got %s", identity.UserID)
	}
	if identity.Email != "user@example.com" {
		t.Errorf("expected Email user@example.com, got %s", identity.Email)
	}
	if identity.Type != TokenTypeUser {
		t.Errorf("expected Type user, got %s", identity.Type)
	}
}

func TestValidateUserToken_Expired(t *testing.T) {
	validator := NewValidator(testConfig())

	claims := &accessClaims{
		Email: "user@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    jwtIssuer,
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		},
	}
	token := signToken(claims)

	_, err := validator.ValidateUserToken(token)
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestValidateUserToken_WrongIssuer(t *testing.T) {
	validator := NewValidator(testConfig())

	claims := &accessClaims{
		Email: "user@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    "wrong-issuer",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	token := signToken(claims)

	_, err := validator.ValidateUserToken(token)
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestValidateUserToken_WrongKey(t *testing.T) {
	validator := NewValidator(testConfig())

	claims := &accessClaims{
		Email: "user@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    jwtIssuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	// Sign with different key.
	wrongToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := wrongToken.SignedString([]byte("wrong-key"))

	_, err := validator.ValidateUserToken(signed)
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestValidateServiceAccountToken(t *testing.T) {
	validator := NewValidator(testConfig())

	claims := &serviceAccountClaims{
		OrgID:  "org-123",
		Scopes: []string{"read", "write"},
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "sa-456",
			Issuer:    jwtIssuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}
	token := signToken(claims)

	identity, err := validator.ValidateServiceAccountToken(token)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if identity.UserID != "sa-456" {
		t.Errorf("expected UserID sa-456, got %s", identity.UserID)
	}
	if identity.OrgID != "org-123" {
		t.Errorf("expected OrgID org-123, got %s", identity.OrgID)
	}
	if identity.Type != TokenTypeServiceAccount {
		t.Errorf("expected Type service_account, got %s", identity.Type)
	}
	if len(identity.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(identity.Scopes))
	}
}

func TestValidateToken_UserFallback(t *testing.T) {
	validator := NewValidator(testConfig())

	claims := &accessClaims{
		Email: "user@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    jwtIssuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	token := signToken(claims)

	identity, err := validator.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if identity.Type != TokenTypeUser {
		t.Errorf("expected Type user, got %s", identity.Type)
	}
}

func TestIdentity_HasScope(t *testing.T) {
	tests := []struct {
		name     string
		identity Identity
		scope    string
		expected bool
	}{
		{
			name:     "has exact scope",
			identity: Identity{Scopes: []string{"read", "write"}},
			scope:    "read",
			expected: true,
		},
		{
			name:     "missing scope",
			identity: Identity{Scopes: []string{"read"}},
			scope:    "write",
			expected: false,
		},
		{
			name:     "wildcard scope",
			identity: Identity{Scopes: []string{"*"}},
			scope:    "anything",
			expected: true,
		},
		{
			name:     "empty scopes",
			identity: Identity{},
			scope:    "read",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.HasScope(tt.scope)
			if got != tt.expected {
				t.Errorf("HasScope(%q) = %v, want %v", tt.scope, got, tt.expected)
			}
		})
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		header   string
		expected string
	}{
		{"Bearer abc123", "abc123"},
		{"bearer abc123", "abc123"},
		{"BEARER abc123", "abc123"},
		{"Bearer  abc123  ", "abc123"},
		{"", ""},
		{"Basic abc123", ""},
		{"Bearer", ""},
		{"Bearer ", ""},
	}

	for _, tt := range tests {
		got := ExtractBearerToken(tt.header)
		if got != tt.expected {
			t.Errorf("ExtractBearerToken(%q) = %q, want %q", tt.header, got, tt.expected)
		}
	}
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()
	id := Identity{UserID: "user-123", Email: "test@example.com"}

	ctx = WithIdentity(ctx, id)

	got, ok := IdentityFromContext(ctx)
	if !ok {
		t.Fatal("expected identity in context")
	}
	if got.UserID != id.UserID {
		t.Errorf("expected UserID %s, got %s", id.UserID, got.UserID)
	}
}

func TestContextHelpers_Missing(t *testing.T) {
	ctx := context.Background()

	_, ok := IdentityFromContext(ctx)
	if ok {
		t.Error("expected no identity in context")
	}
}
