-- Auth service schema.
--
-- Identity tables (users, refresh_tokens, one_time_tokens) are GLOBAL: a user is
-- a single identity that may belong to many organizations, so they are not
-- tenant-scoped and have no RLS. Machine-identity tables (service_accounts,
-- api_tokens) are org-scoped and enforce row-level security.
-- See docs/06-security-design.md sections 1 and 2.

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

-- Shared updated_at trigger (idempotent; also created by the tenant migration).
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- Users
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email                 CITEXT NOT NULL UNIQUE,
    name                  TEXT NOT NULL,
    password_hash         TEXT NOT NULL,
    status                TEXT NOT NULL DEFAULT 'active', -- active | locked | disabled
    email_verified        BOOLEAN NOT NULL DEFAULT false,
    mfa_enabled           BOOLEAN NOT NULL DEFAULT false,
    mfa_secret            TEXT,                            -- base32 TOTP secret (null until setup)
    failed_login_attempts INT NOT NULL DEFAULT 0,
    locked_until          TIMESTAMPTZ,
    version               BIGINT NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Refresh tokens (rotating sessions). Only the SHA-256 hash is stored.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    user_agent  TEXT,
    ip          TEXT,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    replaced_by UUID,                                     -- set on rotation
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS refresh_tokens_user_idx ON refresh_tokens (user_id);

-- ---------------------------------------------------------------------------
-- One-time tokens for email verification and password reset (hash-stored).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS one_time_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose    TEXT NOT NULL,                             -- email_verify | password_reset
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS one_time_tokens_user_purpose_idx
    ON one_time_tokens (user_id, purpose);

-- ---------------------------------------------------------------------------
-- Service accounts (machine identities), org-scoped with RLS.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS service_accounts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL,
    name        TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL DEFAULT 'active',           -- active | disabled
    created_by  UUID,
    version     BIGINT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE INDEX IF NOT EXISTS service_accounts_org_created_idx
    ON service_accounts (org_id, created_at DESC, id DESC);

CREATE TRIGGER service_accounts_set_updated_at
    BEFORE UPDATE ON service_accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE service_accounts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON service_accounts
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- API tokens, owned by a service account and org-scoped with RLS. Only the
-- SHA-256 hash is stored; the plaintext token is shown once at creation.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS api_tokens (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id             UUID NOT NULL,
    service_account_id UUID NOT NULL REFERENCES service_accounts(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    prefix             TEXT NOT NULL,                     -- non-secret display prefix
    token_hash         TEXT NOT NULL UNIQUE,
    scopes             TEXT[] NOT NULL DEFAULT '{}',
    expires_at         TIMESTAMPTZ,
    last_used_at       TIMESTAMPTZ,
    revoked_at         TIMESTAMPTZ,
    version            BIGINT NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS api_tokens_org_created_idx
    ON api_tokens (org_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS api_tokens_sa_idx
    ON api_tokens (service_account_id);

CREATE TRIGGER api_tokens_set_updated_at
    BEFORE UPDATE ON api_tokens
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE api_tokens ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON api_tokens
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
