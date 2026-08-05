package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// TenantRunner runs a function within a tenant-scoped transaction.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// DeploymentReader reads deployment data (from the parent deployment service).
type DeploymentReader interface {
	GetDeployment(ctx context.Context, id string) (*DeploymentInfo, error)
	GetApplication(ctx context.Context, id string) (*ApplicationInfo, error)
}

// ReleaseCreator creates releases (interfaces with parent deployment service).
type ReleaseCreator interface {
	CreateRelease(ctx context.Context, deploymentID, image string, replicas int, config json.RawMessage, userID string) (string, int, error)
	MarkReleaseDeploying(ctx context.Context, releaseID string) error
	MarkReleaseSucceeded(ctx context.Context, releaseID string) error
	MarkReleaseFailed(ctx context.Context, releaseID string, errMsg string) error
}

// DeploymentInfo holds deployment information.
type DeploymentInfo struct {
	ID            string
	OrgID         string
	ApplicationID string
	ClusterID     string
	Image         string
	Replicas      int
	Port          *int
	CPURequest    *string
	CPULimit      *string
	MemoryRequest *string
	MemoryLimit   *string
	EnvVars       json.RawMessage
}

// ApplicationInfo holds application information.
type ApplicationInfo struct {
	ID        string
	OrgID     string
	ProjectID string
	Name      string
	Slug      string
}

// Deps holds pipeline service dependencies.
type Deps struct {
	DesiredStates     DesiredStateStore
	PipelineRuns      PipelineRunStore
	PipelineEvents    PipelineEventStore
	DeploymentMetrics DeploymentMetricsStore
	Deployments       DeploymentReader
	Releases          ReleaseCreator
	OrgMembers        authz.OrgMemberStore
	Outbox            events.Outbox
	Tenant            TenantRunner
	Logger            *slog.Logger
}

// Service implements pipeline orchestration logic.
type Service struct {
	desiredStates  DesiredStateStore
	pipelineRuns   PipelineRunStore
	pipelineEvents PipelineEventStore
	metrics        DeploymentMetricsStore
	deployments    DeploymentReader
	releases       ReleaseCreator
	orgMembers     authz.OrgMemberStore
	outbox         events.Outbox
	tenant         TenantRunner
	authSvc        *authz.AuthorizationService
	generator      *ManifestGenerator
	log            *slog.Logger
}

// NewService creates a new pipeline service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &Service{
		desiredStates:  d.DesiredStates,
		pipelineRuns:   d.PipelineRuns,
		pipelineEvents: d.PipelineEvents,
		metrics:        d.DeploymentMetrics,
		deployments:    d.Deployments,
		releases:       d.Releases,
		orgMembers:     d.OrgMembers,
		outbox:         d.Outbox,
		tenant:         d.Tenant,
		authSvc:        authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		generator:      NewManifestGenerator(),
		log:            d.Logger,
	}
}

// ----------------------------------------------------------------------------
// Pipeline Operations
// ----------------------------------------------------------------------------

// TriggerPipeline starts a new deployment pipeline.
func (s *Service) TriggerPipeline(ctx context.Context, orgID, userID string, req TriggerPipelineRequest) (*PipelineRun, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	if req.SourceRef == "" {
		return nil, apperrors.Validation("source reference is required")
	}

	sourceType := req.SourceType
	if sourceType == "" {
		sourceType = SourceTypeImage
	}

	var pr *PipelineRun
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Verify deployment exists
		dep, err := s.deployments.GetDeployment(ctx, req.DeploymentID)
		if err != nil {
			return err
		}

		pr = &PipelineRun{
			OrgID:        orgID,
			DeploymentID: req.DeploymentID,
			SourceType:   sourceType,
			SourceRef:    req.SourceRef,
			BuildID:      req.BuildID,
			Status:       StatusPending,
			CurrentStage: StageInit,
			TriggeredBy:  TriggerUser,
			CreatedBy:    &userID,
		}

		if err := s.pipelineRuns.Create(ctx, pr); err != nil {
			return err
		}

		// Log event
		s.logEvent(ctx, orgID, pr.ID, "pipeline_created", StageInit,
			fmt.Sprintf("Pipeline triggered for deployment %s", dep.ID), nil)

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Start pipeline execution asynchronously
	go s.executePipeline(context.Background(), orgID, pr.ID)

	return pr, nil
}

// executePipeline runs the pipeline stages.
func (s *Service) executePipeline(ctx context.Context, orgID, pipelineID string) {
	log := s.log.With("pipelineId", pipelineID, "orgId", orgID)

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		pr, err := s.pipelineRuns.GetByID(ctx, pipelineID)
		if err != nil {
			return err
		}

		// Stage: Build (if source is git or requires build)
		if pr.SourceType == SourceTypeGit || pr.SourceType == SourceTypeBuild {
			if err := s.executeBuildStage(ctx, orgID, pr); err != nil {
				return s.failPipeline(ctx, orgID, pr, StageBuild, err)
			}
		}

		// Stage: Release
		if err := s.executeReleaseStage(ctx, orgID, pr); err != nil {
			return s.failPipeline(ctx, orgID, pr, StageRelease, err)
		}

		// Stage: Deploy (generate desired state)
		if err := s.executeDeployStage(ctx, orgID, pr); err != nil {
			return s.failPipeline(ctx, orgID, pr, StageDeploy, err)
		}

		// Mark pipeline as succeeded
		if err := s.pipelineRuns.UpdateStatus(ctx, pr.ID, StatusSucceeded, StageDone, nil, nil); err != nil {
			return err
		}

		s.logEvent(ctx, orgID, pr.ID, "pipeline_succeeded", StageDone, "Pipeline completed successfully", nil)
		return nil
	})

	if err != nil {
		log.Error("pipeline execution failed", "error", err)
	}
}

func (s *Service) executeBuildStage(ctx context.Context, orgID string, pr *PipelineRun) error {
	s.logEvent(ctx, orgID, pr.ID, "stage_started", StageBuild, "Starting build stage", nil)

	if err := s.pipelineRuns.UpdateStatus(ctx, pr.ID, StatusBuilding, StageBuild, nil, nil); err != nil {
		return err
	}

	// If build ID is provided, wait for it to complete
	if pr.BuildID != nil {
		// In a real implementation, this would poll the build service
		// For now, we'll assume the build is already complete and builtImage is set
		s.logEvent(ctx, orgID, pr.ID, "waiting_for_build", StageBuild,
			fmt.Sprintf("Waiting for build %s", *pr.BuildID), nil)

		// TODO: Implement build polling or use events
		// For direct image deployments, this stage is skipped
	}

	s.logEvent(ctx, orgID, pr.ID, "stage_completed", StageBuild, "Build stage completed", nil)
	return nil
}

func (s *Service) executeReleaseStage(ctx context.Context, orgID string, pr *PipelineRun) error {
	s.logEvent(ctx, orgID, pr.ID, "stage_started", StageRelease, "Starting release stage", nil)

	if err := s.pipelineRuns.UpdateStatus(ctx, pr.ID, StatusDeploying, StageRelease, nil, nil); err != nil {
		return err
	}

	// Get deployment info
	dep, err := s.deployments.GetDeployment(ctx, pr.DeploymentID)
	if err != nil {
		return err
	}

	// Determine the image to deploy
	image := pr.SourceRef
	if pr.BuiltImage != nil && *pr.BuiltImage != "" {
		image = *pr.BuiltImage
	}

	// Create release
	userID := ""
	if pr.CreatedBy != nil {
		userID = *pr.CreatedBy
	}

	releaseID, revision, err := s.releases.CreateRelease(ctx, pr.DeploymentID, image, dep.Replicas, dep.EnvVars, userID)
	if err != nil {
		return fmt.Errorf("create release: %w", err)
	}

	// Update pipeline with release ID
	if err := s.pipelineRuns.SetReleaseID(ctx, pr.ID, releaseID); err != nil {
		return err
	}
	pr.ReleaseID = &releaseID

	s.logEvent(ctx, orgID, pr.ID, "release_created", StageRelease,
		fmt.Sprintf("Created release revision %d with image %s", revision, image),
		map[string]any{"releaseId": releaseID, "revision": revision, "image": image})

	// Mark release as deploying
	if err := s.releases.MarkReleaseDeploying(ctx, releaseID); err != nil {
		return err
	}

	s.logEvent(ctx, orgID, pr.ID, "stage_completed", StageRelease, "Release stage completed", nil)
	return nil
}

func (s *Service) executeDeployStage(ctx context.Context, orgID string, pr *PipelineRun) error {
	s.logEvent(ctx, orgID, pr.ID, "stage_started", StageDeploy, "Starting deploy stage", nil)

	// Get deployment and application info
	dep, err := s.deployments.GetDeployment(ctx, pr.DeploymentID)
	if err != nil {
		return err
	}

	app, err := s.deployments.GetApplication(ctx, dep.ApplicationID)
	if err != nil {
		return err
	}

	// Determine image
	image := pr.SourceRef
	if pr.BuiltImage != nil && *pr.BuiltImage != "" {
		image = *pr.BuiltImage
	}

	// Parse env vars
	envVars := ParseEnvVars(dep.EnvVars)

	// Generate manifests
	manifestCfg := DeploymentConfig{
		Name:      app.Slug,
		Namespace: GenerateNamespace(app.Slug, app.Slug),
		Image:     image,
		Replicas:  dep.Replicas,
		Port:      dep.Port,
		EnvVars:   envVars,
		Labels: map[string]string{
			"app.kubernetes.io/instance":   dep.ID,
			"app.kubernetes.io/part-of":    app.Slug,
			"bdsplatform.io/deployment-id": dep.ID,
			"bdsplatform.io/org-id":        orgID,
		},
	}

	if dep.CPURequest != nil {
		manifestCfg.CPURequest = *dep.CPURequest
	}
	if dep.CPULimit != nil {
		manifestCfg.CPULimit = *dep.CPULimit
	}
	if dep.MemoryRequest != nil {
		manifestCfg.MemoryRequest = *dep.MemoryRequest
	}
	if dep.MemoryLimit != nil {
		manifestCfg.MemoryLimit = *dep.MemoryLimit
	}

	generated, err := s.generator.Generate(manifestCfg)
	if err != nil {
		return fmt.Errorf("generate manifests: %w", err)
	}

	manifestsJSON, _ := json.Marshal(generated.Manifests)

	// Create or update desired state
	ds := &DesiredState{
		OrgID:        orgID,
		DeploymentID: pr.DeploymentID,
		ReleaseID:    *pr.ReleaseID,
		ClusterID:    dep.ClusterID,
		Namespace:    generated.Namespace,
		Manifests:    manifestsJSON,
		ManifestHash: generated.Hash,
		SyncStatus:   SyncStatusPending,
	}

	if err := s.desiredStates.Create(ctx, ds); err != nil {
		return fmt.Errorf("create desired state: %w", err)
	}

	s.logEvent(ctx, orgID, pr.ID, "desired_state_created", StageDeploy,
		fmt.Sprintf("Created desired state with hash %s", generated.Hash),
		map[string]any{"desiredStateId": ds.ID, "manifestHash": generated.Hash, "generation": ds.Generation})

	s.logEvent(ctx, orgID, pr.ID, "stage_completed", StageDeploy, "Deploy stage completed - awaiting agent sync", nil)
	return nil
}

func (s *Service) failPipeline(ctx context.Context, orgID string, pr *PipelineRun, stage string, err error) error {
	errMsg := err.Error()
	s.logEvent(ctx, orgID, pr.ID, "pipeline_failed", stage, fmt.Sprintf("Pipeline failed: %s", errMsg), nil)

	if updateErr := s.pipelineRuns.UpdateStatus(ctx, pr.ID, StatusFailed, stage, &errMsg, &stage); updateErr != nil {
		s.log.Error("failed to update pipeline status", "error", updateErr)
	}

	// Mark release as failed if one was created
	if pr.ReleaseID != nil {
		if relErr := s.releases.MarkReleaseFailed(ctx, *pr.ReleaseID, errMsg); relErr != nil {
			s.log.Error("failed to mark release as failed", "error", relErr)
		}
	}

	return err
}

func (s *Service) logEvent(ctx context.Context, orgID, pipelineRunID, eventType, stage, message string, details any) {
	stagePtr := &stage
	pe := &PipelineEvent{
		OrgID:         orgID,
		PipelineRunID: pipelineRunID,
		EventType:     eventType,
		Stage:         stagePtr,
		Message:       message,
		Details:       marshalDetails(details),
	}
	if err := s.pipelineEvents.Create(ctx, pe); err != nil {
		s.log.Error("failed to create pipeline event", "error", err)
	}
}

// ----------------------------------------------------------------------------
// Pipeline Queries
// ----------------------------------------------------------------------------

// GetPipelineRun returns a pipeline run by ID.
func (s *Service) GetPipelineRun(ctx context.Context, orgID, userID, pipelineID string) (*PipelineRun, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var pr *PipelineRun
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		pr, err = s.pipelineRuns.GetByID(ctx, pipelineID)
		return err
	})
	return pr, err
}

// ListPipelineRuns returns pipeline runs for a deployment.
func (s *Service) ListPipelineRuns(ctx context.Context, orgID, userID, deploymentID string, page database.PageRequest) (database.Page[PipelineRun], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[PipelineRun]{}, err
	}

	var out database.Page[PipelineRun]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.pipelineRuns.List(ctx, deploymentID, page)
		return err
	})
	return out, err
}

// GetPipelineEvents returns events for a pipeline run.
func (s *Service) GetPipelineEvents(ctx context.Context, orgID, userID, pipelineID string, page database.PageRequest) (database.Page[PipelineEvent], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[PipelineEvent]{}, err
	}

	var out database.Page[PipelineEvent]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.pipelineEvents.List(ctx, pipelineID, page)
		return err
	})
	return out, err
}

// CancelPipeline cancels an active pipeline.
func (s *Service) CancelPipeline(ctx context.Context, orgID, userID, pipelineID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		pr, err := s.pipelineRuns.GetByID(ctx, pipelineID)
		if err != nil {
			return err
		}

		if pr.Status == StatusSucceeded || pr.Status == StatusFailed || pr.Status == StatusCancelled {
			return apperrors.Validation("pipeline is already finished")
		}

		errMsg := "Cancelled by user"
		if err := s.pipelineRuns.UpdateStatus(ctx, pipelineID, StatusCancelled, pr.CurrentStage, &errMsg, nil); err != nil {
			return err
		}

		s.logEvent(ctx, orgID, pipelineID, "pipeline_cancelled", pr.CurrentStage, "Pipeline cancelled by user", nil)
		return nil
	})
}

// ----------------------------------------------------------------------------
// Agent Reconciliation
// ----------------------------------------------------------------------------

// GetDesiredStateForAgent returns the desired state for a cluster (agent pull).
func (s *Service) GetDesiredStateForAgent(ctx context.Context, orgID, clusterID string) ([]AgentDesiredState, error) {
	var states []DesiredState
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		states, err = s.desiredStates.ListPendingByCluster(ctx, clusterID)
		return err
	})
	if err != nil {
		return nil, err
	}

	result := make([]AgentDesiredState, len(states))
	for i, ds := range states {
		result[i] = ToAgentDesiredState(&ds)
	}
	return result, nil
}

// ReportSync handles sync status reports from agents.
func (s *Service) ReportSync(ctx context.Context, orgID string, req ReportSyncRequest) error {
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		ds, err := s.desiredStates.GetByDeploymentID(ctx, req.DeploymentID)
		if err != nil {
			return err
		}

		if err := s.desiredStates.UpdateSyncStatus(ctx, ds.ID, req.SyncStatus, req.ObservedGeneration, req.ErrorMessage); err != nil {
			return err
		}

		// If sync succeeded and we have a release, mark it as succeeded
		if req.SyncStatus == SyncStatusSynced && ds.ReleaseID != "" {
			if err := s.releases.MarkReleaseSucceeded(ctx, ds.ReleaseID); err != nil {
				s.log.Warn("failed to mark release as succeeded", "releaseId", ds.ReleaseID, "error", err)
			}
		}

		return nil
	})
}

// ReportMetrics handles metrics reports from agents.
func (s *Service) ReportMetrics(ctx context.Context, orgID string, req ReportMetricsRequest) error {
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		dm := &DeploymentMetrics{
			OrgID:               orgID,
			DeploymentID:        req.DeploymentID,
			AvailableReplicas:   req.AvailableReplicas,
			ReadyReplicas:       req.ReadyReplicas,
			UpdatedReplicas:     req.UpdatedReplicas,
			CPUUsageMillicores:  req.CPUUsageMillicores,
			MemoryUsageBytes:    req.MemoryUsageBytes,
			HealthStatus:        req.HealthStatus,
		}
		if dm.HealthStatus == "" {
			dm.HealthStatus = HealthUnknown
		}

		return s.metrics.Upsert(ctx, dm)
	})
}

// GetDesiredState returns the desired state for a deployment.
func (s *Service) GetDesiredState(ctx context.Context, orgID, userID, deploymentID string) (*DesiredState, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var ds *DesiredState
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		ds, err = s.desiredStates.GetByDeploymentID(ctx, deploymentID)
		return err
	})
	return ds, err
}

// GetDeploymentMetrics returns metrics for a deployment.
func (s *Service) GetDeploymentMetrics(ctx context.Context, orgID, userID, deploymentID string) (*DeploymentMetrics, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var dm *DeploymentMetrics
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		dm, err = s.metrics.GetByDeploymentID(ctx, deploymentID)
		return err
	})
	return dm, err
}

// ----------------------------------------------------------------------------
// Rollback
// ----------------------------------------------------------------------------

// TriggerRollback triggers a rollback to a previous release.
func (s *Service) TriggerRollback(ctx context.Context, orgID, userID, deploymentID string, targetRevision *int) (*PipelineRun, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var pr *PipelineRun
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Get current deployment state
		dep, err := s.deployments.GetDeployment(ctx, deploymentID)
		if err != nil {
			return err
		}

		// For rollback, we'll use the previous image
		// In a real implementation, we'd look up the target release and use its image
		// For now, we'll pass the current image and let the release creation handle it
		pr = &PipelineRun{
			OrgID:        orgID,
			DeploymentID: deploymentID,
			SourceType:   SourceTypeImage,
			SourceRef:    dep.Image, // This would be replaced with target revision's image
			Status:       StatusPending,
			CurrentStage: StageInit,
			TriggeredBy:  TriggerRollback,
			CreatedBy:    &userID,
		}

		if err := s.pipelineRuns.Create(ctx, pr); err != nil {
			return err
		}

		s.logEvent(ctx, orgID, pr.ID, "rollback_triggered", StageInit,
			fmt.Sprintf("Rollback triggered for deployment %s", deploymentID),
			map[string]any{"targetRevision": targetRevision})

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Execute pipeline
	go s.executePipeline(context.Background(), orgID, pr.ID)

	return pr, nil
}

// ----------------------------------------------------------------------------
// Quick Deploy (Direct image deployment)
// ----------------------------------------------------------------------------

// QuickDeploy triggers a deployment with a specific image.
func (s *Service) QuickDeploy(ctx context.Context, orgID, userID, deploymentID, image string) (*PipelineRun, error) {
	return s.TriggerPipeline(ctx, orgID, userID, TriggerPipelineRequest{
		DeploymentID: deploymentID,
		SourceType:   SourceTypeImage,
		SourceRef:    image,
	})
}
