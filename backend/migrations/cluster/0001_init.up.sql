-- Cluster service schema: clusters, registration tokens, and heartbeat tracking.
--
-- Phase 1: Existing cluster registration only (no EKS/GKE/AKS creation).

-- ---------------------------------------------------------------------------
-- Clusters
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS clusters (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    slug            TEXT NOT NULL,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending | registering | connected | disconnected | deleted
    
    -- Inventory (populated after agent registration)
    kubernetes_version TEXT,
    node_count         INT,
    cloud_provider     TEXT,    -- aws | gcp | azure | on-prem | other
    region             TEXT,
    
    -- Agent connection
    agent_id           TEXT,                          -- Set when agent registers
    registered_at      TIMESTAMPTZ,
    last_heartbeat_at  TIMESTAMPTZ,
    
    -- Metadata
    labels             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by         UUID,
    version            BIGINT NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, slug)
);

-- Keyset pagination index.
CREATE INDEX IF NOT EXISTS clusters_org_created_idx
    ON clusters (org_id, created_at DESC, id DESC);

-- Query by status within an org.
CREATE INDEX IF NOT EXISTS clusters_org_status_idx
    ON clusters (org_id, status);

-- Find clusters needing heartbeat check.
CREATE INDEX IF NOT EXISTS clusters_heartbeat_idx
    ON clusters (last_heartbeat_at)
    WHERE status = 'connected';

CREATE TRIGGER clusters_set_updated_at
    BEFORE UPDATE ON clusters
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE clusters ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON clusters
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Cluster Registration Tokens
--
-- One-time tokens for agent registration. The plaintext token is returned
-- once at creation; only the hash is stored.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS cluster_registration_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cluster_id    UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    token_hash    TEXT NOT NULL UNIQUE,
    status        TEXT NOT NULL DEFAULT 'active',   -- active | used | revoked | expired
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ,
    used_by_agent TEXT,
    created_by    UUID,
    version       BIGINT NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only one active token per cluster.
CREATE UNIQUE INDEX IF NOT EXISTS cluster_registration_tokens_active_idx
    ON cluster_registration_tokens (cluster_id)
    WHERE status = 'active';

-- Lookup by token hash (cross-tenant, capability-based).
CREATE INDEX IF NOT EXISTS cluster_registration_tokens_hash_idx
    ON cluster_registration_tokens (token_hash);

CREATE TRIGGER cluster_registration_tokens_set_updated_at
    BEFORE UPDATE ON cluster_registration_tokens
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE cluster_registration_tokens ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cluster_registration_tokens
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Cluster Heartbeats (historical log for debugging/audit)
--
-- Current heartbeat is stored in clusters.last_heartbeat_at for efficiency.
-- This table keeps history for troubleshooting connectivity issues.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS cluster_heartbeats (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cluster_id      UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    agent_id        TEXT NOT NULL,
    
    -- Reported inventory
    kubernetes_version TEXT,
    node_count         INT,
    
    -- Health metrics
    api_server_healthy BOOLEAN NOT NULL DEFAULT true,
    
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Query recent heartbeats for a cluster.
CREATE INDEX IF NOT EXISTS cluster_heartbeats_cluster_time_idx
    ON cluster_heartbeats (cluster_id, received_at DESC);

-- Cleanup old heartbeats (keep 7 days by default).
CREATE INDEX IF NOT EXISTS cluster_heartbeats_time_idx
    ON cluster_heartbeats (received_at);

ALTER TABLE cluster_heartbeats ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cluster_heartbeats
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
