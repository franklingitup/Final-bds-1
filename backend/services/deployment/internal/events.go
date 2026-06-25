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

// enqueue builds an envelope and writes it to the transactional outbox.
func (s *Service) enqueue(ctx context.Context, eventType, orgID string, payload any, opts ...events.Option) error {
	e, err := events.New(eventType, eventVersion, orgID, payload, opts...)
	if err != nil {
		return err
	}
	return s.outbox.Enqueue(ctx, e)
}
