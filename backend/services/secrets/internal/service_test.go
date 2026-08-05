package secrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// Test fixtures.
var (
	testOrgID     = "org-123"
	testUserID    = "user-456"
	testProjectID = "project-789"
)

type fakeSecretStore struct {
	secrets map[string]*Secret
	counter int
}

func newFakeSecretStore() *fakeSecretStore {
	return &fakeSecretStore{secrets: make(map[string]*Secret)}
}

func (f *fakeSecretStore) Create(ctx context.Context, s *Secret) error {
	f.counter++
	s.ID = fmt.Sprintf("secret-%s-%d", time.Now().Format("150405"), f.counter)
	s.Version = 1
	s.CreatedAt = time.Now()
	s.UpdatedAt = time.Now()
	f.secrets[s.ID] = s
	return nil
}

func (f *fakeSecretStore) GetByID(ctx context.Context, id string) (*Secret, error) {
	s, ok := f.secrets[id]
	if !ok || s.DeletedAt != nil {
		return nil, apperrors.NotFound("secret not found")
	}
	return s, nil
}

func (f *fakeSecretStore) GetByName(ctx context.Context, projectID, name string) (*Secret, error) {
	for _, s := range f.secrets {
		if s.ProjectID == projectID && s.Name == name && s.DeletedAt == nil {
			return s, nil
		}
	}
	return nil, apperrors.NotFound("secret not found")
}

func (f *fakeSecretStore) List(ctx context.Context, projectID string, page database.PageRequest) (database.Page[Secret], error) {
	var items []Secret
	for _, s := range f.secrets {
		if s.ProjectID == projectID && s.DeletedAt == nil {
			items = append(items, *s)
		}
	}
	return database.Page[Secret]{Items: items}, nil
}

func (f *fakeSecretStore) Update(ctx context.Context, s *Secret) error {
	if _, ok := f.secrets[s.ID]; !ok {
		return apperrors.NotFound("secret not found")
	}
	s.Version++
	s.UpdatedAt = time.Now()
	f.secrets[s.ID] = s
	return nil
}

func (f *fakeSecretStore) Delete(ctx context.Context, id string) error {
	s, ok := f.secrets[id]
	if !ok || s.DeletedAt != nil {
		return apperrors.NotFound("secret not found")
	}
	now := time.Now()
	s.DeletedAt = &now
	return nil
}

// GetSecretsForCluster now requires orgID for defense-in-depth (CRIT-001 fix).
func (f *fakeSecretStore) GetSecretsForCluster(ctx context.Context, orgID, clusterID string) ([]Secret, error) {
	// SECURITY: Validate orgID is provided
	if orgID == "" {
		return nil, ErrInvalidOrgID
	}

	var result []Secret
	for _, s := range f.secrets {
		// SECURITY: Filter by orgID (defense-in-depth, simulating explicit SQL filter)
		if s.DeletedAt == nil && s.OrgID == orgID {
			result = append(result, *s)
		}
	}
	return result, nil
}

// fakeSecretStoreWithOrgTracking tracks which orgID was passed for testing.
type fakeSecretStoreWithOrgTracking struct {
	*fakeSecretStore
	lastOrgID     string
	lastClusterID string
}

type fakeOutbox struct {
	events []events.Envelope
}

func (f *fakeOutbox) Enqueue(ctx context.Context, env events.Envelope) error {
	f.events = append(f.events, env)
	return nil
}

func (f *fakeOutbox) FetchUnpublished(ctx context.Context, limit int) ([]events.OutboxRecord, error) {
	return nil, nil
}

func (f *fakeOutbox) MarkPublished(ctx context.Context, ids []string) error {
	return nil
}

type fakeTenantRunner struct{}

func (f *fakeTenantRunner) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	return fn(ctx)
}

type fakeMemberLookup struct {
	role authz.ProjectRole
}

func (f *fakeMemberLookup) GetByUser(ctx context.Context, projectID, userID string) (authz.ProjectRole, error) {
	if f.role == "" {
		return "", apperrors.NotFound("member not found")
	}
	return f.role, nil
}

func TestService_CreateSecret(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()
	outbox := &fakeOutbox{}

	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     outbox,
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectDeveloper},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	tests := []struct {
		name    string
		req     CreateSecretRequest
		wantErr bool
	}{
		{
			name: "valid secret",
			req: CreateSecretRequest{
				Name:        "DATABASE_URL",
				Value:       "postgres://localhost/test",
				Description: stringPtr("Test database"),
			},
			wantErr: false,
		},
		{
			name: "invalid name - lowercase",
			req: CreateSecretRequest{
				Name:  "database_url",
				Value: "value",
			},
			wantErr: true,
		},
		{
			name: "invalid name - starts with number",
			req: CreateSecretRequest{
				Name:  "1DATABASE",
				Value: "value",
			},
			wantErr: true,
		},
		{
			name: "empty value",
			req: CreateSecretRequest{
				Name:  "EMPTY_SECRET",
				Value: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret, err := svc.CreateSecret(context.Background(), testOrgID, testUserID, testProjectID, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if secret == nil {
					t.Error("expected secret, got nil")
					return
				}
				if secret.Name != tt.req.Name {
					t.Errorf("name: got %q, want %q", secret.Name, tt.req.Name)
				}
				// Verify value is encrypted (not plaintext).
				if string(secret.EncryptedValue) == tt.req.Value {
					t.Error("encrypted value equals plaintext")
				}
				// Verify hash is set.
				if secret.ValueHash == "" {
					t.Error("value hash is empty")
				}
			}
		})
	}
}

func TestService_CreateSecret_DuplicateName(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()

	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     &fakeOutbox{},
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectDeveloper},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	req := CreateSecretRequest{
		Name:  "MY_SECRET",
		Value: "value1",
	}

	// Create first secret.
	_, err := svc.CreateSecret(context.Background(), testOrgID, testUserID, testProjectID, req)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	// Try to create duplicate.
	_, err = svc.CreateSecret(context.Background(), testOrgID, testUserID, testProjectID, req)
	if err == nil {
		t.Error("expected error for duplicate name")
	}
	if apperrors.From(err).Code != apperrors.CodeConflict {
		t.Errorf("expected conflict error, got: %v", err)
	}
}

func TestService_GetSecret(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()
	outbox := &fakeOutbox{}

	// Use developer role which can both create and read secrets.
	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     outbox,
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectDeveloper},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	// Create a secret first.
	created, err := svc.CreateSecret(context.Background(), testOrgID, testUserID, testProjectID, CreateSecretRequest{
		Name:  "TEST_SECRET",
		Value: "test value",
	})
	if err != nil {
		t.Fatalf("CreateSecret failed: %v", err)
	}

	// Get the secret.
	secret, err := svc.GetSecret(context.Background(), testOrgID, testUserID, testProjectID, created.ID)
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}

	if secret.ID != created.ID {
		t.Errorf("id: got %q, want %q", secret.ID, created.ID)
	}
	if secret.Name != "TEST_SECRET" {
		t.Errorf("name: got %q, want %q", secret.Name, "TEST_SECRET")
	}
}

func TestService_UpdateSecret(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()

	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     &fakeOutbox{},
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectDeveloper},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	// Create a secret.
	created, _ := svc.CreateSecret(context.Background(), testOrgID, testUserID, testProjectID, CreateSecretRequest{
		Name:  "UPDATE_TEST",
		Value: "original value",
	})
	originalHash := created.ValueHash

	// Update value.
	newValue := "updated value"
	updated, err := svc.UpdateSecret(context.Background(), testOrgID, testUserID, testProjectID, created.ID, UpdateSecretRequest{
		Value: &newValue,
	})
	if err != nil {
		t.Fatalf("UpdateSecret failed: %v", err)
	}

	if updated.Version != 2 {
		t.Errorf("version: got %d, want 2", updated.Version)
	}
	if updated.ValueHash == originalHash {
		t.Error("value hash should have changed")
	}
}

func TestService_DeleteSecret(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()

	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     &fakeOutbox{},
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectAdmin},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	// Create a secret.
	created, _ := svc.CreateSecret(context.Background(), testOrgID, testUserID, testProjectID, CreateSecretRequest{
		Name:  "DELETE_TEST",
		Value: "to be deleted",
	})

	// Delete it.
	err := svc.DeleteSecret(context.Background(), testOrgID, testUserID, testProjectID, created.ID)
	if err != nil {
		t.Fatalf("DeleteSecret failed: %v", err)
	}

	// Verify it's gone.
	_, err = svc.GetSecret(context.Background(), testOrgID, testUserID, testProjectID, created.ID)
	if err == nil {
		t.Error("expected error for deleted secret")
	}
}

func TestService_Authorization(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)

	tests := []struct {
		name       string
		role       authz.ProjectRole
		action     string
		wantErr    bool
	}{
		{"viewer can read", authz.ProjectViewer, "read", false},
		{"viewer cannot create", authz.ProjectViewer, "create", true},
		{"viewer cannot delete", authz.ProjectViewer, "delete", true},
		{"developer can read", authz.ProjectDeveloper, "read", false},
		{"developer can create", authz.ProjectDeveloper, "create", false},
		{"developer cannot delete", authz.ProjectDeveloper, "delete", true},
		{"admin can read", authz.ProjectAdmin, "read", false},
		{"admin can create", authz.ProjectAdmin, "create", false},
		{"admin can delete", authz.ProjectAdmin, "delete", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeSecretStore()
			svc := NewService(Deps{
				Secrets:    store,
				Encryptor:  enc,
				Outbox:     &fakeOutbox{},
				Tenant:     &fakeTenantRunner{},
				Members:    &fakeMemberLookup{role: tt.role},
				Authorizer: authz.NewPolicyAuthorizer(),
				Logger:     slog.Default(),
			})

			var err error
			ctx := context.Background()

			switch tt.action {
			case "read":
				// First create with admin role.
				adminSvc := NewService(Deps{
					Secrets:    store,
					Encryptor:  enc,
					Outbox:     &fakeOutbox{},
					Tenant:     &fakeTenantRunner{},
					Members:    &fakeMemberLookup{role: authz.ProjectAdmin},
					Authorizer: authz.NewPolicyAuthorizer(),
					Logger:     slog.Default(),
				})
				created, _ := adminSvc.CreateSecret(ctx, testOrgID, testUserID, testProjectID, CreateSecretRequest{
					Name:  "READ_TEST",
					Value: "value",
				})
				_, err = svc.GetSecret(ctx, testOrgID, testUserID, testProjectID, created.ID)

			case "create":
				_, err = svc.CreateSecret(ctx, testOrgID, testUserID, testProjectID, CreateSecretRequest{
					Name:  "CREATE_TEST",
					Value: "value",
				})

			case "delete":
				// First create with admin role.
				adminSvc := NewService(Deps{
					Secrets:    store,
					Encryptor:  enc,
					Outbox:     &fakeOutbox{},
					Tenant:     &fakeTenantRunner{},
					Members:    &fakeMemberLookup{role: authz.ProjectAdmin},
					Authorizer: authz.NewPolicyAuthorizer(),
					Logger:     slog.Default(),
				})
				created, _ := adminSvc.CreateSecret(ctx, testOrgID, testUserID, testProjectID, CreateSecretRequest{
					Name:  "DELETE_TEST",
					Value: "value",
				})
				err = svc.DeleteSecret(ctx, testOrgID, testUserID, testProjectID, created.ID)
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("got error=%v, wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if apperrors.From(err).Code != apperrors.CodeForbidden {
					t.Errorf("expected forbidden error, got: %v", err)
				}
			}
		})
	}
}

func TestService_EventsEmitted(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()
	outbox := &fakeOutbox{}

	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     outbox,
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectAdmin},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	ctx := context.Background()

	// Create.
	secret, _ := svc.CreateSecret(ctx, testOrgID, testUserID, testProjectID, CreateSecretRequest{
		Name:  "EVENT_TEST",
		Value: "value",
	})
	if len(outbox.events) != 1 {
		t.Errorf("expected 1 event, got %d", len(outbox.events))
	}
	if outbox.events[0].Type != EventSecretCreated {
		t.Errorf("expected %s, got %s", EventSecretCreated, outbox.events[0].Type)
	}

	// Update.
	newValue := "new value"
	svc.UpdateSecret(ctx, testOrgID, testUserID, testProjectID, secret.ID, UpdateSecretRequest{Value: &newValue})
	if len(outbox.events) != 2 {
		t.Errorf("expected 2 events, got %d", len(outbox.events))
	}
	if outbox.events[1].Type != EventSecretUpdated {
		t.Errorf("expected %s, got %s", EventSecretUpdated, outbox.events[1].Type)
	}

	// Delete.
	svc.DeleteSecret(ctx, testOrgID, testUserID, testProjectID, secret.ID)
	if len(outbox.events) != 3 {
		t.Errorf("expected 3 events, got %d", len(outbox.events))
	}
	if outbox.events[2].Type != EventSecretDeleted {
		t.Errorf("expected %s, got %s", EventSecretDeleted, outbox.events[2].Type)
	}
}

// SECURITY TEST: Verify plaintext never in events.
func TestService_PlaintextNotInEvents(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()
	outbox := &fakeOutbox{}

	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     outbox,
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectAdmin},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	secretValue := "super_secret_password_123!"
	ctx := context.Background()

	// Create secret.
	secret, _ := svc.CreateSecret(ctx, testOrgID, testUserID, testProjectID, CreateSecretRequest{
		Name:  "SECURITY_TEST",
		Value: secretValue,
	})

	// Update secret.
	newValue := "another_secret_value_456!"
	svc.UpdateSecret(ctx, testOrgID, testUserID, testProjectID, secret.ID, UpdateSecretRequest{Value: &newValue})

	// Check all event payloads don't contain plaintext.
	for _, env := range outbox.events {
		payloadJSON, _ := env.Payload.MarshalJSON()
		payloadStr := string(payloadJSON)

		if contains(payloadStr, secretValue) {
			t.Errorf("event %s payload contains original secret value", env.Type)
		}
		if contains(payloadStr, newValue) {
			t.Errorf("event %s payload contains updated secret value", env.Type)
		}
	}
}

func TestService_GetSecretsForCluster(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()

	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     &fakeOutbox{},
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectAdmin},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	ctx := context.Background()

	// Create secrets.
	svc.CreateSecret(ctx, testOrgID, testUserID, testProjectID, CreateSecretRequest{
		Name:  "SECRET1",
		Value: "value1",
	})
	svc.CreateSecret(ctx, testOrgID, testUserID, testProjectID, CreateSecretRequest{
		Name:  "SECRET2",
		Value: "value2",
	})

	// Get secrets for cluster.
	secrets, err := svc.GetSecretsForCluster(ctx, testOrgID, "cluster-123")
	if err != nil {
		t.Fatalf("GetSecretsForCluster failed: %v", err)
	}

	if len(secrets) != 2 {
		t.Errorf("expected 2 secrets, got %d", len(secrets))
	}

	// Verify values are decrypted.
	for _, s := range secrets {
		if s.Value == "" {
			t.Errorf("secret %s has empty value", s.Name)
		}
	}
}

func stringPtr(s string) *string { return &s }

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && errors.Is(nil, nil) && s != "" && substr != "" && len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsInner(s, substr)))
}

func containsInner(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// SECURITY TESTS - CRIT-001 FIX: Cross-Tenant Isolation
// ============================================================================

// TestSecrets_CrossTenantIsolation verifies that Agent A can only access Org A secrets.
func TestSecrets_CrossTenantIsolation(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()

	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     &fakeOutbox{},
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectAdmin},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	ctx := context.Background()
	orgA := "org-a-uuid"
	orgB := "org-b-uuid"

	// Create secrets for Org A
	svc.CreateSecret(ctx, orgA, testUserID, "project-a", CreateSecretRequest{
		Name:  "ORG_A_SECRET",
		Value: "org-a-value",
	})

	// Create secrets for Org B
	svc.CreateSecret(ctx, orgB, testUserID, "project-b", CreateSecretRequest{
		Name:  "ORG_B_SECRET",
		Value: "org-b-value",
	})

	// Agent A requests secrets (authenticated as Org A)
	secretsForOrgA, err := svc.GetSecretsForCluster(ctx, orgA, "cluster-a")
	if err != nil {
		t.Fatalf("GetSecretsForCluster failed: %v", err)
	}

	// SECURITY ASSERTION: Only Org A secrets should be returned
	for _, s := range secretsForOrgA {
		if s.Name == "ORG_B_SECRET" {
			t.Errorf("SECURITY VIOLATION: Org B secret returned to Org A agent")
		}
	}

	// Verify Org A secret IS returned
	foundOrgA := false
	for _, s := range secretsForOrgA {
		if s.Name == "ORG_A_SECRET" {
			foundOrgA = true
			break
		}
	}
	if !foundOrgA {
		t.Errorf("Org A secret should be returned to Org A agent")
	}

	// Agent B requests secrets (authenticated as Org B)
	secretsForOrgB, err := svc.GetSecretsForCluster(ctx, orgB, "cluster-b")
	if err != nil {
		t.Fatalf("GetSecretsForCluster failed: %v", err)
	}

	// SECURITY ASSERTION: Only Org B secrets should be returned
	for _, s := range secretsForOrgB {
		if s.Name == "ORG_A_SECRET" {
			t.Errorf("SECURITY VIOLATION: Org A secret returned to Org B agent")
		}
	}
}

// TestSecrets_FakeOrgID verifies that using wrong orgID returns zero secrets.
func TestSecrets_FakeOrgID(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()

	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     &fakeOutbox{},
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectAdmin},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	ctx := context.Background()
	realOrgID := "real-org-uuid"
	fakeOrgID := "fake-org-uuid"

	// Create secrets for real org
	svc.CreateSecret(ctx, realOrgID, testUserID, testProjectID, CreateSecretRequest{
		Name:  "REAL_ORG_SECRET",
		Value: "secret-value",
	})

	// ATTACK: Try to access with fake org ID
	secrets, err := svc.GetSecretsForCluster(ctx, fakeOrgID, "cluster-123")
	if err != nil {
		t.Fatalf("GetSecretsForCluster failed: %v", err)
	}

	// SECURITY ASSERTION: Zero secrets should be returned
	if len(secrets) != 0 {
		t.Errorf("SECURITY VIOLATION: Expected 0 secrets for fake org, got %d", len(secrets))
		for _, s := range secrets {
			t.Errorf("  - Leaked secret: %s", s.Name)
		}
	}
}

// TestSecrets_EmptyOrgIDRejected verifies that empty orgID is rejected.
func TestSecrets_EmptyOrgIDRejected(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)
	store := newFakeSecretStore()

	svc := NewService(Deps{
		Secrets:    store,
		Encryptor:  enc,
		Outbox:     &fakeOutbox{},
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectAdmin},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	ctx := context.Background()

	// Create a secret first
	svc.CreateSecret(ctx, testOrgID, testUserID, testProjectID, CreateSecretRequest{
		Name:  "TEST_SECRET",
		Value: "value",
	})

	// ATTACK: Try to access with empty org ID
	secrets, err := svc.GetSecretsForCluster(ctx, "", "cluster-123")

	// SECURITY ASSERTION: Should return an error
	if err == nil {
		t.Errorf("SECURITY VIOLATION: Empty orgID should be rejected")
	}
	if err != ErrInvalidOrgID {
		t.Errorf("Expected ErrInvalidOrgID, got: %v", err)
	}
	if secrets != nil && len(secrets) > 0 {
		t.Errorf("SECURITY VIOLATION: Secrets returned with empty orgID")
	}
}

// TestRepository_OrgIDPassedToQuery verifies repository receives orgID parameter.
func TestRepository_OrgIDPassedToQuery(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)

	// Create a tracking store
	baseStore := newFakeSecretStore()
	trackingStore := &orgTrackingSecretStore{
		fakeSecretStore: baseStore,
	}

	svc := NewService(Deps{
		Secrets:    trackingStore,
		Encryptor:  enc,
		Outbox:     &fakeOutbox{},
		Tenant:     &fakeTenantRunner{},
		Members:    &fakeMemberLookup{role: authz.ProjectAdmin},
		Authorizer: authz.NewPolicyAuthorizer(),
		Logger:     slog.Default(),
	})

	ctx := context.Background()
	expectedOrgID := "expected-org-uuid"
	expectedClusterID := "expected-cluster-uuid"

	svc.GetSecretsForCluster(ctx, expectedOrgID, expectedClusterID)

	// Verify orgID was passed
	if trackingStore.lastOrgID != expectedOrgID {
		t.Errorf("orgID not passed to repository: got %q, want %q", trackingStore.lastOrgID, expectedOrgID)
	}
	if trackingStore.lastClusterID != expectedClusterID {
		t.Errorf("clusterID not passed to repository: got %q, want %q", trackingStore.lastClusterID, expectedClusterID)
	}
}

// orgTrackingSecretStore tracks orgID/clusterID passed to GetSecretsForCluster.
type orgTrackingSecretStore struct {
	*fakeSecretStore
	lastOrgID     string
	lastClusterID string
}

func (s *orgTrackingSecretStore) GetSecretsForCluster(ctx context.Context, orgID, clusterID string) ([]Secret, error) {
	s.lastOrgID = orgID
	s.lastClusterID = clusterID
	return s.fakeSecretStore.GetSecretsForCluster(ctx, orgID, clusterID)
}
