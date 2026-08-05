-- Deployment pipeline schema: desired state, pipeline runs, and agent sync.

-- ---------------------------------------------------------------------------
-- Desired State (Kubernetes manifests for agent to apply)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS desired_states (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    deployment_id  UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    release_id     UUID NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    cluster_id     UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    
    -- Generated manifests
    namespace      TEXT NOT NULL,
    manifests      JSONB NOT NULL,           -- Array of K8s manifests
    manifest_hash  TEXT NOT NULL,            -- Hash for change detection
    
    -- Agent sync status
    sync_status    TEXT NOT NULL DEFAULT 'pending',  -- pending | syncing | synced | failed | stale
    last_synced_at TIMESTAMPTZ,
    last_sync_error TEXT,
    
    -- Generation tracking
    generation     BIGINT NOT NULL DEFAULT 1,
    observed_generation BIGINT DEFAULT 0,    -- What the agent has applied
    
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (deployment_id)  -- One active desired state per deployment
);

CREATE INDEX IF NOT EXISTS desired_states_cluster_idx
    ON desired_states (cluster_id, sync_status);

CREATE INDEX IF NOT EXISTS desired_states_org_idx
    ON desired_states (org_id);

CREATE TRIGGER desired_states_set_updated_at
    BEFORE UPDATE ON desired_states
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE desired_states ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON desired_states
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Pipeline Runs (orchestrates build -> deploy)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS pipeline_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    deployment_id   UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    release_id      UUID REFERENCES releases(id) ON DELETE SET NULL,
    
    -- Pipeline configuration
    source_type     TEXT NOT NULL DEFAULT 'image',  -- image | git | build
    source_ref      TEXT NOT NULL,                   -- image:tag or git ref or build id
    
    -- Build integration (optional)
    build_id        UUID,                            -- Reference to build service
    build_status    TEXT,                            -- queued | building | succeeded | failed
    built_image     TEXT,                            -- Resulting image from build
    
    -- Pipeline state
    status          TEXT NOT NULL DEFAULT 'pending', -- pending | building | deploying | succeeded | failed | cancelled
    current_stage   TEXT NOT NULL DEFAULT 'init',    -- init | build | release | deploy | done
    
    -- Timing
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    
    -- Error tracking
    error_message   TEXT,
    error_stage     TEXT,
    
    -- Metadata
    triggered_by    TEXT NOT NULL DEFAULT 'user',    -- user | webhook | schedule | rollback
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS pipeline_runs_deployment_idx
    ON pipeline_runs (deployment_id, created_at DESC);

CREATE INDEX IF NOT EXISTS pipeline_runs_org_idx
    ON pipeline_runs (org_id, created_at DESC);

CREATE INDEX IF NOT EXISTS pipeline_runs_status_idx
    ON pipeline_runs (status) WHERE status IN ('pending', 'building', 'deploying');

CREATE TRIGGER pipeline_runs_set_updated_at
    BEFORE UPDATE ON pipeline_runs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE pipeline_runs ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pipeline_runs
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Pipeline Events (detailed history)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS pipeline_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    pipeline_run_id UUID NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    
    event_type      TEXT NOT NULL,            -- stage_started | stage_completed | stage_failed | status_changed
    stage           TEXT,                     -- init | build | release | deploy
    message         TEXT NOT NULL,
    details         JSONB,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS pipeline_events_run_idx
    ON pipeline_events (pipeline_run_id, created_at);

ALTER TABLE pipeline_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pipeline_events
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Deployment Metrics (for observability)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS deployment_metrics (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    deployment_id   UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    
    -- Pod status
    available_replicas INT NOT NULL DEFAULT 0,
    ready_replicas     INT NOT NULL DEFAULT 0,
    updated_replicas   INT NOT NULL DEFAULT 0,
    
    -- Resource usage (from metrics-server)
    cpu_usage_millicores  BIGINT,
    memory_usage_bytes    BIGINT,
    
    -- Health
    health_status   TEXT NOT NULL DEFAULT 'unknown',  -- healthy | degraded | unhealthy | unknown
    last_health_check TIMESTAMPTZ,
    
    -- Collected at
    collected_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (deployment_id)  -- One record per deployment (upsert)
);

CREATE INDEX IF NOT EXISTS deployment_metrics_org_idx
    ON deployment_metrics (org_id);

ALTER TABLE deployment_metrics ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON deployment_metrics
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
