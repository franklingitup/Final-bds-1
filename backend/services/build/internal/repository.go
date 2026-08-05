package build

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// TenantRunner runs a function within a tenant-scoped transaction.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// RepositoryStore persists git repositories.
type RepositoryStore interface {
	Create(ctx context.Context, r *GitRepository) error
	GetByID(ctx context.Context, id string) (*GitRepository, error)
	GetByName(ctx context.Context, projectID, name string) (*GitRepository, error)
	List(ctx context.Context, projectID string, req database.PageRequest) (database.Page[GitRepository], error)
	Update(ctx context.Context, r *GitRepository) error
	Delete(ctx context.Context, id string) error
}

// BuildStore persists builds.
type BuildStore interface {
	Create(ctx context.Context, b *Build) error
	GetByID(ctx context.Context, id string) (*Build, error)
	List(ctx context.Context, req database.PageRequest) (database.Page[Build], error)
	ListByRepository(ctx context.Context, repoID string, req database.PageRequest) (database.Page[Build], error)
	Update(ctx context.Context, b *Build) error
	UpdateStatus(ctx context.Context, id, status string, errorMsg *string) error
	MarkStarted(ctx context.Context, id string, commit *string) error
	MarkFinished(ctx context.Context, id, status string, errorMsg *string) error
	IncrementRetryCount(ctx context.Context, id string) error
}

// BuildLogStore persists build logs.
type BuildLogStore interface {
	Append(ctx context.Context, log *BuildLog) error
	AppendBatch(ctx context.Context, logs []*BuildLog) error
	List(ctx context.Context, buildID string, req database.PageRequest) (database.Page[BuildLog], error)
	GetNextSequence(ctx context.Context, buildID string) (int, error)
}

// BuildArtifactStore persists build artifacts.
type BuildArtifactStore interface {
	Create(ctx context.Context, a *BuildArtifact) error
	GetByBuildID(ctx context.Context, buildID string) (*BuildArtifact, error)
	GetByDigest(ctx context.Context, digest string) (*BuildArtifact, error)
}

// BuildQueueStore manages the build queue.
type BuildQueueStore interface {
	Enqueue(ctx context.Context, buildID string, priority int) error
	Dequeue(ctx context.Context, workerID string) (*BuildQueueItem, error)
	Heartbeat(ctx context.Context, buildID, workerID string) error
	Remove(ctx context.Context, buildID string) error
	GetStaleClaims(ctx context.Context, timeout time.Duration) ([]BuildQueueItem, error)
	ReleaseStaleClaims(ctx context.Context, timeout time.Duration) error
}

// ----------------------------------------------------------------------------
// Git Repository Repository
// ----------------------------------------------------------------------------

type repositoryRepo struct{ db *database.DB }

func NewRepositoryStore(db *database.DB) RepositoryStore { return &repositoryRepo{db: db} }

func (r *repositoryRepo) Create(ctx context.Context, repo *GitRepository) error {
	if repo.ID == "" {
		repo.ID = uuid.NewString()
	}
	if repo.DefaultBranch == "" {
		repo.DefaultBranch = "main"
	}
	if repo.AuthType == "" {
		repo.AuthType = AuthTypeNone
	}

	const sql = `
INSERT INTO git_repositories (id, org_id, project_id, name, url, default_branch, auth_type, auth_secret_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING created_at, updated_at, version`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		repo.ID, repo.OrgID, repo.ProjectID, repo.Name, repo.URL, repo.DefaultBranch,
		repo.AuthType, repo.AuthSecretID, repo.CreatedBy)
	return database.MapError(row.Scan(&repo.CreatedAt, &repo.UpdatedAt, &repo.Version))
}

func (r *repositoryRepo) GetByID(ctx context.Context, id string) (*GitRepository, error) {
	repo, err := database.QueryOne[GitRepository](ctx, r.db.Conn(ctx),
		"SELECT * FROM git_repositories WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (r *repositoryRepo) GetByName(ctx context.Context, projectID, name string) (*GitRepository, error) {
	repo, err := database.QueryOne[GitRepository](ctx, r.db.Conn(ctx),
		"SELECT * FROM git_repositories WHERE project_id = $1 AND name = $2", projectID, name)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (r *repositoryRepo) List(ctx context.Context, projectID string, req database.PageRequest) (database.Page[GitRepository], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[GitRepository]{}, err
	}

	var sql string
	var args []any

	if cur.IsZero() {
		sql = "SELECT * FROM git_repositories WHERE project_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2"
		args = []any{projectID, req.Limit + 1}
	} else {
		sql = "SELECT * FROM git_repositories WHERE project_id = $1 AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4"
		args = []any{projectID, cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	items, err := database.QueryAll[GitRepository](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[GitRepository]{}, err
	}
	return database.BuildPage(items, req.Limit, func(r GitRepository) database.Cursor { return r.Cursor() }), nil
}

func (r *repositoryRepo) Update(ctx context.Context, repo *GitRepository) error {
	const sql = `
UPDATE git_repositories
SET name = $1, url = $2, default_branch = $3, auth_type = $4, auth_secret_id = $5, version = version + 1, updated_at = now()
WHERE id = $6 AND version = $7`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		repo.Name, repo.URL, repo.DefaultBranch, repo.AuthType, repo.AuthSecretID, repo.ID, repo.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	repo.Version++
	return nil
}

func (r *repositoryRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM git_repositories WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("repository not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Build Repository
// ----------------------------------------------------------------------------

type buildRepo struct{ db *database.DB }

func NewBuildStore(db *database.DB) BuildStore { return &buildRepo{db: db} }

func (r *buildRepo) Create(ctx context.Context, b *Build) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	if b.Status == "" {
		b.Status = StatusQueued
	}
	if b.ContextPath == "" {
		b.ContextPath = "."
	}
	if b.DockerfilePath == "" {
		b.DockerfilePath = "Dockerfile"
	}
	if b.GitRef == "" {
		b.GitRef = "main"
	}
	if b.BuilderType == "" {
		b.BuilderType = BuilderKaniko
	}
	if len(b.BuildArgs) == 0 {
		b.BuildArgs = []byte("{}")
	}
	if b.MaxRetries == 0 {
		b.MaxRetries = 3
	}
	if b.TimeoutSeconds == 0 {
		b.TimeoutSeconds = 1800
	}

	const sql = `
INSERT INTO builds (
    id, org_id, repository_id, git_url, git_ref, git_commit, context_path, dockerfile_path, build_args,
    target_image, target_registry, push_to_registry, builder_type, status, error_message,
    retry_count, max_retries, parent_build_id, cpu_limit, memory_limit, timeout_seconds, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22
)
RETURNING queued_at, created_at, updated_at, version`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		b.ID, b.OrgID, b.RepositoryID, b.GitURL, b.GitRef, b.GitCommit, b.ContextPath, b.DockerfilePath, b.BuildArgs,
		b.TargetImage, b.TargetRegistry, b.PushToRegistry, b.BuilderType, b.Status, b.ErrorMessage,
		b.RetryCount, b.MaxRetries, b.ParentBuildID, b.CPULimit, b.MemoryLimit, b.TimeoutSeconds, b.CreatedBy)
	return database.MapError(row.Scan(&b.QueuedAt, &b.CreatedAt, &b.UpdatedAt, &b.Version))
}

func (r *buildRepo) GetByID(ctx context.Context, id string) (*Build, error) {
	b, err := database.QueryOne[Build](ctx, r.db.Conn(ctx),
		"SELECT * FROM builds WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *buildRepo) List(ctx context.Context, req database.PageRequest) (database.Page[Build], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Build]{}, err
	}

	var sql string
	var args []any

	if cur.IsZero() {
		sql = "SELECT * FROM builds ORDER BY created_at DESC, id DESC LIMIT $1"
		args = []any{req.Limit + 1}
	} else {
		sql = "SELECT * FROM builds WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3"
		args = []any{cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	items, err := database.QueryAll[Build](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Build]{}, err
	}
	return database.BuildPage(items, req.Limit, func(b Build) database.Cursor { return b.Cursor() }), nil
}

func (r *buildRepo) ListByRepository(ctx context.Context, repoID string, req database.PageRequest) (database.Page[Build], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Build]{}, err
	}

	var sql string
	var args []any

	if cur.IsZero() {
		sql = "SELECT * FROM builds WHERE repository_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2"
		args = []any{repoID, req.Limit + 1}
	} else {
		sql = "SELECT * FROM builds WHERE repository_id = $1 AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4"
		args = []any{repoID, cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	items, err := database.QueryAll[Build](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Build]{}, err
	}
	return database.BuildPage(items, req.Limit, func(b Build) database.Cursor { return b.Cursor() }), nil
}

func (r *buildRepo) Update(ctx context.Context, b *Build) error {
	const sql = `
UPDATE builds
SET git_ref = $1, git_commit = $2, status = $3, error_message = $4, started_at = $5, finished_at = $6,
    retry_count = $7, version = version + 1, updated_at = now()
WHERE id = $8 AND version = $9`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		b.GitRef, b.GitCommit, b.Status, b.ErrorMessage, b.StartedAt, b.FinishedAt,
		b.RetryCount, b.ID, b.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	b.Version++
	return nil
}

func (r *buildRepo) UpdateStatus(ctx context.Context, id, status string, errorMsg *string) error {
	const sql = `UPDATE builds SET status = $1, error_message = $2, version = version + 1, updated_at = now() WHERE id = $3`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, errorMsg, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("build not found")
	}
	return nil
}

func (r *buildRepo) MarkStarted(ctx context.Context, id string, commit *string) error {
	const sql = `UPDATE builds SET status = $1, git_commit = $2, started_at = now(), version = version + 1, updated_at = now() WHERE id = $3`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, StatusBuilding, commit, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("build not found")
	}
	return nil
}

func (r *buildRepo) MarkFinished(ctx context.Context, id, status string, errorMsg *string) error {
	const sql = `UPDATE builds SET status = $1, error_message = $2, finished_at = now(), version = version + 1, updated_at = now() WHERE id = $3`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, errorMsg, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("build not found")
	}
	return nil
}

func (r *buildRepo) IncrementRetryCount(ctx context.Context, id string) error {
	const sql = `UPDATE builds SET retry_count = retry_count + 1, version = version + 1, updated_at = now() WHERE id = $1`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("build not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Build Log Repository
// ----------------------------------------------------------------------------

type buildLogRepo struct{ db *database.DB }

func NewBuildLogStore(db *database.DB) BuildLogStore { return &buildLogRepo{db: db} }

func (r *buildLogRepo) Append(ctx context.Context, log *BuildLog) error {
	if log.ID == "" {
		log.ID = uuid.NewString()
	}
	if log.Stream == "" {
		log.Stream = StreamStdout
	}
	if log.Level == "" {
		log.Level = LevelInfo
	}
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now()
	}
	if len(log.Metadata) == 0 {
		log.Metadata = []byte("{}")
	}

	const sql = `
INSERT INTO build_logs (id, org_id, build_id, sequence, timestamp, stream, level, message, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := r.db.Conn(ctx).Exec(ctx, sql,
		log.ID, log.OrgID, log.BuildID, log.Sequence, log.Timestamp, log.Stream, log.Level, log.Message, log.Metadata)
	return database.MapError(err)
}

func (r *buildLogRepo) AppendBatch(ctx context.Context, logs []*BuildLog) error {
	if len(logs) == 0 {
		return nil
	}
	
	for _, log := range logs {
		if err := r.Append(ctx, log); err != nil {
			return err
		}
	}
	return nil
}

func (r *buildLogRepo) List(ctx context.Context, buildID string, req database.PageRequest) (database.Page[BuildLog], error) {
	req = req.Normalize()
	
	sql := "SELECT * FROM build_logs WHERE build_id = $1 ORDER BY sequence LIMIT $2"
	offset := 0
	if req.Cursor != "" {
		// For logs, cursor is the sequence number
		var seq int
		if _, err := database.DecodeCursor(req.Cursor); err == nil {
			sql = "SELECT * FROM build_logs WHERE build_id = $1 AND sequence > $2 ORDER BY sequence LIMIT $3"
		}
		_ = seq
		offset = 1
	}
	_ = offset

	items, err := database.QueryAll[BuildLog](ctx, r.db.Conn(ctx), sql, buildID, req.Limit+1)
	if err != nil {
		return database.Page[BuildLog]{}, err
	}
	
	var nextCursor string
	if len(items) > req.Limit {
		items = items[:req.Limit]
		nextCursor = items[len(items)-1].ID
	}
	
	return database.Page[BuildLog]{Items: items, NextCursor: nextCursor}, nil
}

func (r *buildLogRepo) GetNextSequence(ctx context.Context, buildID string) (int, error) {
	var seq int
	row := r.db.Conn(ctx).QueryRow(ctx, "SELECT COALESCE(MAX(sequence), 0) + 1 FROM build_logs WHERE build_id = $1", buildID)
	if err := row.Scan(&seq); err != nil {
		return 0, database.MapError(err)
	}
	return seq, nil
}

// ----------------------------------------------------------------------------
// Build Artifact Repository
// ----------------------------------------------------------------------------

type buildArtifactRepo struct{ db *database.DB }

func NewBuildArtifactStore(db *database.DB) BuildArtifactStore { return &buildArtifactRepo{db: db} }

func (r *buildArtifactRepo) Create(ctx context.Context, a *BuildArtifact) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	if a.ManifestType == "" {
		a.ManifestType = "docker"
	}
	if len(a.Labels) == 0 {
		a.Labels = []byte("{}")
	}

	const sql = `
INSERT INTO build_artifacts (id, org_id, build_id, image_digest, image_tag, image_size, manifest_type, manifest, layer_count, layers, dockerfile_hash, build_duration_ms, labels)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING created_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		a.ID, a.OrgID, a.BuildID, a.ImageDigest, a.ImageTag, a.ImageSize, a.ManifestType, a.Manifest,
		a.LayerCount, a.Layers, a.DockerfileHash, a.BuildDurationMs, a.Labels)
	return database.MapError(row.Scan(&a.CreatedAt))
}

func (r *buildArtifactRepo) GetByBuildID(ctx context.Context, buildID string) (*BuildArtifact, error) {
	a, err := database.QueryOne[BuildArtifact](ctx, r.db.Conn(ctx),
		"SELECT * FROM build_artifacts WHERE build_id = $1", buildID)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *buildArtifactRepo) GetByDigest(ctx context.Context, digest string) (*BuildArtifact, error) {
	a, err := database.QueryOne[BuildArtifact](ctx, r.db.Conn(ctx),
		"SELECT * FROM build_artifacts WHERE image_digest = $1", digest)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ----------------------------------------------------------------------------
// Build Queue Repository
// ----------------------------------------------------------------------------

type buildQueueRepo struct{ db *database.DB }

func NewBuildQueueStore(db *database.DB) BuildQueueStore { return &buildQueueRepo{db: db} }

func (r *buildQueueRepo) Enqueue(ctx context.Context, buildID string, priority int) error {
	const sql = `
INSERT INTO build_queue (id, build_id, priority)
VALUES (gen_random_uuid(), $1, $2)
ON CONFLICT (build_id) DO NOTHING`

	_, err := r.db.Conn(ctx).Exec(ctx, sql, buildID, priority)
	return database.MapError(err)
}

func (r *buildQueueRepo) Dequeue(ctx context.Context, workerID string) (*BuildQueueItem, error) {
	const sql = `
WITH claimed AS (
    SELECT id FROM build_queue
    WHERE worker_id IS NULL
    ORDER BY priority DESC, created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE build_queue q
SET worker_id = $1, claimed_at = now(), heartbeat_at = now()
FROM claimed
WHERE q.id = claimed.id
RETURNING q.id, q.build_id, (SELECT org_id FROM builds WHERE id = q.build_id) AS org_id, q.priority, q.worker_id, q.claimed_at, q.heartbeat_at, q.created_at`

	item, err := database.QueryOne[BuildQueueItem](ctx, r.db.Conn(ctx), sql, workerID)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil // No work available
		}
		return nil, err
	}
	return &item, nil
}

func (r *buildQueueRepo) Heartbeat(ctx context.Context, buildID, workerID string) error {
	const sql = `UPDATE build_queue SET heartbeat_at = now() WHERE build_id = $1 AND worker_id = $2`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, buildID, workerID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("queue item not found or not owned by worker")
	}
	return nil
}

func (r *buildQueueRepo) Remove(ctx context.Context, buildID string) error {
	_, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM build_queue WHERE build_id = $1", buildID)
	return database.MapError(err)
}

func (r *buildQueueRepo) GetStaleClaims(ctx context.Context, timeout time.Duration) ([]BuildQueueItem, error) {
	const sql = `
SELECT q.id, q.build_id, b.org_id, q.priority, q.worker_id, q.claimed_at, q.heartbeat_at, q.created_at
FROM build_queue q
JOIN builds b ON b.id = q.build_id
WHERE q.worker_id IS NOT NULL AND q.heartbeat_at < $1`
	return database.QueryAll[BuildQueueItem](ctx, r.db.Conn(ctx), sql, time.Now().Add(-timeout))
}

func (r *buildQueueRepo) ReleaseStaleClaims(ctx context.Context, timeout time.Duration) error {
	const sql = `UPDATE build_queue SET worker_id = NULL, claimed_at = NULL, heartbeat_at = NULL WHERE worker_id IS NOT NULL AND heartbeat_at < $1`
	_, err := r.db.Conn(ctx).Exec(ctx, sql, time.Now().Add(-timeout))
	return database.MapError(err)
}

// Helper to convert map to JSON
func mapToJSON(m map[string]string) json.RawMessage {
	if m == nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(m)
	return b
}
