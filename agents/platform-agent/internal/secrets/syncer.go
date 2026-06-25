// Package secrets implements the secrets synchronization engine.
package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
)

// Config holds secrets syncer configuration.
type Config struct {
	// Interval between sync cycles.
	Interval time.Duration
	// StateFile path for persisting sync state.
	StateFile string
	// Namespace to create secrets in.
	Namespace string
	// AgentCredentials for authenticating with the Secrets Service.
	AgentCredentials controlplane.AgentCredentials
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		Interval:  60 * time.Second,
		StateFile: "/var/lib/platform-agent/secrets-state.json",
		Namespace: "default",
	}
}

// SecretsClient fetches secrets from the control plane.
type SecretsClient interface {
	GetSecrets(ctx context.Context, creds controlplane.AgentCredentials) ([]controlplane.Secret, error)
}

// SecretManager manages Kubernetes Secret resources.
type SecretManager interface {
	ApplySecret(ctx context.Context, spec SecretSpec) (*ApplyResult, error)
	DeleteSecret(ctx context.Context, name string) error
	ListManagedSecrets(ctx context.Context) ([]string, error)
}

// SecretSpec defines a Kubernetes Secret to create/update.
type SecretSpec struct {
	Name        string
	Namespace   string
	ProjectID   string
	Data        map[string][]byte
	Version     int64
	Labels      map[string]string
	Annotations map[string]string
}

// ApplyResult indicates what happened when applying a secret.
type ApplyResult struct {
	Created bool
	Updated bool
}

// SyncerState tracks sync state.
type SyncerState struct {
	// AppliedVersions maps secret name to the last applied version.
	AppliedVersions map[string]int64 `json:"appliedVersions"`
	// LastSync is the timestamp of the last successful sync.
	LastSync time.Time `json:"lastSync"`
}

// Syncer continuously synchronizes secrets from the control plane to Kubernetes.
type Syncer struct {
	client  SecretsClient
	manager SecretManager
	cfg     Config
	state   *SyncerState
	stateMu sync.RWMutex
	log     *slog.Logger
}

// New creates a new Syncer.
func New(client SecretsClient, manager SecretManager, cfg Config, log *slog.Logger) *Syncer {
	return &Syncer{
		client:  client,
		manager: manager,
		cfg:     cfg,
		state: &SyncerState{
			AppliedVersions: make(map[string]int64),
		},
		log: log,
	}
}

// Run starts the synchronization loop and blocks until context is cancelled.
func (s *Syncer) Run(ctx context.Context) error {
	// Load persisted state.
	if err := s.loadState(); err != nil {
		s.log.Warn("failed to load secrets syncer state, starting fresh", "error", err)
	}

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	// Run initial sync.
	s.sync(ctx)

	for {
		select {
		case <-ctx.Done():
			s.log.Info("secrets syncer stopped")
			return ctx.Err()
		case <-ticker.C:
			s.sync(ctx)
		}
	}
}

// sync performs a single synchronization cycle.
func (s *Syncer) sync(ctx context.Context) {
	s.log.Debug("starting secrets sync cycle")

	// Fetch secrets from control plane.
	secrets, err := s.client.GetSecrets(ctx, s.cfg.AgentCredentials)
	if err != nil {
		s.log.Error("failed to fetch secrets", "error", err)
		return
	}

	s.log.Debug("fetched secrets", "count", len(secrets))

	// Group secrets by project.
	projectSecrets := make(map[string]map[string]controlplane.Secret)
	for _, sec := range secrets {
		if projectSecrets[sec.ProjectID] == nil {
			projectSecrets[sec.ProjectID] = make(map[string]controlplane.Secret)
		}
		projectSecrets[sec.ProjectID][sec.Name] = sec
	}

	// Track which secrets we've applied.
	appliedSecretNames := make(map[string]bool)

	// Apply secrets for each project.
	for projectID, secretMap := range projectSecrets {
		if err := s.syncProjectSecrets(ctx, projectID, secretMap, appliedSecretNames); err != nil {
			s.log.Error("failed to sync project secrets",
				"project_id", projectID,
				"error", err)
		}
	}

	// Clean up orphaned secrets.
	if err := s.cleanupOrphanedSecrets(ctx, appliedSecretNames); err != nil {
		s.log.Error("failed to cleanup orphaned secrets", "error", err)
	}

	// Update last sync time and save state.
	s.stateMu.Lock()
	s.state.LastSync = time.Now()
	s.stateMu.Unlock()

	if err := s.saveState(); err != nil {
		s.log.Warn("failed to save secrets syncer state", "error", err)
	}
}

// syncProjectSecrets synchronizes all secrets for a project.
func (s *Syncer) syncProjectSecrets(ctx context.Context, projectID string, secrets map[string]controlplane.Secret, applied map[string]bool) error {
	secretName := s.secretName(projectID)
	applied[secretName] = true

	// Build secret data from all secrets for this project.
	data := make(map[string][]byte)
	var maxVersion int64
	for _, sec := range secrets {
		data[sec.Name] = []byte(sec.Value)
		if sec.Version > maxVersion {
			maxVersion = sec.Version
		}
	}

	// Check if we need to update.
	s.stateMu.RLock()
	appliedVersion := s.state.AppliedVersions[secretName]
	s.stateMu.RUnlock()

	// Skip if all versions are the same.
	// Note: This is a simple check; in production you might want a hash comparison.
	if appliedVersion >= maxVersion {
		s.log.Debug("secret up to date", "name", secretName, "version", maxVersion)
		return nil
	}

	// Build spec.
	spec := SecretSpec{
		Name:      secretName,
		Namespace: s.cfg.Namespace,
		ProjectID: projectID,
		Data:      data,
		Version:   maxVersion,
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "bdsplatform-agent",
			"bdsplatform.io/project-id":    projectID,
		},
		Annotations: map[string]string{
			"bdsplatform.io/version":    fmt.Sprintf("%d", maxVersion),
			"bdsplatform.io/synced-at":  time.Now().Format(time.RFC3339),
			"bdsplatform.io/secret-keys": fmt.Sprintf("%d", len(data)),
		},
	}

	// Apply secret.
	result, err := s.manager.ApplySecret(ctx, spec)
	if err != nil {
		return fmt.Errorf("apply secret %s: %w", secretName, err)
	}

	// Update state.
	s.stateMu.Lock()
	s.state.AppliedVersions[secretName] = maxVersion
	s.stateMu.Unlock()

	s.log.Info("synced project secrets",
		"name", secretName,
		"project_id", projectID,
		"secret_count", len(data),
		"version", maxVersion,
		"created", result.Created,
		"updated", result.Updated)

	return nil
}

// cleanupOrphanedSecrets removes secrets that are no longer needed.
func (s *Syncer) cleanupOrphanedSecrets(ctx context.Context, appliedNames map[string]bool) error {
	managed, err := s.manager.ListManagedSecrets(ctx)
	if err != nil {
		return fmt.Errorf("list managed secrets: %w", err)
	}

	for _, name := range managed {
		if !appliedNames[name] {
			s.log.Info("deleting orphaned secret", "name", name)
			if err := s.manager.DeleteSecret(ctx, name); err != nil {
				s.log.Warn("failed to delete orphaned secret", "name", name, "error", err)
			}

			// Remove from state.
			s.stateMu.Lock()
			delete(s.state.AppliedVersions, name)
			s.stateMu.Unlock()
		}
	}

	return nil
}

// secretName generates the Kubernetes Secret name for a project.
func (s *Syncer) secretName(projectID string) string {
	return fmt.Sprintf("bds-secret-%s", projectID)
}

// loadState loads the syncer state from disk.
func (s *Syncer) loadState() error {
	data, err := os.ReadFile(s.cfg.StateFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if err := json.Unmarshal(data, s.state); err != nil {
		return err
	}

	if s.state.AppliedVersions == nil {
		s.state.AppliedVersions = make(map[string]int64)
	}

	return nil
}

// saveState saves the syncer state to disk.
func (s *Syncer) saveState() error {
	s.stateMu.RLock()
	data, err := json.MarshalIndent(s.state, "", "  ")
	s.stateMu.RUnlock()

	if err != nil {
		return err
	}

	return os.WriteFile(s.cfg.StateFile, data, 0600)
}

// State returns a copy of the current syncer state.
func (s *Syncer) State() SyncerState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	state := SyncerState{
		AppliedVersions: make(map[string]int64),
		LastSync:        s.state.LastSync,
	}
	for k, v := range s.state.AppliedVersions {
		state.AppliedVersions[k] = v
	}
	return state
}
