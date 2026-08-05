package deployment

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// ArgoApplicationStore persists the GitOps binding (argo_applications) for a
// deployment. All methods are RLS-scoped: callers must run them inside a
// tenant transaction (db.WithTenant).
type ArgoApplicationStore interface {
	// Upsert inserts or updates the binding for a deployment. On conflict it
	// updates the desired source/destination/policy fields but preserves the
	// observed status columns (those are owned by UpdateObserved).
	Upsert(ctx context.Context, a *ArgoApplication) error
	// Get returns the binding for a deployment, or a not-found error.
	Get(ctx context.Context, deploymentID string) (*ArgoApplication, error)
	// GetByAppName returns the binding for an Argo CD application name.
	GetByAppName(ctx context.Context, appName string) (*ArgoApplication, error)
	// UpdateObserved persists the last observed Argo CD status for a deployment.
	UpdateObserved(ctx context.Context, a *ArgoApplication) error
	// Delete removes the binding for a deployment.
	Delete(ctx context.Context, deploymentID string) error
}

type argoAppRepo struct{ db *database.DB }

// NewArgoApplicationStore returns a Postgres-backed ArgoApplicationStore.
func NewArgoApplicationStore(db *database.DB) ArgoApplicationStore {
	return &argoAppRepo{db: db}
}

func (r *argoAppRepo) Upsert(ctx context.Context, a *ArgoApplication) error {
	if a.Project == "" {
		a.Project = "default"
	}
	if a.Path == "" {
		a.Path = "."
	}
	if a.TargetRevision == "" {
		a.TargetRevision = "HEAD"
	}
	if a.SourceType == "" {
		a.SourceType = SourceTypeDirectory
	}
	if a.DestServer == "" {
		a.DestServer = "https://kubernetes.default.svc"
	}

	const sql = `
INSERT INTO argo_applications (
    deployment_id, org_id, app_name, project, repo_url, path, target_revision, source_type,
    dest_server, dest_namespace, auto_sync, self_heal, prune)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (deployment_id) DO UPDATE SET
    app_name = EXCLUDED.app_name,
    project = EXCLUDED.project,
    repo_url = EXCLUDED.repo_url,
    path = EXCLUDED.path,
    target_revision = EXCLUDED.target_revision,
    source_type = EXCLUDED.source_type,
    dest_server = EXCLUDED.dest_server,
    dest_namespace = EXCLUDED.dest_namespace,
    auto_sync = EXCLUDED.auto_sync,
    self_heal = EXCLUDED.self_heal,
    prune = EXCLUDED.prune,
    updated_at = now()
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		a.DeploymentID, a.OrgID, a.AppName, a.Project, a.RepoURL, a.Path, a.TargetRevision, a.SourceType,
		a.DestServer, a.DestNamespace, a.AutoSync, a.SelfHeal, a.Prune)
	return database.MapError(row.Scan(&a.CreatedAt, &a.UpdatedAt))
}

func (r *argoAppRepo) Get(ctx context.Context, deploymentID string) (*ArgoApplication, error) {
	a, err := database.QueryOne[ArgoApplication](ctx, r.db.Conn(ctx),
		"SELECT * FROM argo_applications WHERE deployment_id = $1", deploymentID)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *argoAppRepo) GetByAppName(ctx context.Context, appName string) (*ArgoApplication, error) {
	a, err := database.QueryOne[ArgoApplication](ctx, r.db.Conn(ctx),
		"SELECT * FROM argo_applications WHERE app_name = $1", appName)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *argoAppRepo) UpdateObserved(ctx context.Context, a *ArgoApplication) error {
	const sql = `
UPDATE argo_applications SET
    sync_status = $2,
    health_status = $3,
    operation_phase = $4,
    synced_revision = $5,
    drift = $6,
    observed_at = now()
WHERE deployment_id = $1
RETURNING observed_at, updated_at`
	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		a.DeploymentID, a.SyncStatus, a.HealthStatus, a.OperationPhase, a.SyncedRevision, a.Drift)
	return database.MapError(row.Scan(&a.ObservedAt, &a.UpdatedAt))
}

func (r *argoAppRepo) Delete(ctx context.Context, deploymentID string) error {
	_, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM argo_applications WHERE deployment_id = $1", deploymentID)
	return database.MapError(err)
}
