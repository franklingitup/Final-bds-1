package provisioning

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// JWTVerifier verifies JWT tokens.
type JWTVerifier struct {
	signingKey []byte
}

// NewJWTVerifier creates a new JWT verifier.
func NewJWTVerifier(signingKey string) *JWTVerifier {
	return &JWTVerifier{signingKey: []byte(signingKey)}
}

// Verify parses and verifies a JWT token.
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

// AESEncryptor encrypts/decrypts data using AES-256-GCM.
type AESEncryptor struct {
	key []byte
}

// NewAESEncryptor creates a new AES encryptor.
// The key must be 32 bytes for AES-256.
func NewAESEncryptor(key []byte) (*AESEncryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes for AES-256")
	}
	return &AESEncryptor{key: key}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
func (e *AESEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext using AES-256-GCM.
func (e *AESEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// NoopEncryptor is a no-op encryptor for development.
type NoopEncryptor struct{}

// Encrypt returns plaintext unchanged.
func (e *NoopEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	return plaintext, nil
}

// Decrypt returns ciphertext unchanged.
func (e *NoopEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
}
