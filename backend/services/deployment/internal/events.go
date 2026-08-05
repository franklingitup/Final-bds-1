package deployment

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// Deployment domain event types. The Deployment Service is the single owner
// of the "deployment.*" and "application.*" namespaces.
const (
	EventApplicationCreated  = "application.created"
	EventApplicationUpdated  = "application.updated"
	EventApplicationDeleted  = "application.deleted"
	EventDeploymentCreated   = "deployment.created"
	EventDeploymentStarted   = "deployment.started"
	EventDeploymentSucceeded = "deployment.succeeded"
	EventDeploymentFailed    = "deployment.failed"
	EventDeploymentRollback  = "deployment.rollback.requested"
	EventDeploymentDeleted   = "deployment.deleted"

	// Deployment engine (rollout) events. These are additive and emitted by the
	// agent progress ingestion path; the pre-existing deployment.started /
	// succeeded / failed events continue to be emitted by the status path.
	EventDeploymentProgress          = "deployment.progress"
	EventDeploymentHealthy           = "deployment.healthy"
	EventDeploymentTimeout           = "deployment.timeout"
	EventDeploymentRollbackStarted   = "deployment.rollback.started"
	EventDeploymentRollbackCompleted = "deployment.rollback.completed"

	// GitOps (Argo CD) engine events. Additive: emitted only for deployments
	// bound to an Argo CD Application. deployment.rollback.started/completed are
	// reused from the rollout engine above.
	EventDeploymentSyncStarted   = "deployment.sync.started"
	EventDeploymentSyncCompleted = "deployment.sync.completed"
	EventDeploymentSyncFailed    = "deployment.sync.failed"
	EventDeploymentHealthChanged = "deployment.health.changed"
	EventDeploymentDriftDetected = "deployment.drift.detected"

	eventVersion = 1
)

// Event payloads carry domain facts only. Envelope metadata is never duplicated.

type applicationCreatedPayload struct {
	ApplicationID string `json:"applicationId"`
	ProjectID     string `json:"projectId"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	RuntimeType   string `json:"runtimeType"`
	CreatedBy     string `json:"createdBy,omitempty"`
}

type applicationUpdatedPayload struct {
	ApplicationID string `json:"applicationId"`
	Name          string `json:"name"`
	UpdatedBy     string `json:"updatedBy,omitempty"`
}

type applicationDeletedPayload struct {
	ApplicationID string `json:"applicationId"`
	DeletedBy     string `json:"deletedBy,omitempty"`
}

type deploymentCreatedPayload struct {
	DeploymentID  string `json:"deploymentId"`
	ApplicationID string `json:"applicationId"`
	ClusterID     string `json:"clusterId"`
	Image         string `json:"image"`
	Replicas      int    `json:"replicas"`
	Revision      int    `json:"revision"`
	CreatedBy     string `json:"createdBy,omitempty"`
}

type deploymentStartedPayload struct {
	DeploymentID string `json:"deploymentId"`
	ReleaseID    string `json:"releaseId"`
	Revision     int    `json:"revision"`
	Image        string `json:"image"`
}

type deploymentSucceededPayload struct {
	DeploymentID  string `json:"deploymentId"`
	ReleaseID     string `json:"releaseId"`
	Revision      int    `json:"revision"`
	ReadyReplicas int    `json:"readyReplicas"`
}

type deploymentFailedPayload struct {
	DeploymentID string `json:"deploymentId"`
	ReleaseID    string `json:"releaseId"`
	Revision     int    `json:"revision"`
	ErrorMessage string `json:"errorMessage"`
}

type deploymentRollbackPayload struct {
	DeploymentID   string `json:"deploymentId"`
	FromRevision   int    `json:"fromRevision"`
	TargetRevision int    `json:"targetRevision"`
	RequestedBy    string `json:"requestedBy,omitempty"`
}

type deploymentDeletedPayload struct {
	DeploymentID  string `json:"deploymentId"`
	ApplicationID string `json:"applicationId"`
	DeletedBy     string `json:"deletedBy,omitempty"`
}

// deploymentProgressPayload reports an incremental rollout progress snapshot.
type deploymentProgressPayload struct {
	DeploymentID      string `json:"deploymentId"`
	ReleaseID         string `json:"releaseId"`
	Revision          int    `json:"revision"`
	Phase             string `json:"phase"`
	RolloutPercentage int    `json:"rolloutPercentage"`
	ReadyReplicas     int    `json:"readyReplicas"`
	DesiredReplicas   int    `json:"desiredReplicas"`
	Image             string `json:"image,omitempty"`
}

// deploymentHealthyPayload reports that a rollout reached a healthy state.
type deploymentHealthyPayload struct {
	DeploymentID    string  `json:"deploymentId"`
	ReleaseID       string  `json:"releaseId"`
	Revision        int     `json:"revision"`
	ReadyReplicas   int     `json:"readyReplicas"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
}

// deploymentTimeoutPayload reports that a rollout exceeded its progress deadline.
type deploymentTimeoutPayload struct {
	DeploymentID string `json:"deploymentId"`
	ReleaseID    string `json:"releaseId"`
	Revision     int    `json:"revision"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// deploymentRollbackStartedPayload reports that an automatic rollback began.
type deploymentRollbackStartedPayload struct {
	DeploymentID   string `json:"deploymentId"`
	FromReleaseID  string `json:"fromReleaseId"`
	FromRevision   int    `json:"fromRevision"`
	TargetReleaseID string `json:"targetReleaseId"`
	TargetRevision int    `json:"targetRevision"`
	Reason         string `json:"reason,omitempty"`
}

// deploymentRollbackCompletedPayload reports that a rollback release is healthy.
type deploymentRollbackCompletedPayload struct {
	DeploymentID  string `json:"deploymentId"`
	ReleaseID     string `json:"releaseId"`
	Revision      int    `json:"revision"`
	ReadyReplicas int    `json:"readyReplicas"`
}

// deploymentSyncStartedPayload reports that an Argo CD sync operation began.
type deploymentSyncStartedPayload struct {
	DeploymentID string `json:"deploymentId"`
	Application  string `json:"application"`
	Cluster      string `json:"cluster,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	Revision     string `json:"revision,omitempty"`
}

// deploymentSyncCompletedPayload reports that an Argo CD sync succeeded.
type deploymentSyncCompletedPayload struct {
	DeploymentID string `json:"deploymentId"`
	Application  string `json:"application"`
	Revision     string `json:"revision,omitempty"`
	SyncStatus   string `json:"syncStatus"`
	HealthStatus string `json:"healthStatus"`
}

// deploymentSyncFailedPayload reports that an Argo CD sync operation failed.
type deploymentSyncFailedPayload struct {
	DeploymentID string `json:"deploymentId"`
	Application  string `json:"application"`
	Revision     string `json:"revision,omitempty"`
	Phase        string `json:"phase"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// deploymentHealthChangedPayload reports an Argo CD health status transition.
type deploymentHealthChangedPayload struct {
	DeploymentID string `json:"deploymentId"`
	Application  string `json:"application"`
	From         string `json:"from"`
	To           string `json:"to"`
}

// deploymentDriftDetectedPayload reports that Argo CD observed the live state
// diverging from git (OutOfSync or a degraded/missing resource tree).
type deploymentDriftDetectedPayload struct {
	DeploymentID string `json:"deploymentId"`
	Application  string `json:"application"`
	SyncStatus   string `json:"syncStatus"`
	HealthStatus string `json:"healthStatus"`
}

// enqueue builds an envelope and writes it to the transactional outbox.
func (s *Service) enqueue(ctx context.Context, eventType, orgID string, payload any, opts ...events.Option) error {
	e, err := events.New(eventType, eventVersion, orgID, payload, opts...)
	if err != nil {
		return err
	}
	return s.outbox.Enqueue(ctx, e)
}
