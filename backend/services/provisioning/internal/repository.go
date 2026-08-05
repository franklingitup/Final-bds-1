package provisioning

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// TenantRunner runs a function within a tenant-scoped transaction.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// CredentialStore persists cloud credentials.
type CredentialStore interface {
	Create(ctx context.Context, c *CloudCredential) error
	GetByID(ctx context.Context, id string) (*CloudCredential, error)
	GetByName(ctx context.Context, name string) (*CloudCredential, error)
	List(ctx context.Context, orgID string) ([]CloudCredential, error)
	ListByProvider(ctx context.Context, orgID, provider string) ([]CloudCredential, error)
	Update(ctx context.Context, c *CloudCredential) error
	Delete(ctx context.Context, id string) error
}

// TemplateStore persists cluster templates.
type TemplateStore interface {
	Create(ctx context.Context, t *ClusterTemplate) error
	GetByID(ctx context.Context, id string) (*ClusterTemplate, error)
	GetDefault(ctx context.Context, provider string) (*ClusterTemplate, error)
	List(ctx context.Context, orgID string) ([]ClusterTemplate, error)
	ListByProvider(ctx context.Context, orgID, provider string) ([]ClusterTemplate, error)
	Update(ctx context.Context, t *ClusterTemplate) error
	Delete(ctx context.Context, id string) error
}

// RequestStore persists provisioning requests.
type RequestStore interface {
	Create(ctx context.Context, r *ProvisioningRequest) error
	GetByID(ctx context.Context, id string) (*ProvisioningRequest, error)
	GetByName(ctx context.Context, name string) (*ProvisioningRequest, error)
	List(ctx context.Context, orgID string, page database.PageRequest) (database.Page[ProvisioningRequest], error)
	ListByStatus(ctx context.Context, orgID string, statuses []string) ([]ProvisioningRequest, error)
	Update(ctx context.Context, r *ProvisioningRequest) error
	UpdateStatus(ctx context.Context, id, status string, errMsg *string) error
	Delete(ctx context.Context, id string) error
}

// SessionStore persists install sessions.
type SessionStore interface {
	Create(ctx context.Context, s *InstallSession) error
	GetByID(ctx context.Context, id string) (*InstallSession, error)
	GetByToken(ctx context.Context, token string) (*InstallSession, error)
	GetByRequestID(ctx context.Context, requestID string) (*InstallSession, error)
	List(ctx context.Context, orgID string, page database.PageRequest) (database.Page[InstallSession], error)
	Update(ctx context.Context, s *InstallSession) error
	UpdateStatus(ctx context.Context, id, status string) error
	ExpireOldSessions(ctx context.Context) (int, error)
}

// StepStore persists install session steps.
type StepStore interface {
	Create(ctx context.Context, s *InstallSessionStep) error
	GetByID(ctx context.Context, id string) (*InstallSessionStep, error)
	ListBySession(ctx context.Context, sessionID string) ([]InstallSessionStep, error)
	Update(ctx context.Context, s *InstallSessionStep) error
}

// BootstrapTokenStore persists bootstrap tokens.
type BootstrapTokenStore interface {
	Create(ctx context.Context, t *BootstrapToken) error
	GetByHash(ctx context.Context, hash string) (*BootstrapToken, error)
	Use(ctx context.Context, hash, ip string, clusterID *string) error
	Revoke(ctx context.Context, id string) error
	ExpireOldTokens(ctx context.Context) (int, error)
}

// EventStore persists provisioning events.
type EventStore interface {
	Create(ctx context.Context, e *ProvisioningEvent) error
	ListByRequest(ctx context.Context, requestID string, page database.PageRequest) (database.Page[ProvisioningEvent], error)
	ListBySession(ctx context.Context, sessionID string, page database.PageRequest) (database.Page[ProvisioningEvent], error)
}

// ----------------------------------------------------------------------------
// Credential Repository
// ----------------------------------------------------------------------------

type credentialRepo struct{ db *database.DB }

func NewCredentialStore(db *database.DB) CredentialStore { return &credentialRepo{db: db} }

func (r *credentialRepo) Create(ctx context.Context, c *CloudCredential) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}

	const sql = `
INSERT INTO cloud_credentials (id, org_id, name, provider, credentials, region, description, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		c.ID, c.OrgID, c.Name, c.Provider, c.Credentials, c.Region, c.Description, c.CreatedBy)
	return database.MapError(row.Scan(&c.CreatedAt, &c.UpdatedAt))
}

func (r *credentialRepo) GetByID(ctx context.Context, id string) (*CloudCredential, error) {
	c, err := database.QueryOne[CloudCredential](ctx, r.db.Conn(ctx),
		"SELECT * FROM cloud_credentials WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *credentialRepo) GetByName(ctx context.Context, name string) (*CloudCredential, error) {
	c, err := database.QueryOne[CloudCredential](ctx, r.db.Conn(ctx),
		"SELECT * FROM cloud_credentials WHERE name = $1", name)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *credentialRepo) List(ctx context.Context, orgID string) ([]CloudCredential, error) {
	return database.QueryAll[CloudCredential](ctx, r.db.Conn(ctx),
		"SELECT * FROM cloud_credentials WHERE org_id = $1 ORDER BY name", orgID)
}

func (r *credentialRepo) ListByProvider(ctx context.Context, orgID, provider string) ([]CloudCredential, error) {
	return database.QueryAll[CloudCredential](ctx, r.db.Conn(ctx),
		"SELECT * FROM cloud_credentials WHERE org_id = $1 AND provider = $2 ORDER BY name", orgID, provider)
}

func (r *credentialRepo) Update(ctx context.Context, c *CloudCredential) error {
	const sql = `
UPDATE cloud_credentials
SET name = $1, credentials = $2, validated = $3, validated_at = $4, validation_error = $5, region = $6, description = $7, updated_at = now()
WHERE id = $8`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		c.Name, c.Credentials, c.Validated, c.ValidatedAt, c.ValidationError, c.Region, c.Description, c.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *credentialRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM cloud_credentials WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Template Repository
// ----------------------------------------------------------------------------

type templateRepo struct{ db *database.DB }

func NewTemplateStore(db *database.DB) TemplateStore { return &templateRepo{db: db} }

func (r *templateRepo) Create(ctx context.Context, t *ClusterTemplate) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if len(t.Config) == 0 {
		t.Config = []byte("{}")
	}
	if len(t.NodePools) == 0 {
		t.NodePools = []byte("[]")
	}
	if len(t.Networking) == 0 {
		t.Networking = []byte("{}")
	}
	if len(t.Addons) == 0 {
		t.Addons = []byte("[]")
	}

	const sql = `
INSERT INTO cluster_templates (id, org_id, name, provider, config, k8s_version, node_pools, networking, addons, description, is_default)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		t.ID, t.OrgID, t.Name, t.Provider, t.Config, t.K8sVersion, t.NodePools, t.Networking, t.Addons, t.Description, t.IsDefault)
	return database.MapError(row.Scan(&t.CreatedAt, &t.UpdatedAt))
}

func (r *templateRepo) GetByID(ctx context.Context, id string) (*ClusterTemplate, error) {
	t, err := database.QueryOne[ClusterTemplate](ctx, r.db.Conn(ctx),
		"SELECT * FROM cluster_templates WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *templateRepo) GetDefault(ctx context.Context, provider string) (*ClusterTemplate, error) {
	t, err := database.QueryOne[ClusterTemplate](ctx, r.db.Conn(ctx),
		"SELECT * FROM cluster_templates WHERE org_id IS NULL AND provider = $1 AND is_default = true", provider)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *templateRepo) List(ctx context.Context, orgID string) ([]ClusterTemplate, error) {
	return database.QueryAll[ClusterTemplate](ctx, r.db.Conn(ctx),
		"SELECT * FROM cluster_templates WHERE org_id = $1 OR (org_id IS NULL AND is_default = true) ORDER BY provider, name", orgID)
}

func (r *templateRepo) ListByProvider(ctx context.Context, orgID, provider string) ([]ClusterTemplate, error) {
	return database.QueryAll[ClusterTemplate](ctx, r.db.Conn(ctx),
		"SELECT * FROM cluster_templates WHERE (org_id = $1 OR (org_id IS NULL AND is_default = true)) AND provider = $2 ORDER BY name",
		orgID, provider)
}

func (r *templateRepo) Update(ctx context.Context, t *ClusterTemplate) error {
	const sql = `
UPDATE cluster_templates
SET name = $1, k8s_version = $2, node_pools = $3, networking = $4, addons = $5, description = $6, updated_at = now()
WHERE id = $7`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		t.Name, t.K8sVersion, t.NodePools, t.Networking, t.Addons, t.Description, t.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *templateRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM cluster_templates WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Request Repository
// ----------------------------------------------------------------------------

type requestRepo struct{ db *database.DB }

func NewRequestStore(db *database.DB) RequestStore { return &requestRepo{db: db} }

func (r *requestRepo) Create(ctx context.Context, req *ProvisioningRequest) error {
	if req.ID == "" {
		req.ID = uuid.NewString()
	}
	if req.Status == "" {
		req.Status = RequestPending
	}
	if len(req.Config) == 0 {
		req.Config = []byte("{}")
	}
	if len(req.NodePools) == 0 {
		req.NodePools = []byte("[]")
	}

	const sql = `
INSERT INTO provisioning_requests (id, org_id, name, provider, region, credential_id, template_id, config, k8s_version, node_pools, status, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		req.ID, req.OrgID, req.Name, req.Provider, req.Region, req.CredentialID, req.TemplateID, req.Config, req.K8sVersion, req.NodePools, req.Status, req.CreatedBy)
	return database.MapError(row.Scan(&req.CreatedAt, &req.UpdatedAt))
}

func (r *requestRepo) GetByID(ctx context.Context, id string) (*ProvisioningRequest, error) {
	req, err := database.QueryOne[ProvisioningRequest](ctx, r.db.Conn(ctx),
		"SELECT * FROM provisioning_requests WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *requestRepo) GetByName(ctx context.Context, name string) (*ProvisioningRequest, error) {
	req, err := database.QueryOne[ProvisioningRequest](ctx, r.db.Conn(ctx),
		"SELECT * FROM provisioning_requests WHERE name = $1", name)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *requestRepo) List(ctx context.Context, orgID string, page database.PageRequest) (database.Page[ProvisioningRequest], error) {
	page = page.Normalize()
	items, err := database.QueryAll[ProvisioningRequest](ctx, r.db.Conn(ctx),
		"SELECT * FROM provisioning_requests WHERE org_id = $1 ORDER BY created_at DESC LIMIT $2",
		orgID, page.Limit+1)
	if err != nil {
		return database.Page[ProvisioningRequest]{}, err
	}

	if len(items) > page.Limit {
		return database.Page[ProvisioningRequest]{
			Items:      items[:page.Limit],
			NextCursor: items[page.Limit-1].ID,
		}, nil
	}
	return database.Page[ProvisioningRequest]{Items: items}, nil
}

func (r *requestRepo) ListByStatus(ctx context.Context, orgID string, statuses []string) ([]ProvisioningRequest, error) {
	return database.QueryAll[ProvisioningRequest](ctx, r.db.Conn(ctx),
		"SELECT * FROM provisioning_requests WHERE org_id = $1 AND status = ANY($2) ORDER BY created_at DESC",
		orgID, statuses)
}

func (r *requestRepo) Update(ctx context.Context, req *ProvisioningRequest) error {
	const sql = `
UPDATE provisioning_requests
SET terraform_config = $1, terraform_vars = $2, status = $3, cluster_id = $4, error_message = $5, error_details = $6, started_at = $7, completed_at = $8, updated_at = now()
WHERE id = $9`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		req.TerraformConfig, req.TerraformVars, req.Status, req.ClusterID, req.ErrorMessage, req.ErrorDetails, req.StartedAt, req.CompletedAt, req.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *requestRepo) UpdateStatus(ctx context.Context, id, status string, errMsg *string) error {
	const sql = `UPDATE provisioning_requests SET status = $1, error_message = $2, updated_at = now() WHERE id = $3`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, errMsg, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *requestRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM provisioning_requests WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Session Repository
// ----------------------------------------------------------------------------

type sessionRepo struct{ db *database.DB }

func NewSessionStore(db *database.DB) SessionStore { return &sessionRepo{db: db} }

func (r *sessionRepo) Create(ctx context.Context, s *InstallSession) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.Status == "" {
		s.Status = SessionActive
	}
	if len(s.Steps) == 0 {
		s.Steps = []byte("[]")
	}

	const sql = `
INSERT INTO install_sessions (id, org_id, request_id, session_token, current_step, total_steps, completed_steps, steps, status, bootstrap_token, bootstrap_command, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		s.ID, s.OrgID, s.RequestID, s.SessionToken, s.CurrentStep, s.TotalSteps, s.CompletedSteps, s.Steps, s.Status, s.BootstrapToken, s.BootstrapCommand, s.ExpiresAt)
	return database.MapError(row.Scan(&s.CreatedAt, &s.UpdatedAt))
}

func (r *sessionRepo) GetByID(ctx context.Context, id string) (*InstallSession, error) {
	s, err := database.QueryOne[InstallSession](ctx, r.db.Conn(ctx),
		"SELECT * FROM install_sessions WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *sessionRepo) GetByToken(ctx context.Context, token string) (*InstallSession, error) {
	s, err := database.QueryOne[InstallSession](ctx, r.db.Conn(ctx),
		"SELECT * FROM install_sessions WHERE session_token = $1", token)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *sessionRepo) GetByRequestID(ctx context.Context, requestID string) (*InstallSession, error) {
	s, err := database.QueryOne[InstallSession](ctx, r.db.Conn(ctx),
		"SELECT * FROM install_sessions WHERE request_id = $1 ORDER BY created_at DESC LIMIT 1", requestID)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *sessionRepo) List(ctx context.Context, orgID string, page database.PageRequest) (database.Page[InstallSession], error) {
	page = page.Normalize()
	items, err := database.QueryAll[InstallSession](ctx, r.db.Conn(ctx),
		"SELECT * FROM install_sessions WHERE org_id = $1 ORDER BY created_at DESC LIMIT $2",
		orgID, page.Limit+1)
	if err != nil {
		return database.Page[InstallSession]{}, err
	}

	if len(items) > page.Limit {
		return database.Page[InstallSession]{
			Items:      items[:page.Limit],
			NextCursor: items[page.Limit-1].ID,
		}, nil
	}
	return database.Page[InstallSession]{Items: items}, nil
}

func (r *sessionRepo) Update(ctx context.Context, s *InstallSession) error {
	const sql = `
UPDATE install_sessions
SET current_step = $1, completed_steps = $2, steps = $3, status = $4, agent_connected = $5, agent_connected_at = $6, agent_version = $7, started_at = $8, completed_at = $9, updated_at = now()
WHERE id = $10`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		s.CurrentStep, s.CompletedSteps, s.Steps, s.Status, s.AgentConnected, s.AgentConnectedAt, s.AgentVersion, s.StartedAt, s.CompletedAt, s.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *sessionRepo) UpdateStatus(ctx context.Context, id, status string) error {
	var sql string
	if status == SessionCompleted || status == SessionFailed {
		sql = `UPDATE install_sessions SET status = $1, completed_at = now(), updated_at = now() WHERE id = $2`
	} else {
		sql = `UPDATE install_sessions SET status = $1, updated_at = now() WHERE id = $2`
	}

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *sessionRepo) ExpireOldSessions(ctx context.Context) (int, error) {
	const sql = `UPDATE install_sessions SET status = 'expired', updated_at = now() WHERE status = 'active' AND expires_at < now()`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql)
	if err != nil {
		return 0, database.MapError(err)
	}
	return int(tag.RowsAffected()), nil
}

// ----------------------------------------------------------------------------
// Step Repository
// ----------------------------------------------------------------------------

type stepRepo struct{ db *database.DB }

func NewStepStore(db *database.DB) StepStore { return &stepRepo{db: db} }

func (r *stepRepo) Create(ctx context.Context, s *InstallSessionStep) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.Status == "" {
		s.Status = StepPending
	}

	const sql = `
INSERT INTO install_session_steps (id, session_id, step_number, name, description, status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		s.ID, s.SessionID, s.StepNumber, s.Name, s.Description, s.Status)
	return database.MapError(row.Scan(&s.CreatedAt, &s.UpdatedAt))
}

func (r *stepRepo) GetByID(ctx context.Context, id string) (*InstallSessionStep, error) {
	s, err := database.QueryOne[InstallSessionStep](ctx, r.db.Conn(ctx),
		"SELECT * FROM install_session_steps WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *stepRepo) ListBySession(ctx context.Context, sessionID string) ([]InstallSessionStep, error) {
	return database.QueryAll[InstallSessionStep](ctx, r.db.Conn(ctx),
		"SELECT * FROM install_session_steps WHERE session_id = $1 ORDER BY step_number", sessionID)
}

func (r *stepRepo) Update(ctx context.Context, s *InstallSessionStep) error {
	const sql = `
UPDATE install_session_steps
SET status = $1, output = $2, error = $3, started_at = $4, completed_at = $5, duration_ms = $6, updated_at = now()
WHERE id = $7`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		s.Status, s.Output, s.Error, s.StartedAt, s.CompletedAt, s.DurationMs, s.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Bootstrap Token Repository
// ----------------------------------------------------------------------------

type bootstrapTokenRepo struct{ db *database.DB }

func NewBootstrapTokenStore(db *database.DB) BootstrapTokenStore { return &bootstrapTokenRepo{db: db} }

func (r *bootstrapTokenRepo) Create(ctx context.Context, t *BootstrapToken) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Status == "" {
		t.Status = TokenActive
	}

	const sql = `
INSERT INTO bootstrap_tokens (id, org_id, request_id, session_id, token_hash, max_uses, status, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING created_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		t.ID, t.OrgID, t.RequestID, t.SessionID, t.TokenHash, t.MaxUses, t.Status, t.ExpiresAt)
	return database.MapError(row.Scan(&t.CreatedAt))
}

func (r *bootstrapTokenRepo) GetByHash(ctx context.Context, hash string) (*BootstrapToken, error) {
	t, err := database.QueryOne[BootstrapToken](ctx, r.db.Conn(ctx),
		"SELECT * FROM bootstrap_tokens WHERE token_hash = $1 AND status = 'active' AND expires_at > now()", hash)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *bootstrapTokenRepo) Use(ctx context.Context, hash, ip string, clusterID *string) error {
	const sql = `
UPDATE bootstrap_tokens
SET use_count = use_count + 1, last_used_at = now(), used_by_ip = $1, cluster_id = $2, status = CASE WHEN use_count + 1 >= max_uses THEN 'used' ELSE status END
WHERE token_hash = $3 AND status = 'active'`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, ip, clusterID, hash)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *bootstrapTokenRepo) Revoke(ctx context.Context, id string) error {
	const sql = `UPDATE bootstrap_tokens SET status = 'revoked' WHERE id = $1`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *bootstrapTokenRepo) ExpireOldTokens(ctx context.Context) (int, error) {
	const sql = `UPDATE bootstrap_tokens SET status = 'expired' WHERE status = 'active' AND expires_at < now()`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql)
	if err != nil {
		return 0, database.MapError(err)
	}
	return int(tag.RowsAffected()), nil
}

// ----------------------------------------------------------------------------
// Event Repository
// ----------------------------------------------------------------------------

type eventRepo struct{ db *database.DB }

func NewEventStore(db *database.DB) EventStore { return &eventRepo{db: db} }

func (r *eventRepo) Create(ctx context.Context, e *ProvisioningEvent) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if len(e.Details) == 0 {
		e.Details = []byte("{}")
	}

	const sql = `
INSERT INTO provisioning_events (id, org_id, request_id, session_id, event_type, severity, message, details, actor_type, actor_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING created_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		e.ID, e.OrgID, e.RequestID, e.SessionID, e.EventType, e.Severity, e.Message, e.Details, e.ActorType, e.ActorID)
	return database.MapError(row.Scan(&e.CreatedAt))
}

func (r *eventRepo) ListByRequest(ctx context.Context, requestID string, page database.PageRequest) (database.Page[ProvisioningEvent], error) {
	page = page.Normalize()
	items, err := database.QueryAll[ProvisioningEvent](ctx, r.db.Conn(ctx),
		"SELECT * FROM provisioning_events WHERE request_id = $1 ORDER BY created_at DESC LIMIT $2",
		requestID, page.Limit+1)
	if err != nil {
		return database.Page[ProvisioningEvent]{}, err
	}

	if len(items) > page.Limit {
		return database.Page[ProvisioningEvent]{
			Items:      items[:page.Limit],
			NextCursor: items[page.Limit-1].ID,
		}, nil
	}
	return database.Page[ProvisioningEvent]{Items: items}, nil
}

func (r *eventRepo) ListBySession(ctx context.Context, sessionID string, page database.PageRequest) (database.Page[ProvisioningEvent], error) {
	page = page.Normalize()
	items, err := database.QueryAll[ProvisioningEvent](ctx, r.db.Conn(ctx),
		"SELECT * FROM provisioning_events WHERE session_id = $1 ORDER BY created_at DESC LIMIT $2",
		sessionID, page.Limit+1)
	if err != nil {
		return database.Page[ProvisioningEvent]{}, err
	}

	if len(items) > page.Limit {
		return database.Page[ProvisioningEvent]{
			Items:      items[:page.Limit],
			NextCursor: items[page.Limit-1].ID,
		}, nil
	}
	return database.Page[ProvisioningEvent]{Items: items}, nil
}

// Helper for marshaling JSON
func marshalJSON(v interface{}) json.RawMessage {
	if v == nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(v)
	return b
}
