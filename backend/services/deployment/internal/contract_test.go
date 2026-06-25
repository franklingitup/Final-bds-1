package deployment

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Event contract tests verify that deployment domain events adhere to
// the platform's event contract as defined in docs/12-event-catalog.md.

// TestEventNamingConvention verifies event names match the <domain>.<resource>.<action> pattern.
func TestEventNamingConvention(t *testing.T) {
	events := []struct {
		name     string
		constant string
	}{
		{"application.created", EventApplicationCreated},
		{"application.updated", EventApplicationUpdated},
		{"application.deleted", EventApplicationDeleted},
		{"deployment.created", EventDeploymentCreated},
		{"deployment.started", EventDeploymentStarted},
		{"deployment.succeeded", EventDeploymentSucceeded},
		{"deployment.failed", EventDeploymentFailed},
		{"deployment.rollback.requested", EventDeploymentRollback},
	}

	for _, e := range events {
		t.Run(e.constant, func(t *testing.T) {
			assert.Equal(t, e.name, e.constant)

			parts := strings.Split(e.constant, ".")
			assert.GreaterOrEqual(t, len(parts), 2, "event name must have at least domain.action")

			domain := parts[0]
			assert.True(t, domain == "application" || domain == "deployment",
				"domain must be application or deployment")
		})
	}
}

// TestEventVersion verifies all events use version 1.
func TestEventVersion(t *testing.T) {
	assert.Equal(t, 1, eventVersion, "event version should be 1")
}

// TestEventPayloadNoDuplicateMetadata ensures payloads don't contain envelope fields.
func TestEventPayloadNoDuplicateMetadata(t *testing.T) {
	envelopeFields := []string{"eventId", "occurredAt", "correlationId", "traceParent", "actor", "orgId"}

	payloads := []struct {
		name    string
		payload any
	}{
		{"applicationCreatedPayload", applicationCreatedPayload{}},
		{"applicationUpdatedPayload", applicationUpdatedPayload{}},
		{"applicationDeletedPayload", applicationDeletedPayload{}},
		{"deploymentCreatedPayload", deploymentCreatedPayload{}},
		{"deploymentStartedPayload", deploymentStartedPayload{}},
		{"deploymentSucceededPayload", deploymentSucceededPayload{}},
		{"deploymentFailedPayload", deploymentFailedPayload{}},
		{"deploymentRollbackPayload", deploymentRollbackPayload{}},
	}

	for _, p := range payloads {
		t.Run(p.name, func(t *testing.T) {
			// Marshal to JSON to get actual field names.
			data, err := json.Marshal(p.payload)
			require.NoError(t, err)

			var fields map[string]any
			require.NoError(t, json.Unmarshal(data, &fields))

			for _, forbidden := range envelopeFields {
				_, exists := fields[forbidden]
				assert.False(t, exists, "payload should not contain envelope field: %s", forbidden)
			}
		})
	}
}

// TestEventPayloadNoSensitiveData ensures payloads don't expose sensitive information.
func TestEventPayloadNoSensitiveData(t *testing.T) {
	sensitivePatterns := []string{
		"password", "secret", "token", "key", "credential", "apiKey",
	}

	payloads := []struct {
		name    string
		payload any
	}{
		{"applicationCreatedPayload", applicationCreatedPayload{}},
		{"applicationUpdatedPayload", applicationUpdatedPayload{}},
		{"applicationDeletedPayload", applicationDeletedPayload{}},
		{"deploymentCreatedPayload", deploymentCreatedPayload{}},
		{"deploymentStartedPayload", deploymentStartedPayload{}},
		{"deploymentSucceededPayload", deploymentSucceededPayload{}},
		{"deploymentFailedPayload", deploymentFailedPayload{}},
		{"deploymentRollbackPayload", deploymentRollbackPayload{}},
	}

	for _, p := range payloads {
		t.Run(p.name, func(t *testing.T) {
			val := reflect.TypeOf(p.payload)
			for i := 0; i < val.NumField(); i++ {
				field := val.Field(i)
				jsonTag := field.Tag.Get("json")
				jsonName := strings.Split(jsonTag, ",")[0]

				for _, sensitive := range sensitivePatterns {
					assert.False(t,
						strings.Contains(strings.ToLower(jsonName), sensitive),
						"field %s appears to contain sensitive data", jsonName)
				}
			}
		})
	}
}

// TestEventProducerOwnership verifies deployment service owns its events.
func TestEventProducerOwnership(t *testing.T) {
	deploymentEvents := []string{
		EventApplicationCreated,
		EventApplicationUpdated,
		EventApplicationDeleted,
		EventDeploymentCreated,
		EventDeploymentStarted,
		EventDeploymentSucceeded,
		EventDeploymentFailed,
		EventDeploymentRollback,
	}

	for _, event := range deploymentEvents {
		t.Run(event, func(t *testing.T) {
			parts := strings.Split(event, ".")
			domain := parts[0]
			assert.True(t,
				domain == "application" || domain == "deployment",
				"deployment service should only own application.* and deployment.* events")
		})
	}
}

// TestApplicationCreatedPayloadSchema verifies required fields.
func TestApplicationCreatedPayloadSchema(t *testing.T) {
	payload := applicationCreatedPayload{
		ApplicationID: "app-123",
		ProjectID:     "proj-456",
		Name:          "My App",
		Slug:          "my-app",
		RuntimeType:   RuntimeContainer,
		CreatedBy:     "user-789",
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))

	requiredFields := []string{"applicationId", "projectId", "name", "slug", "runtimeType"}
	for _, field := range requiredFields {
		_, exists := decoded[field]
		assert.True(t, exists, "required field %s missing", field)
	}
}

// TestDeploymentCreatedPayloadSchema verifies required fields.
func TestDeploymentCreatedPayloadSchema(t *testing.T) {
	payload := deploymentCreatedPayload{
		DeploymentID:  "dep-123",
		ApplicationID: "app-456",
		ClusterID:     "cluster-789",
		Image:         "nginx:latest",
		Replicas:      3,
		Revision:      1,
		CreatedBy:     "user-000",
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))

	requiredFields := []string{"deploymentId", "applicationId", "clusterId", "image", "replicas", "revision"}
	for _, field := range requiredFields {
		_, exists := decoded[field]
		assert.True(t, exists, "required field %s missing", field)
	}
}

// TestDeploymentStartedPayloadSchema verifies required fields.
func TestDeploymentStartedPayloadSchema(t *testing.T) {
	payload := deploymentStartedPayload{
		DeploymentID: "dep-123",
		ReleaseID:    "rel-456",
		Revision:     2,
		Image:        "nginx:2.0",
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))

	requiredFields := []string{"deploymentId", "releaseId", "revision", "image"}
	for _, field := range requiredFields {
		_, exists := decoded[field]
		assert.True(t, exists, "required field %s missing", field)
	}
}

// TestDeploymentSucceededPayloadSchema verifies required fields.
func TestDeploymentSucceededPayloadSchema(t *testing.T) {
	payload := deploymentSucceededPayload{
		DeploymentID:  "dep-123",
		ReleaseID:     "rel-456",
		Revision:      2,
		ReadyReplicas: 3,
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))

	requiredFields := []string{"deploymentId", "releaseId", "revision", "readyReplicas"}
	for _, field := range requiredFields {
		_, exists := decoded[field]
		assert.True(t, exists, "required field %s missing", field)
	}
}

// TestDeploymentFailedPayloadSchema verifies required fields.
func TestDeploymentFailedPayloadSchema(t *testing.T) {
	payload := deploymentFailedPayload{
		DeploymentID: "dep-123",
		ReleaseID:    "rel-456",
		Revision:     2,
		ErrorMessage: "ImagePullBackOff",
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))

	requiredFields := []string{"deploymentId", "releaseId", "revision", "errorMessage"}
	for _, field := range requiredFields {
		_, exists := decoded[field]
		assert.True(t, exists, "required field %s missing", field)
	}
}

// TestDeploymentRollbackPayloadSchema verifies required fields.
func TestDeploymentRollbackPayloadSchema(t *testing.T) {
	payload := deploymentRollbackPayload{
		DeploymentID:   "dep-123",
		FromRevision:   3,
		TargetRevision: 1,
		RequestedBy:    "user-456",
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))

	requiredFields := []string{"deploymentId", "fromRevision", "targetRevision"}
	for _, field := range requiredFields {
		_, exists := decoded[field]
		assert.True(t, exists, "required field %s missing", field)
	}
}

// TestEventPayloadJSONRoundTrip verifies payloads serialize/deserialize correctly.
func TestEventPayloadJSONRoundTrip(t *testing.T) {
	payloads := []any{
		applicationCreatedPayload{
			ApplicationID: "app-123",
			ProjectID:     "proj-456",
			Name:          "Test App",
			Slug:          "test-app",
			RuntimeType:   RuntimeContainer,
			CreatedBy:     "user-789",
		},
		deploymentCreatedPayload{
			DeploymentID:  "dep-123",
			ApplicationID: "app-456",
			ClusterID:     "cluster-789",
			Image:         "nginx:latest",
			Replicas:      3,
			Revision:      1,
			CreatedBy:     "user-000",
		},
		deploymentFailedPayload{
			DeploymentID: "dep-123",
			ReleaseID:    "rel-456",
			Revision:     2,
			ErrorMessage: "CrashLoopBackOff: container \"app\" keeps restarting",
		},
	}

	for _, original := range payloads {
		t.Run(reflect.TypeOf(original).Name(), func(t *testing.T) {
			data, err := json.Marshal(original)
			require.NoError(t, err)

			// Verify it's valid JSON.
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(data, &decoded))

			// Re-marshal and compare.
			data2, err := json.Marshal(decoded)
			require.NoError(t, err)
			assert.JSONEq(t, string(data), string(data2))
		})
	}
}
