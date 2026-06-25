package auth

import (
	"strings"
	"testing"
)

func TestHashToken_StableAndHex(t *testing.T) {
	h1 := hashToken("secret-token")
	h2 := hashToken("secret-token")
	if h1 != h2 {
		t.Error("hashToken must be deterministic")
	}
	if len(h1) != 64 { // sha256 hex
		t.Errorf("hash length = %d, want 64", len(h1))
	}
	if hashToken("other") == h1 {
		t.Error("different inputs must hash differently")
	}
}

func TestNewAPIToken_FormatAndHash(t *testing.T) {
	plaintext, prefix, hash, err := newAPIToken()
	if err != nil {
		t.Fatalf("newAPIToken: %v", err)
	}
	if !strings.HasPrefix(plaintext, prefix+"_") {
		t.Errorf("plaintext %q should start with prefix %q", plaintext, prefix)
	}
	if !strings.HasPrefix(prefix, apiTokenPrefix+"_") {
		t.Errorf("prefix %q should start with %q", prefix, apiTokenPrefix)
	}
	if hash != hashToken(plaintext) {
		t.Error("stored hash must equal hashToken(plaintext)")
	}
}

func TestGenerateSecret_Unique(t *testing.T) {
	a, _ := generateSecret(32)
	b, _ := generateSecret(32)
	if a == b {
		t.Error("expected unique random secrets")
	}
}
