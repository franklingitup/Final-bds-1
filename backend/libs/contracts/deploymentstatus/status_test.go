package deploymentstatus

import "testing"

func TestReleaseStatus_String(t *testing.T) {
	tests := []struct {
		status ReleaseStatus
		want   string
	}{
		{ReleasePending, "pending"},
		{ReleaseDeploying, "deploying"},
		{ReleaseSucceeded, "succeeded"},
		{ReleaseFailed, "failed"},
		{ReleaseRolledBack, "rolled_back"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("ReleaseStatus.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReleaseStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   ReleaseStatus
		terminal bool
	}{
		{ReleasePending, false},
		{ReleaseDeploying, false},
		{ReleaseSucceeded, true},
		{ReleaseFailed, true},
		{ReleaseRolledBack, true},
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.terminal {
				t.Errorf("ReleaseStatus.IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}

func TestReleaseStatus_IsActive(t *testing.T) {
	tests := []struct {
		status ReleaseStatus
		active bool
	}{
		{ReleasePending, true},
		{ReleaseDeploying, true},
		{ReleaseSucceeded, false},
		{ReleaseFailed, false},
		{ReleaseRolledBack, false},
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			if got := tt.status.IsActive(); got != tt.active {
				t.Errorf("ReleaseStatus.IsActive() = %v, want %v", got, tt.active)
			}
		})
	}
}

func TestValidReleaseStatus(t *testing.T) {
	validStatuses := []string{"pending", "deploying", "succeeded", "failed", "rolled_back"}
	invalidStatuses := []string{"started", "running", "unknown", "", "PENDING"}

	for _, s := range validStatuses {
		if !ValidReleaseStatus(s) {
			t.Errorf("ValidReleaseStatus(%q) = false, want true", s)
		}
	}

	for _, s := range invalidStatuses {
		if ValidReleaseStatus(s) {
			t.Errorf("ValidReleaseStatus(%q) = true, want false", s)
		}
	}
}

func TestValidAgentReleaseStatus(t *testing.T) {
	validStatuses := []string{"deploying", "succeeded", "failed"}
	invalidStatuses := []string{"pending", "rolled_back", "started", "running", ""}

	for _, s := range validStatuses {
		if !ValidAgentReleaseStatus(s) {
			t.Errorf("ValidAgentReleaseStatus(%q) = false, want true", s)
		}
	}

	for _, s := range invalidStatuses {
		if ValidAgentReleaseStatus(s) {
			t.Errorf("ValidAgentReleaseStatus(%q) = true, want false", s)
		}
	}
}

func TestDeploymentStatus_String(t *testing.T) {
	tests := []struct {
		status DeploymentStatus
		want   string
	}{
		{DeploymentPending, "pending"},
		{DeploymentRunning, "running"},
		{DeploymentSucceeded, "succeeded"},
		{DeploymentFailed, "failed"},
		{DeploymentRolledBack, "rolled_back"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("DeploymentStatus.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidDeploymentStatus(t *testing.T) {
	validStatuses := []string{"pending", "running", "succeeded", "failed", "rolled_back"}
	invalidStatuses := []string{"deploying", "started", "unknown", ""}

	for _, s := range validStatuses {
		if !ValidDeploymentStatus(s) {
			t.Errorf("ValidDeploymentStatus(%q) = false, want true", s)
		}
	}

	for _, s := range invalidStatuses {
		if ValidDeploymentStatus(s) {
			t.Errorf("ValidDeploymentStatus(%q) = true, want false", s)
		}
	}
}
