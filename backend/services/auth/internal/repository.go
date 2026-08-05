package auth

import (
	"context"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// ----------------------------------------------------------------------------
// Store interfaces. The service depends on these so it can be unit-tested with
// in-memory fakes. Postgres implementations follow.
// ----------------------------------------------------------------------------

// Transactor runs a function within a database transaction. *database.DB
// satisfies it; identity flows use it to persist multiple rows atomically.
type Transactor interface {
	Tx(ctx context.Context, fn database.TxFunc) error
}

// TenantRunner runs a function within a tenant-scoped transaction so RLS applies
// to org-owned tables. *database.DB satisfies it.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// UserStore persists user identities.
type UserStore interface {
	Create(ctx context.Context, u *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, u *User) error
}

// SessionStore persists rotating refresh tokens.
type SessionStore interface {
	Create(ctx context.Context, t *RefreshToken) error
	GetByHash(ctx context.Context, hash string) (*RefreshToken, error)
	Revoke(ctx context.Context, id string, replacedBy *string) error
	RevokeAllForUser(ctx context.Context, userID string) error
}

// TokenRevoker records a revoked token or session in a fast, shared store
// (Redis) keyed by an opaque ID, with a TTL derived from expiresAt. The API
// gateway consults the same store after signature validation, so a revocation
// written here takes effect across every gateway replica before the token's
// natural expiry.
//
// *security.TokenRevocationList satisfies it. It is optional: when the auth
// service has no Redis configured the revoker is nil and the database session
// state (refresh_tokens.revoked_at) remains the durable source of truth for
// refresh/logout. Access-token cut-off before expiry then relies on Redis being
// present, which is the intended production configuration.
type TokenRevoker interface {
	Revoke(ctx context.Context, tokenID string, expiresAt time.Time) error
}

// OneTimeTokenStore persists email-verification and password-reset tokens.
type OneTimeTokenStore interface {
	Create(ctx context.Context, t *OneTimeToken) error
	GetByHash(ctx context.Context, hash string) (*OneTimeToken, error)
	MarkUsed(ctx context.Context, id string) error
	InvalidateForUser(ctx context.Context, userID, purpose string) error
}

// ServiceAccountStore persists org-scoped machine identities. Callers must run
// methods within a tenant-scoped context (TenantRunner.WithTenant).
type ServiceAccountStore interface {
	Create(ctx context.Context, sa *ServiceAccount) error
	GetByID(ctx context.Context, id string) (*ServiceAccount, error)
	List(ctx context.Context, req database.PageRequest) (database.Page[ServiceAccount], error)
	Delete(ctx context.Context, id string) error
}

// APITokenStore persists org-scoped API tokens.
type APITokenStore interface {
	Create(ctx context.Context, t *APIToken) error
	ListByOrg(ctx context.Context, req database.PageRequest) (database.Page[APIToken], error)
	Revoke(ctx context.Context, id string) error
}

// ----------------------------------------------------------------------------
// Postgres implementations.
// ----------------------------------------------------------------------------

type userRepo struct{ db *database.DB }

// NewUserStore returns a Postgres-backed UserStore.
func NewUserStore(db *database.DB) UserStore { return &userRepo{db: db} }

func (r *userRepo) Create(ctx context.Context, u *User) error {
	const sql = `
INSERT INTO users (email, name, password_hash, status, email_verified)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at, updated_at, version`
	row := r.db.Conn(ctx).QueryRow(ctx, sql, u.Email, u.Name, u.PasswordHash, u.Status, u.EmailVerified)
	if err := row.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.Version); err != nil {
		return database.MapError(err)
	}
	return nil
}

func (r *userRepo) GetByID(ctx context.Context, id string) (*User, error) {
	u, err := database.QueryOne[User](ctx, r.db.Conn(ctx), "SELECT * FROM users WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *userRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	u, err := database.QueryOne[User](ctx, r.db.Conn(ctx), "SELECT * FROM users WHERE email = $1", email)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *userRepo) Update(ctx context.Context, u *User) error {
	const sql = `
UPDATE users
SET name = $1, password_hash = $2, status = $3, email_verified = $4,
    mfa_enabled = $5, mfa_secret = $6, failed_login_attempts = $7,
    locked_until = $8, version = version + 1, updated_at = now()
WHERE id = $9 AND version = $10`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		u.Name, u.PasswordHash, u.Status, u.EmailVerified,
		u.MFAEnabled, u.MFASecret, u.FailedLoginAttempts, u.LockedUntil,
		u.ID, u.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	u.Version++
	return nil
}

type sessionRepo struct{ db *database.DB }

// NewSessionStore returns a Postgres-backed SessionStore.
func NewSessionStore(db *database.DB) SessionStore { return &sessionRepo{db: db} }

func (r *sessionRepo) Create(ctx context.Context, t *RefreshToken) error {
	const sql = `
INSERT INTO refresh_tokens (user_id, token_hash, user_agent, ip, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at`
	row := r.db.Conn(ctx).QueryRow(ctx, sql, t.UserID, t.TokenHash, t.UserAgent, t.IP, t.ExpiresAt)
	return database.MapError(row.Scan(&t.ID, &t.CreatedAt))
}

func (r *sessionRepo) GetByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	t, err := database.QueryOne[RefreshToken](ctx, r.db.Conn(ctx),
		"SELECT * FROM refresh_tokens WHERE token_hash = $1", hash)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *sessionRepo) Revoke(ctx context.Context, id string, replacedBy *string) error {
	_, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE refresh_tokens SET revoked_at = now(), replaced_by = $2 WHERE id = $1 AND revoked_at IS NULL",
		id, replacedBy)
	return database.MapError(err)
}

func (r *sessionRepo) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL",
		userID)
	return database.MapError(err)
}

type oneTimeTokenRepo struct{ db *database.DB }

// NewOneTimeTokenStore returns a Postgres-backed OneTimeTokenStore.
func NewOneTimeTokenStore(db *database.DB) OneTimeTokenStore { return &oneTimeTokenRepo{db: db} }

func (r *oneTimeTokenRepo) Create(ctx context.Context, t *OneTimeToken) error {
	const sql = `
INSERT INTO one_time_tokens (user_id, purpose, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, created_at`
	row := r.db.Conn(ctx).QueryRow(ctx, sql, t.UserID, t.Purpose, t.TokenHash, t.ExpiresAt)
	return database.MapError(row.Scan(&t.ID, &t.CreatedAt))
}

func (r *oneTimeTokenRepo) GetByHash(ctx context.Context, hash string) (*OneTimeToken, error) {
	t, err := database.QueryOne[OneTimeToken](ctx, r.db.Conn(ctx),
		"SELECT * FROM one_time_tokens WHERE token_hash = $1", hash)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *oneTimeTokenRepo) MarkUsed(ctx context.Context, id string) error {
	_, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE one_time_tokens SET used_at = now() WHERE id = $1 AND used_at IS NULL", id)
	return database.MapError(err)
}

func (r *oneTimeTokenRepo) InvalidateForUser(ctx context.Context, userID, purpose string) error {
	_, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE one_time_tokens SET used_at = now() WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL",
		userID, purpose)
	return database.MapError(err)
}

type serviceAccountRepo struct{ db *database.DB }

// NewServiceAccountStore returns a Postgres-backed ServiceAccountStore.
func NewServiceAccountStore(db *database.DB) ServiceAccountStore {
	return &serviceAccountRepo{db: db}
}

func (r *serviceAccountRepo) Create(ctx context.Context, sa *ServiceAccount) error {
	const sql = `
INSERT INTO service_accounts (org_id, name, description, status, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at, updated_at, version`
	row := r.db.Conn(ctx).QueryRow(ctx, sql, sa.OrgID, sa.Name, sa.Description, sa.Status, sa.CreatedBy)
	return database.MapError(row.Scan(&sa.ID, &sa.CreatedAt, &sa.UpdatedAt, &sa.Version))
}

func (r *serviceAccountRepo) GetByID(ctx context.Context, id string) (*ServiceAccount, error) {
	sa, err := database.QueryOne[ServiceAccount](ctx, r.db.Conn(ctx),
		"SELECT * FROM service_accounts WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &sa, nil
}

func (r *serviceAccountRepo) List(ctx context.Context, req database.PageRequest) (database.Page[ServiceAccount], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[ServiceAccount]{}, err
	}

	var (
		sql  string
		args []any
	)
	if cur.IsZero() {
		sql = "SELECT * FROM service_accounts ORDER BY created_at DESC, id DESC LIMIT $1"
		args = []any{req.Limit + 1}
	} else {
		sql = "SELECT * FROM service_accounts WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3"
		args = []any{cur.CreatedAt, cur.ID, req.Limit + 1}
	}
	items, err := database.QueryAll[ServiceAccount](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[ServiceAccount]{}, err
	}
	return database.BuildPage(items, req.Limit, func(s ServiceAccount) database.Cursor { return s.Cursor() }), nil
}

func (r *serviceAccountRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM service_accounts WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("service account not found")
	}
	return nil
}

type apiTokenRepo struct{ db *database.DB }

// NewAPITokenStore returns a Postgres-backed APITokenStore.
func NewAPITokenStore(db *database.DB) APITokenStore { return &apiTokenRepo{db: db} }

func (r *apiTokenRepo) Create(ctx context.Context, t *APIToken) error {
	const sql = `
INSERT INTO api_tokens (org_id, service_account_id, name, prefix, token_hash, scopes, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at, updated_at, version`
	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		t.OrgID, t.ServiceAccountID, t.Name, t.Prefix, t.TokenHash, t.Scopes, t.ExpiresAt)
	return database.MapError(row.Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt, &t.Version))
}

func (r *apiTokenRepo) ListByOrg(ctx context.Context, req database.PageRequest) (database.Page[APIToken], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[APIToken]{}, err
	}

	var (
		sql  string
		args []any
	)
	if cur.IsZero() {
		sql = "SELECT * FROM api_tokens ORDER BY created_at DESC, id DESC LIMIT $1"
		args = []any{req.Limit + 1}
	} else {
		sql = "SELECT * FROM api_tokens WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3"
		args = []any{cur.CreatedAt, cur.ID, req.Limit + 1}
	}
	items, err := database.QueryAll[APIToken](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[APIToken]{}, err
	}
	return database.BuildPage(items, req.Limit, func(t APIToken) database.Cursor { return t.Cursor() }), nil
}

func (r *apiTokenRepo) Revoke(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx,
		"UPDATE api_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("api token not found")
	}
	return nil
}

// scopesOrEmpty normalizes a nil scope slice so it persists as an empty array.
func scopesOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
