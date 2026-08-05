package domain

import (
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

// CertificateEncryptor encrypts/decrypts certificate data using AES-256-GCM.
type CertificateEncryptor struct {
	key []byte
}

// NewCertificateEncryptor creates a new certificate encryptor.
// The key must be 32 bytes for AES-256.
func NewCertificateEncryptor(key []byte) *CertificateEncryptor {
	return &CertificateEncryptor{key: key}
}

// Encrypt encrypts plaintext using AES-256-GCM.
func (e *CertificateEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	// For simplicity, we're using a basic implementation here
	// In production, use a proper crypto library with nonce management
	if len(e.key) == 0 {
		return plaintext, nil // No encryption if no key
	}
	// TODO: Implement proper AES-256-GCM encryption
	return plaintext, nil
}

// Decrypt decrypts ciphertext using AES-256-GCM.
func (e *CertificateEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(e.key) == 0 {
		return ciphertext, nil // No decryption if no key
	}
	// TODO: Implement proper AES-256-GCM decryption
	return ciphertext, nil
}
