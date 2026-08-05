package pipeline

import (
	"encoding/json"
	"time"
)

// Pipeline status constants.
const (
	StatusPending   = "pending"
	StatusBuilding  = "building"
	StatusDeploying = "deploying"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Pipeline stage constants.
const (
	StageInit    = "init"
	StageBuild   = "build"
	StageRelease = "release"
	StageDeploy  = "deploy"
	StageDone    = "done"
)

// Source type constants.
const (
	SourceTypeImage = "image"
	SourceTypeGit   = "git"
	SourceTypeBuild = "build"
)

// Sync status constants.
const (
	SyncStatusPending = "pending"
	SyncStatusSyncing = "syncing"
	SyncStatusSynced  = "synced"
	SyncStatusFailed  = "failed"
	SyncStatusStale   = "stale"
)

// Trigger type constants.
const (
	TriggerUser     = "user"
	TriggerWebhook  = "webhook"
	TriggerSchedule = "schedule"
	TriggerRollback = "rollback"
)

// Health status constants.
const (
	HealthHealthy   = "healthy"
	HealthDegraded  = "degraded"
	HealthUnhealthy = "unhealthy"
	HealthUnknown   = "unknown"
)

// DesiredState represents the desired Kubernetes state for a deployment.
type DesiredState struct {
	ID                 string          `db:"id"`
	OrgID              string          `db:"org_id"`
	DeploymentID       string          `db:"deployment_id"`
	ReleaseID          string          `db:"release_id"`
	ClusterID          string          `db:"cluster_id"`
	Namespace          string          `db:"namespace"`
	Manifests          json.RawMessage `db:"manifests"`
	ManifestHash       string          `db:"manifest_hash"`
	SyncStatus         string          `db:"sync_status"`
	LastSyncedAt       *time.Time      `db:"last_synced_at"`
	LastSyncError      *string         `db:"last_sync_error"`
	Generation         int64           `db:"generation"`
	ObservedGeneration int64           `db:"observed_generation"`
	CreatedAt          time.Time       `db:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at"`
}

// PipelineRun represents a pipeline execution.
type PipelineRun struct {
	ID           string     `db:"id"`
	OrgID        string     `db:"org_id"`
	DeploymentID string     `db:"deployment_id"`
	ReleaseID    *string    `db:"release_id"`
	SourceType   string     `db:"source_type"`
	SourceRef    string     `db:"source_ref"`
	BuildID      *string    `db:"build_id"`
	BuildStatus  *string    `db:"build_status"`
	BuiltImage   *string    `db:"built_image"`
	Status       string     `db:"status"`
	CurrentStage string     `db:"current_stage"`
	StartedAt    *time.Time `db:"started_at"`
	FinishedAt   *time.Time `db:"finished_at"`
	ErrorMessage *string    `db:"error_message"`
	ErrorStage   *string    `db:"error_stage"`
	TriggeredBy  string     `db:"triggered_by"`
	CreatedBy    *string    `db:"created_by"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
}

// PipelineEvent represents an event in a pipeline run.
type PipelineEvent struct {
	ID            string          `db:"id"`
	OrgID         string          `db:"org_id"`
	PipelineRunID string          `db:"pipeline_run_id"`
	EventType     string          `db:"event_type"`
	Stage         *string         `db:"stage"`
	Message       string          `db:"message"`
	Details       json.RawMessage `db:"details"`
	CreatedAt     time.Time       `db:"created_at"`
}

// DeploymentMetrics represents deployment health metrics.
type DeploymentMetrics struct {
	ID                  string     `db:"id"`
	OrgID               string     `db:"org_id"`
	DeploymentID        string     `db:"deployment_id"`
	AvailableReplicas   int        `db:"available_replicas"`
	ReadyReplicas       int        `db:"ready_replicas"`
	UpdatedReplicas     int        `db:"updated_replicas"`
	CPUUsageMillicores  *int64     `db:"cpu_usage_millicores"`
	MemoryUsageBytes    *int64     `db:"memory_usage_bytes"`
	HealthStatus        string     `db:"health_status"`
	LastHealthCheck     *time.Time `db:"last_health_check"`
	CollectedAt         time.Time  `db:"collected_at"`
}

// ----------------------------------------------------------------------------
// Request DTOs
// ----------------------------------------------------------------------------

// TriggerPipelineRequest is the request to trigger a pipeline.
type TriggerPipelineRequest struct {
	DeploymentID string  `json:"deploymentId"`
	SourceType   string  `json:"sourceType,omitempty"` // image | git | build
	SourceRef    string  `json:"sourceRef"`            // image:tag, branch/commit, or build id
	BuildID      *string `json:"buildId,omitempty"`    // If triggering from build
}

// UpdatePipelineStatusRequest is the request to update pipeline status.
type UpdatePipelineStatusRequest struct {
	Status       string  `json:"status"`
	CurrentStage string  `json:"currentStage,omitempty"`
	BuildStatus  *string `json:"buildStatus,omitempty"`
	BuiltImage   *string `json:"builtImage,omitempty"`
	ErrorMessage *string `json:"errorMessage,omitempty"`
}

// ReportSyncRequest is the request from agent to report sync status.
type ReportSyncRequest struct {
	DeploymentID       string `json:"deploymentId"`
	ObservedGeneration int64  `json:"observedGeneration"`
	SyncStatus         string `json:"syncStatus"`
	ErrorMessage       *string `json:"errorMessage,omitempty"`
}

// ReportMetricsRequest is the request from agent to report metrics.
type ReportMetricsRequest struct {
	DeploymentID        string `json:"deploymentId"`
	AvailableReplicas   int    `json:"availableReplicas"`
	ReadyReplicas       int    `json:"readyReplicas"`
	UpdatedReplicas     int    `json:"updatedReplicas"`
	CPUUsageMillicores  *int64 `json:"cpuUsageMillicores,omitempty"`
	MemoryUsageBytes    *int64 `json:"memoryUsageBytes,omitempty"`
	HealthStatus        string `json:"healthStatus,omitempty"`
}

// ----------------------------------------------------------------------------
// View Models
// ----------------------------------------------------------------------------

// DesiredStateView is the API response for desired state.
type DesiredStateView struct {
	ID                 string          `json:"id"`
	DeploymentID       string          `json:"deploymentId"`
	ReleaseID          string          `json:"releaseId"`
	ClusterID          string          `json:"clusterId"`
	Namespace          string          `json:"namespace"`
	Manifests          json.RawMessage `json:"manifests"`
	ManifestHash       string          `json:"manifestHash"`
	SyncStatus         string          `json:"syncStatus"`
	Generation         int64           `json:"generation"`
	ObservedGeneration int64           `json:"observedGeneration"`
	LastSyncedAt       *time.Time      `json:"lastSyncedAt,omitempty"`
	UpdatedAt          time.Time       `json:"updatedAt"`
}

func ToDesiredStateView(ds *DesiredState) DesiredStateView {
	return DesiredStateView{
		ID:                 ds.ID,
		DeploymentID:       ds.DeploymentID,
		ReleaseID:          ds.ReleaseID,
		ClusterID:          ds.ClusterID,
		Namespace:          ds.Namespace,
		Manifests:          ds.Manifests,
		ManifestHash:       ds.ManifestHash,
		SyncStatus:         ds.SyncStatus,
		Generation:         ds.Generation,
		ObservedGeneration: ds.ObservedGeneration,
		LastSyncedAt:       ds.LastSyncedAt,
		UpdatedAt:          ds.UpdatedAt,
	}
}

// PipelineRunView is the API response for a pipeline run.
type PipelineRunView struct {
	ID           string     `json:"id"`
	DeploymentID string     `json:"deploymentId"`
	ReleaseID    *string    `json:"releaseId,omitempty"`
	SourceType   string     `json:"sourceType"`
	SourceRef    string     `json:"sourceRef"`
	BuildID      *string    `json:"buildId,omitempty"`
	BuildStatus  *string    `json:"buildStatus,omitempty"`
	BuiltImage   *string    `json:"builtImage,omitempty"`
	Status       string     `json:"status"`
	CurrentStage string     `json:"currentStage"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	ErrorMessage *string    `json:"errorMessage,omitempty"`
	ErrorStage   *string    `json:"errorStage,omitempty"`
	TriggeredBy  string     `json:"triggeredBy"`
	CreatedAt    time.Time  `json:"createdAt"`
}

func ToPipelineRunView(pr *PipelineRun) PipelineRunView {
	return PipelineRunView{
		ID:           pr.ID,
		DeploymentID: pr.DeploymentID,
		ReleaseID:    pr.ReleaseID,
		SourceType:   pr.SourceType,
		SourceRef:    pr.SourceRef,
		BuildID:      pr.BuildID,
		BuildStatus:  pr.BuildStatus,
		BuiltImage:   pr.BuiltImage,
		Status:       pr.Status,
		CurrentStage: pr.CurrentStage,
		StartedAt:    pr.StartedAt,
		FinishedAt:   pr.FinishedAt,
		ErrorMessage: pr.ErrorMessage,
		ErrorStage:   pr.ErrorStage,
		TriggeredBy:  pr.TriggeredBy,
		CreatedAt:    pr.CreatedAt,
	}
}

// PipelineEventView is the API response for a pipeline event.
type PipelineEventView struct {
	ID        string          `json:"id"`
	EventType string          `json:"eventType"`
	Stage     *string         `json:"stage,omitempty"`
	Message   string          `json:"message"`
	Details   json.RawMessage `json:"details,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

func ToPipelineEventView(pe *PipelineEvent) PipelineEventView {
	return PipelineEventView{
		ID:        pe.ID,
		EventType: pe.EventType,
		Stage:     pe.Stage,
		Message:   pe.Message,
		Details:   pe.Details,
		CreatedAt: pe.CreatedAt,
	}
}

// DeploymentMetricsView is the API response for deployment metrics.
type DeploymentMetricsView struct {
	DeploymentID        string     `json:"deploymentId"`
	AvailableReplicas   int        `json:"availableReplicas"`
	ReadyReplicas       int        `json:"readyReplicas"`
	UpdatedReplicas     int        `json:"updatedReplicas"`
	CPUUsageMillicores  *int64     `json:"cpuUsageMillicores,omitempty"`
	MemoryUsageBytes    *int64     `json:"memoryUsageBytes,omitempty"`
	HealthStatus        string     `json:"healthStatus"`
	LastHealthCheck     *time.Time `json:"lastHealthCheck,omitempty"`
	CollectedAt         time.Time  `json:"collectedAt"`
}

func ToDeploymentMetricsView(dm *DeploymentMetrics) DeploymentMetricsView {
	return DeploymentMetricsView{
		DeploymentID:        dm.DeploymentID,
		AvailableReplicas:   dm.AvailableReplicas,
		ReadyReplicas:       dm.ReadyReplicas,
		UpdatedReplicas:     dm.UpdatedReplicas,
		CPUUsageMillicores:  dm.CPUUsageMillicores,
		MemoryUsageBytes:    dm.MemoryUsageBytes,
		HealthStatus:        dm.HealthStatus,
		LastHealthCheck:     dm.LastHealthCheck,
		CollectedAt:         dm.CollectedAt,
	}
}

// AgentDesiredState is the response format for agents pulling desired state.
type AgentDesiredState struct {
	DeploymentID string          `json:"deploymentId"`
	ReleaseID    string          `json:"releaseId"`
	Namespace    string          `json:"namespace"`
	Manifests    json.RawMessage `json:"manifests"`
	ManifestHash string          `json:"manifestHash"`
	Generation   int64           `json:"generation"`
}

func ToAgentDesiredState(ds *DesiredState) AgentDesiredState {
	return AgentDesiredState{
		DeploymentID: ds.DeploymentID,
		ReleaseID:    ds.ReleaseID,
		Namespace:    ds.Namespace,
		Manifests:    ds.Manifests,
		ManifestHash: ds.ManifestHash,
		Generation:   ds.Generation,
	}
}
