package secrets

import (
	"encoding/json"
	"strings"
	"testing"
)

// Contract tests verify that events conform to the event catalog.
// These tests prevent accidental schema drift.

func TestEventContract_SecretCreated(t *testing.T) {
	payload := secretCreatedPayload{
		SecretID:  "secret-123",
		ProjectID: "project-456",
		Name:      "DATABASE_URL",
		Version:   1,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	// Verify required fields.
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	required := []string{"secretId", "projectId", "name", "version"}
	for _, field := range required {
		if _, ok := decoded[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// SECURITY: Verify no sensitive fields.
	forbidden := []string{"value", "encryptedValue", "plaintext", "secret"}
	for _, field := range forbidden {
		if _, ok := decoded[field]; ok {
			t.Errorf("payload contains forbidden field: %s", field)
		}
	}

	// Verify no sensitive values in serialized form.
	dataStr := string(data)
	if strings.Contains(dataStr, "postgres://") {
		t.Error("payload contains database connection string")
	}
	if strings.Contains(dataStr, "sk_live_") {
		t.Error("payload contains API key prefix")
	}
}

func TestEventContract_SecretUpdated(t *testing.T) {
	payload := secretUpdatedPayload{
		SecretID:  "secret-123",
		ProjectID: "project-456",
		Name:      "DATABASE_URL",
		Version:   2,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	required := []string{"secretId", "projectId", "name", "version"}
	for _, field := range required {
		if _, ok := decoded[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// SECURITY: Verify no sensitive fields.
	forbidden := []string{"value", "encryptedValue", "oldValue", "newValue", "plaintext"}
	for _, field := range forbidden {
		if _, ok := decoded[field]; ok {
			t.Errorf("payload contains forbidden field: %s", field)
		}
	}
}

func TestEventContract_SecretDeleted(t *testing.T) {
	payload := secretDeletedPayload{
		SecretID:  "secret-123",
		ProjectID: "project-456",
		Name:      "DATABASE_URL",
		DeletedBy: "user-789",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	required := []string{"secretId", "projectId", "name", "deletedBy"}
	for _, field := range required {
		if _, ok := decoded[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// SECURITY: Verify no sensitive fields.
	forbidden := []string{"value", "encryptedValue", "plaintext"}
	for _, field := range forbidden {
		if _, ok := decoded[field]; ok {
			t.Errorf("payload contains forbidden field: %s", field)
		}
	}
}

func TestEventContract_EventNames(t *testing.T) {
	tests := []struct {
		constant string
		expected string
	}{
		{EventSecretCreated, "secret.created.v1"},
		{EventSecretUpdated, "secret.updated.v1"},
		{EventSecretDeleted, "secret.deleted.v1"},
	}

	for _, tt := range tests {
		if tt.constant != tt.expected {
			t.Errorf("event name mismatch: got %q, want %q", tt.constant, tt.expected)
		}
	}
}

func TestEventContract_NamingConvention(t *testing.T) {
	events := []string{
		EventSecretCreated,
		EventSecretUpdated,
		EventSecretDeleted,
	}

	for _, event := range events {
		// Format: <domain>.<resource>.<action>.v<version>
		parts := strings.Split(event, ".")
		if len(parts) != 3 {
			t.Errorf("event %q doesn't follow naming convention", event)
			continue
		}

		domain := parts[0]
		action := parts[1]
		version := parts[2]

		if domain != "secret" {
			t.Errorf("event %q: domain should be 'secret', got %q", event, domain)
		}

		validActions := map[string]bool{"created": true, "updated": true, "deleted": true}
		if !validActions[action] {
			t.Errorf("event %q: invalid action %q", event, action)
		}

		if !strings.HasPrefix(version, "v") {
			t.Errorf("event %q: version should start with 'v', got %q", event, version)
		}
	}
}

// SECURITY: Verify view models never expose plaintext.
func TestSecretView_NeverExposesPlaintext(t *testing.T) {
	secret := &Secret{
		EncryptedValue: []byte("encrypted-data-here"),
		ValueHash:      "sha256hash",
	}
	secret.ID = "secret-123"
	secret.OrgID = "org-456"
	secret.ProjectID = "project-789"
	secret.Name = "MY_SECRET"

	view := secret.ToView()
	data, _ := json.Marshal(view)
	dataStr := string(data)

	// Verify encrypted value is not in view.
	if strings.Contains(dataStr, "encrypted") {
		t.Error("view contains 'encrypted' - might be exposing encrypted value")
	}

	// Verify value hash is not in view (no need to expose).
	if strings.Contains(dataStr, "sha256") {
		t.Error("view contains value hash")
	}

	// Verify no value field.
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	forbidden := []string{"value", "encryptedValue", "encrypted_value", "valueHash", "value_hash"}
	for _, field := range forbidden {
		if _, ok := decoded[field]; ok {
			t.Errorf("view contains forbidden field: %s", field)
		}
	}
}

// SECURITY: Verify AgentSecret does contain value (for agent only).
func TestAgentSecret_ContainsValue(t *testing.T) {
	agentSecret := AgentSecret{
		ProjectID: "project-123",
		Name:      "DATABASE_URL",
		Value:     "postgres://localhost/db",
		Version:   1,
	}

	data, _ := json.Marshal(agentSecret)

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	// Agent secrets SHOULD contain value.
	if _, ok := decoded["value"]; !ok {
		t.Error("agent secret should contain value field")
	}
}
