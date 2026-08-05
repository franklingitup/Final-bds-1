package build

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// Deps holds service dependencies.
type Deps struct {
	Repositories   RepositoryStore
	Builds         BuildStore
	BuildLogs      BuildLogStore
	BuildArtifacts BuildArtifactStore
	BuildQueue     BuildQueueStore
	OrgMembers     authz.OrgMemberStore
	Outbox         events.Outbox
	Tenant         TenantRunner
	Logger         *slog.Logger
	Now            func() time.Time
}

// Service implements build domain logic.
type Service struct {
	repos      RepositoryStore
	builds     BuildStore
	logs       BuildLogStore
	artifacts  BuildArtifactStore
	queue      BuildQueueStore
	orgMembers authz.OrgMemberStore
	outbox     events.Outbox
	tenant     TenantRunner
	authSvc    *authz.AuthorizationService
	log        *slog.Logger
	now        func() time.Time
}

// NewService creates a new build service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{
		repos:      d.Repositories,
		builds:     d.Builds,
		logs:       d.BuildLogs,
		artifacts:  d.BuildArtifacts,
		queue:      d.BuildQueue,
		orgMembers: d.OrgMembers,
		outbox:     d.Outbox,
		tenant:     d.Tenant,
		authSvc:    authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		log:        d.Logger,
		now:        d.Now,
	}
}

// ----------------------------------------------------------------------------
// Repositories
// ----------------------------------------------------------------------------

// CreateRepository registers a new Git repository.
func (s *Service) CreateRepository(ctx context.Context, orgID, projectID, userID string, req CreateRepositoryRequest) (*GitRepository, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errNameRequired
	}

	repoURL := strings.TrimSpace(req.URL)
	if repoURL == "" {
		return nil, errURLRequired
	}
	if !isValidGitURL(repoURL) {
		return nil, errInvalidGitURL
	}

	defaultBranch := strings.TrimSpace(req.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	authType := strings.TrimSpace(req.AuthType)
	if authType == "" {
		authType = AuthTypeNone
	}

	repo := &GitRepository{
		ProjectID:     projectID,
		Name:          name,
		URL:           repoURL,
		DefaultBranch: defaultBranch,
		AuthType:      authType,
		AuthSecretID:  req.AuthSecretID,
	}
	repo.OrgID = orgID
	repo.CreatedBy = &userID

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.repos.Create(ctx, repo); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errRepositoryNameTaken
			}
			return err
		}
		return s.enqueue(ctx, EventRepositoryCreated, orgID, repositoryCreatedPayload{
			RepositoryID: repo.ID,
			ProjectID:    projectID,
			Name:         repo.Name,
			URL:          repo.URL,
			CreatedBy:    userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "repository", ID: repo.ID}))
	})
	if err != nil {
		return nil, err
	}
	return repo, nil
}

// GetRepository returns a repository by ID.
func (s *Service) GetRepository(ctx context.Context, orgID, userID, repoID string) (*GitRepository, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var repo *GitRepository
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		repo, err = s.repos.GetByID(ctx, repoID)
		return err
	})
	return repo, err
}

// ListRepositories returns repositories for a project.
func (s *Service) ListRepositories(ctx context.Context, orgID, userID, projectID string, page database.PageRequest) (database.Page[GitRepository], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[GitRepository]{}, err
	}

	var out database.Page[GitRepository]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.repos.List(ctx, projectID, page)
		return err
	})
	return out, err
}

// UpdateRepository updates a repository.
func (s *Service) UpdateRepository(ctx context.Context, orgID, userID, repoID string, req UpdateRepositoryRequest) (*GitRepository, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var repo *GitRepository
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		repo, err = s.repos.GetByID(ctx, repoID)
		if err != nil {
			return err
		}

		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return errNameRequired
			}
			repo.Name = name
		}
		if req.URL != nil {
			repoURL := strings.TrimSpace(*req.URL)
			if repoURL == "" {
				return errURLRequired
			}
			if !isValidGitURL(repoURL) {
				return errInvalidGitURL
			}
			repo.URL = repoURL
		}
		if req.DefaultBranch != nil {
			repo.DefaultBranch = strings.TrimSpace(*req.DefaultBranch)
		}
		if req.AuthType != nil {
			repo.AuthType = strings.TrimSpace(*req.AuthType)
		}
		if req.AuthSecretID != nil {
			repo.AuthSecretID = req.AuthSecretID
		}

		if err := s.repos.Update(ctx, repo); err != nil {
			return err
		}

		return s.enqueue(ctx, EventRepositoryUpdated, orgID, repositoryUpdatedPayload{
			RepositoryID: repo.ID,
			Name:         repo.Name,
			UpdatedBy:    userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "repository", ID: repo.ID}))
	})
	return repo, err
}

// DeleteRepository deletes a repository.
func (s *Service) DeleteRepository(ctx context.Context, orgID, userID, repoID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.repos.Delete(ctx, repoID); err != nil {
			return err
		}
		return s.enqueue(ctx, EventRepositoryDeleted, orgID, repositoryDeletedPayload{
			RepositoryID: repoID,
			DeletedBy:    userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "repository", ID: repoID}))
	})
}

// ----------------------------------------------------------------------------
// Builds
// ----------------------------------------------------------------------------

// CreateBuild triggers a new build.
func (s *Service) CreateBuild(ctx context.Context, orgID, userID string, req CreateBuildRequest) (*Build, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	// Validate source
	if req.RepositoryID == nil && req.GitURL == nil {
		return nil, errInvalidSource
	}

	// Validate target
	if strings.TrimSpace(req.TargetImage) == "" {
		return nil, errInvalidTargetImage
	}
	if strings.TrimSpace(req.TargetRegistry) == "" {
		return nil, errInvalidRegistry
	}

	// Validate git URL if provided directly
	if req.GitURL != nil && !isValidGitURL(*req.GitURL) {
		return nil, errInvalidGitURL
	}

	// Set defaults
	contextPath := strings.TrimSpace(req.ContextPath)
	if contextPath == "" {
		contextPath = "."
	}

	dockerfilePath := strings.TrimSpace(req.DockerfilePath)
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}

	gitRef := strings.TrimSpace(req.GitRef)
	if gitRef == "" {
		gitRef = "main"
	}

	builderType := strings.TrimSpace(req.BuilderType)
	if builderType == "" {
		builderType = BuilderKaniko
	}

	pushToRegistry := true
	if req.PushToRegistry != nil {
		pushToRegistry = *req.PushToRegistry
	}

	buildArgs, _ := json.Marshal(req.BuildArgs)
	if req.BuildArgs == nil {
		buildArgs = []byte("{}")
	}

	b := &Build{
		RepositoryID:   req.RepositoryID,
		GitURL:         req.GitURL,
		GitRef:         gitRef,
		ContextPath:    contextPath,
		DockerfilePath: dockerfilePath,
		BuildArgs:      buildArgs,
		TargetImage:    strings.TrimSpace(req.TargetImage),
		TargetRegistry: strings.TrimSpace(req.TargetRegistry),
		PushToRegistry: pushToRegistry,
		BuilderType:    builderType,
		Status:         StatusQueued,
		CPULimit:       req.CPULimit,
		MemoryLimit:    req.MemoryLimit,
		TimeoutSeconds: 1800,
	}
	if req.TimeoutSeconds != nil {
		b.TimeoutSeconds = *req.TimeoutSeconds
	}
	b.OrgID = orgID
	b.CreatedBy = &userID

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// If repository ID provided, verify it exists and get git URL
		if req.RepositoryID != nil {
			repo, err := s.repos.GetByID(ctx, *req.RepositoryID)
			if err != nil {
				if database.IsNotFound(err) {
					return errRepositoryNotFound
				}
				return err
			}
			b.GitURL = &repo.URL
			if gitRef == "main" && repo.DefaultBranch != "" {
				b.GitRef = repo.DefaultBranch
			}
		}

		// Create build
		if err := s.builds.Create(ctx, b); err != nil {
			return err
		}

		// Add to queue
		if err := s.queue.Enqueue(ctx, b.ID, 0); err != nil {
			return err
		}

		// Emit event
		repoID := ""
		if b.RepositoryID != nil {
			repoID = *b.RepositoryID
		}
		gitURL := ""
		if b.GitURL != nil {
			gitURL = *b.GitURL
		}

		return s.enqueue(ctx, EventBuildQueued, orgID, buildQueuedPayload{
			BuildID:        b.ID,
			RepositoryID:   repoID,
			GitURL:         gitURL,
			GitRef:         b.GitRef,
			TargetImage:    b.TargetImage,
			TargetRegistry: b.TargetRegistry,
			BuilderType:    b.BuilderType,
			CreatedBy:      userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "build", ID: b.ID}))
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// GetBuild returns a build by ID.
func (s *Service) GetBuild(ctx context.Context, orgID, userID, buildID string) (*Build, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var b *Build
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		b, err = s.builds.GetByID(ctx, buildID)
		return err
	})
	return b, err
}

// ListBuilds returns builds for the organization.
func (s *Service) ListBuilds(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[Build], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Build]{}, err
	}

	var out database.Page[Build]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.builds.List(ctx, page)
		return err
	})
	return out, err
}

// CancelBuild cancels a queued or running build.
func (s *Service) CancelBuild(ctx context.Context, orgID, userID, buildID string) (*Build, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var b *Build
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		b, err = s.builds.GetByID(ctx, buildID)
		if err != nil {
			return err
		}

		if !b.CanCancel() {
			return errBuildNotCancellable
		}

		b.Status = StatusCancelled
		b.CancelledBy = &userID
		now := s.now()
		b.FinishedAt = &now

		if err := s.builds.Update(ctx, b); err != nil {
			return err
		}

		// Remove from queue if present
		if err := s.queue.Remove(ctx, buildID); err != nil {
			s.log.Warn("failed to remove build from queue", "buildId", buildID, "error", err)
		}

		return s.enqueue(ctx, EventBuildCancelled, orgID, buildCancelledPayload{
			BuildID:     buildID,
			CancelledBy: userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "build", ID: buildID}))
	})
	return b, err
}

// RetryBuild retries a failed build.
func (s *Service) RetryBuild(ctx context.Context, orgID, userID, buildID string, req RetryBuildRequest) (*Build, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var newBuild *Build
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		b, err := s.builds.GetByID(ctx, buildID)
		if err != nil {
			return err
		}

		if !b.CanRetry() {
			return errBuildNotRetryable
		}

		// Create a new build based on the original
		newBuild = &Build{
			RepositoryID:   b.RepositoryID,
			GitURL:         b.GitURL,
			GitRef:         b.GitRef,
			ContextPath:    b.ContextPath,
			DockerfilePath: b.DockerfilePath,
			BuildArgs:      b.BuildArgs,
			TargetImage:    b.TargetImage,
			TargetRegistry: b.TargetRegistry,
			PushToRegistry: b.PushToRegistry,
			BuilderType:    b.BuilderType,
			Status:         StatusQueued,
			MaxRetries:     b.MaxRetries,
			ParentBuildID:  &buildID,
			CPULimit:       b.CPULimit,
			MemoryLimit:    b.MemoryLimit,
			TimeoutSeconds: b.TimeoutSeconds,
		}
		newBuild.OrgID = orgID
		newBuild.CreatedBy = &userID

		if req.ResetRetryCount {
			newBuild.RetryCount = 0
		} else {
			newBuild.RetryCount = b.RetryCount + 1
		}

		if err := s.builds.Create(ctx, newBuild); err != nil {
			return err
		}

		// Add to queue with higher priority
		if err := s.queue.Enqueue(ctx, newBuild.ID, 1); err != nil {
			return err
		}

		return s.enqueue(ctx, EventBuildRetried, orgID, buildRetriedPayload{
			BuildID:       newBuild.ID,
			ParentBuildID: buildID,
			RetryCount:    newBuild.RetryCount,
			RetriedBy:     userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "build", ID: newBuild.ID}))
	})
	return newBuild, err
}

// GetBuildLogs returns logs for a build.
func (s *Service) GetBuildLogs(ctx context.Context, orgID, userID, buildID string, page database.PageRequest) (database.Page[BuildLog], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[BuildLog]{}, err
	}

	var out database.Page[BuildLog]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.logs.List(ctx, buildID, page)
		return err
	})
	return out, err
}

// GetBuildArtifact returns the artifact for a build.
func (s *Service) GetBuildArtifact(ctx context.Context, orgID, userID, buildID string) (*BuildArtifact, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var a *BuildArtifact
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		a, err = s.artifacts.GetByBuildID(ctx, buildID)
		return err
	})
	return a, err
}

// ----------------------------------------------------------------------------
// Worker Operations (internal, no user auth)
// ----------------------------------------------------------------------------

// ClaimBuild claims a build from the queue for a worker.
func (s *Service) ClaimBuild(ctx context.Context, workerID string) (*Build, error) {
	item, err := s.queue.Dequeue(ctx, workerID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, nil // No work available
	}

	// Get the full build details within tenant context
	var b *Build
	err = s.tenant.WithTenant(ctx, item.OrgID, func(ctx context.Context) error {
		var err error
		b, err = s.builds.GetByID(ctx, item.BuildID)
		return err
	})
	if err != nil {
		// Release the claim if we can't get the build
		_ = s.queue.Remove(ctx, item.BuildID)
		return nil, err
	}

	return b, nil
}

// MarkBuildStarted marks a build as started by a worker.
func (s *Service) MarkBuildStarted(ctx context.Context, orgID, buildID, workerID string, commit *string) error {
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.builds.MarkStarted(ctx, buildID, commit); err != nil {
			return err
		}

		commitStr := ""
		if commit != nil {
			commitStr = *commit
		}

		return s.enqueue(ctx, EventBuildStarted, orgID, buildStartedPayload{
			BuildID:   buildID,
			WorkerID:  workerID,
			GitCommit: commitStr,
		}, events.WithResource(events.Resource{Type: "build", ID: buildID}))
	})
}

// MarkBuildSucceeded marks a build as succeeded.
func (s *Service) MarkBuildSucceeded(ctx context.Context, orgID, buildID string, artifact *BuildArtifact, durationMs int64) error {
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Load the build to carry its target image into the success event so
		// deployment consumers can resolve which deployments use this image.
		b, err := s.builds.GetByID(ctx, buildID)
		if err != nil {
			return err
		}

		if err := s.builds.MarkFinished(ctx, buildID, StatusSucceeded, nil); err != nil {
			return err
		}

		// Save artifact
		artifact.OrgID = orgID
		artifact.BuildID = buildID
		artifact.BuildDurationMs = &durationMs
		if err := s.artifacts.Create(ctx, artifact); err != nil {
			return err
		}

		// Remove from queue
		if err := s.queue.Remove(ctx, buildID); err != nil {
			s.log.Warn("failed to remove build from queue", "buildId", buildID, "error", err)
		}

		imageSize := int64(0)
		if artifact.ImageSize != nil {
			imageSize = *artifact.ImageSize
		}

		return s.enqueue(ctx, EventBuildSucceeded, orgID, buildSucceededPayload{
			BuildID:     buildID,
			ImageDigest: artifact.ImageDigest,
			ImageTag:    artifact.ImageTag,
			Image:       b.TargetImage,
			Registry:    b.TargetRegistry,
			DurationMs:  durationMs,
			ImageSize:   imageSize,
		}, events.WithResource(events.Resource{Type: "build", ID: buildID}))
	})
}

// MarkBuildFailed marks a build as failed.
func (s *Service) MarkBuildFailed(ctx context.Context, orgID, buildID string, errorMsg, stage string) error {
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		b, err := s.builds.GetByID(ctx, buildID)
		if err != nil {
			return err
		}

		if err := s.builds.MarkFinished(ctx, buildID, StatusFailed, &errorMsg); err != nil {
			return err
		}

		// Remove from queue
		if err := s.queue.Remove(ctx, buildID); err != nil {
			s.log.Warn("failed to remove build from queue", "buildId", buildID, "error", err)
		}

		return s.enqueue(ctx, EventBuildFailed, orgID, buildFailedPayload{
			BuildID:      buildID,
			ErrorMessage: errorMsg,
			Stage:        stage,
			RetryCount:   b.RetryCount,
		}, events.WithResource(events.Resource{Type: "build", ID: buildID}))
	})
}

// AppendBuildLog appends a log entry to a build.
func (s *Service) AppendBuildLog(ctx context.Context, orgID, buildID string, level, stream, message string, metadata map[string]any) error {
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		seq, err := s.logs.GetNextSequence(ctx, buildID)
		if err != nil {
			return err
		}

		metaJSON, _ := json.Marshal(metadata)
		if metadata == nil {
			metaJSON = []byte("{}")
		}

		log := &BuildLog{
			OrgID:     orgID,
			BuildID:   buildID,
			Sequence:  seq,
			Timestamp: s.now(),
			Stream:    stream,
			Level:     level,
			Message:   message,
			Metadata:  metaJSON,
		}

		return s.logs.Append(ctx, log)
	})
}

// HeartbeatBuild updates the heartbeat for a claimed build.
func (s *Service) HeartbeatBuild(ctx context.Context, buildID, workerID string) error {
	return s.queue.Heartbeat(ctx, buildID, workerID)
}

// ReleaseStaleClaims releases builds that have been claimed but not heartbeated.
func (s *Service) ReleaseStaleClaims(ctx context.Context, timeout time.Duration) error {
	return s.queue.ReleaseStaleClaims(ctx, timeout)
}

// ----------------------------------------------------------------------------
// Webhook-Triggered Builds
// ----------------------------------------------------------------------------

// WebhookBuildRequest contains parameters for a webhook-triggered build.
type WebhookBuildRequest struct {
	GitURL         string
	GitRef         string
	GitCommit      string
	TargetImage    string
	TargetRegistry string
	RepositoryName string
}

// TriggerBuildFromWebhook creates a build from a webhook event.
// This is called by the GitHub service when a push webhook is received.
func (s *Service) TriggerBuildFromWebhook(ctx context.Context, orgID string, req WebhookBuildRequest) (string, error) {
	if req.GitURL == "" {
		return "", errInvalidSource
	}
	if req.TargetImage == "" {
		return "", errInvalidTargetImage
	}
	if req.TargetRegistry == "" {
		return "", errInvalidRegistry
	}

	b := &Build{
		GitURL:         &req.GitURL,
		GitRef:         req.GitRef,
		GitCommit:      &req.GitCommit,
		ContextPath:    ".",
		DockerfilePath: "Dockerfile",
		BuildArgs:      []byte("{}"),
		TargetImage:    req.TargetImage,
		TargetRegistry: req.TargetRegistry,
		PushToRegistry: true,
		BuilderType:    BuilderKaniko,
		Status:         StatusQueued,
		TimeoutSeconds: 1800,
	}
	b.OrgID = orgID

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.builds.Create(ctx, b); err != nil {
			return err
		}

		if err := s.queue.Enqueue(ctx, b.ID, 0); err != nil {
			return err
		}

		return s.enqueue(ctx, EventBuildQueued, orgID, buildQueuedPayload{
			BuildID:        b.ID,
			GitURL:         req.GitURL,
			GitRef:         b.GitRef,
			TargetImage:    b.TargetImage,
			TargetRegistry: b.TargetRegistry,
			BuilderType:    b.BuilderType,
		}, events.WithResource(events.Resource{Type: "build", ID: b.ID}))
	})
	if err != nil {
		return "", err
	}

	s.log.Info("created build from webhook",
		"buildId", b.ID,
		"orgId", orgID,
		"gitUrl", req.GitURL,
		"gitRef", req.GitRef,
	)

	return b.ID, nil
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func isValidGitURL(rawURL string) bool {
	// Handle SSH URLs
	if strings.HasPrefix(rawURL, "git@") {
		return strings.Contains(rawURL, ":")
	}

	// Handle HTTP(S) URLs
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
