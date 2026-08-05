-- Provisioning service schema: cloud providers, install sessions, and cluster bootstrapping.

-- ---------------------------------------------------------------------------
-- Cloud Credentials (encrypted)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS cloud_credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    name            TEXT NOT NULL,
    provider        TEXT NOT NULL,                      -- aws | azure | gcp
    
    -- Encrypted credential data
    credentials     BYTEA NOT NULL,
    
    -- Validation status
    validated       BOOLEAN NOT NULL DEFAULT false,
    validated_at    TIMESTAMPTZ,
    validation_error TEXT,
    
    -- Metadata
    region          TEXT,                               -- Default region
    description     TEXT,
    
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, name)
);

CREATE INDEX IF NOT EXISTS cloud_credentials_org_idx
    ON cloud_credentials (org_id, provider);

CREATE TRIGGER cloud_credentials_set_updated_at
    BEFORE UPDATE ON cloud_credentials
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE cloud_credentials ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cloud_credentials
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Cluster Templates (predefined configurations)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS cluster_templates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID REFERENCES organizations(id) ON DELETE CASCADE,  -- NULL = system template
    
    name            TEXT NOT NULL,
    provider        TEXT NOT NULL,                      -- aws | azure | gcp
    
    -- Configuration
    config          JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- Kubernetes version
    k8s_version     TEXT NOT NULL DEFAULT '1.28',
    
    -- Node configuration
    node_pools      JSONB NOT NULL DEFAULT '[]'::jsonb,
    
    -- Networking
    networking      JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- Add-ons
    addons          JSONB NOT NULL DEFAULT '[]'::jsonb,
    
    -- Metadata
    description     TEXT,
    is_default      BOOLEAN NOT NULL DEFAULT false,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (COALESCE(org_id, '00000000-0000-0000-0000-000000000000'::uuid), name)
);

CREATE INDEX IF NOT EXISTS cluster_templates_provider_idx
    ON cluster_templates (provider, is_default);

CREATE TRIGGER cluster_templates_set_updated_at
    BEFORE UPDATE ON cluster_templates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE cluster_templates ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cluster_templates
    USING (org_id IS NULL OR org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Provisioning Requests
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS provisioning_requests (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    -- Request details
    name            TEXT NOT NULL,
    provider        TEXT NOT NULL,
    region          TEXT NOT NULL,
    
    -- Credential reference
    credential_id   UUID REFERENCES cloud_credentials(id) ON DELETE SET NULL,
    
    -- Template reference
    template_id     UUID REFERENCES cluster_templates(id) ON DELETE SET NULL,
    
    -- Configuration overrides
    config          JSONB NOT NULL DEFAULT '{}'::jsonb,
    k8s_version     TEXT NOT NULL DEFAULT '1.28',
    node_pools      JSONB NOT NULL DEFAULT '[]'::jsonb,
    
    -- Generated Terraform
    terraform_config TEXT,
    terraform_vars   JSONB,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'pending',    -- pending | generating | ready | provisioning | completed | failed | cancelled
    
    -- Cluster link (set after successful provisioning)
    cluster_id      UUID REFERENCES clusters(id) ON DELETE SET NULL,
    
    -- Error tracking
    error_message   TEXT,
    error_details   JSONB,
    
    -- Timing
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, name)
);

CREATE INDEX IF NOT EXISTS provisioning_requests_org_idx
    ON provisioning_requests (org_id, status);

CREATE INDEX IF NOT EXISTS provisioning_requests_status_idx
    ON provisioning_requests (status) WHERE status IN ('pending', 'generating', 'provisioning');

CREATE TRIGGER provisioning_requests_set_updated_at
    BEFORE UPDATE ON provisioning_requests
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE provisioning_requests ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON provisioning_requests
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Install Sessions (tracks user provisioning progress)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS install_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    request_id      UUID NOT NULL REFERENCES provisioning_requests(id) ON DELETE CASCADE,
    
    -- Session details
    session_token   TEXT NOT NULL UNIQUE,               -- Token for CLI/agent to report progress
    
    -- Progress tracking
    current_step    TEXT NOT NULL DEFAULT 'initializing',
    total_steps     INT NOT NULL DEFAULT 0,
    completed_steps INT NOT NULL DEFAULT 0,
    
    -- Step details
    steps           JSONB NOT NULL DEFAULT '[]'::jsonb,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'active',     -- active | completed | failed | expired | cancelled
    
    -- Bootstrap info
    bootstrap_token TEXT,                               -- Token for cluster registration
    bootstrap_command TEXT,                             -- Command to run on cluster
    
    -- Agent connection
    agent_connected     BOOLEAN NOT NULL DEFAULT false,
    agent_connected_at  TIMESTAMPTZ,
    agent_version       TEXT,
    
    -- Timing
    expires_at      TIMESTAMPTZ NOT NULL,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS install_sessions_org_idx
    ON install_sessions (org_id, status);

CREATE INDEX IF NOT EXISTS install_sessions_token_idx
    ON install_sessions (session_token);

CREATE INDEX IF NOT EXISTS install_sessions_request_idx
    ON install_sessions (request_id);

CREATE TRIGGER install_sessions_set_updated_at
    BEFORE UPDATE ON install_sessions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE install_sessions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON install_sessions
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Install Session Steps (detailed step progress)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS install_session_steps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES install_sessions(id) ON DELETE CASCADE,
    
    -- Step details
    step_number     INT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'pending',    -- pending | running | completed | failed | skipped
    
    -- Output
    output          TEXT,
    error           TEXT,
    
    -- Timing
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    duration_ms     BIGINT,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (session_id, step_number)
);

CREATE INDEX IF NOT EXISTS install_session_steps_session_idx
    ON install_session_steps (session_id, step_number);

CREATE TRIGGER install_session_steps_set_updated_at
    BEFORE UPDATE ON install_session_steps
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Provisioning Events
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS provisioning_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    request_id      UUID REFERENCES provisioning_requests(id) ON DELETE CASCADE,
    session_id      UUID REFERENCES install_sessions(id) ON DELETE CASCADE,
    
    -- Event details
    event_type      TEXT NOT NULL,
    severity        TEXT NOT NULL DEFAULT 'info',
    message         TEXT NOT NULL,
    
    -- Additional data
    details         JSONB,
    
    -- Actor
    actor_type      TEXT,                               -- user | system | agent
    actor_id        TEXT,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS provisioning_events_request_idx
    ON provisioning_events (request_id, created_at DESC);

CREATE INDEX IF NOT EXISTS provisioning_events_session_idx
    ON provisioning_events (session_id, created_at DESC);

CREATE INDEX IF NOT EXISTS provisioning_events_org_idx
    ON provisioning_events (org_id, created_at DESC);

ALTER TABLE provisioning_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON provisioning_events
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Bootstrap Tokens (for cluster registration)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS bootstrap_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    request_id      UUID REFERENCES provisioning_requests(id) ON DELETE CASCADE,
    session_id      UUID REFERENCES install_sessions(id) ON DELETE CASCADE,
    
    -- Token
    token_hash      TEXT NOT NULL UNIQUE,               -- SHA256 hash of token
    
    -- Usage limits
    max_uses        INT NOT NULL DEFAULT 1,
    use_count       INT NOT NULL DEFAULT 0,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'active',     -- active | used | expired | revoked
    
    -- Expiry
    expires_at      TIMESTAMPTZ NOT NULL,
    
    -- Usage tracking
    last_used_at    TIMESTAMPTZ,
    used_by_ip      TEXT,
    
    -- Linked cluster (set after use)
    cluster_id      UUID REFERENCES clusters(id) ON DELETE SET NULL,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS bootstrap_tokens_hash_idx
    ON bootstrap_tokens (token_hash) WHERE status = 'active';

CREATE INDEX IF NOT EXISTS bootstrap_tokens_org_idx
    ON bootstrap_tokens (org_id, status);

ALTER TABLE bootstrap_tokens ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON bootstrap_tokens
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
