-- Deployment service schema: applications, deployments, and releases.

-- ---------------------------------------------------------------------------
-- Applications
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS applications (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id   UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    slug         TEXT NOT NULL,
    description  TEXT,
    runtime_type TEXT NOT NULL DEFAULT 'container',  -- container | function | job
    
    created_by   UUID,
    version      BIGINT NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (project_id, slug)
);

CREATE INDEX IF NOT EXISTS applications_org_created_idx
    ON applications (org_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS applications_project_idx
    ON applications (project_id, created_at DESC);

CREATE TRIGGER applications_set_updated_at
    BEFORE UPDATE ON applications
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE applications ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON applications
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Deployments
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS deployments (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    cluster_id     UUID NOT NULL REFERENCES clusters(id) ON DELETE RESTRICT,
    
    -- Configuration
    image          TEXT NOT NULL,
    replicas       INT NOT NULL DEFAULT 1,
    cpu_request    TEXT,                    -- e.g., "100m"
    cpu_limit      TEXT,                    -- e.g., "500m"
    memory_request TEXT,                    -- e.g., "128Mi"
    memory_limit   TEXT,                    -- e.g., "512Mi"
    port           INT,                     -- Container port
    env_vars       JSONB NOT NULL DEFAULT '[]'::jsonb,
    
    -- Status
    status         TEXT NOT NULL DEFAULT 'pending',  -- pending | running | succeeded | failed | rolled_back
    
    -- Rollout tracking
    ready_replicas   INT NOT NULL DEFAULT 0,
    desired_replicas INT NOT NULL DEFAULT 1,
    
    created_by     UUID,
    version        BIGINT NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS deployments_org_created_idx
    ON deployments (org_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS deployments_app_created_idx
    ON deployments (application_id, created_at DESC);

CREATE INDEX IF NOT EXISTS deployments_cluster_idx
    ON deployments (cluster_id, status);

CREATE TRIGGER deployments_set_updated_at
    BEFORE UPDATE ON deployments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE deployments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON deployments
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Releases (immutable deployment snapshots)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS releases (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    deployment_id  UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    
    revision       INT NOT NULL,
    image          TEXT NOT NULL,
    replicas       INT NOT NULL,
    config_hash    TEXT NOT NULL,           -- Hash of full config for change detection
    config         JSONB NOT NULL,          -- Full config snapshot
    
    status         TEXT NOT NULL DEFAULT 'pending',  -- pending | deploying | succeeded | failed | rolled_back
    
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    error_message  TEXT,
    
    created_by     UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (deployment_id, revision)
);

CREATE INDEX IF NOT EXISTS releases_org_created_idx
    ON releases (org_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS releases_deployment_rev_idx
    ON releases (deployment_id, revision DESC);

ALTER TABLE releases ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON releases
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- Prevent updates/deletes on releases (immutable)
CREATE OR REPLACE FUNCTION prevent_release_modification()
RETURNS TRIGGER AS $$
BEGIN
    -- Allow status updates only
    IF TG_OP = 'UPDATE' THEN
        IF OLD.revision != NEW.revision OR OLD.image != NEW.image OR OLD.config != NEW.config THEN
            RAISE EXCEPTION 'Releases are immutable; only status fields may be updated';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'Releases are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER releases_immutable
    BEFORE UPDATE OR DELETE ON releases
    FOR EACH ROW EXECUTE FUNCTION prevent_release_modification();
