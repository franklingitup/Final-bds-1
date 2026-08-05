// Package build builds container images from Git or uploaded source and pushes
// them to the registry.
package build

import (
	"encoding/json"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// Build status constants.
const (
	StatusQueued    = "queued"
	StatusCloning   = "cloning"
	StatusBuilding  = "building"
	StatusPushing   = "pushing"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Builder type constants.
const (
	BuilderKaniko   = "kaniko"
	BuilderBuildKit = "buildkit"
)

// Auth type constants for git repositories.
const (
	AuthTypeNone      = "none"
	AuthTypeToken     = "token"
	AuthTypeSSHKey    = "ssh_key"
	AuthTypeDeployKey = "deploy_key"
)

// Log stream constants.
const (
	StreamStdout = "stdout"
	StreamStderr = "stderr"
	StreamSystem = "system"
)

// Log level constants.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// GitRepository represents a registered Git repository.
type GitRepository struct {
	database.TenantModel
	ProjectID       string  `db:"project_id"`
	Name            string  `db:"name"`
	URL             string  `db:"url"`
	DefaultBranch   string  `db:"default_branch"`
	AuthType        string  `db:"auth_type"`
	AuthSecretID    *string `db:"auth_secret_id"`
	WebhookSecret   *string `db:"webhook_secret"`
	CreatedBy       *string `db:"created_by"`
}

// Build represents a container image build job.
type Build struct {
	database.TenantModel
	
	// Source configuration
	RepositoryID   *string         `db:"repository_id"`
	GitURL         *string         `db:"git_url"`
	GitRef         string          `db:"git_ref"`
	GitCommit      *string         `db:"git_commit"`
	
	// Build context
	ContextPath    string          `db:"context_path"`
	DockerfilePath string          `db:"dockerfile_path"`
	BuildArgs      json.RawMessage `db:"build_args"`
	
	// Target configuration
	TargetImage    string          `db:"target_image"`
	TargetRegistry string          `db:"target_registry"`
	PushToRegistry bool            `db:"push_to_registry"`
	
	// Build engine
	BuilderType    string          `db:"builder_type"`
	
	// Status tracking
	Status         string          `db:"status"`
	
	// Timing
	QueuedAt       time.Time       `db:"queued_at"`
	StartedAt      *time.Time      `db:"started_at"`
	FinishedAt     *time.Time      `db:"finished_at"`
	
	// Error handling
	ErrorMessage   *string         `db:"error_message"`
	RetryCount     int             `db:"retry_count"`
	MaxRetries     int             `db:"max_retries"`
	ParentBuildID  *string         `db:"parent_build_id"`
	
	// Resource limits
	CPULimit       *string         `db:"cpu_limit"`
	MemoryLimit    *string         `db:"memory_limit"`
	TimeoutSeconds int             `db:"timeout_seconds"`
	
	// Audit
	CreatedBy      *string         `db:"created_by"`
	CancelledBy    *string         `db:"cancelled_by"`
}

// BuildLog represents a log entry from a build.
type BuildLog struct {
	ID        string          `db:"id"`
	OrgID     string          `db:"org_id"`
	BuildID   string          `db:"build_id"`
	Sequence  int             `db:"sequence"`
	Timestamp time.Time       `db:"timestamp"`
	Stream    string          `db:"stream"`
	Level     string          `db:"level"`
	Message   string          `db:"message"`
	Metadata  json.RawMessage `db:"metadata"`
}

// BuildArtifact represents the output of a successful build.
type BuildArtifact struct {
	ID              string          `db:"id"`
	OrgID           string          `db:"org_id"`
	BuildID         string          `db:"build_id"`
	ImageDigest     string          `db:"image_digest"`
	ImageTag        string          `db:"image_tag"`
	ImageSize       *int64          `db:"image_size"`
	ManifestType    string          `db:"manifest_type"`
	Manifest        json.RawMessage `db:"manifest"`
	LayerCount      *int            `db:"layer_count"`
	Layers          json.RawMessage `db:"layers"`
	DockerfileHash  *string         `db:"dockerfile_hash"`
	BuildDurationMs *int64          `db:"build_duration_ms"`
	Labels          json.RawMessage `db:"labels"`
	CreatedAt       time.Time       `db:"created_at"`
}

// BuildQueueItem represents an item in the build queue.
type BuildQueueItem struct {
	ID          string     `db:"id"`
	BuildID     string     `db:"build_id"`
	OrgID       string     `db:"org_id"` // Joined from builds table
	Priority    int        `db:"priority"`
	WorkerID    *string    `db:"worker_id"`
	ClaimedAt   *time.Time `db:"claimed_at"`
	HeartbeatAt *time.Time `db:"heartbeat_at"`
	CreatedAt   time.Time  `db:"created_at"`
}

// ----------------------------------------------------------------------------
// Request DTOs
// ----------------------------------------------------------------------------

// CreateRepositoryRequest is the request to register a git repository.
type CreateRepositoryRequest struct {
	ProjectID     string  `json:"projectId"`
	Name          string  `json:"name"`
	URL           string  `json:"url"`
	DefaultBranch string  `json:"defaultBranch,omitempty"`
	AuthType      string  `json:"authType,omitempty"`
	AuthSecretID  *string `json:"authSecretId,omitempty"`
}

// UpdateRepositoryRequest is the request to update a git repository.
type UpdateRepositoryRequest struct {
	Name          *string `json:"name,omitempty"`
	URL           *string `json:"url,omitempty"`
	DefaultBranch *string `json:"defaultBranch,omitempty"`
	AuthType      *string `json:"authType,omitempty"`
	AuthSecretID  *string `json:"authSecretId,omitempty"`
}

// CreateBuildRequest is the request to trigger a new build.
type CreateBuildRequest struct {
	// Source (one of repositoryId or gitUrl required)
	RepositoryID   *string           `json:"repositoryId,omitempty"`
	GitURL         *string           `json:"gitUrl,omitempty"`
	GitRef         string            `json:"gitRef,omitempty"`
	
	// Build context
	ContextPath    string            `json:"contextPath,omitempty"`
	DockerfilePath string            `json:"dockerfilePath,omitempty"`
	BuildArgs      map[string]string `json:"buildArgs,omitempty"`
	
	// Target
	TargetImage    string            `json:"targetImage"`
	TargetRegistry string            `json:"targetRegistry"`
	PushToRegistry *bool             `json:"pushToRegistry,omitempty"`
	
	// Options
	BuilderType    string            `json:"builderType,omitempty"`
	CPULimit       *string           `json:"cpuLimit,omitempty"`
	MemoryLimit    *string           `json:"memoryLimit,omitempty"`
	TimeoutSeconds *int              `json:"timeoutSeconds,omitempty"`
}

// RetryBuildRequest is the request to retry a failed build.
type RetryBuildRequest struct {
	ResetRetryCount bool `json:"resetRetryCount,omitempty"`
}

// ----------------------------------------------------------------------------
// View Models
// ----------------------------------------------------------------------------

// RepositoryView is the API response for a git repository.
type RepositoryView struct {
	ID            string    `json:"id"`
	OrgID         string    `json:"organizationId"`
	ProjectID     string    `json:"projectId"`
	Name          string    `json:"name"`
	URL           string    `json:"url"`
	DefaultBranch string    `json:"defaultBranch"`
	AuthType      string    `json:"authType"`
	CreatedAt     time.Time `json:"createdAt"`
}

func toRepositoryView(r *GitRepository) RepositoryView {
	return RepositoryView{
		ID:            r.ID,
		OrgID:         r.OrgID,
		ProjectID:     r.ProjectID,
		Name:          r.Name,
		URL:           r.URL,
		DefaultBranch: r.DefaultBranch,
		AuthType:      r.AuthType,
		CreatedAt:     r.CreatedAt,
	}
}

// BuildView is the API response for a build.
type BuildView struct {
	ID             string            `json:"id"`
	OrgID          string            `json:"organizationId"`
	RepositoryID   *string           `json:"repositoryId,omitempty"`
	GitURL         *string           `json:"gitUrl,omitempty"`
	GitRef         string            `json:"gitRef"`
	GitCommit      *string           `json:"gitCommit,omitempty"`
	ContextPath    string            `json:"contextPath"`
	DockerfilePath string            `json:"dockerfilePath"`
	BuildArgs      map[string]string `json:"buildArgs,omitempty"`
	TargetImage    string            `json:"targetImage"`
	TargetRegistry string            `json:"targetRegistry"`
	PushToRegistry bool              `json:"pushToRegistry"`
	BuilderType    string            `json:"builderType"`
	Status         string            `json:"status"`
	QueuedAt       time.Time         `json:"queuedAt"`
	StartedAt      *time.Time        `json:"startedAt,omitempty"`
	FinishedAt     *time.Time        `json:"finishedAt,omitempty"`
	DurationMs     *int64            `json:"durationMs,omitempty"`
	ErrorMessage   *string           `json:"errorMessage,omitempty"`
	RetryCount     int               `json:"retryCount"`
	MaxRetries     int               `json:"maxRetries"`
	ParentBuildID  *string           `json:"parentBuildId,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
}

func toBuildView(b *Build) BuildView {
	var buildArgs map[string]string
	if len(b.BuildArgs) > 0 {
		_ = json.Unmarshal(b.BuildArgs, &buildArgs)
	}
	
	view := BuildView{
		ID:             b.ID,
		OrgID:          b.OrgID,
		RepositoryID:   b.RepositoryID,
		GitURL:         b.GitURL,
		GitRef:         b.GitRef,
		GitCommit:      b.GitCommit,
		ContextPath:    b.ContextPath,
		DockerfilePath: b.DockerfilePath,
		BuildArgs:      buildArgs,
		TargetImage:    b.TargetImage,
		TargetRegistry: b.TargetRegistry,
		PushToRegistry: b.PushToRegistry,
		BuilderType:    b.BuilderType,
		Status:         b.Status,
		QueuedAt:       b.QueuedAt,
		StartedAt:      b.StartedAt,
		FinishedAt:     b.FinishedAt,
		ErrorMessage:   b.ErrorMessage,
		RetryCount:     b.RetryCount,
		MaxRetries:     b.MaxRetries,
		ParentBuildID:  b.ParentBuildID,
		CreatedAt:      b.CreatedAt,
	}
	
	if b.StartedAt != nil && b.FinishedAt != nil {
		duration := b.FinishedAt.Sub(*b.StartedAt).Milliseconds()
		view.DurationMs = &duration
	}
	
	return view
}

// BuildLogView is the API response for a build log entry.
type BuildLogView struct {
	ID        string          `json:"id"`
	BuildID   string          `json:"buildId"`
	Sequence  int             `json:"sequence"`
	Timestamp time.Time       `json:"timestamp"`
	Stream    string          `json:"stream"`
	Level     string          `json:"level"`
	Message   string          `json:"message"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

func toBuildLogView(l *BuildLog) BuildLogView {
	return BuildLogView{
		ID:        l.ID,
		BuildID:   l.BuildID,
		Sequence:  l.Sequence,
		Timestamp: l.Timestamp,
		Stream:    l.Stream,
		Level:     l.Level,
		Message:   l.Message,
		Metadata:  l.Metadata,
	}
}

// BuildArtifactView is the API response for a build artifact.
type BuildArtifactView struct {
	ID              string            `json:"id"`
	BuildID         string            `json:"buildId"`
	ImageDigest     string            `json:"imageDigest"`
	ImageTag        string            `json:"imageTag"`
	ImageSize       *int64            `json:"imageSize,omitempty"`
	ManifestType    string            `json:"manifestType"`
	LayerCount      *int              `json:"layerCount,omitempty"`
	DockerfileHash  *string           `json:"dockerfileHash,omitempty"`
	BuildDurationMs *int64            `json:"buildDurationMs,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
}

func toBuildArtifactView(a *BuildArtifact) BuildArtifactView {
	var labels map[string]string
	if len(a.Labels) > 0 {
		_ = json.Unmarshal(a.Labels, &labels)
	}
	
	return BuildArtifactView{
		ID:              a.ID,
		BuildID:         a.BuildID,
		ImageDigest:     a.ImageDigest,
		ImageTag:        a.ImageTag,
		ImageSize:       a.ImageSize,
		ManifestType:    a.ManifestType,
		LayerCount:      a.LayerCount,
		DockerfileHash:  a.DockerfileHash,
		BuildDurationMs: a.BuildDurationMs,
		Labels:          labels,
		CreatedAt:       a.CreatedAt,
	}
}

// IsTerminal returns true if the build is in a terminal state.
func (b *Build) IsTerminal() bool {
	switch b.Status {
	case StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// CanRetry returns true if the build can be retried.
func (b *Build) CanRetry() bool {
	return b.Status == StatusFailed && b.RetryCount < b.MaxRetries
}

// CanCancel returns true if the build can be cancelled.
func (b *Build) CanCancel() bool {
	return !b.IsTerminal()
}
