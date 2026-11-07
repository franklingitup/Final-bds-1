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
	// EnvironmentID optionally binds this deployment to a named environment.
	// NULL = unbound (legacy) deployment; the agent receives Namespace="" for it
	// and applies its own fallback — preserving pre-environment behaviour exactly.
	// Additive and optional — omitting it leaves existing deployments unchanged.
	EnvironmentID *string `db:"environment_id"`
}

// Promotion statuses.
const (
	PromotionStatusPending  = "pending"
	PromotionStatusApproved = "approved"
	PromotionStatusRejected = "rejected"
	PromotionStatusApplied  = "applied"
)

// Environment represents a named deployment target (cluster + namespace) within an application.
// An application can have multiple environments (e.g., staging, production).
type Environment struct {
	ID            string    `db:"id"`
	OrgID         string    `db:"org_id"`
	ApplicationID string    `db:"application_id"`
	Name          string    `db:"name"`
	Namespace     string    `db:"namespace"`
	ClusterID     string    `db:"cluster_id"`
	OrderIndex    int       `db:"order_index"`
	CreatedBy     *string   `db:"created_by"`
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
}

// PromotionRequest tracks a request to promote a release from one environment to another.
// Promotion requires explicit approval (ActionManageProject) before the release is applied.
type PromotionRequest struct {
	ID                string     `db:"id"`
	OrgID             string     `db:"org_id"`
	ApplicationID     string     `db:"application_id"` // denormalized for per-app queries
	ReleaseID         string     `db:"release_id"`
	FromEnvironmentID string     `db:"from_environment_id"`
	ToEnvironmentID   string     `db:"to_environment_id"`
	RequestedBy       string     `db:"requested_by"`
	Status            string     `db:"status"`
	ApprovedBy        *string    `db:"approved_by"`
	ApprovedAt        *time.Time `db:"approved_at"`
	AppliedAt         *time.Time `db:"applied_at"`
	RejectionReason   *string    `db:"rejection_reason"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
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
	// GitOps optionally binds the new deployment to an Argo CD Application. When
	// present (and the GitOps engine is enabled) the deployment is created as a
	// GitOps deployment: an Argo CD Application is generated from this source.
	// Additive and optional — omitting it preserves the existing image-based
	// deployment behaviour exactly.
	GitOps *GitOpsSource `json:"gitops,omitempty"`
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

// ----------------------------------------------------------------------------
// Environment + Promotion request DTOs
// ----------------------------------------------------------------------------

type CreateEnvironmentRequest struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	ClusterID  string `json:"clusterId"`
	OrderIndex int    `json:"orderIndex,omitempty"`
}

type UpdateEnvironmentRequest struct {
	Name       *string `json:"name,omitempty"`
	Namespace  *string `json:"namespace,omitempty"`
	OrderIndex *int    `json:"orderIndex,omitempty"`
}

type RequestPromotionRequest struct {
	ReleaseID         string `json:"releaseId"`
	FromEnvironmentID string `json:"fromEnvironmentId"`
	ToEnvironmentID   string `json:"toEnvironmentId"`
}

type RejectPromotionRequest struct {
	Reason string `json:"reason,omitempty"`
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
	EnvironmentID   *string   `json:"environmentId,omitempty"`
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
		EnvironmentID:   d.EnvironmentID,
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

type EnvironmentView struct {
	ID            string    `json:"id"`
	OrgID         string    `json:"organizationId"`
	ApplicationID string    `json:"applicationId"`
	Name          string    `json:"name"`
	Namespace     string    `json:"namespace"`
	ClusterID     string    `json:"clusterId"`
	OrderIndex    int       `json:"orderIndex"`
	CreatedAt     time.Time `json:"createdAt"`
}

func toEnvironmentView(e *Environment) EnvironmentView {
	return EnvironmentView{
		ID:            e.ID,
		OrgID:         e.OrgID,
		ApplicationID: e.ApplicationID,
		Name:          e.Name,
		Namespace:     e.Namespace,
		ClusterID:     e.ClusterID,
		OrderIndex:    e.OrderIndex,
		CreatedAt:     e.CreatedAt,
	}
}

type PromotionRequestView struct {
	ID                string     `json:"id"`
	OrgID             string     `json:"organizationId"`
	ApplicationID     string     `json:"applicationId"`
	ReleaseID         string     `json:"releaseId"`
	FromEnvironmentID string     `json:"fromEnvironmentId"`
	ToEnvironmentID   string     `json:"toEnvironmentId"`
	RequestedBy       string     `json:"requestedBy"`
	Status            string     `json:"status"`
	ApprovedBy        *string    `json:"approvedBy,omitempty"`
	ApprovedAt        *time.Time `json:"approvedAt,omitempty"`
	AppliedAt         *time.Time `json:"appliedAt,omitempty"`
	RejectionReason   *string    `json:"rejectionReason,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

func toPromotionView(p *PromotionRequest) PromotionRequestView {
	return PromotionRequestView{
		ID:                p.ID,
		OrgID:             p.OrgID,
		ApplicationID:     p.ApplicationID,
		ReleaseID:         p.ReleaseID,
		FromEnvironmentID: p.FromEnvironmentID,
		ToEnvironmentID:   p.ToEnvironmentID,
		RequestedBy:       p.RequestedBy,
		Status:            p.Status,
		ApprovedBy:        p.ApprovedBy,
		ApprovedAt:        p.ApprovedAt,
		AppliedAt:         p.AppliedAt,
		RejectionReason:   p.RejectionReason,
		CreatedAt:         p.CreatedAt,
	}
}
