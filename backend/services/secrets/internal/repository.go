package secrets

import (
	"context"
	"fmt"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/jackc/pgx/v5"
)

// SecretStore defines the repository interface for secrets.
type SecretStore interface {
	Create(ctx context.Context, s *Secret) error
	GetByID(ctx context.Context, id string) (*Secret, error)
	GetByName(ctx context.Context, projectID, name string) (*Secret, error)
	List(ctx context.Context, projectID string, page database.PageRequest) (database.Page[Secret], error)
	Update(ctx context.Context, s *Secret) error
	Delete(ctx context.Context, id string) error

	// GetSecretsForCluster retrieves secrets for projects with deployments on the cluster.
	// SECURITY: The orgID parameter is required for defense-in-depth filtering.
	// This method must be called within a tenant context (WithTenant) AND with explicit org_id.
	// Both RLS and explicit filtering are enforced - they must coexist.
	GetSecretsForCluster(ctx context.Context, orgID, clusterID string) ([]Secret, error)
}

// AccessLogStore defines the repository interface for secret access logs.
type AccessLogStore interface {
	Create(ctx context.Context, log *SecretAccessLog) error
	ListBySecret(ctx context.Context, secretID string, limit int) ([]SecretAccessLog, error)
}

// secretRepo implements SecretStore using PostgreSQL.
type secretRepo struct {
	db *database.DB
}

// NewSecretRepository creates a new secret repository.
func NewSecretRepository(db *database.DB) SecretStore {
	return &secretRepo{db: db}
}

// Create inserts a new secret.
func (r *secretRepo) Create(ctx context.Context, s *Secret) error {
	const sql = `
		INSERT INTO secrets (org_id, project_id, name, description, encrypted_value, value_hash, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		RETURNING id, version, created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		s.OrgID,
		s.ProjectID,
		s.Name,
		s.Description,
		s.EncryptedValue,
		s.ValueHash,
		s.CreatedBy,
	)

	err := row.Scan(&s.ID, &s.Version, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return database.MapError(err)
	}
	return nil
}

// GetByID retrieves a secret by ID (excludes deleted).
func (r *secretRepo) GetByID(ctx context.Context, id string) (*Secret, error) {
	const sql = `
		SELECT id, org_id, project_id, name, description, encrypted_value, value_hash,
		       version, created_by, updated_by, created_at, updated_at, deleted_at
		FROM secrets
		WHERE id = $1 AND deleted_at IS NULL`

	var s Secret
	row := r.db.Conn(ctx).QueryRow(ctx, sql, id)
	err := row.Scan(
		&s.ID, &s.OrgID, &s.ProjectID, &s.Name, &s.Description,
		&s.EncryptedValue, &s.ValueHash, &s.Version,
		&s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt, &s.DeletedAt,
	)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &s, nil
}

// GetByName retrieves a secret by project and name (excludes deleted).
func (r *secretRepo) GetByName(ctx context.Context, projectID, name string) (*Secret, error) {
	const sql = `
		SELECT id, org_id, project_id, name, description, encrypted_value, value_hash,
		       version, created_by, updated_by, created_at, updated_at, deleted_at
		FROM secrets
		WHERE project_id = $1 AND name = $2 AND deleted_at IS NULL`

	var s Secret
	row := r.db.Conn(ctx).QueryRow(ctx, sql, projectID, name)
	err := row.Scan(
		&s.ID, &s.OrgID, &s.ProjectID, &s.Name, &s.Description,
		&s.EncryptedValue, &s.ValueHash, &s.Version,
		&s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt, &s.DeletedAt,
	)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &s, nil
}

// List returns a paginated list of secrets for a project (excludes deleted).
func (r *secretRepo) List(ctx context.Context, projectID string, page database.PageRequest) (database.Page[Secret], error) {
	args := []any{projectID}
	argN := 1

	whereClause := "WHERE project_id = $1 AND deleted_at IS NULL"

	// Apply cursor-based pagination
	if page.Cursor != "" {
		cursor, err := database.DecodeCursor(page.Cursor)
		if err != nil {
			return database.Page[Secret]{}, fmt.Errorf("invalid cursor: %w", err)
		}
		argN++
		args = append(args, cursor.CreatedAt)
		argN++
		args = append(args, cursor.ID)
		whereClause += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", argN-1, argN)
	}

	limit := page.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	// Query with limit + 1 to detect hasMore
	sql := fmt.Sprintf(`
		SELECT id, org_id, project_id, name, description, encrypted_value, value_hash,
		       version, created_by, updated_by, created_at, updated_at, deleted_at
		FROM secrets
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT %d`, whereClause, limit+1)

	rows, err := r.db.Conn(ctx).Query(ctx, sql, args...)
	if err != nil {
		return database.Page[Secret]{}, database.MapError(err)
	}
	defer rows.Close()

	var secrets []Secret
	for rows.Next() {
		var s Secret
		err := rows.Scan(
			&s.ID, &s.OrgID, &s.ProjectID, &s.Name, &s.Description,
			&s.EncryptedValue, &s.ValueHash, &s.Version,
			&s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt, &s.DeletedAt,
		)
		if err != nil {
			return database.Page[Secret]{}, database.MapError(err)
		}
		secrets = append(secrets, s)
	}

	if err := rows.Err(); err != nil {
		return database.Page[Secret]{}, database.MapError(err)
	}

	result := database.Page[Secret]{
		Items: secrets,
	}

	if len(secrets) > limit {
		result.Items = secrets[:limit]
		last := result.Items[len(result.Items)-1]
		result.NextCursor = database.EncodeCursor(last.Cursor())
	}

	return result, nil
}

// Update updates a secret (with optimistic locking).
func (r *secretRepo) Update(ctx context.Context, s *Secret) error {
	const sql = `
		UPDATE secrets
		SET description = $1, encrypted_value = $2, value_hash = $3, updated_by = $4, version = version + 1
		WHERE id = $5 AND version = $6 AND deleted_at IS NULL
		RETURNING version, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		s.Description,
		s.EncryptedValue,
		s.ValueHash,
		s.UpdatedBy,
		s.ID,
		s.Version,
	)

	err := row.Scan(&s.Version, &s.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return database.ErrConcurrentModification
		}
		return database.MapError(err)
	}
	return nil
}

// Delete soft-deletes a secret.
func (r *secretRepo) Delete(ctx context.Context, id string) error {
	const sql = `
		UPDATE secrets
		SET deleted_at = $1
		WHERE id = $2 AND deleted_at IS NULL`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, time.Now(), id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// GetSecretsForCluster retrieves all secrets for projects that have deployments on the cluster.
//
// SECURITY (CRIT-001 FIX):
// This method implements defense-in-depth with BOTH:
//   1. RLS enforcement via tenant context (caller must use db.WithTenant)
//   2. Explicit org_id filter in SQL query
//
// Both mechanisms must coexist - RLS alone is insufficient if bypassed (superuser, migrations).
// The explicit filter ensures tenant isolation even without RLS.
//
// Parameters:
//   - orgID: The authenticated organization ID (REQUIRED, must not be empty)
//   - clusterID: The cluster to query secrets for
//
// Returns ErrInvalidOrgID if orgID is empty.
func (r *secretRepo) GetSecretsForCluster(ctx context.Context, orgID, clusterID string) ([]Secret, error) {
	// SECURITY: Validate orgID is provided - defense against programming errors
	if orgID == "" {
		return nil, ErrInvalidOrgID
	}

	// SECURITY: Explicit org_id filter on ALL joined tables for defense-in-depth.
	// This filter works independently of RLS - both must pass for rows to be returned.
	const sql = `
		SELECT DISTINCT s.id, s.org_id, s.project_id, s.name, s.description, 
		       s.encrypted_value, s.value_hash, s.version,
		       s.created_by, s.updated_by, s.created_at, s.updated_at, s.deleted_at
		FROM secrets s
		INNER JOIN applications a ON a.org_id = $1
		INNER JOIN deployments d ON d.application_id = a.id 
		                        AND d.org_id = $1
		                        AND a.project_id = s.project_id
		WHERE d.cluster_id = $2
		  AND d.org_id = $1
		  AND s.org_id = $1
		  AND d.status NOT IN ('deleted', 'deleting')
		  AND s.deleted_at IS NULL
		ORDER BY s.project_id, s.name`

	rows, err := r.db.Conn(ctx).Query(ctx, sql, orgID, clusterID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	var secrets []Secret
	for rows.Next() {
		var s Secret
		err := rows.Scan(
			&s.ID, &s.OrgID, &s.ProjectID, &s.Name, &s.Description,
			&s.EncryptedValue, &s.ValueHash, &s.Version,
			&s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt, &s.DeletedAt,
		)
		if err != nil {
			return nil, database.MapError(err)
		}
		secrets = append(secrets, s)
	}

	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}

	return secrets, nil
}

// accessLogRepo implements AccessLogStore.
type accessLogRepo struct {
	db *database.DB
}

// NewAccessLogRepository creates a new access log repository.
func NewAccessLogRepository(db *database.DB) AccessLogStore {
	return &accessLogRepo{db: db}
}

// Create inserts a new access log entry.
func (r *accessLogRepo) Create(ctx context.Context, log *SecretAccessLog) error {
	const sql = `
		INSERT INTO secret_access_logs (org_id, secret_id, action, performed_by, metadata)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, performed_at`

	metadata := log.Metadata
	if len(metadata) == 0 {
		metadata = []byte("{}")
	}

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		log.OrgID,
		log.SecretID,
		log.Action,
		log.PerformedBy,
		metadata,
	)

	return row.Scan(&log.ID, &log.PerformedAt)
}

// ListBySecret returns access logs for a secret.
func (r *accessLogRepo) ListBySecret(ctx context.Context, secretID string, limit int) ([]SecretAccessLog, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	const sql = `
		SELECT id, org_id, secret_id, action, performed_by, performed_at, metadata
		FROM secret_access_logs
		WHERE secret_id = $1
		ORDER BY performed_at DESC
		LIMIT $2`

	rows, err := r.db.Conn(ctx).Query(ctx, sql, secretID, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	var logs []SecretAccessLog
	for rows.Next() {
		var l SecretAccessLog
		err := rows.Scan(&l.ID, &l.OrgID, &l.SecretID, &l.Action, &l.PerformedBy, &l.PerformedAt, &l.Metadata)
		if err != nil {
			return nil, database.MapError(err)
		}
		logs = append(logs, l)
	}

	return logs, rows.Err()
}
