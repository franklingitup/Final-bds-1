package github

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// ConnectionStore persists GitHub connections.
type ConnectionStore interface {
	Create(ctx context.Context, c *GitHubConnection) error
	GetByID(ctx context.Context, id string) (*GitHubConnection, error)
	GetByName(ctx context.Context, orgID, name string) (*GitHubConnection, error)
	List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[GitHubConnection], error)
	Update(ctx context.Context, c *GitHubConnection) error
	Delete(ctx context.Context, id string) error
	UpdateLastUsed(ctx context.Context, id string) error
	UpdateLastValidated(ctx context.Context, id string) error
	UpdateStatus(ctx context.Context, id, status string, errorMsg *string) error
}

// RepositoryStore persists GitHub repositories.
type RepositoryStore interface {
	Create(ctx context.Context, r *GitHubRepository) error
	GetByID(ctx context.Context, id string) (*GitHubRepository, error)
	GetByGitHubID(ctx context.Context, orgID string, githubID int64) (*GitHubRepository, error)
	GetByFullName(ctx context.Context, orgID, fullName string) (*GitHubRepository, error)
	List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[GitHubRepository], error)
	ListByConnection(ctx context.Context, connectionID string, req database.PageRequest) (database.Page[GitHubRepository], error)
	Update(ctx context.Context, r *GitHubRepository) error
	Delete(ctx context.Context, id string) error
	UpdateSyncStatus(ctx context.Context, id string, syncError *string) error
}

// WebhookStore persists GitHub webhooks.
type WebhookStore interface {
	Create(ctx context.Context, w *GitHubWebhook) error
	GetByID(ctx context.Context, id string) (*GitHubWebhook, error)
	GetByRepositoryID(ctx context.Context, repositoryID string) (*GitHubWebhook, error)
	GetByGitHubHookID(ctx context.Context, hookID int64) (*GitHubWebhook, error)
	List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[GitHubWebhook], error)
	Update(ctx context.Context, w *GitHubWebhook) error
	Delete(ctx context.Context, id string) error
	UpdateDelivery(ctx context.Context, id string, lastError *string) error
}

// WebhookDeliveryStore persists webhook deliveries.
type WebhookDeliveryStore interface {
	Create(ctx context.Context, d *GitHubWebhookDelivery) error
	GetByID(ctx context.Context, id string) (*GitHubWebhookDelivery, error)
	GetByDeliveryID(ctx context.Context, deliveryID string) (*GitHubWebhookDelivery, error)
	List(ctx context.Context, webhookID string, req database.PageRequest) (database.Page[GitHubWebhookDelivery], error)
	UpdateStatus(ctx context.Context, id, status string, errorMsg *string) error
}

// OAuthStateStore persists OAuth states.
type OAuthStateStore interface {
	Create(ctx context.Context, s *GitHubOAuthState) error
	GetByState(ctx context.Context, state string) (*GitHubOAuthState, error)
	Delete(ctx context.Context, id string) error
	DeleteExpired(ctx context.Context) error
}

// ----------------------------------------------------------------------------
// Connection Repository
// ----------------------------------------------------------------------------

type connectionRepo struct{ db *database.DB }

func NewConnectionStore(db *database.DB) ConnectionStore { return &connectionRepo{db: db} }

func (r *connectionRepo) Create(ctx context.Context, c *GitHubConnection) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.Status == "" {
		c.Status = StatusActive
	}
	if c.ConnectionType == "" {
		c.ConnectionType = ConnectionTypePAT
	}

	const sql = `
INSERT INTO github_connections (id, org_id, connection_type, name, github_user_id, github_username, github_avatar,
    access_token, refresh_token, token_expires_at, scopes, token_hash, status, error_message, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING created_at, updated_at, version`

	scopesJSON, _ := json.Marshal(c.Scopes)
	if c.Scopes == nil {
		scopesJSON = []byte("[]")
	}

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		c.ID, c.OrgID, c.ConnectionType, c.Name, c.GitHubUserID, c.GitHubUsername, c.GitHubAvatar,
		c.AccessToken, c.RefreshToken, c.TokenExpiresAt, scopesJSON, c.TokenHash,
		c.Status, c.ErrorMessage, c.CreatedBy)
	return database.MapError(row.Scan(&c.CreatedAt, &c.UpdatedAt, &c.Version))
}

func (r *connectionRepo) GetByID(ctx context.Context, id string) (*GitHubConnection, error) {
	const sql = `SELECT id, org_id, connection_type, name, github_user_id, github_username, github_avatar,
		access_token, refresh_token, token_expires_at, scopes, token_hash, last_used_at, last_validated_at,
		status, error_message, created_by, version, created_at, updated_at
		FROM github_connections WHERE id = $1`

	row := r.db.Conn(ctx).QueryRow(ctx, sql, id)
	return scanConnection(row)
}

func (r *connectionRepo) GetByName(ctx context.Context, orgID, name string) (*GitHubConnection, error) {
	const sql = `SELECT id, org_id, connection_type, name, github_user_id, github_username, github_avatar,
		access_token, refresh_token, token_expires_at, scopes, token_hash, last_used_at, last_validated_at,
		status, error_message, created_by, version, created_at, updated_at
		FROM github_connections WHERE org_id = $1 AND name = $2`

	row := r.db.Conn(ctx).QueryRow(ctx, sql, orgID, name)
	return scanConnection(row)
}

func scanConnection(row pgx.Row) (*GitHubConnection, error) {
	var c GitHubConnection
	var scopesJSON []byte
	err := row.Scan(
		&c.ID, &c.OrgID, &c.ConnectionType, &c.Name, &c.GitHubUserID, &c.GitHubUsername, &c.GitHubAvatar,
		&c.AccessToken, &c.RefreshToken, &c.TokenExpiresAt, &scopesJSON, &c.TokenHash,
		&c.LastUsedAt, &c.LastValidatedAt, &c.Status, &c.ErrorMessage, &c.CreatedBy,
		&c.Version, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, database.MapError(err)
	}
	if len(scopesJSON) > 0 {
		_ = json.Unmarshal(scopesJSON, &c.Scopes)
	}
	return &c, nil
}

func (r *connectionRepo) List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[GitHubConnection], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[GitHubConnection]{}, err
	}

	var sql string
	var args []any

	baseSQL := `SELECT id, org_id, connection_type, name, github_user_id, github_username, github_avatar,
		access_token, refresh_token, token_expires_at, scopes, token_hash, last_used_at, last_validated_at,
		status, error_message, created_by, version, created_at, updated_at
		FROM github_connections WHERE org_id = $1`

	if cur.IsZero() {
		sql = baseSQL + ` ORDER BY created_at DESC, id DESC LIMIT $2`
		args = []any{orgID, req.Limit + 1}
	} else {
		sql = baseSQL + ` AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4`
		args = []any{orgID, cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	rows, err := r.db.Conn(ctx).Query(ctx, sql, args...)
	if err != nil {
		return database.Page[GitHubConnection]{}, database.MapError(err)
	}
	defer rows.Close()

	var items []GitHubConnection
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return database.Page[GitHubConnection]{}, err
		}
		items = append(items, *c)
	}

	return database.BuildPage(items, req.Limit, func(c GitHubConnection) database.Cursor { return c.Cursor() }), nil
}

func (r *connectionRepo) Update(ctx context.Context, c *GitHubConnection) error {
	const sql = `
UPDATE github_connections
SET name = $1, github_user_id = $2, github_username = $3, github_avatar = $4,
    access_token = $5, refresh_token = $6, token_expires_at = $7, scopes = $8, token_hash = $9,
    status = $10, error_message = $11, version = version + 1, updated_at = now()
WHERE id = $12 AND version = $13`

	scopesJSON, _ := json.Marshal(c.Scopes)
	if c.Scopes == nil {
		scopesJSON = []byte("[]")
	}

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		c.Name, c.GitHubUserID, c.GitHubUsername, c.GitHubAvatar,
		c.AccessToken, c.RefreshToken, c.TokenExpiresAt, scopesJSON, c.TokenHash,
		c.Status, c.ErrorMessage, c.ID, c.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	c.Version++
	return nil
}

func (r *connectionRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM github_connections WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("github connection not found")
	}
	return nil
}

func (r *connectionRepo) UpdateLastUsed(ctx context.Context, id string) error {
	_, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE github_connections SET last_used_at = now(), updated_at = now() WHERE id = $1", id)
	return database.MapError(err)
}

func (r *connectionRepo) UpdateLastValidated(ctx context.Context, id string) error {
	_, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE github_connections SET last_validated_at = now(), updated_at = now() WHERE id = $1", id)
	return database.MapError(err)
}

func (r *connectionRepo) UpdateStatus(ctx context.Context, id, status string, errorMsg *string) error {
	_, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE github_connections SET status = $1, error_message = $2, updated_at = now() WHERE id = $3",
		status, errorMsg, id)
	return database.MapError(err)
}

// ----------------------------------------------------------------------------
// Repository Repository
// ----------------------------------------------------------------------------

type repositoryRepo struct{ db *database.DB }

func NewRepositoryStore(db *database.DB) RepositoryStore { return &repositoryRepo{db: db} }

func (r *repositoryRepo) Create(ctx context.Context, repo *GitHubRepository) error {
	if repo.ID == "" {
		repo.ID = uuid.NewString()
	}
	if len(repo.Languages) == 0 {
		repo.Languages = []byte("{}")
	}
	if len(repo.Permissions) == 0 {
		repo.Permissions = []byte("{}")
	}

	const sql = `
INSERT INTO github_repositories (id, org_id, connection_id, github_repo_id, owner, name, full_name,
    description, html_url, clone_url, ssh_url, default_branch, is_private, is_fork, is_archived,
    stars_count, forks_count, watchers_count, open_issues_count, topics, language, languages,
    permissions, last_synced_at, sync_error, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)
RETURNING created_at, updated_at, version`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		repo.ID, repo.OrgID, repo.ConnectionID, repo.GitHubRepoID, repo.Owner, repo.Name, repo.FullName,
		repo.Description, repo.HTMLURL, repo.CloneURL, repo.SSHURL, repo.DefaultBranch, repo.IsPrivate,
		repo.IsFork, repo.IsArchived, repo.StarsCount, repo.ForksCount, repo.WatchersCount,
		repo.OpenIssuesCount, topicsToJSON(repo.Topics), repo.Language, repo.Languages, repo.Permissions,
		repo.LastSyncedAt, repo.SyncError, repo.CreatedBy)
	return database.MapError(row.Scan(&repo.CreatedAt, &repo.UpdatedAt, &repo.Version))
}

func (r *repositoryRepo) GetByID(ctx context.Context, id string) (*GitHubRepository, error) {
	repo, err := database.QueryOne[GitHubRepository](ctx, r.db.Conn(ctx),
		"SELECT * FROM github_repositories WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (r *repositoryRepo) GetByGitHubID(ctx context.Context, orgID string, githubID int64) (*GitHubRepository, error) {
	repo, err := database.QueryOne[GitHubRepository](ctx, r.db.Conn(ctx),
		"SELECT * FROM github_repositories WHERE org_id = $1 AND github_repo_id = $2", orgID, githubID)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (r *repositoryRepo) GetByFullName(ctx context.Context, orgID, fullName string) (*GitHubRepository, error) {
	repo, err := database.QueryOne[GitHubRepository](ctx, r.db.Conn(ctx),
		"SELECT * FROM github_repositories WHERE org_id = $1 AND full_name = $2", orgID, fullName)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (r *repositoryRepo) List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[GitHubRepository], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[GitHubRepository]{}, err
	}

	var sql string
	var args []any

	if cur.IsZero() {
		sql = "SELECT * FROM github_repositories WHERE org_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2"
		args = []any{orgID, req.Limit + 1}
	} else {
		sql = "SELECT * FROM github_repositories WHERE org_id = $1 AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4"
		args = []any{orgID, cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	items, err := database.QueryAll[GitHubRepository](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[GitHubRepository]{}, err
	}
	return database.BuildPage(items, req.Limit, func(r GitHubRepository) database.Cursor { return r.Cursor() }), nil
}

func (r *repositoryRepo) ListByConnection(ctx context.Context, connectionID string, req database.PageRequest) (database.Page[GitHubRepository], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[GitHubRepository]{}, err
	}

	var sql string
	var args []any

	if cur.IsZero() {
		sql = "SELECT * FROM github_repositories WHERE connection_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2"
		args = []any{connectionID, req.Limit + 1}
	} else {
		sql = "SELECT * FROM github_repositories WHERE connection_id = $1 AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4"
		args = []any{connectionID, cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	items, err := database.QueryAll[GitHubRepository](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[GitHubRepository]{}, err
	}
	return database.BuildPage(items, req.Limit, func(r GitHubRepository) database.Cursor { return r.Cursor() }), nil
}

func (r *repositoryRepo) Update(ctx context.Context, repo *GitHubRepository) error {
	const sql = `
UPDATE github_repositories
SET description = $1, html_url = $2, clone_url = $3, ssh_url = $4, default_branch = $5,
    is_private = $6, is_fork = $7, is_archived = $8, stars_count = $9, forks_count = $10,
    watchers_count = $11, open_issues_count = $12, topics = $13, language = $14, languages = $15,
    permissions = $16, last_synced_at = $17, sync_error = $18, version = version + 1, updated_at = now()
WHERE id = $19 AND version = $20`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		repo.Description, repo.HTMLURL, repo.CloneURL, repo.SSHURL, repo.DefaultBranch,
		repo.IsPrivate, repo.IsFork, repo.IsArchived, repo.StarsCount, repo.ForksCount,
		repo.WatchersCount, repo.OpenIssuesCount, topicsToJSON(repo.Topics), repo.Language, repo.Languages,
		repo.Permissions, repo.LastSyncedAt, repo.SyncError, repo.ID, repo.Version)
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
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM github_repositories WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("github repository not found")
	}
	return nil
}

func (r *repositoryRepo) UpdateSyncStatus(ctx context.Context, id string, syncError *string) error {
	_, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE github_repositories SET last_synced_at = now(), sync_error = $1, updated_at = now() WHERE id = $2",
		syncError, id)
	return database.MapError(err)
}

// ----------------------------------------------------------------------------
// Webhook Repository
// ----------------------------------------------------------------------------

type webhookRepo struct{ db *database.DB }

func NewWebhookStore(db *database.DB) WebhookStore { return &webhookRepo{db: db} }

func (r *webhookRepo) Create(ctx context.Context, w *GitHubWebhook) error {
	if w.ID == "" {
		w.ID = uuid.NewString()
	}
	if w.Status == "" {
		w.Status = WebhookStatusActive
	}

	const sql = `
INSERT INTO github_webhooks (id, org_id, repository_id, github_hook_id, events, webhook_url, secret, secret_hash, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING created_at, updated_at`

	eventsJSON, _ := json.Marshal(w.Events)
	if w.Events == nil {
		eventsJSON = []byte("[]")
	}

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		w.ID, w.OrgID, w.RepositoryID, w.GitHubHookID, eventsJSON, w.WebhookURL, w.Secret, w.SecretHash, w.Status)
	return database.MapError(row.Scan(&w.CreatedAt, &w.UpdatedAt))
}

func (r *webhookRepo) GetByID(ctx context.Context, id string) (*GitHubWebhook, error) {
	const sql = `SELECT id, org_id, repository_id, github_hook_id, events, webhook_url, secret, secret_hash,
		status, last_delivery_at, last_error, delivery_count, created_at, updated_at
		FROM github_webhooks WHERE id = $1`

	row := r.db.Conn(ctx).QueryRow(ctx, sql, id)
	return scanWebhook(row)
}

func (r *webhookRepo) GetByRepositoryID(ctx context.Context, repositoryID string) (*GitHubWebhook, error) {
	const sql = `SELECT id, org_id, repository_id, github_hook_id, events, webhook_url, secret, secret_hash,
		status, last_delivery_at, last_error, delivery_count, created_at, updated_at
		FROM github_webhooks WHERE repository_id = $1 LIMIT 1`

	row := r.db.Conn(ctx).QueryRow(ctx, sql, repositoryID)
	return scanWebhook(row)
}

func (r *webhookRepo) GetByGitHubHookID(ctx context.Context, hookID int64) (*GitHubWebhook, error) {
	const sql = `SELECT id, org_id, repository_id, github_hook_id, events, webhook_url, secret, secret_hash,
		status, last_delivery_at, last_error, delivery_count, created_at, updated_at
		FROM github_webhooks WHERE github_hook_id = $1`

	row := r.db.Conn(ctx).QueryRow(ctx, sql, hookID)
	return scanWebhook(row)
}

func scanWebhook(row pgx.Row) (*GitHubWebhook, error) {
	var w GitHubWebhook
	var eventsJSON []byte
	err := row.Scan(
		&w.ID, &w.OrgID, &w.RepositoryID, &w.GitHubHookID, &eventsJSON, &w.WebhookURL,
		&w.Secret, &w.SecretHash, &w.Status, &w.LastDeliveryAt, &w.LastError, &w.DeliveryCount,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return nil, database.MapError(err)
	}
	if len(eventsJSON) > 0 {
		_ = json.Unmarshal(eventsJSON, &w.Events)
	}
	return &w, nil
}

func (r *webhookRepo) List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[GitHubWebhook], error) {
	// Simple list without pagination for now
	const sql = `SELECT id, org_id, repository_id, github_hook_id, events, webhook_url, secret, secret_hash,
		status, last_delivery_at, last_error, delivery_count, created_at, updated_at
		FROM github_webhooks WHERE org_id = $1 ORDER BY created_at DESC`

	rows, err := r.db.Conn(ctx).Query(ctx, sql, orgID)
	if err != nil {
		return database.Page[GitHubWebhook]{}, database.MapError(err)
	}
	defer rows.Close()

	var items []GitHubWebhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return database.Page[GitHubWebhook]{}, err
		}
		items = append(items, *w)
	}

	return database.Page[GitHubWebhook]{Items: items}, nil
}

func (r *webhookRepo) Update(ctx context.Context, w *GitHubWebhook) error {
	const sql = `
UPDATE github_webhooks
SET events = $1, webhook_url = $2, status = $3, updated_at = now()
WHERE id = $4`

	eventsJSON, _ := json.Marshal(w.Events)
	if w.Events == nil {
		eventsJSON = []byte("[]")
	}

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, eventsJSON, w.WebhookURL, w.Status, w.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("github webhook not found")
	}
	return nil
}

func (r *webhookRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM github_webhooks WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("github webhook not found")
	}
	return nil
}

func (r *webhookRepo) UpdateDelivery(ctx context.Context, id string, lastError *string) error {
	status := WebhookStatusActive
	if lastError != nil && *lastError != "" {
		status = WebhookStatusFailed
	}
	_, err := r.db.Conn(ctx).Exec(ctx,
		`UPDATE github_webhooks SET last_delivery_at = now(), last_error = $1, delivery_count = delivery_count + 1,
		status = $2, updated_at = now() WHERE id = $3`,
		lastError, status, id)
	return database.MapError(err)
}

// ----------------------------------------------------------------------------
// Webhook Delivery Repository
// ----------------------------------------------------------------------------

type webhookDeliveryRepo struct{ db *database.DB }

func NewWebhookDeliveryStore(db *database.DB) WebhookDeliveryStore { return &webhookDeliveryRepo{db: db} }

func (r *webhookDeliveryRepo) Create(ctx context.Context, d *GitHubWebhookDelivery) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if d.Status == "" {
		d.Status = DeliveryStatusReceived
	}
	if len(d.Payload) == 0 {
		d.Payload = []byte("{}")
	}

	const sql = `
INSERT INTO github_webhook_deliveries (id, org_id, webhook_id, github_delivery_id, event_type, action,
    payload, headers, signature, signature_valid, status, sender_login, sender_id, repository_name, ref)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING received_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		d.ID, d.OrgID, d.WebhookID, d.GitHubDeliveryID, d.EventType, d.Action,
		d.Payload, d.Headers, d.Signature, d.SignatureValid, d.Status,
		d.SenderLogin, d.SenderID, d.RepositoryName, d.Ref)
	return database.MapError(row.Scan(&d.ReceivedAt))
}

func (r *webhookDeliveryRepo) GetByID(ctx context.Context, id string) (*GitHubWebhookDelivery, error) {
	d, err := database.QueryOne[GitHubWebhookDelivery](ctx, r.db.Conn(ctx),
		"SELECT * FROM github_webhook_deliveries WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *webhookDeliveryRepo) GetByDeliveryID(ctx context.Context, deliveryID string) (*GitHubWebhookDelivery, error) {
	d, err := database.QueryOne[GitHubWebhookDelivery](ctx, r.db.Conn(ctx),
		"SELECT * FROM github_webhook_deliveries WHERE github_delivery_id = $1", deliveryID)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *webhookDeliveryRepo) List(ctx context.Context, webhookID string, req database.PageRequest) (database.Page[GitHubWebhookDelivery], error) {
	req = req.Normalize()
	items, err := database.QueryAll[GitHubWebhookDelivery](ctx, r.db.Conn(ctx),
		"SELECT * FROM github_webhook_deliveries WHERE webhook_id = $1 ORDER BY received_at DESC LIMIT $2",
		webhookID, req.Limit+1)
	if err != nil {
		return database.Page[GitHubWebhookDelivery]{}, err
	}

	var nextCursor string
	if len(items) > req.Limit {
		items = items[:req.Limit]
		nextCursor = items[len(items)-1].ID
	}

	return database.Page[GitHubWebhookDelivery]{Items: items, NextCursor: nextCursor}, nil
}

func (r *webhookDeliveryRepo) UpdateStatus(ctx context.Context, id, status string, errorMsg *string) error {
	_, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE github_webhook_deliveries SET status = $1, error_message = $2, processed_at = now() WHERE id = $3",
		status, errorMsg, id)
	return database.MapError(err)
}

// ----------------------------------------------------------------------------
// OAuth State Repository
// ----------------------------------------------------------------------------

type oauthStateRepo struct{ db *database.DB }

func NewOAuthStateStore(db *database.DB) OAuthStateStore { return &oauthStateRepo{db: db} }

func (r *oauthStateRepo) Create(ctx context.Context, s *GitHubOAuthState) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}

	const sql = `
INSERT INTO github_oauth_states (id, org_id, user_id, state, redirect_url, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING created_at`

	expiresAt := time.Now().Add(10 * time.Minute)
	if s.ExpiresAt.IsZero() {
		s.ExpiresAt = expiresAt
	}

	row := r.db.Conn(ctx).QueryRow(ctx, sql, s.ID, s.OrgID, s.UserID, s.State, s.RedirectURL, s.ExpiresAt)
	return database.MapError(row.Scan(&s.CreatedAt))
}

func (r *oauthStateRepo) GetByState(ctx context.Context, state string) (*GitHubOAuthState, error) {
	s, err := database.QueryOne[GitHubOAuthState](ctx, r.db.Conn(ctx),
		"SELECT * FROM github_oauth_states WHERE state = $1 AND expires_at > now()", state)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *oauthStateRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM github_oauth_states WHERE id = $1", id)
	return database.MapError(err)
}

func (r *oauthStateRepo) DeleteExpired(ctx context.Context) error {
	_, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM github_oauth_states WHERE expires_at < now()")
	return database.MapError(err)
}

// Helper to convert permissions map to JSON
func permissionsToJSON(perms map[string]bool) json.RawMessage {
	if perms == nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(perms)
	return b
}

// Helper to convert topics slice to JSON
func topicsToJSON(topics []string) []byte {
	if topics == nil {
		return []byte("[]")
	}
	b, _ := json.Marshal(topics)
	return b
}
