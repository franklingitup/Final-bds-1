package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// SSO key encryption mirrors the domain service's CertificateEncryptor
// (AES-256-GCM, CERTIFICATE_ENCRYPTION_KEY). Kept in-package so auth does not
// import the domain service.

var (
	errInvalidSSOKey        = errors.New("auth: invalid certificate encryption key (must be 32 bytes for AES-256)")
	errSSOEncryptionFailed  = errors.New("auth: sso key encryption failed")
	errSSODecryptionFailed  = errors.New("auth: sso key decryption failed")
	errInvalidSSOCiphertext = errors.New("auth: invalid sso key ciphertext format")
)

const (
	ssoKeySize   = 32
	ssoNonceSize = 12
)

// SSOKeyEncryptor encrypts/decrypts SP private keys at rest.
type SSOKeyEncryptor struct {
	gcm cipher.AEAD
}

// NewSSOKeyEncryptor creates an encryptor from a base64-encoded 32-byte AES-256 key.
func NewSSOKeyEncryptor(keyBase64 string) (*SSOKeyEncryptor, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidSSOKey, err)
	}
	return NewSSOKeyEncryptorFromBytes(key)
}

// NewSSOKeyEncryptorFromBytes creates an encryptor from a raw 32-byte key.
func NewSSOKeyEncryptorFromBytes(key []byte) (*SSOKeyEncryptor, error) {
	if len(key) != ssoKeySize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", errInvalidSSOKey, len(key), ssoKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidSSOKey, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidSSOKey, err)
	}
	return &SSOKeyEncryptor{gcm: gcm}, nil
}

// Encrypt returns nonce || ciphertext || tag.
func (e *SSOKeyEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, ssoNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%w: generate nonce: %v", errSSOEncryptionFailed, err)
	}
	return e.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt authenticates and decrypts data produced by Encrypt.
func (e *SSOKeyEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < ssoNonceSize+e.gcm.Overhead() {
		return nil, errInvalidSSOCiphertext
	}
	nonce := ciphertext[:ssoNonceSize]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext[ssoNonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSSODecryptionFailed, err)
	}
	return plaintext, nil
}
