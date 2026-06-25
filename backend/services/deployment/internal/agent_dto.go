package deployment

import "encoding/json"

// DesiredDeployment represents the desired state for a deployment as expected by the agent.
// This is a read model that joins data from applications, deployments, and releases.
type DesiredDeployment struct {
	DeploymentID string `json:"deploymentId"`
	ReleaseID    string `json:"releaseId"`

	ApplicationID   string `json:"applicationId"`
	ApplicationName string `json:"applicationName"`
	ApplicationSlug string `json:"applicationSlug"`

	Namespace string `json:"namespace,omitempty"`

	Image    string `json:"image"`
	Revision int    `json:"revision"`
	Replicas int    `json:"replicas"`

	Port    *int     `json:"port,omitempty"`
	EnvVars []EnvVar `json:"envVars,omitempty"`

	ResourceRequests *ResourceSpec `json:"resourceRequests,omitempty"`
	ResourceLimits   *ResourceSpec `json:"resourceLimits,omitempty"`

	Status string `json:"status"`
}

// ResourceSpec represents CPU and memory resources in a nested format.
type ResourceSpec struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// DesiredStateResponse is the response for the agent desired state endpoint.
type DesiredStateResponse struct {
	Items      []DesiredDeployment `json:"items"`
	ClusterID  string              `json:"clusterId"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

// desiredDeploymentRow is the raw database row from the join query.
type desiredDeploymentRow struct {
	DeploymentID    string  `db:"deployment_id"`
	ReleaseID       string  `db:"release_id"`
	ApplicationID   string  `db:"application_id"`
	ApplicationName string  `db:"application_name"`
	ApplicationSlug string  `db:"application_slug"`
	Image           string  `db:"image"`
	Revision        int     `db:"revision"`
	Replicas        int     `db:"replicas"`
	Port            *int    `db:"port"`
	EnvVars         []byte  `db:"env_vars"`
	CPURequest      *string `db:"cpu_request"`
	CPULimit        *string `db:"cpu_limit"`
	MemoryRequest   *string `db:"memory_request"`
	MemoryLimit     *string `db:"memory_limit"`
	Status          string  `db:"status"`
}

// toDesiredDeployment converts a database row to the agent DTO.
func (r *desiredDeploymentRow) toDesiredDeployment(namespace string) DesiredDeployment {
	dd := DesiredDeployment{
		DeploymentID:    r.DeploymentID,
		ReleaseID:       r.ReleaseID,
		ApplicationID:   r.ApplicationID,
		ApplicationName: r.ApplicationName,
		ApplicationSlug: r.ApplicationSlug,
		Namespace:       namespace,
		Image:           r.Image,
		Revision:        r.Revision,
		Replicas:        r.Replicas,
		Port:            r.Port,
		Status:          r.Status,
	}

	// Parse env vars.
	if len(r.EnvVars) > 0 {
		var envVars []EnvVar
		if err := json.Unmarshal(r.EnvVars, &envVars); err == nil {
			dd.EnvVars = envVars
		}
	}

	// Build resource requests if any are set.
	if r.CPURequest != nil || r.MemoryRequest != nil {
		dd.ResourceRequests = &ResourceSpec{}
		if r.CPURequest != nil {
			dd.ResourceRequests.CPU = *r.CPURequest
		}
		if r.MemoryRequest != nil {
			dd.ResourceRequests.Memory = *r.MemoryRequest
		}
	}

	// Build resource limits if any are set.
	if r.CPULimit != nil || r.MemoryLimit != nil {
		dd.ResourceLimits = &ResourceSpec{}
		if r.CPULimit != nil {
			dd.ResourceLimits.CPU = *r.CPULimit
		}
		if r.MemoryLimit != nil {
			dd.ResourceLimits.Memory = *r.MemoryLimit
		}
	}

	return dd
}
