package deployment

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// TenantRunner runs a function within a tenant-scoped transaction.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// ApplicationStore persists applications.
type ApplicationStore interface {
	Create(ctx context.Context, a *Application) error
	GetByID(ctx context.Context, id string) (*Application, error)
	GetBySlug(ctx context.Context, projectID, slug string) (*Application, error)
	List(ctx context.Context, projectID string, req database.PageRequest) (database.Page[Application], error)
	Update(ctx context.Context, a *Application) error
	Delete(ctx context.Context, id string) error
}

// DeploymentStore persists deployments.
type DeploymentStore interface {
	Create(ctx context.Context, d *Deployment) error
	GetByID(ctx context.Context, id string) (*Deployment, error)
	List(ctx context.Context, appID string, req database.PageRequest) (database.Page[Deployment], error)
	ListByOrg(ctx context.Context, req database.PageRequest) (database.Page[Deployment], error)
	// ListAllActive returns every non-deleted deployment visible in the current
	// tenant context (RLS-scoped). Used by the build consumer to match built
	// images to the deployments that run them.
	ListAllActive(ctx context.Context) ([]Deployment, error)
	ListByCluster(ctx context.Context, clusterID string, req database.PageRequest) (database.Page[Deployment], error)
	Update(ctx context.Context, d *Deployment) error
	UpdateStatus(ctx context.Context, id, status string, readyReplicas *int, errorMsg *string) error
	Delete(ctx context.Context, id string) error
	SoftDelete(ctx context.Context, id string) error
}

// ReleaseStore persists releases.
type ReleaseStore interface {
	Create(ctx context.Context, r *Release) error
	GetByID(ctx context.Context, id string) (*Release, error)
	GetByRevision(ctx context.Context, deploymentID string, revision int) (*Release, error)
	GetLatest(ctx context.Context, deploymentID string) (*Release, error)
	GetPreviousSuccessful(ctx context.Context, deploymentID string, beforeRevision int) (*Release, error)
	List(ctx context.Context, deploymentID string, req database.PageRequest) (database.Page[Release], error)
	UpdateStatus(ctx context.Context, id, status string, errorMsg *string) error
	MarkStarted(ctx context.Context, id string) error
	MarkFinished(ctx context.Context, id, status string, errorMsg *string) error
}

// ----------------------------------------------------------------------------
// Application Repository
// ----------------------------------------------------------------------------

type applicationRepo struct{ db *database.DB }

func NewApplicationStore(db *database.DB) ApplicationStore { return &applicationRepo{db: db} }

func (r *applicationRepo) Create(ctx context.Context, a *Application) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	if a.RuntimeType == "" {
		a.RuntimeType = RuntimeContainer
	}

	const sql = `
INSERT INTO applications (id, org_id, project_id, name, slug, description, runtime_type, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING created_at, updated_at, version`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		a.ID, a.OrgID, a.ProjectID, a.Name, a.Slug, a.Description, a.RuntimeType, a.CreatedBy)
	return database.MapError(row.Scan(&a.CreatedAt, &a.UpdatedAt, &a.Version))
}

func (r *applicationRepo) GetByID(ctx context.Context, id string) (*Application, error) {
	a, err := database.QueryOne[Application](ctx, r.db.Conn(ctx),
		"SELECT * FROM applications WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *applicationRepo) GetBySlug(ctx context.Context, projectID, slug string) (*Application, error) {
	a, err := database.QueryOne[Application](ctx, r.db.Conn(ctx),
		"SELECT * FROM applications WHERE project_id = $1 AND slug = $2", projectID, slug)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *applicationRepo) List(ctx context.Context, projectID string, req database.PageRequest) (database.Page[Application], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Application]{}, err
	}

	var sql string
	var args []any

	if cur.IsZero() {
		sql = "SELECT * FROM applications WHERE project_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2"
		args = []any{projectID, req.Limit + 1}
	} else {
		sql = "SELECT * FROM applications WHERE project_id = $1 AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4"
		args = []any{projectID, cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	items, err := database.QueryAll[Application](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Application]{}, err
	}
	return database.BuildPage(items, req.Limit, func(a Application) database.Cursor { return a.Cursor() }), nil
}

func (r *applicationRepo) Update(ctx context.Context, a *Application) error {
	const sql = `
UPDATE applications
SET name = $1, description = $2, version = version + 1, updated_at = now()
WHERE id = $3 AND version = $4`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, a.Name, a.Description, a.ID, a.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	a.Version++
	return nil
}

func (r *applicationRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM applications WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("application not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Deployment Repository
// ----------------------------------------------------------------------------

type deploymentRepo struct{ db *database.DB }

func NewDeploymentStore(db *database.DB) DeploymentStore { return &deploymentRepo{db: db} }

func (r *deploymentRepo) Create(ctx context.Context, d *Deployment) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if d.Status == "" {
		d.Status = StatusPending
	}
	if len(d.EnvVars) == 0 {
		d.EnvVars = []byte("[]")
	}
	d.DesiredReplicas = d.Replicas

	const sql = `
INSERT INTO deployments (id, org_id, application_id, cluster_id, image, replicas, 
    cpu_request, cpu_limit, memory_request, memory_limit, port, env_vars, 
    status, desired_replicas, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING created_at, updated_at, version`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		d.ID, d.OrgID, d.ApplicationID, d.ClusterID, d.Image, d.Replicas,
		d.CPURequest, d.CPULimit, d.MemoryRequest, d.MemoryLimit, d.Port, d.EnvVars,
		d.Status, d.DesiredReplicas, d.CreatedBy)
	return database.MapError(row.Scan(&d.CreatedAt, &d.UpdatedAt, &d.Version))
}

func (r *deploymentRepo) GetByID(ctx context.Context, id string) (*Deployment, error) {
	d, err := database.QueryOne[Deployment](ctx, r.db.Conn(ctx),
		"SELECT * FROM deployments WHERE id = $1 AND status != 'deleted'", id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *deploymentRepo) List(ctx context.Context, appID string, req database.PageRequest) (database.Page[Deployment], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Deployment]{}, err
	}

	var sql string
	var args []any

	if cur.IsZero() {
		sql = "SELECT * FROM deployments WHERE application_id = $1 AND status != 'deleted' ORDER BY created_at DESC, id DESC LIMIT $2"
		args = []any{appID, req.Limit + 1}
	} else {
		sql = "SELECT * FROM deployments WHERE application_id = $1 AND status != 'deleted' AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4"
		args = []any{appID, cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	items, err := database.QueryAll[Deployment](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Deployment]{}, err
	}
	return database.BuildPage(items, req.Limit, func(d Deployment) database.Cursor { return d.Cursor() }), nil
}

func (r *deploymentRepo) ListByCluster(ctx context.Context, clusterID string, req database.PageRequest) (database.Page[Deployment], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Deployment]{}, err
	}

	var sql string
	var args []any

	if cur.IsZero() {
		sql = "SELECT * FROM deployments WHERE cluster_id = $1 AND status != 'deleted' ORDER BY created_at DESC, id DESC LIMIT $2"
		args = []any{clusterID, req.Limit + 1}
	} else {
		sql = "SELECT * FROM deployments WHERE cluster_id = $1 AND status != 'deleted' AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4"
		args = []any{clusterID, cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	items, err := database.QueryAll[Deployment](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Deployment]{}, err
	}
	return database.BuildPage(items, req.Limit, func(d Deployment) database.Cursor { return d.Cursor() }), nil
}

func (r *deploymentRepo) Update(ctx context.Context, d *Deployment) error {
	const sql = `
UPDATE deployments
SET image = $1, replicas = $2, cpu_request = $3, cpu_limit = $4, 
    memory_request = $5, memory_limit = $6, port = $7, env_vars = $8,
    desired_replicas = $9, version = version + 1, updated_at = now()
WHERE id = $10 AND version = $11`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		d.Image, d.Replicas, d.CPURequest, d.CPULimit,
		d.MemoryRequest, d.MemoryLimit, d.Port, d.EnvVars,
		d.Replicas, d.ID, d.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	d.Version++
	d.DesiredReplicas = d.Replicas
	return nil
}

func (r *deploymentRepo) UpdateStatus(ctx context.Context, id, status string, readyReplicas *int, errorMsg *string) error {
	var sql string
	var args []any

	if readyReplicas != nil {
		sql = `UPDATE deployments SET status = $1, ready_replicas = $2, version = version + 1, updated_at = now() WHERE id = $3`
		args = []any{status, *readyReplicas, id}
	} else {
		sql = `UPDATE deployments SET status = $1, version = version + 1, updated_at = now() WHERE id = $2`
		args = []any{status, id}
	}

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("deployment not found")
	}
	return nil
}

func (r *deploymentRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM deployments WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("deployment not found")
	}
	return nil
}

// ListByOrg lists all deployments in the org (tenant-scoped via RLS).
func (r *deploymentRepo) ListByOrg(ctx context.Context, req database.PageRequest) (database.Page[Deployment], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Deployment]{}, err
	}

	var sql string
	var args []any

	if cur.IsZero() {
		sql = "SELECT * FROM deployments WHERE status != 'deleted' ORDER BY created_at DESC, id DESC LIMIT $1"
		args = []any{req.Limit + 1}
	} else {
		sql = "SELECT * FROM deployments WHERE status != 'deleted' AND (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3"
		args = []any{cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	items, err := database.QueryAll[Deployment](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Deployment]{}, err
	}
	return database.BuildPage(items, req.Limit, func(d Deployment) database.Cursor { return d.Cursor() }), nil
}

// ListAllActive returns all non-deleted deployments in the tenant context.
// Callers must run this within a tenant-scoped transaction so RLS isolates the
// org. Results are unpaginated; deployments-per-org is bounded in practice.
func (r *deploymentRepo) ListAllActive(ctx context.Context) ([]Deployment, error) {
	items, err := database.QueryAll[Deployment](ctx, r.db.Conn(ctx),
		"SELECT * FROM deployments WHERE status != 'deleted' ORDER BY created_at DESC, id DESC")
	if err != nil {
		return nil, err
	}
	return items, nil
}

// SoftDelete marks a deployment as deleted but keeps the record.
func (r *deploymentRepo) SoftDelete(ctx context.Context, id string) error {
	const sql = `UPDATE deployments SET status = 'deleted', version = version + 1, updated_at = now() WHERE id = $1`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("deployment not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Release Repository
// ----------------------------------------------------------------------------

type releaseRepo struct{ db *database.DB }

func NewReleaseStore(db *database.DB) ReleaseStore { return &releaseRepo{db: db} }

func (r *releaseRepo) Create(ctx context.Context, rel *Release) error {
	if rel.ID == "" {
		rel.ID = uuid.NewString()
	}
	if rel.Status == "" {
		rel.Status = ReleaseStatusPending
	}

	const sql = `
INSERT INTO releases (id, org_id, deployment_id, revision, image, replicas, config_hash, config, status, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING created_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		rel.ID, rel.OrgID, rel.DeploymentID, rel.Revision, rel.Image, rel.Replicas,
		rel.ConfigHash, rel.Config, rel.Status, rel.CreatedBy)
	return database.MapError(row.Scan(&rel.CreatedAt))
}

func (r *releaseRepo) GetByID(ctx context.Context, id string) (*Release, error) {
	rel, err := database.QueryOne[Release](ctx, r.db.Conn(ctx),
		"SELECT * FROM releases WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &rel, nil
}

func (r *releaseRepo) GetByRevision(ctx context.Context, deploymentID string, revision int) (*Release, error) {
	rel, err := database.QueryOne[Release](ctx, r.db.Conn(ctx),
		"SELECT * FROM releases WHERE deployment_id = $1 AND revision = $2", deploymentID, revision)
	if err != nil {
		return nil, err
	}
	return &rel, nil
}

func (r *releaseRepo) GetLatest(ctx context.Context, deploymentID string) (*Release, error) {
	rel, err := database.QueryOne[Release](ctx, r.db.Conn(ctx),
		"SELECT * FROM releases WHERE deployment_id = $1 ORDER BY revision DESC LIMIT 1", deploymentID)
	if err != nil {
		return nil, err
	}
	return &rel, nil
}

func (r *releaseRepo) GetPreviousSuccessful(ctx context.Context, deploymentID string, beforeRevision int) (*Release, error) {
	rel, err := database.QueryOne[Release](ctx, r.db.Conn(ctx),
		`SELECT * FROM releases 
		 WHERE deployment_id = $1 AND revision < $2 AND status = 'succeeded' 
		 ORDER BY revision DESC LIMIT 1`, deploymentID, beforeRevision)
	if err != nil {
		return nil, err
	}
	return &rel, nil
}

func (r *releaseRepo) List(ctx context.Context, deploymentID string, req database.PageRequest) (database.Page[Release], error) {
	req = req.Normalize()

	items, err := database.QueryAll[Release](ctx, r.db.Conn(ctx),
		"SELECT * FROM releases WHERE deployment_id = $1 ORDER BY revision DESC LIMIT $2",
		deploymentID, req.Limit+1)
	if err != nil {
		return database.Page[Release]{}, err
	}

	// Simple pagination for releases (by revision)
	if len(items) > req.Limit {
		return database.Page[Release]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[Release]{Items: items}, nil
}

func (r *releaseRepo) UpdateStatus(ctx context.Context, id, status string, errorMsg *string) error {
	const sql = `UPDATE releases SET status = $1, error_message = $2 WHERE id = $3`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, errorMsg, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("release not found")
	}
	return nil
}

func (r *releaseRepo) MarkStarted(ctx context.Context, id string) error {
	const sql = `UPDATE releases SET status = 'deploying', started_at = now() WHERE id = $1`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("release not found")
	}
	return nil
}

func (r *releaseRepo) MarkFinished(ctx context.Context, id, status string, errorMsg *string) error {
	const sql = `UPDATE releases SET status = $1, finished_at = now(), error_message = $2 WHERE id = $3`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, errorMsg, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("release not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Processed Event Repository (consumer idempotency / deduplication)
// ----------------------------------------------------------------------------

// ProcessedEventStore records which events a durable consumer has already
// handled so at-least-once redelivery is idempotent.
type ProcessedEventStore interface {
	// MarkProcessed records (consumer, eventID) as handled. It returns true when
	// the row was newly inserted (first delivery) and false when the event was
	// already processed (a duplicate). The write is idempotent and must run
	// inside the same transaction as the work it guards so both commit atomically.
	MarkProcessed(ctx context.Context, consumer, eventID, orgID string) (bool, error)
}

type processedEventRepo struct{ db *database.DB }

// NewProcessedEventStore returns a Postgres-backed ProcessedEventStore.
func NewProcessedEventStore(db *database.DB) ProcessedEventStore { return &processedEventRepo{db: db} }

func (r *processedEventRepo) MarkProcessed(ctx context.Context, consumer, eventID, orgID string) (bool, error) {
	const sql = `
INSERT INTO deployment_processed_events (consumer, event_id, org_id)
VALUES ($1, $2, $3)
ON CONFLICT (consumer, event_id) DO NOTHING`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, consumer, eventID, orgID)
	if err != nil {
		return false, database.MapError(err)
	}
	return tag.RowsAffected() > 0, nil
}

// ----------------------------------------------------------------------------
// Helper: compute config hash
// ----------------------------------------------------------------------------

func computeConfigHash(d *Deployment) string {
	// Simple hash based on key config values
	data, _ := json.Marshal(map[string]any{
		"image":         d.Image,
		"replicas":      d.Replicas,
		"cpuRequest":    d.CPURequest,
		"cpuLimit":      d.CPULimit,
		"memoryRequest": d.MemoryRequest,
		"memoryLimit":   d.MemoryLimit,
		"port":          d.Port,
		"envVars":       string(d.EnvVars),
	})
	// Use a simple hash for now
	h := uint64(0)
	for _, b := range data {
		h = h*31 + uint64(b)
	}
	return string(rune(h))
}
