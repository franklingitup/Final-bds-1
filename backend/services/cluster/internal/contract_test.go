package cluster

import (
	"reflect"
	"regexp"
	"testing"
)

// eventContract defines expected properties for a domain event.
type eventContract struct {
	Name    string // Canonical event name without version.
	Version int
	Owner   string // Producing service.
}

var clusterContracts = []eventContract{
	{Name: "cluster.created", Version: 1, Owner: "cluster"},
	{Name: "cluster.registration.token.created", Version: 1, Owner: "cluster"},
	{Name: "cluster.registered", Version: 1, Owner: "cluster"},
	{Name: "cluster.heartbeat.received", Version: 1, Owner: "cluster"},
	{Name: "cluster.disconnected", Version: 1, Owner: "cluster"},
	{Name: "cluster.deleted", Version: 1, Owner: "cluster"},
}

// TestClusterEventNamesAndOwnership verifies that all event constants follow
// the canonical naming convention and are owned by the cluster service.
func TestClusterEventNamesAndOwnership(t *testing.T) {
	eventConstants := map[string]bool{
		EventClusterCreated:           true,
		EventRegistrationTokenCreated: true,
		EventClusterRegistered:        true,
		EventClusterHeartbeatReceived: true,
		EventClusterDisconnected:      true,
		EventClusterDeleted:           true,
	}

	// Verify naming convention: domain.resource.action (no version suffix in constant).
	namingPattern := regexp.MustCompile(`^cluster\.[a-z]+(\.[a-z]+)*$`)
	for name := range eventConstants {
		if !namingPattern.MatchString(name) {
			t.Errorf("event %q does not match naming convention <domain>.<resource>.<action>", name)
		}
	}

	// Verify all contracts have matching constants.
	for _, c := range clusterContracts {
		if !eventConstants[c.Name] {
			t.Errorf("contract %q has no matching event constant", c.Name)
		}
		if c.Owner != "cluster" {
			t.Errorf("event %q owned by %q, expected cluster", c.Name, c.Owner)
		}
		if c.Version != eventVersion {
			t.Errorf("event %q version = %d, want %d", c.Name, c.Version, eventVersion)
		}
	}
}

// TestClusterEventPayloadsAreClean verifies that event payloads do not
// duplicate envelope metadata or contain sensitive data.
func TestClusterEventPayloadsAreClean(t *testing.T) {
	// Forbidden fields that belong in the envelope, not the payload.
	forbiddenFields := []string{
		"eventId", "eventName", "occurredAt", "correlationId", "traceparent",
		"actor", "orgId", "organizationId",
	}

	// Sensitive fields that should never appear in payloads.
	sensitiveFields := []string{
		"token",        // Plaintext registration tokens go through notifier.
		"tokenHash",    // Internal implementation detail.
		"password",     // Should never exist but check anyway.
		"secret",       // Generic sensitive marker.
		"apiKey",       // Should never be in events.
		"refreshToken", // Should never be in events.
	}

	payloadTypes := []struct {
		name    string
		payload any
	}{
		{"cluster.created", clusterCreatedPayload{}},
		{"cluster.registration.token.created", tokenCreatedPayload{}},
		{"cluster.registered", clusterRegisteredPayload{}},
		{"cluster.heartbeat.received", heartbeatReceivedPayload{}},
		{"cluster.disconnected", clusterDisconnectedPayload{}},
		{"cluster.deleted", clusterDeletedPayload{}},
	}

	for _, pt := range payloadTypes {
		fields := getJSONFields(pt.payload)

		for _, forbidden := range forbiddenFields {
			if _, ok := fields[forbidden]; ok {
				t.Errorf("%s payload contains forbidden field %q (belongs in envelope)", pt.name, forbidden)
			}
		}

		for _, sensitive := range sensitiveFields {
			if _, ok := fields[sensitive]; ok {
				t.Errorf("%s payload contains sensitive field %q", pt.name, sensitive)
			}
		}
	}
}

// TestClusterEventPayloadContents verifies specific payload content rules.
func TestClusterEventPayloadContents(t *testing.T) {
	t.Run("tokenCreatedPayload has deliveryRef", func(t *testing.T) {
		fields := getJSONFields(tokenCreatedPayload{})
		if _, ok := fields["deliveryRef"]; !ok {
			t.Error("tokenCreatedPayload must have deliveryRef field for out-of-band token delivery")
		}
		if _, ok := fields["token"]; ok {
			t.Error("tokenCreatedPayload must not contain plaintext token")
		}
	})

	t.Run("heartbeatReceivedPayload includes health status", func(t *testing.T) {
		fields := getJSONFields(heartbeatReceivedPayload{})
		if _, ok := fields["apiServerHealthy"]; !ok {
			t.Error("heartbeatReceivedPayload must include apiServerHealthy field")
		}
	})

	t.Run("clusterRegisteredPayload includes inventory", func(t *testing.T) {
		fields := getJSONFields(clusterRegisteredPayload{})
		required := []string{"clusterId", "agentId", "kubernetesVersion", "nodeCount"}
		for _, f := range required {
			if _, ok := fields[f]; !ok {
				t.Errorf("clusterRegisteredPayload missing required field %q", f)
			}
		}
	})
}

// getJSONFields extracts JSON field names from a struct via reflection.
func getJSONFields(v any) map[string]bool {
	fields := make(map[string]bool)
	extractFields(v, fields)
	return fields
}

func extractFields(v any, fields map[string]bool) {
	// Use JSON tag names from struct fields.
	t := reflect.TypeOf(v)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		// Remove omitempty suffix.
		name := tag
		if idx := len(tag) - 1; idx > 0 {
			for j, c := range tag {
				if c == ',' {
					name = tag[:j]
					break
				}
			}
		}
		fields[name] = true
	}
}
