package github

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// Encryption errors.
var (
	ErrInvalidKey        = errors.New("github: invalid encryption key (must be 32 bytes for AES-256)")
	ErrEncryptionFailed  = errors.New("github: encryption failed")
	ErrDecryptionFailed  = errors.New("github: decryption failed")
	ErrInvalidCiphertext = errors.New("github: invalid ciphertext format")
)

const (
	keySize   = 32 // 256 bits
	nonceSize = 12 // 96 bits (standard for GCM)
	tagSize   = 16 // 128 bits (GCM authentication tag)
)

// TokenEncryptor provides encryption for GitHub tokens using AES-256-GCM.
type TokenEncryptor struct {
	gcm cipher.AEAD
}

// NewTokenEncryptor creates a TokenEncryptor from a base64-encoded key.
func NewTokenEncryptor(keyBase64 string) (*TokenEncryptor, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return NewTokenEncryptorFromBytes(key)
}

// NewTokenEncryptorFromBytes creates a TokenEncryptor from raw key bytes.
func NewTokenEncryptorFromBytes(key []byte) (*TokenEncryptor, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidKey, len(key), keySize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}

	return &TokenEncryptor{gcm: gcm}, nil
}

// Encrypt encrypts a token using AES-256-GCM.
func (e *TokenEncryptor) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%w: failed to generate nonce: %v", ErrEncryptionFailed, err)
	}

	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return ciphertext, nil
}

// Decrypt decrypts a token encrypted with Encrypt.
func (e *TokenEncryptor) Decrypt(ciphertext []byte) (string, error) {
	if len(ciphertext) < nonceSize+tagSize {
		return "", ErrInvalidCiphertext
	}

	nonce := ciphertext[:nonceSize]
	encryptedData := ciphertext[nonceSize:]

	plaintext, err := e.gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDecryptionFailed, err)
	}

	return string(plaintext), nil
}

// HashToken computes a SHA-256 hash of the token for change detection.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// GenerateWebhookSecret generates a random webhook secret.
func GenerateWebhookSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", fmt.Errorf("failed to generate webhook secret: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateOAuthState generates a random OAuth state for CSRF protection.
func GenerateOAuthState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", fmt.Errorf("failed to generate OAuth state: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// VerifyWebhookSignature verifies a GitHub webhook signature.
// The signature format is "sha256=<hex-encoded-hmac>".
func VerifyWebhookSignature(payload []byte, secret, signature string) bool {
	if len(signature) < 7 || signature[:7] != "sha256=" {
		return false
	}

	expectedSig, err := hex.DecodeString(signature[7:])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actualSig := mac.Sum(nil)

	return hmac.Equal(actualSig, expectedSig)
}
