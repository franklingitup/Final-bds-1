package domain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/bdsplatform/platform/backend/libs/config"
)

// JWTVerifier verifies JWT tokens.
type JWTVerifier struct {
	signingKey []byte
}

// NewTokenVerifier creates a new JWT verifier.
func NewTokenVerifier(cfg config.AuthConfig) *JWTVerifier {
	return &JWTVerifier{signingKey: []byte(cfg.JWTSigningKey)}
}

// Verify validates a JWT token and returns the identity.
func (v *JWTVerifier) Verify(tokenString string) (*Identity, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return v.signingKey, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}

	sub, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)

	// Handle sub formats: "user:<uuid>" or just "<uuid>"
	userID := sub
	if strings.HasPrefix(sub, "user:") {
		userID = strings.TrimPrefix(sub, "user:")
	}

	return &Identity{
		UserID: userID,
		Email:  email,
	}, nil
}

// Certificate encryption errors.
var (
	ErrInvalidKey        = errors.New("domain: invalid certificate encryption key (must be 32 bytes for AES-256)")
	ErrEncryptionFailed  = errors.New("domain: certificate encryption failed")
	ErrDecryptionFailed  = errors.New("domain: certificate decryption failed")
	ErrInvalidCiphertext = errors.New("domain: invalid certificate ciphertext format")
)

const (
	certificateEncryptionKeySize = 32
	certificateNonceSize         = 12
)

// CertificateEncryptor encrypts/decrypts certificate data using AES-256-GCM.
type CertificateEncryptor struct {
	gcm cipher.AEAD
}

// NewCertificateEncryptor creates a certificate encryptor from a base64-encoded
// 32-byte AES-256 key.
func NewCertificateEncryptor(keyBase64 string) (*CertificateEncryptor, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return NewCertificateEncryptorFromBytes(key)
}

// NewCertificateEncryptorFromBytes creates a certificate encryptor from a raw
// 32-byte AES-256 key.
func NewCertificateEncryptorFromBytes(key []byte) (*CertificateEncryptor, error) {
	if len(key) != certificateEncryptionKeySize {
		return nil, fmt.Errorf(
			"%w: got %d bytes, want %d",
			ErrInvalidKey,
			len(key),
			certificateEncryptionKeySize,
		)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}

	return &CertificateEncryptor{gcm: gcm}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// The returned format is nonce || ciphertext || authentication tag.
func (e *CertificateEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, certificateNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%w: generate nonce: %v", ErrEncryptionFailed, err)
	}

	return e.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt authenticates and decrypts data produced by Encrypt.
func (e *CertificateEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < certificateNonceSize+e.gcm.Overhead() {
		return nil, ErrInvalidCiphertext
	}

	nonce := ciphertext[:certificateNonceSize]
	encryptedData := ciphertext[certificateNonceSize:]

	plaintext, err := e.gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptionFailed, err)
	}
	return plaintext, nil
}

// GenerateCertificateEncryptionKey returns a new base64-encoded 32-byte key.
func GenerateCertificateEncryptionKey() (string, error) {
	key := make([]byte, certificateEncryptionKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("domain: generate certificate encryption key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
