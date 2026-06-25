package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// apiTokenPrefix namespaces API tokens so they are recognizable in logs and
// secret scanners (without revealing the secret).
const apiTokenPrefix = "bdsp"

// generateSecret returns a cryptographically random, URL-safe token string with
// the requested number of random bytes.
func generateSecret(numBytes int) (string, error) {
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex-encoded SHA-256 of a token. Opaque tokens are stored
// and looked up by this hash so the plaintext never touches the database.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newAPIToken returns a plaintext API token of the form
// "bdsp_<prefix>_<secret>" along with its display prefix ("bdsp_<prefix>") and
// storage hash. The prefix is non-secret and helps identify a token in lists.
func newAPIToken() (plaintext, prefix, hash string, err error) {
	idPart, err := generateSecret(4)
	if err != nil {
		return "", "", "", err
	}
	secret, err := generateSecret(24)
	if err != nil {
		return "", "", "", err
	}
	prefix = fmt.Sprintf("%s_%s", apiTokenPrefix, idPart)
	plaintext = fmt.Sprintf("%s_%s", prefix, secret)
	return plaintext, prefix, hashToken(plaintext), nil
}
