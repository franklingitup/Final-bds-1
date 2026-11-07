package deployment

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDesiredDeploymentRow_UnboundDeployment_NamespaceEmpty is the explicit
// regression guard for the non-breaking guarantee at the heart of Phase 1:
//
// "Adding environment support must not change anything for pre-existing,
// unbound deployments. They must continue to produce Namespace="" so the
// agent keeps applying its own default — exactly the behaviour today."
//
// The invariant is enforced at two layers:
//  1. SQL:  COALESCE(e.namespace, '') → agent_repository.go passes the empty
//           string to toDesiredDeployment when environment_id IS NULL.
//  2. Code: toDesiredDeployment("") → sets DesiredDeployment.Namespace = "".
//
// This test covers layer 2. If anyone changes toDesiredDeployment to inject
// a default namespace when the argument is "", this test fails immediately and
// the reviewer is forced to reconsider whether the pre-existing behaviour is
// intentionally being broken.
func TestDesiredDeploymentRow_UnboundDeployment_NamespaceEmpty(t *testing.T) {
	row := &desiredDeploymentRow{
		DeploymentID:    "dep-abc",
		ReleaseID:       "rel-abc",
		ApplicationID:   "app-abc",
		ApplicationName: "My App",
		ApplicationSlug: "my-app",
		ProjectID:       "proj-abc",
		ProjectSlug:     "my-project",
		Image:           "registry/myapp:v1",
		Revision:        1,
		Replicas:        2,
		Status:          StatusPending,
		// Namespace is "" — simulates COALESCE(e.namespace, '') for a deployment
		// where environment_id IS NULL (no environment bound).
		Namespace: "",
	}

	dd := row.toDesiredDeployment(row.Namespace)

	require.Equal(t, "", dd.Namespace,
		"unbound deployment (environment_id IS NULL) must produce Namespace=\"\" so the "+
			"agent continues to apply its own default namespace, preserving pre-environment "+
			"behaviour exactly — this is the non-breaking guarantee for Phase 1")
}

// TestDesiredDeploymentRow_BoundDeployment_NamespacePropagated confirms the
// happy path: an environment-bound deployment carries its environment's namespace
// through to the agent DTO.
func TestDesiredDeploymentRow_BoundDeployment_NamespacePropagated(t *testing.T) {
	row := &desiredDeploymentRow{
		DeploymentID:    "dep-xyz",
		ReleaseID:       "rel-xyz",
		ApplicationID:   "app-xyz",
		ApplicationName: "Prod App",
		ApplicationSlug: "prod-app",
		ProjectID:       "proj-xyz",
		ProjectSlug:     "my-project",
		Image:           "registry/myapp@sha256:abc123",
		Revision:        3,
		Replicas:        5,
		Status:          StatusRunning,
		// Namespace populated from environments.namespace via COALESCE.
		Namespace: "prod-myapp",
	}

	dd := row.toDesiredDeployment(row.Namespace)

	require.Equal(t, "prod-myapp", dd.Namespace,
		"environment-bound deployment must carry the environment's namespace to the agent")
}
