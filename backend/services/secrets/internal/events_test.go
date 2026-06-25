package secrets

import (
	"testing"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// TestEventTypeNaming_NoDoubleVersion verifies that event types do NOT include
// .v1 suffix (SEC-CRIT-05). The version is set separately in the envelope.
func TestEventTypeNaming_NoDoubleVersion(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
	}{
		{"secret.created", EventSecretCreated},
		{"secret.updated", EventSecretUpdated},
		{"secret.deleted", EventSecretDeleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Event types should NOT contain ".v1"
			if tt.eventType != tt.name {
				t.Errorf("Event type mismatch: got %s, want %s", tt.eventType, tt.name)
			}

			// Verify envelope construction doesn't double the version
			env, err := events.New(tt.eventType, eventVersion, "org-123", nil)
			if err != nil {
				t.Fatalf("events.New() error: %v", err)
			}

			// Type should be exactly the event type (no version)
			if env.Type != tt.name {
				t.Errorf("Envelope.Type = %s, want %s", env.Type, tt.name)
			}

			// Version should be set separately
			if env.Version != eventVersion {
				t.Errorf("Envelope.Version = %d, want %d", env.Version, eventVersion)
			}
		})
	}
}

// TestEventSubject_NoDoubleVersion verifies that the NATS subject does not
// have a double version suffix.
func TestEventSubject_NoDoubleVersion(t *testing.T) {
	env, err := events.New(EventSecretCreated, eventVersion, "org-123", nil)
	if err != nil {
		t.Fatalf("events.New() error: %v", err)
	}

	// Generate subject using the events package
	subject := events.Subject("evt", env)

	// Subject should be "evt.secret.created.v1", NOT "evt.secret.created.v1.v1"
	expected := "evt.secret.created.v1"
	if subject != expected {
		t.Errorf("Subject = %s, want %s", subject, expected)
	}
}
