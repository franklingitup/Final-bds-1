package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters (RFC 6238), compatible with standard authenticator apps.
const (
	totpDigits     = 6
	totpPeriod     = 30 * time.Second
	totpSecretLen  = 20 // 160-bit secret
	totpSkewWindow = 1  // accept codes +/- 1 step to tolerate clock drift
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// generateTOTPSecret returns a new base32-encoded TOTP secret.
func generateTOTPSecret() (string, error) {
	b := make([]byte, totpSecretLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate totp secret: %w", err)
	}
	return totpEncoding.EncodeToString(b), nil
}

// totpCodeAt computes the TOTP code for a base32 secret at time t.
func totpCodeAt(secret string, t time.Time) (string, error) {
	key, err := totpEncoding.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", fmt.Errorf("auth: decode totp secret: %w", err)
	}
	counter := uint64(t.Unix()) / uint64(totpPeriod.Seconds())

	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	// Dynamic truncation (RFC 4226).
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])

	mod := uint32(1)
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, bin%mod), nil
}

// verifyTOTP reports whether code is valid for the secret at time t, allowing a
// small window of clock drift. Comparison is constant-time.
func verifyTOTP(secret, code string, t time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	for skew := -totpSkewWindow; skew <= totpSkewWindow; skew++ {
		want, err := totpCodeAt(secret, t.Add(time.Duration(skew)*totpPeriod))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// otpauthURL builds an otpauth:// URI for provisioning the secret into an
// authenticator app (e.g. via QR code).
func otpauthURL(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", totpDigits))
	q.Set("period", fmt.Sprintf("%d", int(totpPeriod.Seconds())))
	return fmt.Sprintf("otpauth://totp/%s?%s", label, q.Encode())
}
