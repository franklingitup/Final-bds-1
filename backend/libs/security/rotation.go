package security

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"
)

// SecretRotator manages automatic secret rotation.
type SecretRotator struct {
	store          RotationStore
	encryptor      *EnvelopeEncryptor
	rotationPeriod time.Duration
	mu             sync.RWMutex
	stopCh         chan struct{}
	wg             sync.WaitGroup
}

// RotationStore persists rotated secrets.
type RotationStore interface {
	// GetSecret retrieves a secret by name and version.
	GetSecret(ctx context.Context, name string, version int) (*RotatedSecret, error)
	// GetCurrentSecret retrieves the current version of a secret.
	GetCurrentSecret(ctx context.Context, name string) (*RotatedSecret, error)
	// ListSecretVersions lists all versions of a secret.
	ListSecretVersions(ctx context.Context, name string) ([]RotatedSecret, error)
	// SaveSecret saves a new secret version.
	SaveSecret(ctx context.Context, secret *RotatedSecret) error
	// SetCurrentVersion sets the current version for a secret.
	SetCurrentVersion(ctx context.Context, name string, version int) error
	// DeleteOldVersions deletes versions older than the specified version.
	DeleteOldVersions(ctx context.Context, name string, keepVersions int) error
}

// RotatedSecret represents a versioned secret.
type RotatedSecret struct {
	Name           string
	Version        int
	EncryptedValue []byte
	CreatedAt      time.Time
	ExpiresAt      time.Time
	IsCurrent      bool
	Metadata       map[string]string
}

// RotationConfig configures secret rotation.
type RotationConfig struct {
	Name           string
	RotationPeriod time.Duration
	GracePeriod    time.Duration // How long old versions remain valid
	Generator      SecretGenerator
	OnRotate       func(ctx context.Context, newSecret []byte) error
	KeepVersions   int
}

// SecretGenerator generates new secret values.
type SecretGenerator interface {
	Generate(ctx context.Context) ([]byte, error)
}

// RandomSecretGenerator generates random secrets of a specified length.
type RandomSecretGenerator struct {
	Length int
}

// Generate creates a random secret.
func (g *RandomSecretGenerator) Generate(ctx context.Context) ([]byte, error) {
	secret := make([]byte, g.Length)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// Base64SecretGenerator generates base64-encoded random secrets.
type Base64SecretGenerator struct {
	ByteLength int
}

// Generate creates a base64-encoded random secret.
func (g *Base64SecretGenerator) Generate(ctx context.Context) ([]byte, error) {
	raw := make([]byte, g.ByteLength)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return nil, err
	}
	return []byte(base64.StdEncoding.EncodeToString(raw)), nil
}

// NewSecretRotator creates a new secret rotator.
func NewSecretRotator(store RotationStore, encryptor *EnvelopeEncryptor, rotationPeriod time.Duration) *SecretRotator {
	return &SecretRotator{
		store:          store,
		encryptor:      encryptor,
		rotationPeriod: rotationPeriod,
		stopCh:         make(chan struct{}),
	}
}

// Start begins the rotation background process.
func (r *SecretRotator) Start(ctx context.Context, configs []RotationConfig) {
	r.wg.Add(1)
	go r.rotationLoop(ctx, configs)
}

// Stop stops the rotation background process.
func (r *SecretRotator) Stop() {
	close(r.stopCh)
	r.wg.Wait()
}

func (r *SecretRotator) rotationLoop(ctx context.Context, configs []RotationConfig) {
	defer r.wg.Done()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, cfg := range configs {
				if err := r.checkAndRotate(ctx, cfg); err != nil {
					// Log error but continue
					fmt.Printf("rotation error for %s: %v\n", cfg.Name, err)
				}
			}
		}
	}
}

func (r *SecretRotator) checkAndRotate(ctx context.Context, cfg RotationConfig) error {
	current, err := r.store.GetCurrentSecret(ctx, cfg.Name)
	if err != nil {
		// No current secret, create initial
		return r.rotate(ctx, cfg, 1)
	}

	// Check if rotation is needed
	if time.Now().After(current.ExpiresAt.Add(-cfg.GracePeriod)) {
		return r.rotate(ctx, cfg, current.Version+1)
	}

	return nil
}

func (r *SecretRotator) rotate(ctx context.Context, cfg RotationConfig, newVersion int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Generate new secret
	plaintext, err := cfg.Generator.Generate(ctx)
	if err != nil {
		return fmt.Errorf("generate secret: %w", err)
	}
	defer zeroBytes(plaintext)

	// Encrypt secret
	encrypted, err := r.encryptor.Encrypt(ctx, plaintext, []byte(cfg.Name))
	if err != nil {
		return fmt.Errorf("encrypt secret: %w", err)
	}

	// Create rotated secret
	now := time.Now()
	secret := &RotatedSecret{
		Name:           cfg.Name,
		Version:        newVersion,
		EncryptedValue: encrypted,
		CreatedAt:      now,
		ExpiresAt:      now.Add(cfg.RotationPeriod),
		IsCurrent:      true,
	}

	// Save new version
	if err := r.store.SaveSecret(ctx, secret); err != nil {
		return fmt.Errorf("save secret: %w", err)
	}

	// Update current version
	if err := r.store.SetCurrentVersion(ctx, cfg.Name, newVersion); err != nil {
		return fmt.Errorf("set current version: %w", err)
	}

	// Call rotation callback
	if cfg.OnRotate != nil {
		if err := cfg.OnRotate(ctx, plaintext); err != nil {
			return fmt.Errorf("rotation callback: %w", err)
		}
	}

	// Cleanup old versions
	if cfg.KeepVersions > 0 {
		if err := r.store.DeleteOldVersions(ctx, cfg.Name, cfg.KeepVersions); err != nil {
			// Log but don't fail
			fmt.Printf("cleanup old versions: %v\n", err)
		}
	}

	return nil
}

// GetSecret retrieves and decrypts the current version of a secret.
func (r *SecretRotator) GetSecret(ctx context.Context, name string) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	secret, err := r.store.GetCurrentSecret(ctx, name)
	if err != nil {
		return nil, err
	}

	return r.encryptor.Decrypt(ctx, secret.EncryptedValue)
}

// GetSecretVersion retrieves and decrypts a specific version.
func (r *SecretRotator) GetSecretVersion(ctx context.Context, name string, version int) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	secret, err := r.store.GetSecret(ctx, name, version)
	if err != nil {
		return nil, err
	}

	return r.encryptor.Decrypt(ctx, secret.EncryptedValue)
}

// ForceRotate triggers immediate rotation.
func (r *SecretRotator) ForceRotate(ctx context.Context, cfg RotationConfig) error {
	current, err := r.store.GetCurrentSecret(ctx, cfg.Name)
	if err != nil {
		return r.rotate(ctx, cfg, 1)
	}
	return r.rotate(ctx, cfg, current.Version+1)
}

// RotationStatus represents the status of a secret's rotation.
type RotationStatus struct {
	Name           string
	CurrentVersion int
	CreatedAt      time.Time
	ExpiresAt      time.Time
	VersionCount   int
	NeedsRotation  bool
}

// GetStatus returns the rotation status for a secret.
func (r *SecretRotator) GetStatus(ctx context.Context, name string) (*RotationStatus, error) {
	current, err := r.store.GetCurrentSecret(ctx, name)
	if err != nil {
		return nil, err
	}

	versions, err := r.store.ListSecretVersions(ctx, name)
	if err != nil {
		return nil, err
	}

	return &RotationStatus{
		Name:           name,
		CurrentVersion: current.Version,
		CreatedAt:      current.CreatedAt,
		ExpiresAt:      current.ExpiresAt,
		VersionCount:   len(versions),
		NeedsRotation:  time.Now().After(current.ExpiresAt),
	}, nil
}

// JWTKeyRotator handles JWT signing key rotation.
type JWTKeyRotator struct {
	rotator     *SecretRotator
	secretName  string
	currentKey  []byte
	previousKey []byte
	mu          sync.RWMutex
}

// NewJWTKeyRotator creates a JWT key rotator.
func NewJWTKeyRotator(rotator *SecretRotator, secretName string) *JWTKeyRotator {
	return &JWTKeyRotator{
		rotator:    rotator,
		secretName: secretName,
	}
}

// GetSigningKey returns the current signing key.
func (j *JWTKeyRotator) GetSigningKey() []byte {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.currentKey
}

// GetVerificationKeys returns keys that can be used for verification.
func (j *JWTKeyRotator) GetVerificationKeys() [][]byte {
	j.mu.RLock()
	defer j.mu.RUnlock()

	keys := [][]byte{j.currentKey}
	if j.previousKey != nil {
		keys = append(keys, j.previousKey)
	}
	return keys
}

// Refresh loads keys from the rotator.
func (j *JWTKeyRotator) Refresh(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	current, err := j.rotator.GetSecret(ctx, j.secretName)
	if err != nil {
		return err
	}

	// Keep previous key for verification
	j.previousKey = j.currentKey
	j.currentKey = current

	return nil
}
