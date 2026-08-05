package secrets

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
)

type fakeSecretsClient struct {
	secrets []controlplane.Secret
}

func (c *fakeSecretsClient) GetSecrets(ctx context.Context, creds controlplane.AgentCredentials) ([]controlplane.Secret, error) {
	return c.secrets, nil
}

type recordingSecretManager struct {
	applies atomic.Int32
	lists   atomic.Int32
}

func (m *recordingSecretManager) ApplySecret(ctx context.Context, spec SecretSpec) (*ApplyResult, error) {
	m.applies.Add(1)
	return &ApplyResult{Created: true}, nil
}

func (m *recordingSecretManager) DeleteSecret(ctx context.Context, name string) error { return nil }

func (m *recordingSecretManager) ListManagedSecrets(ctx context.Context) ([]string, error) {
	m.lists.Add(1)
	return nil, nil
}

func syncerTestConfig(t *testing.T) Config {
	return Config{
		Interval:  time.Hour,
		StateFile: filepath.Join(t.TempDir(), "secrets-state.json"),
		Namespace: "default",
	}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSyncerFollowerDoesNotSync(t *testing.T) {
	client := &fakeSecretsClient{secrets: []controlplane.Secret{
		{ProjectID: "proj-1", Name: "DB_PASSWORD", Value: "secret", Version: 1},
	}}
	manager := &recordingSecretManager{}

	cfg := syncerTestConfig(t)
	cfg.IsLeader = func() bool { return false }

	s := New(client, manager, cfg, quietLogger())
	s.sync(context.Background())

	assert.Zero(t, manager.applies.Load(), "follower must not apply secrets")
	assert.Zero(t, manager.lists.Load(), "follower must not touch the cluster at all")
}

func TestSyncerLeaderSyncs(t *testing.T) {
	client := &fakeSecretsClient{secrets: []controlplane.Secret{
		{ProjectID: "proj-1", Name: "DB_PASSWORD", Value: "secret", Version: 1},
	}}
	manager := &recordingSecretManager{}

	cfg := syncerTestConfig(t)
	cfg.IsLeader = func() bool { return true }

	s := New(client, manager, cfg, quietLogger())
	s.sync(context.Background())

	assert.Equal(t, int32(1), manager.applies.Load(), "leader must apply secrets")
}

func TestSyncerNilGateSyncs(t *testing.T) {
	client := &fakeSecretsClient{secrets: []controlplane.Secret{
		{ProjectID: "proj-1", Name: "DB_PASSWORD", Value: "secret", Version: 1},
	}}
	manager := &recordingSecretManager{}

	cfg := syncerTestConfig(t) // IsLeader nil => legacy behaviour
	s := New(client, manager, cfg, quietLogger())
	s.sync(context.Background())

	assert.Equal(t, int32(1), manager.applies.Load(), "nil gate must sync (legacy behaviour)")
}
