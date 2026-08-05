-- GitHub integration schema: connections, webhooks, and repository cache.

-- ---------------------------------------------------------------------------
-- GitHub Connections (OAuth or PAT)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS github_connections (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    -- Connection type
    connection_type TEXT NOT NULL DEFAULT 'pat',  -- oauth | pat | app
    name            TEXT NOT NULL,
    
    -- GitHub user info (populated after connection)
    github_user_id  BIGINT,
    github_username TEXT,
    github_avatar   TEXT,
    
    -- OAuth tokens (encrypted)
    access_token    BYTEA,                -- Encrypted access token
    refresh_token   BYTEA,                -- Encrypted refresh token (for OAuth)
    token_expires_at TIMESTAMPTZ,
    scopes          JSONB DEFAULT '[]'::jsonb,  -- Granted scopes
    
    -- Token metadata
    token_hash      TEXT,                 -- SHA-256 of token for change detection
    last_used_at    TIMESTAMPTZ,
    last_validated_at TIMESTAMPTZ,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'active',  -- active | revoked | expired | invalid
    error_message   TEXT,
    
    created_by      UUID,
    version         BIGINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, name)
);

CREATE INDEX IF NOT EXISTS github_connections_org_idx
    ON github_connections (org_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS github_connections_user_idx
    ON github_connections (github_user_id);

CREATE TRIGGER github_connections_set_updated_at
    BEFORE UPDATE ON github_connections
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE github_connections ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON github_connections
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- GitHub Repositories (cached metadata)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS github_repositories (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id    UUID NOT NULL REFERENCES github_connections(id) ON DELETE CASCADE,
    
    -- Repository identifiers
    github_repo_id   BIGINT NOT NULL,
    owner            TEXT NOT NULL,
    name             TEXT NOT NULL,
    full_name        TEXT NOT NULL,       -- owner/name
    
    -- Repository metadata
    description      TEXT,
    html_url         TEXT NOT NULL,
    clone_url        TEXT NOT NULL,
    ssh_url          TEXT,
    default_branch   TEXT NOT NULL DEFAULT 'main',
    is_private       BOOLEAN NOT NULL DEFAULT false,
    is_fork          BOOLEAN NOT NULL DEFAULT false,
    is_archived      BOOLEAN NOT NULL DEFAULT false,
    
    -- Statistics
    stars_count      INT DEFAULT 0,
    forks_count      INT DEFAULT 0,
    watchers_count   INT DEFAULT 0,
    open_issues_count INT DEFAULT 0,
    
    -- Topics/languages
    topics           JSONB DEFAULT '[]'::jsonb,
    language         TEXT,
    languages        JSONB DEFAULT '{}'::jsonb,
    
    -- Permissions (what can we do with this repo)
    permissions      JSONB DEFAULT '{}'::jsonb,
    
    -- Sync status
    last_synced_at   TIMESTAMPTZ,
    sync_error       TEXT,
    
    created_by       UUID,
    version          BIGINT NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, github_repo_id)
);

CREATE INDEX IF NOT EXISTS github_repositories_org_idx
    ON github_repositories (org_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS github_repositories_connection_idx
    ON github_repositories (connection_id);

CREATE INDEX IF NOT EXISTS github_repositories_full_name_idx
    ON github_repositories (full_name);

CREATE TRIGGER github_repositories_set_updated_at
    BEFORE UPDATE ON github_repositories
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE github_repositories ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON github_repositories
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- GitHub Webhooks
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS github_webhooks (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    repository_id    UUID NOT NULL REFERENCES github_repositories(id) ON DELETE CASCADE,
    
    -- Webhook identifiers
    github_hook_id   BIGINT NOT NULL,
    
    -- Webhook configuration
    events           JSONB NOT NULL DEFAULT '["push", "pull_request"]'::jsonb,
    webhook_url      TEXT NOT NULL,
    secret           BYTEA NOT NULL,      -- Encrypted webhook secret
    secret_hash      TEXT NOT NULL,       -- For verification
    
    -- Status
    status           TEXT NOT NULL DEFAULT 'active',  -- active | inactive | failed
    last_delivery_at TIMESTAMPTZ,
    last_error       TEXT,
    delivery_count   INT DEFAULT 0,
    
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (repository_id, github_hook_id)
);

CREATE INDEX IF NOT EXISTS github_webhooks_repository_idx
    ON github_webhooks (repository_id);

CREATE INDEX IF NOT EXISTS github_webhooks_org_idx
    ON github_webhooks (org_id);

CREATE TRIGGER github_webhooks_set_updated_at
    BEFORE UPDATE ON github_webhooks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE github_webhooks ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON github_webhooks
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- GitHub Webhook Deliveries (audit log)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS github_webhook_deliveries (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    webhook_id       UUID NOT NULL REFERENCES github_webhooks(id) ON DELETE CASCADE,
    
    -- Delivery metadata
    github_delivery_id TEXT NOT NULL,
    event_type       TEXT NOT NULL,       -- push, pull_request, etc.
    action           TEXT,                -- opened, closed, synchronize, etc.
    
    -- Payload (stored for debugging/replay)
    payload          JSONB NOT NULL,
    headers          JSONB,
    
    -- Signature verification
    signature        TEXT,
    signature_valid  BOOLEAN NOT NULL DEFAULT false,
    
    -- Processing status
    status           TEXT NOT NULL DEFAULT 'received',  -- received | processed | failed | ignored
    processed_at     TIMESTAMPTZ,
    error_message    TEXT,
    
    -- Source info
    sender_login     TEXT,
    sender_id        BIGINT,
    repository_name  TEXT,
    ref              TEXT,                -- For push events: refs/heads/main
    
    received_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS github_webhook_deliveries_webhook_idx
    ON github_webhook_deliveries (webhook_id, received_at DESC);

CREATE INDEX IF NOT EXISTS github_webhook_deliveries_org_idx
    ON github_webhook_deliveries (org_id, received_at DESC);

CREATE INDEX IF NOT EXISTS github_webhook_deliveries_delivery_id_idx
    ON github_webhook_deliveries (github_delivery_id);

ALTER TABLE github_webhook_deliveries ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON github_webhook_deliveries
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- OAuth State (for CSRF protection during OAuth flow)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS github_oauth_states (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id          UUID NOT NULL,
    
    state            TEXT NOT NULL UNIQUE,
    redirect_url     TEXT,
    
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at       TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '10 minutes')
);

CREATE INDEX IF NOT EXISTS github_oauth_states_state_idx
    ON github_oauth_states (state);

CREATE INDEX IF NOT EXISTS github_oauth_states_expires_idx
    ON github_oauth_states (expires_at);
