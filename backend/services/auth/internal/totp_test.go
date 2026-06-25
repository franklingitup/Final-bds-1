package auth

import (
	"testing"
	"time"
)

// RFC 6238 reference secret "12345678901234567890" (ASCII) in base32.
const rfc6238SecretBase32 = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTP_RFC6238Vector(t *testing.T) {
	// At T=59s the SHA-1 8-digit code is 94287082; truncated to 6 digits: 287082.
	code, err := totpCodeAt(rfc6238SecretBase32, time.Unix(59, 0).UTC())
	if err != nil {
		t.Fatalf("totp: %v", err)
	}
	if code != "287082" {
		t.Errorf("code = %s, want 287082", code)
	}
}

func TestTOTP_VerifyRoundTrip(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	now := time.Now()
	code, err := totpCodeAt(secret, now)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if !verifyTOTP(secret, code, now) {
		t.Error("expected valid code to verify")
	}
	if verifyTOTP(secret, "000000", now.Add(10*time.Minute)) {
		t.Error("did not expect arbitrary code to verify")
	}
}

func TestTOTP_AcceptsClockDrift(t *testing.T) {
	secret, _ := generateTOTPSecret()
	now := time.Now()
	// Code from the previous step should still verify within the skew window.
	prev, _ := totpCodeAt(secret, now.Add(-totpPeriod))
	if !verifyTOTP(secret, prev, now) {
		t.Error("expected previous-step code to verify within skew window")
	}
}
