-- Build service schema: builds, build_logs, build_artifacts.

-- ---------------------------------------------------------------------------
-- Git Repositories
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS git_repositories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id      UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    
    name            TEXT NOT NULL,
    url             TEXT NOT NULL,            -- https://github.com/org/repo.git
    default_branch  TEXT NOT NULL DEFAULT 'main',
    
    -- Credentials (encrypted at rest)
    auth_type       TEXT NOT NULL DEFAULT 'none',  -- none | token | ssh_key | deploy_key
    auth_secret_id  UUID,                     -- Reference to secrets service
    
    -- Webhook configuration
    webhook_secret  TEXT,
    
    created_by      UUID,
    version         BIGINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, project_id, name)
);

CREATE INDEX IF NOT EXISTS git_repositories_org_idx
    ON git_repositories (org_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS git_repositories_project_idx
    ON git_repositories (project_id);

CREATE TRIGGER git_repositories_set_updated_at
    BEFORE UPDATE ON git_repositories
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE git_repositories ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON git_repositories
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Builds
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS builds (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    -- Source configuration
    repository_id   UUID REFERENCES git_repositories(id) ON DELETE SET NULL,
    git_url         TEXT,                     -- Direct URL if no repository linked
    git_ref         TEXT NOT NULL DEFAULT 'main',  -- branch, tag, or commit
    git_commit      TEXT,                     -- Resolved commit SHA
    
    -- Build context
    context_path    TEXT NOT NULL DEFAULT '.',     -- Subpath in repository
    dockerfile_path TEXT NOT NULL DEFAULT 'Dockerfile',
    build_args      JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- Target configuration
    target_image    TEXT NOT NULL,            -- Full image:tag
    target_registry TEXT NOT NULL,            -- Registry host
    push_to_registry BOOLEAN NOT NULL DEFAULT true,
    
    -- Build engine
    builder_type    TEXT NOT NULL DEFAULT 'kaniko',  -- kaniko | buildkit
    
    -- Status tracking
    status          TEXT NOT NULL DEFAULT 'queued',  -- queued | cloning | building | pushing | succeeded | failed | cancelled
    
    -- Timing
    queued_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    
    -- Error handling
    error_message   TEXT,
    retry_count     INT NOT NULL DEFAULT 0,
    max_retries     INT NOT NULL DEFAULT 3,
    parent_build_id UUID REFERENCES builds(id),  -- For retried builds
    
    -- Resource limits
    cpu_limit       TEXT DEFAULT '2',         -- CPU cores
    memory_limit    TEXT DEFAULT '4Gi',       -- Memory
    timeout_seconds INT NOT NULL DEFAULT 1800, -- 30 minutes default
    
    -- Audit
    created_by      UUID,
    cancelled_by    UUID,
    
    version         BIGINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS builds_org_created_idx
    ON builds (org_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS builds_status_idx
    ON builds (status, queued_at);

CREATE INDEX IF NOT EXISTS builds_repository_idx
    ON builds (repository_id, created_at DESC);

CREATE INDEX IF NOT EXISTS builds_parent_idx
    ON builds (parent_build_id);

CREATE TRIGGER builds_set_updated_at
    BEFORE UPDATE ON builds
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE builds ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON builds
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Build Logs
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS build_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    build_id        UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    
    sequence        INT NOT NULL,             -- Ordering within build
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT now(),
    stream          TEXT NOT NULL DEFAULT 'stdout',  -- stdout | stderr | system
    level           TEXT NOT NULL DEFAULT 'info',    -- debug | info | warn | error
    message         TEXT NOT NULL,
    
    -- Structured data
    metadata        JSONB DEFAULT '{}'::jsonb,
    
    UNIQUE (build_id, sequence)
);

CREATE INDEX IF NOT EXISTS build_logs_build_seq_idx
    ON build_logs (build_id, sequence);

CREATE INDEX IF NOT EXISTS build_logs_org_idx
    ON build_logs (org_id);

ALTER TABLE build_logs ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON build_logs
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Build Artifacts
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS build_artifacts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    build_id        UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    
    -- Image metadata
    image_digest    TEXT NOT NULL,            -- sha256:...
    image_tag       TEXT NOT NULL,
    image_size      BIGINT,                   -- Size in bytes
    
    -- Manifest
    manifest_type   TEXT NOT NULL DEFAULT 'docker',  -- docker | oci
    manifest        JSONB,                    -- Full manifest if stored
    
    -- Layers
    layer_count     INT,
    layers          JSONB,                    -- Array of layer digests
    
    -- Build metadata
    dockerfile_hash TEXT,
    build_duration_ms BIGINT,
    
    -- Labels from image
    labels          JSONB DEFAULT '{}'::jsonb,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (build_id, image_digest)
);

CREATE INDEX IF NOT EXISTS build_artifacts_build_idx
    ON build_artifacts (build_id);

CREATE INDEX IF NOT EXISTS build_artifacts_org_idx
    ON build_artifacts (org_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS build_artifacts_digest_idx
    ON build_artifacts (image_digest);

ALTER TABLE build_artifacts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON build_artifacts
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Build Queue (for worker coordination)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS build_queue (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    build_id        UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE UNIQUE,
    priority        INT NOT NULL DEFAULT 0,   -- Higher = more urgent
    
    -- Worker assignment
    worker_id       TEXT,
    claimed_at      TIMESTAMPTZ,
    heartbeat_at    TIMESTAMPTZ,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS build_queue_pending_idx
    ON build_queue (priority DESC, created_at) WHERE worker_id IS NULL;

CREATE INDEX IF NOT EXISTS build_queue_worker_idx
    ON build_queue (worker_id, heartbeat_at) WHERE worker_id IS NOT NULL;
