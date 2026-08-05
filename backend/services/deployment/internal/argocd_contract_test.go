package deployment

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// TestGitOpsEventContract pins the wire contract (event type + payload field
// names) of the GitOps engine events. Downstream consumers depend on these, so
// changes here are intentional contract changes.
func TestGitOpsEventContract(t *testing.T) {
	cases := []struct {
		eventType string
		payload   any
		wantKeys  []string
	}{
		{
			EventDeploymentSyncStarted,
			deploymentSyncStartedPayload{DeploymentID: "d", Application: "a", Cluster: "c", Namespace: "n", Revision: "r"},
			[]string{"deploymentId", "application", "cluster", "namespace", "revision"},
		},
		{
			EventDeploymentSyncCompleted,
			deploymentSyncCompletedPayload{DeploymentID: "d", Application: "a", Revision: "r", SyncStatus: "Synced", HealthStatus: "Healthy"},
			[]string{"deploymentId", "application", "revision", "syncStatus", "healthStatus"},
		},
		{
			EventDeploymentSyncFailed,
			deploymentSyncFailedPayload{DeploymentID: "d", Application: "a", Revision: "r", Phase: "Failed", ErrorMessage: "boom"},
			[]string{"deploymentId", "application", "revision", "phase", "errorMessage"},
		},
		{
			EventDeploymentHealthChanged,
			deploymentHealthChangedPayload{DeploymentID: "d", Application: "a", From: "Progressing", To: "Healthy"},
			[]string{"deploymentId", "application", "from", "to"},
		},
		{
			EventDeploymentDriftDetected,
			deploymentDriftDetectedPayload{DeploymentID: "d", Application: "a", SyncStatus: "OutOfSync", HealthStatus: "Healthy"},
			[]string{"deploymentId", "application", "syncStatus", "healthStatus"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.eventType, func(t *testing.T) {
			evt, err := events.New(tc.eventType, eventVersion, "org-1", tc.payload,
				events.WithResource(events.Resource{Type: "deployment", ID: "d"}))
			require.NoError(t, err)
			assert.Equal(t, tc.eventType, evt.Type)
			assert.Equal(t, "org-1", evt.OrgID)

			var m map[string]any
			require.NoError(t, json.Unmarshal(evt.Payload, &m))
			for _, k := range tc.wantKeys {
				_, ok := m[k]
				assert.Truef(t, ok, "payload for %s missing key %q", tc.eventType, k)
			}
		})
	}
}
