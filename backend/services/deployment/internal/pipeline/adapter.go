package pipeline

import (
	"context"
	"encoding/json"
)

// DeploymentServiceAdapter adapts the main deployment service for pipeline use.
type DeploymentServiceAdapter struct {
	deployments DeploymentStore
	apps        ApplicationStore
	releases    ReleaseStore
	orgID       string
}

// DeploymentStore is the interface for deployment storage.
type DeploymentStore interface {
	GetByID(ctx context.Context, id string) (*DeploymentRecord, error)
}

// ApplicationStore is the interface for application storage.
type ApplicationStore interface {
	GetByID(ctx context.Context, id string) (*ApplicationRecord, error)
}

// ReleaseStore is the interface for release storage.
type ReleaseStore interface {
	Create(ctx context.Context, r *ReleaseRecord) error
	GetLatest(ctx context.Context, deploymentID string) (*ReleaseRecord, error)
	MarkStarted(ctx context.Context, id string) error
	MarkFinished(ctx context.Context, id, status string, errorMsg *string) error
}

// DeploymentRecord represents a deployment from storage.
type DeploymentRecord struct {
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

// ApplicationRecord represents an application from storage.
type ApplicationRecord struct {
	ID        string
	OrgID     string
	ProjectID string
	Name      string
	Slug      string
}

// ReleaseRecord represents a release for storage.
type ReleaseRecord struct {
	ID           string
	OrgID        string
	DeploymentID string
	Revision     int
	Image        string
	Replicas     int
	ConfigHash   string
	Config       json.RawMessage
	Status       string
	CreatedBy    *string
}

// NewDeploymentServiceAdapter creates a new adapter.
func NewDeploymentServiceAdapter(deployments DeploymentStore, apps ApplicationStore, releases ReleaseStore) *DeploymentServiceAdapter {
	return &DeploymentServiceAdapter{
		deployments: deployments,
		apps:        apps,
		releases:    releases,
	}
}

// GetDeployment implements DeploymentReader.
func (a *DeploymentServiceAdapter) GetDeployment(ctx context.Context, id string) (*DeploymentInfo, error) {
	d, err := a.deployments.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &DeploymentInfo{
		ID:            d.ID,
		OrgID:         d.OrgID,
		ApplicationID: d.ApplicationID,
		ClusterID:     d.ClusterID,
		Image:         d.Image,
		Replicas:      d.Replicas,
		Port:          d.Port,
		CPURequest:    d.CPURequest,
		CPULimit:      d.CPULimit,
		MemoryRequest: d.MemoryRequest,
		MemoryLimit:   d.MemoryLimit,
		EnvVars:       d.EnvVars,
	}, nil
}

// GetApplication implements DeploymentReader.
func (a *DeploymentServiceAdapter) GetApplication(ctx context.Context, id string) (*ApplicationInfo, error) {
	app, err := a.apps.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ApplicationInfo{
		ID:        app.ID,
		OrgID:     app.OrgID,
		ProjectID: app.ProjectID,
		Name:      app.Name,
		Slug:      app.Slug,
	}, nil
}

// CreateRelease implements ReleaseCreator.
func (a *DeploymentServiceAdapter) CreateRelease(ctx context.Context, deploymentID, image string, replicas int, config json.RawMessage, userID string) (string, int, error) {
	// Get next revision
	latest, _ := a.releases.GetLatest(ctx, deploymentID)
	nextRevision := 1
	if latest != nil {
		nextRevision = latest.Revision + 1
	}

	// Get deployment for org_id
	dep, err := a.deployments.GetByID(ctx, deploymentID)
	if err != nil {
		return "", 0, err
	}

	configHash := hashConfigBytes(config)

	r := &ReleaseRecord{
		OrgID:        dep.OrgID,
		DeploymentID: deploymentID,
		Revision:     nextRevision,
		Image:        image,
		Replicas:     replicas,
		ConfigHash:   configHash,
		Config:       config,
		Status:       "pending",
		CreatedBy:    &userID,
	}

	if err := a.releases.Create(ctx, r); err != nil {
		return "", 0, err
	}

	return r.ID, r.Revision, nil
}

// MarkReleaseDeploying implements ReleaseCreator.
func (a *DeploymentServiceAdapter) MarkReleaseDeploying(ctx context.Context, releaseID string) error {
	return a.releases.MarkStarted(ctx, releaseID)
}

// MarkReleaseSucceeded implements ReleaseCreator.
func (a *DeploymentServiceAdapter) MarkReleaseSucceeded(ctx context.Context, releaseID string) error {
	return a.releases.MarkFinished(ctx, releaseID, "succeeded", nil)
}

// MarkReleaseFailed implements ReleaseCreator.
func (a *DeploymentServiceAdapter) MarkReleaseFailed(ctx context.Context, releaseID string, errMsg string) error {
	return a.releases.MarkFinished(ctx, releaseID, "failed", &errMsg)
}

// Helper
func hashConfigBytes(config json.RawMessage) string {
	if len(config) == 0 {
		return ""
	}
	// Simple hash for now
	h := uint64(0)
	for _, b := range config {
		h = h*31 + uint64(b)
	}
	return string(rune(h % 1000000))
}
