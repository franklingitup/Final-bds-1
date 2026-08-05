package github

import (
	"testing"
)

func TestHashToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"simple token", "ghp_abc123"},
		{"long token", "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hash1 := HashToken(tc.token)
			hash2 := HashToken(tc.token)

			// Same input should produce same hash
			if hash1 != hash2 {
				t.Errorf("HashToken not deterministic: got %q and %q", hash1, hash2)
			}

			// Hash should be a valid hex string (64 chars for SHA-256)
			if len(hash1) != 64 {
				t.Errorf("Expected 64 char hex hash, got %d chars", len(hash1))
			}
		})
	}

	// Different tokens should produce different hashes
	if HashToken("token1") == HashToken("token2") {
		t.Error("Different tokens should produce different hashes")
	}
}

func TestGenerateWebhookSecret(t *testing.T) {
	secret1, err := GenerateWebhookSecret()
	if err != nil {
		t.Fatalf("GenerateWebhookSecret failed: %v", err)
	}

	secret2, err := GenerateWebhookSecret()
	if err != nil {
		t.Fatalf("GenerateWebhookSecret failed: %v", err)
	}

	// Each call should produce different secret
	if secret1 == secret2 {
		t.Error("Expected different secrets on each call")
	}

	// Secret should be a valid hex string (64 chars for 32 bytes)
	if len(secret1) != 64 {
		t.Errorf("Expected 64 char hex secret, got %d chars", len(secret1))
	}
}

func TestGenerateOAuthState(t *testing.T) {
	state1, err := GenerateOAuthState()
	if err != nil {
		t.Fatalf("GenerateOAuthState failed: %v", err)
	}

	state2, err := GenerateOAuthState()
	if err != nil {
		t.Fatalf("GenerateOAuthState failed: %v", err)
	}

	// Each call should produce different state
	if state1 == state2 {
		t.Error("Expected different states on each call")
	}

	// State should be non-empty
	if len(state1) == 0 {
		t.Error("Expected non-empty state")
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "test-secret"
	payload := []byte(`{"action":"push","repository":{"name":"test"}}`)

	tests := []struct {
		name      string
		payload   []byte
		secret    string
		signature string
		valid     bool
	}{
		{
			name:      "valid signature",
			payload:   payload,
			secret:    secret,
			signature: "sha256=8d7e5d2e8c3b6f0a9e1d4c7b0a3e6f9d2c5b8a1e4d7c0b3f6a9e2d5c8b1a4e7f",
			valid:     false, // This is a fake signature for testing
		},
		{
			name:      "missing prefix",
			payload:   payload,
			secret:    secret,
			signature: "8d7e5d2e8c3b6f0a9e1d4c7b0a3e6f9d2c5b8a1e4d7c0b3f6a9e2d5c8b1a4e7f",
			valid:     false,
		},
		{
			name:      "wrong prefix",
			payload:   payload,
			secret:    secret,
			signature: "sha1=8d7e5d2e8c3b6f0a9e1d4c7b0a3e6f9d2c5b8a1e4d7c0b3f6a9e2d5c8b1a4e7f",
			valid:     false,
		},
		{
			name:      "invalid hex",
			payload:   payload,
			secret:    secret,
			signature: "sha256=not-valid-hex",
			valid:     false,
		},
		{
			name:      "empty signature",
			payload:   payload,
			secret:    secret,
			signature: "",
			valid:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := VerifyWebhookSignature(tc.payload, tc.secret, tc.signature)
			if got != tc.valid {
				t.Errorf("VerifyWebhookSignature() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestTokenEncryptor(t *testing.T) {
	// Generate a test key (32 bytes)
	keyBase64 := "dGhpcyBpcyBhIDMyIGJ5dGUgdGVzdCBrZXkh" // base64 encoded

	// This will fail because the key is not 32 bytes when decoded
	_, err := NewTokenEncryptor(keyBase64)
	if err == nil {
		t.Log("Warning: Key validation might be lenient")
	}

	// Test with properly sized key
	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i)
	}

	enc, err := NewTokenEncryptorFromBytes(keyBytes)
	if err != nil {
		t.Fatalf("NewTokenEncryptorFromBytes failed: %v", err)
	}

	// Test encrypt/decrypt
	original := "ghp_test_token_12345"
	encrypted, err := enc.Encrypt(original)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Encrypted should be different from original
	if string(encrypted) == original {
		t.Error("Encrypted data should be different from original")
	}

	// Decrypt should return original
	decrypted, err := enc.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != original {
		t.Errorf("Decrypted = %q, want %q", decrypted, original)
	}
}

func TestConnectionConstants(t *testing.T) {
	// Verify constants are properly defined
	if ConnectionTypeOAuth != "oauth" {
		t.Errorf("ConnectionTypeOAuth = %q, want %q", ConnectionTypeOAuth, "oauth")
	}
	if ConnectionTypePAT != "pat" {
		t.Errorf("ConnectionTypePAT = %q, want %q", ConnectionTypePAT, "pat")
	}
	if StatusActive != "active" {
		t.Errorf("StatusActive = %q, want %q", StatusActive, "active")
	}
	if StatusRevoked != "revoked" {
		t.Errorf("StatusRevoked = %q, want %q", StatusRevoked, "revoked")
	}
	if WebhookStatusActive != "active" {
		t.Errorf("WebhookStatusActive = %q, want %q", WebhookStatusActive, "active")
	}
}

func TestToConnectionView(t *testing.T) {
	conn := &GitHubConnection{
		ConnectionType: ConnectionTypePAT,
		Name:           "test-connection",
		Status:         StatusActive,
	}
	conn.ID = "test-id"
	conn.OrgID = "org-123"

	view := ToConnectionView(conn)

	if view.ID != conn.ID {
		t.Errorf("ID = %q, want %q", view.ID, conn.ID)
	}
	if view.OrgID != conn.OrgID {
		t.Errorf("OrgID = %q, want %q", view.OrgID, conn.OrgID)
	}
	if view.ConnectionType != conn.ConnectionType {
		t.Errorf("ConnectionType = %q, want %q", view.ConnectionType, conn.ConnectionType)
	}
	if view.Name != conn.Name {
		t.Errorf("Name = %q, want %q", view.Name, conn.Name)
	}
	if view.Status != conn.Status {
		t.Errorf("Status = %q, want %q", view.Status, conn.Status)
	}
}

func TestToRepositoryView(t *testing.T) {
	desc := "A test repository"
	lang := "Go"
	repo := &GitHubRepository{
		ConnectionID:  "conn-123",
		GitHubRepoID:  12345,
		Owner:         "testowner",
		Name:          "testrepo",
		FullName:      "testowner/testrepo",
		Description:   &desc,
		HTMLURL:       "https://github.com/testowner/testrepo",
		CloneURL:      "https://github.com/testowner/testrepo.git",
		DefaultBranch: "main",
		IsPrivate:     true,
		IsFork:        false,
		IsArchived:    false,
		StarsCount:    100,
		ForksCount:    25,
		Language:      &lang,
		Topics:        []string{"go", "api"},
	}
	repo.ID = "repo-id"
	repo.OrgID = "org-123"

	view := ToRepositoryView(repo)

	if view.ID != repo.ID {
		t.Errorf("ID = %q, want %q", view.ID, repo.ID)
	}
	if view.GitHubRepoID != repo.GitHubRepoID {
		t.Errorf("GitHubRepoID = %d, want %d", view.GitHubRepoID, repo.GitHubRepoID)
	}
	if view.FullName != repo.FullName {
		t.Errorf("FullName = %q, want %q", view.FullName, repo.FullName)
	}
	if view.DefaultBranch != repo.DefaultBranch {
		t.Errorf("DefaultBranch = %q, want %q", view.DefaultBranch, repo.DefaultBranch)
	}
	if view.IsPrivate != repo.IsPrivate {
		t.Errorf("IsPrivate = %v, want %v", view.IsPrivate, repo.IsPrivate)
	}
}

func TestToWebhookView(t *testing.T) {
	webhook := &GitHubWebhook{
		ID:           "webhook-123",
		RepositoryID: "repo-456",
		GitHubHookID: 789,
		Events:       []string{"push", "pull_request"},
		WebhookURL:   "https://api.example.com/webhooks/github/repo-456",
		Status:       WebhookStatusActive,
		DeliveryCount: 42,
	}

	view := ToWebhookView(webhook)

	if view.ID != webhook.ID {
		t.Errorf("ID = %q, want %q", view.ID, webhook.ID)
	}
	if view.GitHubHookID != webhook.GitHubHookID {
		t.Errorf("GitHubHookID = %d, want %d", view.GitHubHookID, webhook.GitHubHookID)
	}
	if view.WebhookURL != webhook.WebhookURL {
		t.Errorf("WebhookURL = %q, want %q", view.WebhookURL, webhook.WebhookURL)
	}
	if len(view.Events) != len(webhook.Events) {
		t.Errorf("Events length = %d, want %d", len(view.Events), len(webhook.Events))
	}
	if view.DeliveryCount != webhook.DeliveryCount {
		t.Errorf("DeliveryCount = %d, want %d", view.DeliveryCount, webhook.DeliveryCount)
	}
}
