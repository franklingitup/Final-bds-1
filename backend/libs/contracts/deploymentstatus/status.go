// Package deploymentstatus defines the canonical deployment and release status
// values shared across the control plane and platform agents.
//
// This is the single source of truth for deployment lifecycle states.
// All services and agents MUST use these constants.
package deploymentstatus

// ReleaseStatus represents the status of a release (immutable deployment snapshot).
// These are the only valid values for release status fields.
type ReleaseStatus string

const (
	// ReleasePending indicates the release has been created but not yet started.
	ReleasePending ReleaseStatus = "pending"

	// ReleaseDeploying indicates the release is actively being deployed.
	// The platform agent reports this when it begins reconciling the deployment.
	ReleaseDeploying ReleaseStatus = "deploying"

	// ReleaseSucceeded indicates the release completed successfully.
	// All desired replicas are ready and healthy.
	ReleaseSucceeded ReleaseStatus = "succeeded"

	// ReleaseFailed indicates the release failed.
	// The deployment could not reach the desired state.
	ReleaseFailed ReleaseStatus = "failed"

	// ReleaseRolledBack indicates the release was superseded by a rollback.
	ReleaseRolledBack ReleaseStatus = "rolled_back"
)

// String returns the string representation of the status.
func (s ReleaseStatus) String() string {
	return string(s)
}

// IsTerminal returns true if the status represents a terminal state.
func (s ReleaseStatus) IsTerminal() bool {
	switch s {
	case ReleaseSucceeded, ReleaseFailed, ReleaseRolledBack:
		return true
	default:
		return false
	}
}

// IsActive returns true if the release is still in progress.
func (s ReleaseStatus) IsActive() bool {
	switch s {
	case ReleasePending, ReleaseDeploying:
		return true
	default:
		return false
	}
}

// ValidReleaseStatus checks if a string is a valid release status.
func ValidReleaseStatus(s string) bool {
	switch ReleaseStatus(s) {
	case ReleasePending, ReleaseDeploying, ReleaseSucceeded, ReleaseFailed, ReleaseRolledBack:
		return true
	default:
		return false
	}
}

// ValidAgentReleaseStatus checks if a string is a valid status that agents can report.
// Agents can only report deploying, succeeded, or failed.
func ValidAgentReleaseStatus(s string) bool {
	switch ReleaseStatus(s) {
	case ReleaseDeploying, ReleaseSucceeded, ReleaseFailed:
		return true
	default:
		return false
	}
}

// DeploymentStatus represents the overall status of a deployment.
type DeploymentStatus string

const (
	// DeploymentPending indicates no release has been deployed yet.
	DeploymentPending DeploymentStatus = "pending"

	// DeploymentRunning indicates the deployment is active and healthy.
	DeploymentRunning DeploymentStatus = "running"

	// DeploymentSucceeded indicates the latest release succeeded.
	DeploymentSucceeded DeploymentStatus = "succeeded"

	// DeploymentFailed indicates the latest release failed.
	DeploymentFailed DeploymentStatus = "failed"

	// DeploymentRolledBack indicates the deployment was rolled back.
	DeploymentRolledBack DeploymentStatus = "rolled_back"
)

// String returns the string representation of the status.
func (s DeploymentStatus) String() string {
	return string(s)
}

// ValidDeploymentStatus checks if a string is a valid deployment status.
func ValidDeploymentStatus(s string) bool {
	switch DeploymentStatus(s) {
	case DeploymentPending, DeploymentRunning, DeploymentSucceeded, DeploymentFailed, DeploymentRolledBack:
		return true
	default:
		return false
	}
}
