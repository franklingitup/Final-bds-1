package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// Deps holds service dependencies.
type Deps struct {
	Applications ApplicationStore
	Deployments  DeploymentStore
	Releases     ReleaseStore
	OrgMembers   authz.OrgMemberStore // For org membership authorization
	Outbox       events.Outbox
	Tenant       TenantRunner
	Logger       *slog.Logger
	Now          func() time.Time
}

// Service implements deployment domain logic.
type Service struct {
	apps       ApplicationStore
	deps       DeploymentStore
	rels       ReleaseStore
	orgMembers authz.OrgMemberStore
	outbox     events.Outbox
	tenant     TenantRunner
	authSvc    *authz.AuthorizationService
	log        *slog.Logger
	now        func() time.Time
}

// NewService creates a new deployment service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{
		apps:       d.Applications,
		deps:       d.Deployments,
		rels:       d.Releases,
		orgMembers: d.OrgMembers,
		outbox:     d.Outbox,
		tenant:     d.Tenant,
		authSvc:    authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		log:        d.Logger,
		now:        d.Now,
	}
}

// ----------------------------------------------------------------------------
// Applications
// ----------------------------------------------------------------------------

// CreateApplication creates a new application within a project.
// SECURITY: Requires org membership with deployment write privileges.
func (s *Service) CreateApplication(ctx context.Context, orgID, projectID, userID string, req CreateApplicationRequest) (*Application, error) {
	// SECURITY: Verify caller has deployment write privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	if err := validateName(name); err != nil {
		return nil, err
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if !slugPattern.MatchString(slug) {
		return nil, apperrors.Validation("slug must be 2-64 chars of lowercase letters, digits, or hyphens")
	}

	runtimeType := req.RuntimeType
	if runtimeType == "" {
		runtimeType = RuntimeContainer
	}
	if runtimeType != RuntimeContainer && runtimeType != RuntimeFunction && runtimeType != RuntimeJob {
		return nil, apperrors.Validation("runtime type must be container, function, or job")
	}

	app := &Application{
		ProjectID:   projectID,
		Name:        name,
		Slug:        slug,
		Description: req.Description,
		RuntimeType: runtimeType,
	}
	app.OrgID = orgID
	app.CreatedBy = &userID

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.apps.Create(ctx, app); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errSlugTaken
			}
			return err
		}
		return s.enqueue(ctx, EventApplicationCreated, orgID, applicationCreatedPayload{
			ApplicationID: app.ID,
			ProjectID:     projectID,
			Name:          app.Name,
			Slug:          app.Slug,
			RuntimeType:   app.RuntimeType,
			CreatedBy:     userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "application", ID: app.ID}))
	})
	if err != nil {
		return nil, err
	}
	return app, nil
}

// GetApplication returns an application by ID.
// SECURITY: Requires org membership to read applications.
func (s *Service) GetApplication(ctx context.Context, orgID, userID, appID string) (*Application, error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var app *Application
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		app, err = s.apps.GetByID(ctx, appID)
		return err
	})
	return app, err
}

// ListApplications returns applications for a project.
// SECURITY: Requires org membership to list applications.
func (s *Service) ListApplications(ctx context.Context, orgID, userID, projectID string, page database.PageRequest) (database.Page[Application], error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Application]{}, err
	}

	var out database.Page[Application]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.apps.List(ctx, projectID, page)
		return err
	})
	return out, err
}

// UpdateApplication updates an application.
// SECURITY: Requires org membership with deployment write privileges.
func (s *Service) UpdateApplication(ctx context.Context, orgID, userID, appID string, req UpdateApplicationRequest) (*Application, error) {
	// SECURITY: Verify caller has deployment write privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var app *Application
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		app, err = s.apps.GetByID(ctx, appID)
		if err != nil {
			return err
		}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if err := validateName(name); err != nil {
				return err
			}
			app.Name = name
		}
		if req.Description != nil {
			app.Description = req.Description
		}
		if err := s.apps.Update(ctx, app); err != nil {
			return err
		}
		return s.enqueue(ctx, EventApplicationUpdated, orgID, applicationUpdatedPayload{
			ApplicationID: app.ID,
			Name:          app.Name,
			UpdatedBy:     userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "application", ID: app.ID}))
	})
	return app, err
}

// DeleteApplication deletes an application.
// SECURITY: Requires org membership with deployment write privileges.
func (s *Service) DeleteApplication(ctx context.Context, orgID, userID, appID string) error {
	// SECURITY: Verify caller has deployment write privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.apps.Delete(ctx, appID); err != nil {
			return err
		}
		return s.enqueue(ctx, EventApplicationDeleted, orgID, applicationDeletedPayload{
			ApplicationID: appID,
			DeletedBy:     userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "application", ID: appID}))
	})
}

// ----------------------------------------------------------------------------
// Deployments
// ----------------------------------------------------------------------------

// CreateDeployment creates a new deployment and initial release.
// SECURITY: Requires org membership with deployment write privileges.
func (s *Service) CreateDeployment(ctx context.Context, orgID, userID string, req CreateDeploymentRequest) (*Deployment, *Release, error) {
	// SECURITY: Verify caller has deployment write privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, nil, err
	}

	if strings.TrimSpace(req.Image) == "" {
		return nil, nil, errInvalidImage
	}
	if req.Replicas < 1 {
		return nil, nil, errInvalidReplicas
	}

	envVars, _ := json.Marshal(req.EnvVars)
	if len(req.EnvVars) == 0 {
		envVars = []byte("[]")
	}

	dep := &Deployment{
		ApplicationID: req.ApplicationID,
		ClusterID:     req.ClusterID,
		Image:         req.Image,
		Replicas:      req.Replicas,
		CPURequest:    req.CPURequest,
		CPULimit:      req.CPULimit,
		MemoryRequest: req.MemoryRequest,
		MemoryLimit:   req.MemoryLimit,
		Port:          req.Port,
		EnvVars:       envVars,
		Status:        StatusPending,
	}
	dep.OrgID = orgID
	dep.CreatedBy = &userID

	var rel *Release

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Verify application exists.
		_, err := s.apps.GetByID(ctx, req.ApplicationID)
		if err != nil {
			if database.IsNotFound(err) {
				return errAppNotFound
			}
			return err
		}

		// Create deployment.
		if err := s.deps.Create(ctx, dep); err != nil {
			return err
		}

		// Create initial release (revision 1).
		config, _ := json.Marshal(map[string]any{
			"image":         dep.Image,
			"replicas":      dep.Replicas,
			"cpuRequest":    dep.CPURequest,
			"cpuLimit":      dep.CPULimit,
			"memoryRequest": dep.MemoryRequest,
			"memoryLimit":   dep.MemoryLimit,
			"port":          dep.Port,
			"envVars":       req.EnvVars,
		})

		rel = &Release{
			OrgID:        orgID,
			DeploymentID: dep.ID,
			Revision:     1,
			Image:        dep.Image,
			Replicas:     dep.Replicas,
			ConfigHash:   hashConfig(config),
			Config:       config,
			Status:       ReleaseStatusPending,
		}
		rel.CreatedBy = &userID

		if err := s.rels.Create(ctx, rel); err != nil {
			return err
		}

		return s.enqueue(ctx, EventDeploymentCreated, orgID, deploymentCreatedPayload{
			DeploymentID:  dep.ID,
			ApplicationID: dep.ApplicationID,
			ClusterID:     dep.ClusterID,
			Image:         dep.Image,
			Replicas:      dep.Replicas,
			Revision:      1,
			CreatedBy:     userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "deployment", ID: dep.ID}))
	})

	if err != nil {
		return nil, nil, err
	}
	return dep, rel, nil
}

// GetDeployment returns a deployment by ID.
// SECURITY: Requires org membership to read deployments.
func (s *Service) GetDeployment(ctx context.Context, orgID, userID, depID string) (*Deployment, *int, error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, nil, err
	}

	var dep *Deployment
	var currentRev *int
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		dep, err = s.deps.GetByID(ctx, depID)
		if err != nil {
			return err
		}
		// Get latest revision.
		rel, err := s.rels.GetLatest(ctx, depID)
		if err == nil {
			currentRev = &rel.Revision
		}
		return nil
	})
	return dep, currentRev, err
}

// ListDeployments returns deployments for an application.
// SECURITY: Requires org membership to list deployments.
func (s *Service) ListDeployments(ctx context.Context, orgID, userID, appID string, page database.PageRequest) (database.Page[Deployment], error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Deployment]{}, err
	}

	var out database.Page[Deployment]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.deps.List(ctx, appID, page)
		return err
	})
	return out, err
}

// ListDeploymentsByCluster returns deployments for a cluster (for agent pull).
// SECURITY: Requires org membership to list deployments.
func (s *Service) ListDeploymentsByCluster(ctx context.Context, orgID, userID, clusterID string, page database.PageRequest) (database.Page[Deployment], error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Deployment]{}, err
	}

	var out database.Page[Deployment]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.deps.ListByCluster(ctx, clusterID, page)
		return err
	})
	return out, err
}

// ListOrgDeployments returns all deployments in an organization.
// SECURITY: Requires org membership to list deployments.
func (s *Service) ListOrgDeployments(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[Deployment], error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Deployment]{}, err
	}

	var out database.Page[Deployment]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.deps.ListByOrg(ctx, page)
		return err
	})
	return out, err
}

// DeleteDeployment soft-deletes a deployment.
// SECURITY: Requires org membership with deployment write privileges.
func (s *Service) DeleteDeployment(ctx context.Context, orgID, userID, depID string) error {
	// SECURITY: Verify caller has deployment write privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		dep, err := s.deps.GetByID(ctx, depID)
		if err != nil {
			return err
		}

		if err := s.deps.SoftDelete(ctx, depID); err != nil {
			return err
		}

		return s.enqueue(ctx, EventDeploymentDeleted, orgID, deploymentDeletedPayload{
			DeploymentID:  depID,
			ApplicationID: dep.ApplicationID,
			DeletedBy:     userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "deployment", ID: depID}))
	})
}

// UpdateDeployment updates a deployment and creates a new release.
// SECURITY: Requires org membership with deployment write privileges.
func (s *Service) UpdateDeployment(ctx context.Context, orgID, userID, depID string, req UpdateDeploymentRequest) (*Deployment, *Release, error) {
	// SECURITY: Verify caller has deployment write privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, nil, err
	}

	var dep *Deployment
	var rel *Release

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		dep, err = s.deps.GetByID(ctx, depID)
		if err != nil {
			return err
		}

		// Apply updates.
		if req.Image != nil {
			dep.Image = *req.Image
		}
		if req.Replicas != nil {
			if *req.Replicas < 1 {
				return errInvalidReplicas
			}
			dep.Replicas = *req.Replicas
		}
		if req.CPURequest != nil {
			dep.CPURequest = req.CPURequest
		}
		if req.CPULimit != nil {
			dep.CPULimit = req.CPULimit
		}
		if req.MemoryRequest != nil {
			dep.MemoryRequest = req.MemoryRequest
		}
		if req.MemoryLimit != nil {
			dep.MemoryLimit = req.MemoryLimit
		}
		if req.Port != nil {
			dep.Port = req.Port
		}
		if req.EnvVars != nil {
			envVars, _ := json.Marshal(req.EnvVars)
			dep.EnvVars = envVars
		}

		if err := s.deps.Update(ctx, dep); err != nil {
			return err
		}

		// Create new release.
		latestRel, _ := s.rels.GetLatest(ctx, depID)
		nextRevision := 1
		if latestRel != nil {
			nextRevision = latestRel.Revision + 1
		}

		config, _ := json.Marshal(map[string]any{
			"image":         dep.Image,
			"replicas":      dep.Replicas,
			"cpuRequest":    dep.CPURequest,
			"cpuLimit":      dep.CPULimit,
			"memoryRequest": dep.MemoryRequest,
			"memoryLimit":   dep.MemoryLimit,
			"port":          dep.Port,
			"envVars":       json.RawMessage(dep.EnvVars),
		})

		rel = &Release{
			OrgID:        orgID,
			DeploymentID: depID,
			Revision:     nextRevision,
			Image:        dep.Image,
			Replicas:     dep.Replicas,
			ConfigHash:   hashConfig(config),
			Config:       config,
			Status:       ReleaseStatusPending,
		}
		rel.CreatedBy = &userID

		if err := s.rels.Create(ctx, rel); err != nil {
			return err
		}

		return s.enqueue(ctx, EventDeploymentCreated, orgID, deploymentCreatedPayload{
			DeploymentID:  dep.ID,
			ApplicationID: dep.ApplicationID,
			ClusterID:     dep.ClusterID,
			Image:         dep.Image,
			Replicas:      dep.Replicas,
			Revision:      nextRevision,
			CreatedBy:     userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "deployment", ID: dep.ID}))
	})

	return dep, rel, err
}

// Rollback rolls back to a previous successful release.
// SECURITY: Requires org membership with deployment write privileges.
func (s *Service) Rollback(ctx context.Context, orgID, userID, depID string, req RollbackRequest) (*Deployment, *Release, error) {
	// SECURITY: Verify caller has deployment write privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, nil, err
	}

	var dep *Deployment
	var newRel *Release

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		dep, err = s.deps.GetByID(ctx, depID)
		if err != nil {
			return err
		}

		latestRel, err := s.rels.GetLatest(ctx, depID)
		if err != nil {
			return err
		}

		var targetRel *Release
		if req.TargetRevision != nil {
			targetRel, err = s.rels.GetByRevision(ctx, depID, *req.TargetRevision)
			if err != nil {
				return errReleaseNotFound
			}
		} else {
			targetRel, err = s.rels.GetPreviousSuccessful(ctx, depID, latestRel.Revision)
			if err != nil {
				return errNoRollbackTarget
			}
		}

		// Apply target config to deployment.
		var config map[string]any
		if err := json.Unmarshal(targetRel.Config, &config); err != nil {
			return err
		}

		dep.Image = targetRel.Image
		dep.Replicas = targetRel.Replicas
		dep.Status = StatusPending

		if err := s.deps.Update(ctx, dep); err != nil {
			return err
		}

		// Create new release from target.
		newRel = &Release{
			OrgID:        orgID,
			DeploymentID: depID,
			Revision:     latestRel.Revision + 1,
			Image:        targetRel.Image,
			Replicas:     targetRel.Replicas,
			ConfigHash:   targetRel.ConfigHash,
			Config:       targetRel.Config,
			Status:       ReleaseStatusPending,
		}
		newRel.CreatedBy = &userID

		if err := s.rels.Create(ctx, newRel); err != nil {
			return err
		}

		return s.enqueue(ctx, EventDeploymentRollback, orgID, deploymentRollbackPayload{
			DeploymentID:   depID,
			FromRevision:   latestRel.Revision,
			TargetRevision: targetRel.Revision,
			RequestedBy:    userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "deployment", ID: depID}))
	})

	return dep, newRel, err
}

// ----------------------------------------------------------------------------
// Status Updates (called by users - requires authorization)
// ----------------------------------------------------------------------------

// MarkDeploymentStarted marks a release as started.
// SECURITY: Requires org membership with deployment privileges.
func (s *Service) MarkDeploymentStarted(ctx context.Context, orgID, userID, depID, releaseID string) error {
	// SECURITY: Verify caller has deployment privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		rel, err := s.rels.GetByID(ctx, releaseID)
		if err != nil {
			return err
		}

		if err := s.rels.MarkStarted(ctx, releaseID); err != nil {
			return err
		}

		if err := s.deps.UpdateStatus(ctx, depID, StatusRunning, nil, nil); err != nil {
			return err
		}

		return s.enqueue(ctx, EventDeploymentStarted, orgID, deploymentStartedPayload{
			DeploymentID: depID,
			ReleaseID:    releaseID,
			Revision:     rel.Revision,
			Image:        rel.Image,
		}, events.WithResource(events.Resource{Type: "deployment", ID: depID}))
	})
}

// MarkDeploymentSucceeded marks a release as succeeded.
// SECURITY: Requires org membership with deployment privileges.
func (s *Service) MarkDeploymentSucceeded(ctx context.Context, orgID, userID, depID, releaseID string, readyReplicas int) error {
	// SECURITY: Verify caller has deployment privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		rel, err := s.rels.GetByID(ctx, releaseID)
		if err != nil {
			return err
		}

		if err := s.rels.MarkFinished(ctx, releaseID, ReleaseStatusSucceeded, nil); err != nil {
			return err
		}

		if err := s.deps.UpdateStatus(ctx, depID, StatusSucceeded, &readyReplicas, nil); err != nil {
			return err
		}

		return s.enqueue(ctx, EventDeploymentSucceeded, orgID, deploymentSucceededPayload{
			DeploymentID:  depID,
			ReleaseID:     releaseID,
			Revision:      rel.Revision,
			ReadyReplicas: readyReplicas,
		}, events.WithResource(events.Resource{Type: "deployment", ID: depID}))
	})
}

// MarkDeploymentFailed marks a release as failed.
// SECURITY: Requires org membership with deployment privileges.
func (s *Service) MarkDeploymentFailed(ctx context.Context, orgID, userID, depID, releaseID string, errorMsg string) error {
	// SECURITY: Verify caller has deployment privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		rel, err := s.rels.GetByID(ctx, releaseID)
		if err != nil {
			return err
		}

		if err := s.rels.MarkFinished(ctx, releaseID, ReleaseStatusFailed, &errorMsg); err != nil {
			return err
		}

		if err := s.deps.UpdateStatus(ctx, depID, StatusFailed, nil, &errorMsg); err != nil {
			return err
		}

		return s.enqueue(ctx, EventDeploymentFailed, orgID, deploymentFailedPayload{
			DeploymentID: depID,
			ReleaseID:    releaseID,
			Revision:     rel.Revision,
			ErrorMessage: errorMsg,
		}, events.WithResource(events.Resource{Type: "deployment", ID: depID}))
	})
}

// ----------------------------------------------------------------------------
// Releases
// ----------------------------------------------------------------------------

// ListReleases returns releases for a deployment.
// ListReleases returns releases for a deployment.
// SECURITY: Requires org membership to list releases.
func (s *Service) ListReleases(ctx context.Context, orgID, userID, depID string, page database.PageRequest) (database.Page[Release], error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Release]{}, err
	}

	var out database.Page[Release]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.rels.List(ctx, depID, page)
		return err
	})
	return out, err
}

// GetRelease returns a release by ID.
// SECURITY: Requires org membership to read releases.
func (s *Service) GetRelease(ctx context.Context, orgID, userID, releaseID string) (*Release, error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var rel *Release
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		rel, err = s.rels.GetByID(ctx, releaseID)
		return err
	})
	return rel, err
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func validateName(name string) error {
	if l := len(name); l < 2 || l > 64 {
		return apperrors.Validation("name must be between 2 and 64 characters")
	}
	return nil
}

func hashConfig(config []byte) string {
	h := sha256.Sum256(config)
	return hex.EncodeToString(h[:8])
}
