package pipeline

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// DesiredStateStore persists desired states.
type DesiredStateStore interface {
	Create(ctx context.Context, ds *DesiredState) error
	GetByID(ctx context.Context, id string) (*DesiredState, error)
	GetByDeploymentID(ctx context.Context, deploymentID string) (*DesiredState, error)
	ListByCluster(ctx context.Context, clusterID string, req database.PageRequest) (database.Page[DesiredState], error)
	ListPendingByCluster(ctx context.Context, clusterID string) ([]DesiredState, error)
	Update(ctx context.Context, ds *DesiredState) error
	UpdateSyncStatus(ctx context.Context, id, status string, observedGen int64, errMsg *string) error
	Delete(ctx context.Context, id string) error
}

// PipelineRunStore persists pipeline runs.
type PipelineRunStore interface {
	Create(ctx context.Context, pr *PipelineRun) error
	GetByID(ctx context.Context, id string) (*PipelineRun, error)
	GetLatestByDeployment(ctx context.Context, deploymentID string) (*PipelineRun, error)
	List(ctx context.Context, deploymentID string, req database.PageRequest) (database.Page[PipelineRun], error)
	ListActive(ctx context.Context) ([]PipelineRun, error)
	Update(ctx context.Context, pr *PipelineRun) error
	UpdateStatus(ctx context.Context, id, status, stage string, errMsg, errStage *string) error
	UpdateBuild(ctx context.Context, id string, buildStatus, builtImage *string) error
	SetReleaseID(ctx context.Context, id, releaseID string) error
}

// PipelineEventStore persists pipeline events.
type PipelineEventStore interface {
	Create(ctx context.Context, pe *PipelineEvent) error
	List(ctx context.Context, pipelineRunID string, req database.PageRequest) (database.Page[PipelineEvent], error)
}

// DeploymentMetricsStore persists deployment metrics.
type DeploymentMetricsStore interface {
	Upsert(ctx context.Context, dm *DeploymentMetrics) error
	GetByDeploymentID(ctx context.Context, deploymentID string) (*DeploymentMetrics, error)
	List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[DeploymentMetrics], error)
}

// ----------------------------------------------------------------------------
// DesiredState Repository
// ----------------------------------------------------------------------------

type desiredStateRepo struct{ db *database.DB }

func NewDesiredStateStore(db *database.DB) DesiredStateStore { return &desiredStateRepo{db: db} }

func (r *desiredStateRepo) Create(ctx context.Context, ds *DesiredState) error {
	if ds.ID == "" {
		ds.ID = uuid.NewString()
	}
	if ds.SyncStatus == "" {
		ds.SyncStatus = SyncStatusPending
	}
	if ds.Generation == 0 {
		ds.Generation = 1
	}

	const sql = `
INSERT INTO desired_states (id, org_id, deployment_id, release_id, cluster_id, 
    namespace, manifests, manifest_hash, sync_status, generation)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (deployment_id) DO UPDATE SET
    release_id = EXCLUDED.release_id,
    namespace = EXCLUDED.namespace,
    manifests = EXCLUDED.manifests,
    manifest_hash = EXCLUDED.manifest_hash,
    sync_status = 'pending',
    generation = desired_states.generation + 1,
    observed_generation = desired_states.observed_generation,
    updated_at = now()
RETURNING id, created_at, updated_at, generation`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		ds.ID, ds.OrgID, ds.DeploymentID, ds.ReleaseID, ds.ClusterID,
		ds.Namespace, ds.Manifests, ds.ManifestHash, ds.SyncStatus, ds.Generation)
	return database.MapError(row.Scan(&ds.ID, &ds.CreatedAt, &ds.UpdatedAt, &ds.Generation))
}

func (r *desiredStateRepo) GetByID(ctx context.Context, id string) (*DesiredState, error) {
	ds, err := database.QueryOne[DesiredState](ctx, r.db.Conn(ctx),
		"SELECT * FROM desired_states WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &ds, nil
}

func (r *desiredStateRepo) GetByDeploymentID(ctx context.Context, deploymentID string) (*DesiredState, error) {
	ds, err := database.QueryOne[DesiredState](ctx, r.db.Conn(ctx),
		"SELECT * FROM desired_states WHERE deployment_id = $1", deploymentID)
	if err != nil {
		return nil, err
	}
	return &ds, nil
}

func (r *desiredStateRepo) ListByCluster(ctx context.Context, clusterID string, req database.PageRequest) (database.Page[DesiredState], error) {
	req = req.Normalize()
	items, err := database.QueryAll[DesiredState](ctx, r.db.Conn(ctx),
		"SELECT * FROM desired_states WHERE cluster_id = $1 ORDER BY updated_at DESC LIMIT $2",
		clusterID, req.Limit+1)
	if err != nil {
		return database.Page[DesiredState]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[DesiredState]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[DesiredState]{Items: items}, nil
}

func (r *desiredStateRepo) ListPendingByCluster(ctx context.Context, clusterID string) ([]DesiredState, error) {
	return database.QueryAll[DesiredState](ctx, r.db.Conn(ctx),
		`SELECT * FROM desired_states 
		 WHERE cluster_id = $1 AND (sync_status = 'pending' OR generation > observed_generation)
		 ORDER BY updated_at ASC`, clusterID)
}

func (r *desiredStateRepo) Update(ctx context.Context, ds *DesiredState) error {
	const sql = `
UPDATE desired_states
SET release_id = $1, namespace = $2, manifests = $3, manifest_hash = $4, 
    sync_status = 'pending', generation = generation + 1, updated_at = now()
WHERE id = $5`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		ds.ReleaseID, ds.Namespace, ds.Manifests, ds.ManifestHash, ds.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("desired state not found")
	}
	ds.Generation++
	ds.SyncStatus = SyncStatusPending
	return nil
}

func (r *desiredStateRepo) UpdateSyncStatus(ctx context.Context, id, status string, observedGen int64, errMsg *string) error {
	const sql = `
UPDATE desired_states
SET sync_status = $1, observed_generation = $2, last_synced_at = now(), last_sync_error = $3, updated_at = now()
WHERE id = $4`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, observedGen, errMsg, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("desired state not found")
	}
	return nil
}

func (r *desiredStateRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM desired_states WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("desired state not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// PipelineRun Repository
// ----------------------------------------------------------------------------

type pipelineRunRepo struct{ db *database.DB }

func NewPipelineRunStore(db *database.DB) PipelineRunStore { return &pipelineRunRepo{db: db} }

func (r *pipelineRunRepo) Create(ctx context.Context, pr *PipelineRun) error {
	if pr.ID == "" {
		pr.ID = uuid.NewString()
	}
	if pr.Status == "" {
		pr.Status = StatusPending
	}
	if pr.CurrentStage == "" {
		pr.CurrentStage = StageInit
	}
	if pr.SourceType == "" {
		pr.SourceType = SourceTypeImage
	}
	if pr.TriggeredBy == "" {
		pr.TriggeredBy = TriggerUser
	}

	const sql = `
INSERT INTO pipeline_runs (id, org_id, deployment_id, release_id, source_type, source_ref, 
    build_id, build_status, built_image, status, current_stage, triggered_by, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		pr.ID, pr.OrgID, pr.DeploymentID, pr.ReleaseID, pr.SourceType, pr.SourceRef,
		pr.BuildID, pr.BuildStatus, pr.BuiltImage, pr.Status, pr.CurrentStage,
		pr.TriggeredBy, pr.CreatedBy)
	return database.MapError(row.Scan(&pr.CreatedAt, &pr.UpdatedAt))
}

func (r *pipelineRunRepo) GetByID(ctx context.Context, id string) (*PipelineRun, error) {
	pr, err := database.QueryOne[PipelineRun](ctx, r.db.Conn(ctx),
		"SELECT * FROM pipeline_runs WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &pr, nil
}

func (r *pipelineRunRepo) GetLatestByDeployment(ctx context.Context, deploymentID string) (*PipelineRun, error) {
	pr, err := database.QueryOne[PipelineRun](ctx, r.db.Conn(ctx),
		"SELECT * FROM pipeline_runs WHERE deployment_id = $1 ORDER BY created_at DESC LIMIT 1",
		deploymentID)
	if err != nil {
		return nil, err
	}
	return &pr, nil
}

func (r *pipelineRunRepo) List(ctx context.Context, deploymentID string, req database.PageRequest) (database.Page[PipelineRun], error) {
	req = req.Normalize()
	items, err := database.QueryAll[PipelineRun](ctx, r.db.Conn(ctx),
		"SELECT * FROM pipeline_runs WHERE deployment_id = $1 ORDER BY created_at DESC LIMIT $2",
		deploymentID, req.Limit+1)
	if err != nil {
		return database.Page[PipelineRun]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[PipelineRun]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[PipelineRun]{Items: items}, nil
}

func (r *pipelineRunRepo) ListActive(ctx context.Context) ([]PipelineRun, error) {
	return database.QueryAll[PipelineRun](ctx, r.db.Conn(ctx),
		`SELECT * FROM pipeline_runs WHERE status IN ('pending', 'building', 'deploying') ORDER BY created_at ASC`)
}

func (r *pipelineRunRepo) Update(ctx context.Context, pr *PipelineRun) error {
	const sql = `
UPDATE pipeline_runs
SET release_id = $1, build_id = $2, build_status = $3, built_image = $4,
    status = $5, current_stage = $6, started_at = $7, finished_at = $8,
    error_message = $9, error_stage = $10, updated_at = now()
WHERE id = $11`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		pr.ReleaseID, pr.BuildID, pr.BuildStatus, pr.BuiltImage,
		pr.Status, pr.CurrentStage, pr.StartedAt, pr.FinishedAt,
		pr.ErrorMessage, pr.ErrorStage, pr.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("pipeline run not found")
	}
	return nil
}

func (r *pipelineRunRepo) UpdateStatus(ctx context.Context, id, status, stage string, errMsg, errStage *string) error {
	var sql string
	var args []any

	if status == StatusSucceeded || status == StatusFailed || status == StatusCancelled {
		sql = `UPDATE pipeline_runs SET status = $1, current_stage = $2, error_message = $3, error_stage = $4, 
               finished_at = now(), updated_at = now() WHERE id = $5`
		args = []any{status, stage, errMsg, errStage, id}
	} else if status == StatusBuilding || status == StatusDeploying {
		sql = `UPDATE pipeline_runs SET status = $1, current_stage = $2, 
               started_at = COALESCE(started_at, now()), updated_at = now() WHERE id = $3`
		args = []any{status, stage, id}
	} else {
		sql = `UPDATE pipeline_runs SET status = $1, current_stage = $2, updated_at = now() WHERE id = $3`
		args = []any{status, stage, id}
	}

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("pipeline run not found")
	}
	return nil
}

func (r *pipelineRunRepo) UpdateBuild(ctx context.Context, id string, buildStatus, builtImage *string) error {
	const sql = `UPDATE pipeline_runs SET build_status = $1, built_image = $2, updated_at = now() WHERE id = $3`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, buildStatus, builtImage, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("pipeline run not found")
	}
	return nil
}

func (r *pipelineRunRepo) SetReleaseID(ctx context.Context, id, releaseID string) error {
	const sql = `UPDATE pipeline_runs SET release_id = $1, updated_at = now() WHERE id = $2`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, releaseID, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("pipeline run not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// PipelineEvent Repository
// ----------------------------------------------------------------------------

type pipelineEventRepo struct{ db *database.DB }

func NewPipelineEventStore(db *database.DB) PipelineEventStore { return &pipelineEventRepo{db: db} }

func (r *pipelineEventRepo) Create(ctx context.Context, pe *PipelineEvent) error {
	if pe.ID == "" {
		pe.ID = uuid.NewString()
	}
	if len(pe.Details) == 0 {
		pe.Details = []byte("{}")
	}

	const sql = `
INSERT INTO pipeline_events (id, org_id, pipeline_run_id, event_type, stage, message, details)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING created_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		pe.ID, pe.OrgID, pe.PipelineRunID, pe.EventType, pe.Stage, pe.Message, pe.Details)
	return database.MapError(row.Scan(&pe.CreatedAt))
}

func (r *pipelineEventRepo) List(ctx context.Context, pipelineRunID string, req database.PageRequest) (database.Page[PipelineEvent], error) {
	req = req.Normalize()
	items, err := database.QueryAll[PipelineEvent](ctx, r.db.Conn(ctx),
		"SELECT * FROM pipeline_events WHERE pipeline_run_id = $1 ORDER BY created_at ASC LIMIT $2",
		pipelineRunID, req.Limit+1)
	if err != nil {
		return database.Page[PipelineEvent]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[PipelineEvent]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[PipelineEvent]{Items: items}, nil
}

// ----------------------------------------------------------------------------
// DeploymentMetrics Repository
// ----------------------------------------------------------------------------

type deploymentMetricsRepo struct{ db *database.DB }

func NewDeploymentMetricsStore(db *database.DB) DeploymentMetricsStore {
	return &deploymentMetricsRepo{db: db}
}

func (r *deploymentMetricsRepo) Upsert(ctx context.Context, dm *DeploymentMetrics) error {
	if dm.ID == "" {
		dm.ID = uuid.NewString()
	}
	if dm.HealthStatus == "" {
		dm.HealthStatus = HealthUnknown
	}

	const sql = `
INSERT INTO deployment_metrics (id, org_id, deployment_id, available_replicas, ready_replicas, 
    updated_replicas, cpu_usage_millicores, memory_usage_bytes, health_status, last_health_check, collected_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
ON CONFLICT (deployment_id) DO UPDATE SET
    available_replicas = EXCLUDED.available_replicas,
    ready_replicas = EXCLUDED.ready_replicas,
    updated_replicas = EXCLUDED.updated_replicas,
    cpu_usage_millicores = EXCLUDED.cpu_usage_millicores,
    memory_usage_bytes = EXCLUDED.memory_usage_bytes,
    health_status = EXCLUDED.health_status,
    last_health_check = now(),
    collected_at = now()
RETURNING id, collected_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		dm.ID, dm.OrgID, dm.DeploymentID, dm.AvailableReplicas, dm.ReadyReplicas,
		dm.UpdatedReplicas, dm.CPUUsageMillicores, dm.MemoryUsageBytes, dm.HealthStatus)
	return database.MapError(row.Scan(&dm.ID, &dm.CollectedAt))
}

func (r *deploymentMetricsRepo) GetByDeploymentID(ctx context.Context, deploymentID string) (*DeploymentMetrics, error) {
	dm, err := database.QueryOne[DeploymentMetrics](ctx, r.db.Conn(ctx),
		"SELECT * FROM deployment_metrics WHERE deployment_id = $1", deploymentID)
	if err != nil {
		return nil, err
	}
	return &dm, nil
}

func (r *deploymentMetricsRepo) List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[DeploymentMetrics], error) {
	req = req.Normalize()
	items, err := database.QueryAll[DeploymentMetrics](ctx, r.db.Conn(ctx),
		"SELECT * FROM deployment_metrics WHERE org_id = $1 ORDER BY collected_at DESC LIMIT $2",
		orgID, req.Limit+1)
	if err != nil {
		return database.Page[DeploymentMetrics]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[DeploymentMetrics]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[DeploymentMetrics]{Items: items}, nil
}

// Helper to marshal details to JSON
func marshalDetails(v any) json.RawMessage {
	if v == nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(v)
	return b
}
