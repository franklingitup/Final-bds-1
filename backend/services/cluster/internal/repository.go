package cluster

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

// ClusterStore persists clusters within a tenant scope.
type ClusterStore interface {
	Create(ctx context.Context, c *Cluster) error
	GetByID(ctx context.Context, id string) (*Cluster, error)
	GetByIDWithoutTenant(ctx context.Context, id string) (*Cluster, error)
	GetBySlug(ctx context.Context, slug string) (*Cluster, error)
	List(ctx context.Context, req database.PageRequest, status string) (database.Page[Cluster], error)
	Update(ctx context.Context, c *Cluster) error
	Delete(ctx context.Context, id string) error
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateHeartbeat(ctx context.Context, id string, at time.Time, k8sVersion string, nodeCount int) error
	RegisterAgent(ctx context.Context, id string, agentID, k8sVersion string, nodeCount int, cloudProvider, region *string, registeredAt time.Time) error
}

// TokenStore persists registration tokens.
type TokenStore interface {
	Create(ctx context.Context, t *RegistrationToken) error
	GetByHash(ctx context.Context, hash string) (*RegistrationToken, error)
	GetActiveByCluster(ctx context.Context, clusterID string) (*RegistrationToken, error)
	MarkUsed(ctx context.Context, id, agentID string) error
	Revoke(ctx context.Context, id string) error
}

// HeartbeatStore persists heartbeat history.
type HeartbeatStore interface {
	Create(ctx context.Context, h *Heartbeat) error
	ListByCluster(ctx context.Context, clusterID string, limit int) ([]Heartbeat, error)
}

// ----------------------------------------------------------------------------
// Cluster Repository
// ----------------------------------------------------------------------------

type clusterRepo struct{ db *database.DB }

func NewClusterStore(db *database.DB) ClusterStore { return &clusterRepo{db: db} }

func (r *clusterRepo) Create(ctx context.Context, c *Cluster) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.Status == "" {
		c.Status = StatusPending
	}
	if len(c.Labels) == 0 {
		c.Labels = []byte("{}")
	}

	const sql = `
INSERT INTO clusters (id, org_id, name, slug, description, status, cloud_provider, region, labels, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING created_at, updated_at, version`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		c.ID, c.OrgID, c.Name, c.Slug, c.Description, c.Status,
		c.CloudProvider, c.Region, c.Labels, c.CreatedBy)
	return database.MapError(row.Scan(&c.CreatedAt, &c.UpdatedAt, &c.Version))
}

func (r *clusterRepo) GetByID(ctx context.Context, id string) (*Cluster, error) {
	c, err := database.QueryOne[Cluster](ctx, r.db.Conn(ctx),
		"SELECT * FROM clusters WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetByIDWithoutTenant fetches a cluster by ID without tenant context (bypasses RLS).
// This is used for agent authentication where we need to validate credentials
// before we know the organization ID.
func (r *clusterRepo) GetByIDWithoutTenant(ctx context.Context, id string) (*Cluster, error) {
	c, err := database.QueryOne[Cluster](ctx, r.db.Pool,
		"SELECT * FROM clusters WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *clusterRepo) GetBySlug(ctx context.Context, slug string) (*Cluster, error) {
	c, err := database.QueryOne[Cluster](ctx, r.db.Conn(ctx),
		"SELECT * FROM clusters WHERE slug = $1", slug)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *clusterRepo) List(ctx context.Context, req database.PageRequest, status string) (database.Page[Cluster], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Cluster]{}, err
	}

	var (
		sql  string
		args []any
	)

	if status != "" {
		if cur.IsZero() {
			sql = "SELECT * FROM clusters WHERE status = $1 ORDER BY created_at DESC, id DESC LIMIT $2"
			args = []any{status, req.Limit + 1}
		} else {
			sql = "SELECT * FROM clusters WHERE status = $1 AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4"
			args = []any{status, cur.CreatedAt, cur.ID, req.Limit + 1}
		}
	} else {
		if cur.IsZero() {
			sql = "SELECT * FROM clusters WHERE status != 'deleted' ORDER BY created_at DESC, id DESC LIMIT $1"
			args = []any{req.Limit + 1}
		} else {
			sql = "SELECT * FROM clusters WHERE status != 'deleted' AND (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3"
			args = []any{cur.CreatedAt, cur.ID, req.Limit + 1}
		}
	}

	items, err := database.QueryAll[Cluster](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Cluster]{}, err
	}
	return database.BuildPage(items, req.Limit, func(c Cluster) database.Cursor { return c.Cursor() }), nil
}

func (r *clusterRepo) Update(ctx context.Context, c *Cluster) error {
	const sql = `
UPDATE clusters
SET name = $1, description = $2, labels = $3, version = version + 1, updated_at = now()
WHERE id = $4 AND version = $5`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, c.Name, c.Description, c.Labels, c.ID, c.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	c.Version++
	return nil
}

func (r *clusterRepo) Delete(ctx context.Context, id string) error {
	const sql = `UPDATE clusters SET status = 'deleted', version = version + 1, updated_at = now() WHERE id = $1`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("cluster not found")
	}
	return nil
}

func (r *clusterRepo) UpdateStatus(ctx context.Context, id, status string) error {
	const sql = `UPDATE clusters SET status = $1, version = version + 1, updated_at = now() WHERE id = $2`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("cluster not found")
	}
	return nil
}

func (r *clusterRepo) UpdateHeartbeat(ctx context.Context, id string, at time.Time, k8sVersion string, nodeCount int) error {
	const sql = `
UPDATE clusters 
SET last_heartbeat_at = $1, kubernetes_version = $2, node_count = $3, 
    status = 'connected', version = version + 1, updated_at = now() 
WHERE id = $4`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, at, k8sVersion, nodeCount, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("cluster not found")
	}
	return nil
}

func (r *clusterRepo) RegisterAgent(ctx context.Context, id string, agentID, k8sVersion string, nodeCount int, cloudProvider, region *string, registeredAt time.Time) error {
	const sql = `
UPDATE clusters 
SET agent_id = $1, kubernetes_version = $2, node_count = $3, 
    cloud_provider = COALESCE($4, cloud_provider), region = COALESCE($5, region),
    registered_at = $6, last_heartbeat_at = $6, status = 'connected',
    version = version + 1, updated_at = now()
WHERE id = $7`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, agentID, k8sVersion, nodeCount, cloudProvider, region, registeredAt, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("cluster not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Token Repository
// ----------------------------------------------------------------------------

type tokenRepo struct{ db *database.DB }

func NewTokenStore(db *database.DB) TokenStore { return &tokenRepo{db: db} }

func (r *tokenRepo) Create(ctx context.Context, t *RegistrationToken) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Status == "" {
		t.Status = TokenStatusActive
	}

	const sql = `
INSERT INTO cluster_registration_tokens (id, org_id, cluster_id, token_hash, status, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING created_at, updated_at, version`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		t.ID, t.OrgID, t.ClusterID, t.TokenHash, t.Status, t.ExpiresAt, t.CreatedBy)
	return database.MapError(row.Scan(&t.CreatedAt, &t.UpdatedAt, &t.Version))
}

// GetByHash reads a token by hash (cross-tenant, capability-based).
func (r *tokenRepo) GetByHash(ctx context.Context, hash string) (*RegistrationToken, error) {
	t, err := database.QueryOne[RegistrationToken](ctx, r.db.Pool,
		"SELECT * FROM cluster_registration_tokens WHERE token_hash = $1", hash)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *tokenRepo) GetActiveByCluster(ctx context.Context, clusterID string) (*RegistrationToken, error) {
	t, err := database.QueryOne[RegistrationToken](ctx, r.db.Conn(ctx),
		"SELECT * FROM cluster_registration_tokens WHERE cluster_id = $1 AND status = 'active'", clusterID)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *tokenRepo) MarkUsed(ctx context.Context, id, agentID string) error {
	const sql = `
UPDATE cluster_registration_tokens 
SET status = 'used', used_at = now(), used_by_agent = $1, version = version + 1, updated_at = now() 
WHERE id = $2 AND status = 'active'`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, agentID, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("token not found or not active")
	}
	return nil
}

func (r *tokenRepo) Revoke(ctx context.Context, id string) error {
	const sql = `
UPDATE cluster_registration_tokens 
SET status = 'revoked', version = version + 1, updated_at = now() 
WHERE id = $1 AND status = 'active'`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("token not found or not active")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Heartbeat Repository
// ----------------------------------------------------------------------------

type heartbeatRepo struct{ db *database.DB }

func NewHeartbeatStore(db *database.DB) HeartbeatStore { return &heartbeatRepo{db: db} }

func (r *heartbeatRepo) Create(ctx context.Context, h *Heartbeat) error {
	if h.ID == "" {
		h.ID = uuid.NewString()
	}

	const sql = `
INSERT INTO cluster_heartbeats (id, org_id, cluster_id, agent_id, kubernetes_version, node_count, api_server_healthy, received_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.db.Conn(ctx).Exec(ctx, sql,
		h.ID, h.OrgID, h.ClusterID, h.AgentID, h.KubernetesVersion, h.NodeCount, h.APIServerHealthy, h.ReceivedAt)
	return database.MapError(err)
}

func (r *heartbeatRepo) ListByCluster(ctx context.Context, clusterID string, limit int) ([]Heartbeat, error) {
	if limit <= 0 {
		limit = 50
	}
	return database.QueryAll[Heartbeat](ctx, r.db.Conn(ctx),
		"SELECT * FROM cluster_heartbeats WHERE cluster_id = $1 ORDER BY received_at DESC LIMIT $2",
		clusterID, limit)
}

// ----------------------------------------------------------------------------
// Helper: marshal labels
// ----------------------------------------------------------------------------

func marshalLabels(labels map[string]string) json.RawMessage {
	if labels == nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(labels)
	return b
}
