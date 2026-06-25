// Package secrets implements secure secret management for the platform.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
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
	ErrInvalidKey        = errors.New("secrets: invalid master key (must be 32 bytes for AES-256)")
	ErrEncryptionFailed  = errors.New("secrets: encryption failed")
	ErrDecryptionFailed  = errors.New("secrets: decryption failed")
	ErrInvalidCiphertext = errors.New("secrets: invalid ciphertext format")
)

const (
	// AES-256-GCM parameters.
	keySize   = 32 // 256 bits
	nonceSize = 12 // 96 bits (standard for GCM)
	tagSize   = 16 // 128 bits (GCM authentication tag)
)

// Encryptor provides envelope encryption for secrets using AES-256-GCM.
// The master key should be loaded from SECRETS_MASTER_KEY environment variable.
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptor creates an Encryptor from a base64-encoded master key.
// The key must be exactly 32 bytes (256 bits) when decoded.
func NewEncryptor(masterKeyBase64 string) (*Encryptor, error) {
	key, err := base64.StdEncoding.DecodeString(masterKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return NewEncryptorFromBytes(key)
}

// NewEncryptorFromBytes creates an Encryptor from raw key bytes.
func NewEncryptorFromBytes(key []byte) (*Encryptor, error) {
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

	return &Encryptor{gcm: gcm}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns: nonce (12 bytes) || ciphertext || tag (16 bytes)
//
// The nonce is randomly generated for each encryption to ensure
// that the same plaintext produces different ciphertext each time.
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	// Generate random nonce.
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%w: failed to generate nonce: %v", ErrEncryptionFailed, err)
	}

	// Encrypt with GCM (includes authentication tag).
	// Seal appends the ciphertext and tag to the nonce.
	ciphertext := e.gcm.Seal(nonce, nonce, plaintext, nil)

	return ciphertext, nil
}

// Decrypt decrypts ciphertext encrypted with Encrypt.
// Input format: nonce (12 bytes) || ciphertext || tag (16 bytes)
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < nonceSize+tagSize {
		return nil, ErrInvalidCiphertext
	}

	nonce := ciphertext[:nonceSize]
	encryptedData := ciphertext[nonceSize:]

	plaintext, err := e.gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptionFailed, err)
	}

	return plaintext, nil
}

// EncryptString encrypts a string value and returns the encrypted bytes.
func (e *Encryptor) EncryptString(plaintext string) ([]byte, error) {
	return e.Encrypt([]byte(plaintext))
}

// DecryptString decrypts ciphertext and returns the plaintext string.
func (e *Encryptor) DecryptString(ciphertext []byte) (string, error) {
	plaintext, err := e.Decrypt(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// HashValue computes a SHA-256 hash of the plaintext value.
// This is used for change detection without requiring decryption.
func HashValue(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// GenerateMasterKey generates a new random master key suitable for use
// with NewEncryptor. Returns a base64-encoded string.
//
// Use this to generate a new SECRETS_MASTER_KEY value:
//
//	key, _ := secrets.GenerateMasterKey()
//	fmt.Println(key)
func GenerateMasterKey() (string, error) {
	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("failed to generate master key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
