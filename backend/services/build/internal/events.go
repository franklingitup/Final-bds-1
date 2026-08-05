package build

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// Build domain event types. The Build Service is the single owner
// of the "build.*" and "repository.*" namespaces.
const (
	EventRepositoryCreated = "repository.created"
	EventRepositoryUpdated = "repository.updated"
	EventRepositoryDeleted = "repository.deleted"
	
	EventBuildQueued    = "build.queued"
	EventBuildStarted   = "build.started"
	EventBuildSucceeded = "build.succeeded"
	EventBuildFailed    = "build.failed"
	EventBuildCancelled = "build.cancelled"
	EventBuildRetried   = "build.retried"

	eventVersion = 1
)

// Event payloads carry domain facts only. Envelope metadata is never duplicated.

type repositoryCreatedPayload struct {
	RepositoryID string `json:"repositoryId"`
	ProjectID    string `json:"projectId"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	CreatedBy    string `json:"createdBy,omitempty"`
}

type repositoryUpdatedPayload struct {
	RepositoryID string `json:"repositoryId"`
	Name         string `json:"name"`
	UpdatedBy    string `json:"updatedBy,omitempty"`
}

type repositoryDeletedPayload struct {
	RepositoryID string `json:"repositoryId"`
	DeletedBy    string `json:"deletedBy,omitempty"`
}

type buildQueuedPayload struct {
	BuildID        string `json:"buildId"`
	RepositoryID   string `json:"repositoryId,omitempty"`
	GitURL         string `json:"gitUrl,omitempty"`
	GitRef         string `json:"gitRef"`
	TargetImage    string `json:"targetImage"`
	TargetRegistry string `json:"targetRegistry"`
	BuilderType    string `json:"builderType"`
	CreatedBy      string `json:"createdBy,omitempty"`
}

type buildStartedPayload struct {
	BuildID   string `json:"buildId"`
	WorkerID  string `json:"workerId,omitempty"`
	GitCommit string `json:"gitCommit,omitempty"`
}

type buildSucceededPayload struct {
	BuildID     string `json:"buildId"`
	ImageDigest string `json:"imageDigest"`
	ImageTag    string `json:"imageTag"`
	// Image and Registry identify the target image the build pushed. They let
	// downstream consumers (e.g. the deployment service) resolve which
	// deployments run this image and pin them to the immutable digest. These
	// fields are additive to the v1 contract; older consumers ignore them.
	Image      string `json:"image,omitempty"`
	Registry   string `json:"registry,omitempty"`
	DurationMs int64  `json:"durationMs"`
	ImageSize  int64  `json:"imageSize,omitempty"`
}

type buildFailedPayload struct {
	BuildID      string `json:"buildId"`
	ErrorMessage string `json:"errorMessage"`
	Stage        string `json:"stage"` // cloning | building | pushing
	RetryCount   int    `json:"retryCount"`
}

type buildCancelledPayload struct {
	BuildID     string `json:"buildId"`
	CancelledBy string `json:"cancelledBy,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type buildRetriedPayload struct {
	BuildID       string `json:"buildId"`
	ParentBuildID string `json:"parentBuildId"`
	RetryCount    int    `json:"retryCount"`
	RetriedBy     string `json:"retriedBy,omitempty"`
}

// enqueue builds an envelope and writes it to the transactional outbox.
func (s *Service) enqueue(ctx context.Context, eventType, orgID string, payload any, opts ...events.Option) error {
	e, err := events.New(eventType, eventVersion, orgID, payload, opts...)
	if err != nil {
		return err
	}
	return s.outbox.Enqueue(ctx, e)
}
