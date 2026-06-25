package deployment

import (
	"encoding/json"
	"time"

	"github.com/bdsplatform/platform/backend/libs/contracts/deploymentstatus"
	"github.com/bdsplatform/platform/backend/libs/database"
)

// Runtime types.
const (
	RuntimeContainer = "container"
	RuntimeFunction  = "function"
	RuntimeJob       = "job"
)

// Deployment statuses - aliases for the canonical shared package.
// Use deploymentstatus.DeploymentStatus constants directly when possible.
const (
	StatusPending    = string(deploymentstatus.DeploymentPending)
	StatusRunning    = string(deploymentstatus.DeploymentRunning)
	StatusSucceeded  = string(deploymentstatus.DeploymentSucceeded)
	StatusFailed     = string(deploymentstatus.DeploymentFailed)
	StatusRolledBack = string(deploymentstatus.DeploymentRolledBack)
)

// Release statuses - aliases for the canonical shared package.
// Use deploymentstatus.ReleaseStatus constants directly when possible.
const (
	ReleaseStatusPending    = string(deploymentstatus.ReleasePending)
	ReleaseStatusDeploying  = string(deploymentstatus.ReleaseDeploying)
	ReleaseStatusSucceeded  = string(deploymentstatus.ReleaseSucceeded)
	ReleaseStatusFailed     = string(deploymentstatus.ReleaseFailed)
	ReleaseStatusRolledBack = string(deploymentstatus.ReleaseRolledBack)
)

// Application represents a deployable application within a project.
type Application struct {
	database.TenantModel
	ProjectID   string  `db:"project_id"`
	Name        string  `db:"name"`
	Slug        string  `db:"slug"`
	Description *string `db:"description"`
	RuntimeType string  `db:"runtime_type"`
	CreatedBy   *string `db:"created_by"`
}

// Deployment represents a deployment of an application to a cluster.
type Deployment struct {
	database.TenantModel
	ApplicationID   string          `db:"application_id"`
	ClusterID       string          `db:"cluster_id"`
	Image           string          `db:"image"`
	Replicas        int             `db:"replicas"`
	CPURequest      *string         `db:"cpu_request"`
	CPULimit        *string         `db:"cpu_limit"`
	MemoryRequest   *string         `db:"memory_request"`
	MemoryLimit     *string         `db:"memory_limit"`
	Port            *int            `db:"port"`
	EnvVars         json.RawMessage `db:"env_vars"`
	Status          string          `db:"status"`
	ReadyReplicas   int             `db:"ready_replicas"`
	DesiredReplicas int             `db:"desired_replicas"`
	CreatedBy       *string         `db:"created_by"`
}

// Release is an immutable snapshot of a deployment revision.
type Release struct {
	ID           string          `db:"id"`
	OrgID        string          `db:"org_id"`
	DeploymentID string          `db:"deployment_id"`
	Revision     int             `db:"revision"`
	Image        string          `db:"image"`
	Replicas     int             `db:"replicas"`
	ConfigHash   string          `db:"config_hash"`
	Config       json.RawMessage `db:"config"`
	Status       string          `db:"status"`
	StartedAt    *time.Time      `db:"started_at"`
	FinishedAt   *time.Time      `db:"finished_at"`
	ErrorMessage *string         `db:"error_message"`
	CreatedBy    *string         `db:"created_by"`
	CreatedAt    time.Time       `db:"created_at"`
}

// EnvVar represents an environment variable.
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ----------------------------------------------------------------------------
// Request DTOs
// ----------------------------------------------------------------------------

type CreateApplicationRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description,omitempty"`
	RuntimeType string  `json:"runtimeType,omitempty"`
}

type UpdateApplicationRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type CreateDeploymentRequest struct {
	ApplicationID string   `json:"applicationId"`
	ClusterID     string   `json:"clusterId"`
	Image         string   `json:"image"`
	Replicas      int      `json:"replicas"`
	CPURequest    *string  `json:"cpuRequest,omitempty"`
	CPULimit      *string  `json:"cpuLimit,omitempty"`
	MemoryRequest *string  `json:"memoryRequest,omitempty"`
	MemoryLimit   *string  `json:"memoryLimit,omitempty"`
	Port          *int     `json:"port,omitempty"`
	EnvVars       []EnvVar `json:"envVars,omitempty"`
}

type UpdateDeploymentRequest struct {
	Image         *string  `json:"image,omitempty"`
	Replicas      *int     `json:"replicas,omitempty"`
	CPURequest    *string  `json:"cpuRequest,omitempty"`
	CPULimit      *string  `json:"cpuLimit,omitempty"`
	MemoryRequest *string  `json:"memoryRequest,omitempty"`
	MemoryLimit   *string  `json:"memoryLimit,omitempty"`
	Port          *int     `json:"port,omitempty"`
	EnvVars       []EnvVar `json:"envVars,omitempty"`
}

type RollbackRequest struct {
	TargetRevision *int `json:"targetRevision,omitempty"` // If nil, rolls back to previous.
}

type UpdateStatusRequest struct {
	Status        string  `json:"status"`
	ReadyReplicas *int    `json:"readyReplicas,omitempty"`
	ErrorMessage  *string `json:"errorMessage,omitempty"`
}

// ----------------------------------------------------------------------------
// View Models
// ----------------------------------------------------------------------------

type ApplicationView struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"organizationId"`
	ProjectID   string    `json:"projectId"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	RuntimeType string    `json:"runtimeType"`
	CreatedAt   time.Time `json:"createdAt"`
}

func toApplicationView(a *Application) ApplicationView {
	desc := ""
	if a.Description != nil {
		desc = *a.Description
	}
	return ApplicationView{
		ID:          a.ID,
		OrgID:       a.OrgID,
		ProjectID:   a.ProjectID,
		Name:        a.Name,
		Slug:        a.Slug,
		Description: desc,
		RuntimeType: a.RuntimeType,
		CreatedAt:   a.CreatedAt,
	}
}

type DeploymentView struct {
	ID              string    `json:"id"`
	OrgID           string    `json:"organizationId"`
	ApplicationID   string    `json:"applicationId"`
	ClusterID       string    `json:"clusterId"`
	Image           string    `json:"image"`
	Replicas        int       `json:"replicas"`
	CPURequest      string    `json:"cpuRequest,omitempty"`
	CPULimit        string    `json:"cpuLimit,omitempty"`
	MemoryRequest   string    `json:"memoryRequest,omitempty"`
	MemoryLimit     string    `json:"memoryLimit,omitempty"`
	Port            *int      `json:"port,omitempty"`
	EnvVars         []EnvVar  `json:"envVars,omitempty"`
	Status          string    `json:"status"`
	ReadyReplicas   int       `json:"readyReplicas"`
	DesiredReplicas int       `json:"desiredReplicas"`
	CurrentRevision *int      `json:"currentRevision,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toDeploymentView(d *Deployment, currentRevision *int) DeploymentView {
	var envVars []EnvVar
	if len(d.EnvVars) > 0 {
		_ = json.Unmarshal(d.EnvVars, &envVars)
	}

	return DeploymentView{
		ID:              d.ID,
		OrgID:           d.OrgID,
		ApplicationID:   d.ApplicationID,
		ClusterID:       d.ClusterID,
		Image:           d.Image,
		Replicas:        d.Replicas,
		CPURequest:      deref(d.CPURequest),
		CPULimit:        deref(d.CPULimit),
		MemoryRequest:   deref(d.MemoryRequest),
		MemoryLimit:     deref(d.MemoryLimit),
		Port:            d.Port,
		EnvVars:         envVars,
		Status:          d.Status,
		ReadyReplicas:   d.ReadyReplicas,
		DesiredReplicas: d.DesiredReplicas,
		CurrentRevision: currentRevision,
		CreatedAt:       d.CreatedAt,
	}
}

type ReleaseView struct {
	ID           string     `json:"id"`
	DeploymentID string     `json:"deploymentId"`
	Revision     int        `json:"revision"`
	Image        string     `json:"image"`
	Replicas     int        `json:"replicas"`
	Status       string     `json:"status"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

func toReleaseView(r *Release) ReleaseView {
	return ReleaseView{
		ID:           r.ID,
		DeploymentID: r.DeploymentID,
		Revision:     r.Revision,
		Image:        r.Image,
		Replicas:     r.Replicas,
		Status:       r.Status,
		StartedAt:    r.StartedAt,
		FinishedAt:   r.FinishedAt,
		ErrorMessage: deref(r.ErrorMessage),
		CreatedAt:    r.CreatedAt,
	}
}
