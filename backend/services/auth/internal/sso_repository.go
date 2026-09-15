package auth

import (
	"context"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// SSOOrganizationStore resolves the public organization slug used to initiate
// SSO. Implementations must not expose any other organization data.
type SSOOrganizationStore interface {
	GetIDBySlug(ctx context.Context, slug string) (string, error)
}

// SSOMemberStore performs idempotent JIT membership provisioning. Calls must
// run inside TenantRunner.WithTenant for the target organization.
type SSOMemberStore interface {
	Ensure(ctx context.Context, userID string, role authz.OrgRole) error
}

// SSOHandoffStore persists short-lived, single-use SAML request state and
// browser exchange codes. Calls must run in tenant context.
type SSOHandoffStore interface {
	CreateLoginState(ctx context.Context, state *SSOLoginState) error
	ConsumeLoginState(ctx context.Context, orgID, stateHash string, now time.Time) (*SSOLoginState, error)
	CreateExchangeCode(ctx context.Context, code *SSOExchangeCode) error
	ConsumeExchangeCode(ctx context.Context, orgID, codeHash string, now time.Time) (*SSOExchangeCode, error)
}

// SSOLoginState binds RelayState to the exact AuthnRequest and organization.
type SSOLoginState struct {
	OrgID     string
	StateHash string
	RequestID string
	ExpiresAt time.Time
}

// SSOExchangeCode maps an opaque one-time code to a user until one redemption.
// Tokens are not stored; they are minted at exchange time.
type SSOExchangeCode struct {
	OrgID     string
	UserID    string
	CodeHash  string
	ExpiresAt time.Time
}

type ssoOrganizationRepo struct{ db *database.DB }

// NewSSOOrganizationStore returns the minimal cross-tenant organization lookup
// needed by the public SSO login route.
func NewSSOOrganizationStore(db *database.DB) SSOOrganizationStore {
	return &ssoOrganizationRepo{db: db}
}

func (r *ssoOrganizationRepo) GetIDBySlug(ctx context.Context, slug string) (string, error) {
	var id string
	if err := r.db.Pool.QueryRow(ctx,
		"SELECT id FROM organizations WHERE slug = $1 AND status = 'active'", slug).Scan(&id); err != nil {
		return "", database.MapError(err)
	}
	return id, nil
}

type ssoMemberRepo struct{ db *database.DB }

// NewSSOMemberStore returns the JIT membership store.
func NewSSOMemberStore(db *database.DB) SSOMemberStore { return &ssoMemberRepo{db: db} }

func (r *ssoMemberRepo) Ensure(ctx context.Context, userID string, role authz.OrgRole) error {
	_, err := r.db.Conn(ctx).Exec(ctx, `
INSERT INTO organization_members (org_id, user_id, role, status)
VALUES (current_setting('app.current_org_id')::uuid, $1, $2, 'active')
ON CONFLICT (org_id, user_id) DO NOTHING`, userID, role)
	return database.MapError(err)
}

type ssoHandoffRepo struct{ db *database.DB }

// NewSSOHandoffStore returns the Postgres-backed SAML handoff store.
func NewSSOHandoffStore(db *database.DB) SSOHandoffStore { return &ssoHandoffRepo{db: db} }

func (r *ssoHandoffRepo) CreateLoginState(ctx context.Context, state *SSOLoginState) error {
	_, err := r.db.Conn(ctx).Exec(ctx, `
INSERT INTO sso_login_states (org_id, state_hash, request_id, expires_at)
VALUES ($1, $2, $3, $4)`,
		state.OrgID, state.StateHash, state.RequestID, state.ExpiresAt)
	return database.MapError(err)
}

func (r *ssoHandoffRepo) ConsumeLoginState(ctx context.Context, orgID, stateHash string, now time.Time) (*SSOLoginState, error) {
	var state SSOLoginState
	err := r.db.Conn(ctx).QueryRow(ctx, `
UPDATE sso_login_states
SET used_at = $3
WHERE org_id = $1 AND state_hash = $2 AND used_at IS NULL AND expires_at > $3
RETURNING org_id, state_hash, request_id, expires_at`,
		orgID, stateHash, now).Scan(&state.OrgID, &state.StateHash, &state.RequestID, &state.ExpiresAt)
	if err != nil {
		mapped := database.MapError(err)
		if database.IsNotFound(mapped) {
			return nil, apperrors.Unauthorized("invalid or expired SSO state")
		}
		return nil, mapped
	}
	return &state, nil
}

func (r *ssoHandoffRepo) CreateExchangeCode(ctx context.Context, code *SSOExchangeCode) error {
	_, err := r.db.Conn(ctx).Exec(ctx, `
INSERT INTO sso_exchange_codes (org_id, user_id, code_hash, expires_at)
VALUES ($1, $2, $3, $4)`,
		code.OrgID, code.UserID, code.CodeHash, code.ExpiresAt)
	return database.MapError(err)
}

func (r *ssoHandoffRepo) ConsumeExchangeCode(ctx context.Context, orgID, codeHash string, now time.Time) (*SSOExchangeCode, error) {
	var code SSOExchangeCode
	err := r.db.Conn(ctx).QueryRow(ctx, `
UPDATE sso_exchange_codes
SET used_at = $3
WHERE org_id = $1 AND code_hash = $2 AND used_at IS NULL AND expires_at > $3
RETURNING org_id, user_id, code_hash, expires_at`,
		orgID, codeHash, now).Scan(&code.OrgID, &code.UserID, &code.CodeHash, &code.ExpiresAt)
	if err != nil {
		mapped := database.MapError(err)
		if database.IsNotFound(mapped) {
			return nil, apperrors.Unauthorized("invalid or expired SSO exchange code")
		}
		return nil, mapped
	}
	return &code, nil
}
