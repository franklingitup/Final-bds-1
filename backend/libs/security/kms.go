// Package security provides production security features including encryption,
// authentication, and access control.
package security

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// KMSProvider is the interface for key management services.
type KMSProvider interface {
	// GenerateDataKey generates a new data encryption key.
	GenerateDataKey(ctx context.Context, keyID string) (*DataKey, error)
	// DecryptDataKey decrypts an encrypted data key.
	DecryptDataKey(ctx context.Context, keyID string, encryptedKey []byte) ([]byte, error)
	// Encrypt encrypts data using KMS directly (for small data).
	Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error)
	// Decrypt decrypts data using KMS directly.
	Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error)
}

// DataKey represents a data encryption key.
type DataKey struct {
	Plaintext    []byte // The plaintext key (use immediately, then zero)
	EncryptedKey []byte // The encrypted key (store this)
	KeyID        string // The KMS key ID used
}

// EnvelopeEncryptor implements envelope encryption using a KMS provider.
type EnvelopeEncryptor struct {
	kms          KMSProvider
	defaultKeyID string
	cache        *keyCache
}

// NewEnvelopeEncryptor creates a new envelope encryptor.
func NewEnvelopeEncryptor(kms KMSProvider, defaultKeyID string) *EnvelopeEncryptor {
	return &EnvelopeEncryptor{
		kms:          kms,
		defaultKeyID: defaultKeyID,
		cache:        newKeyCache(5*time.Minute, 100),
	}
}

// EncryptedEnvelope contains encrypted data with its envelope.
type EncryptedEnvelope struct {
	Version      int    `json:"v"`
	KeyID        string `json:"kid"`
	EncryptedKey []byte `json:"ek"`
	Nonce        []byte `json:"n"`
	Ciphertext   []byte `json:"ct"`
	AAD          []byte `json:"aad,omitempty"`
}

// Encrypt encrypts data using envelope encryption.
func (e *EnvelopeEncryptor) Encrypt(ctx context.Context, plaintext []byte, aad []byte) ([]byte, error) {
	return e.EncryptWithKeyID(ctx, e.defaultKeyID, plaintext, aad)
}

// EncryptWithKeyID encrypts data using a specific KMS key.
func (e *EnvelopeEncryptor) EncryptWithKeyID(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	// Generate a data encryption key (DEK)
	dek, err := e.kms.GenerateDataKey(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("generate data key: %w", err)
	}
	defer zeroBytes(dek.Plaintext)

	// Create AES-GCM cipher
	block, err := aes.NewCipher(dek.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Encrypt with AAD
	ciphertext := gcm.Seal(nil, nonce, plaintext, aad)

	// Create envelope
	envelope := EncryptedEnvelope{
		Version:      1,
		KeyID:        keyID,
		EncryptedKey: dek.EncryptedKey,
		Nonce:        nonce,
		Ciphertext:   ciphertext,
		AAD:          aad,
	}

	return json.Marshal(envelope)
}

// Decrypt decrypts data encrypted with envelope encryption.
func (e *EnvelopeEncryptor) Decrypt(ctx context.Context, data []byte) ([]byte, error) {
	var envelope EncryptedEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse envelope: %w", err)
	}

	// Check cache first
	cacheKey := base64.StdEncoding.EncodeToString(envelope.EncryptedKey)
	plainKey, found := e.cache.get(cacheKey)
	
	if !found {
		// Decrypt the data encryption key
		var err error
		plainKey, err = e.kms.DecryptDataKey(ctx, envelope.KeyID, envelope.EncryptedKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt data key: %w", err)
		}
		e.cache.set(cacheKey, plainKey)
	}

	// Create AES-GCM cipher
	block, err := aes.NewCipher(plainKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	// Decrypt with AAD verification
	plaintext, err := gcm.Open(nil, envelope.Nonce, envelope.Ciphertext, envelope.AAD)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// keyCache caches decrypted data encryption keys.
type keyCache struct {
	entries map[string]*keyCacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
	maxSize int
}

type keyCacheEntry struct {
	key       []byte
	expiresAt time.Time
}

func newKeyCache(ttl time.Duration, maxSize int) *keyCache {
	return &keyCache{
		entries: make(map[string]*keyCacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

func (c *keyCache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.key, true
}

func (c *keyCache) set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[key] = &keyCacheEntry{
		key:       value,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *keyCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for k, v := range c.entries {
		if oldestKey == "" || v.expiresAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.expiresAt
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// LocalKMS is a local KMS implementation for development/testing.
type LocalKMS struct {
	masterKey []byte
}

// NewLocalKMS creates a local KMS with the given master key (32 bytes).
func NewLocalKMS(masterKey []byte) (*LocalKMS, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes")
	}
	return &LocalKMS{masterKey: masterKey}, nil
}

// NewLocalKMSFromBase64 creates a local KMS from a base64-encoded key.
func NewLocalKMSFromBase64(key string) (*LocalKMS, error) {
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	return NewLocalKMS(decoded)
}

// GenerateDataKey generates a new data encryption key.
func (k *LocalKMS) GenerateDataKey(ctx context.Context, keyID string) (*DataKey, error) {
	// Generate random DEK
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, err
	}

	// Encrypt DEK with master key
	block, err := aes.NewCipher(k.masterKey)
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

	encryptedKey := gcm.Seal(nonce, nonce, dek, nil)

	return &DataKey{
		Plaintext:    dek,
		EncryptedKey: encryptedKey,
		KeyID:        keyID,
	}, nil
}

// DecryptDataKey decrypts an encrypted data key.
func (k *LocalKMS) DecryptDataKey(ctx context.Context, keyID string, encryptedKey []byte) ([]byte, error) {
	block, err := aes.NewCipher(k.masterKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(encryptedKey) < nonceSize {
		return nil, fmt.Errorf("encrypted key too short")
	}

	nonce, ciphertext := encryptedKey[:nonceSize], encryptedKey[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// Encrypt encrypts data directly (for small data).
func (k *LocalKMS) Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(k.masterKey)
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

// Decrypt decrypts data directly.
func (k *LocalKMS) Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(k.masterKey)
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

	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, data, nil)
}

// zeroBytes zeros a byte slice.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

var _ KMSProvider = (*LocalKMS)(nil)
