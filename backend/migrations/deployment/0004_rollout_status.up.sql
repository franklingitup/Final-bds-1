-- Rollout status: the deployment engine's per-release rollout snapshot.
--
-- The platform agent continuously reports rich rollout progress (state machine
-- phase, replica counts, conditions, rollout percentage) for the release it is
-- reconciling. This table stores the latest snapshot per (deployment, release)
-- so the Deployment Service can drive its state machine, emit engine events,
-- and expose progress without changing the existing deployments/releases rows.
--
-- Additive and backward compatible: no existing table is modified.

CREATE TABLE IF NOT EXISTS rollout_status (
    deployment_id        UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    release_id           UUID NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    org_id               UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    -- Rollout state machine phase:
    -- Pending | Scheduling | Reconciling | RollingOut | Healthy | Failed | Rollback
    phase                TEXT NOT NULL DEFAULT 'Pending',

    revision             INT NOT NULL DEFAULT 0,
    image                TEXT NOT NULL DEFAULT '',

    -- Replica accounting mirrored from the Kubernetes Deployment status.
    desired_replicas     INT NOT NULL DEFAULT 0,
    ready_replicas       INT NOT NULL DEFAULT 0,
    updated_replicas     INT NOT NULL DEFAULT 0,
    available_replicas   INT NOT NULL DEFAULT 0,
    unavailable_replicas INT NOT NULL DEFAULT 0,

    observed_generation  BIGINT NOT NULL DEFAULT 0,
    rollout_percentage   INT NOT NULL DEFAULT 0,

    -- Verbatim Deployment conditions (Progressing / Available / ReplicaFailure).
    conditions           JSONB NOT NULL DEFAULT '[]'::jsonb,
    error_message        TEXT,

    -- Set when this release was created by an automatic rollback so the engine
    -- can emit deployment.rollback.completed when it becomes healthy.
    is_rollback          BOOLEAN NOT NULL DEFAULT false,

    started_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (deployment_id, release_id)
);

CREATE INDEX IF NOT EXISTS rollout_status_org_idx
    ON rollout_status (org_id);

CREATE INDEX IF NOT EXISTS rollout_status_deployment_idx
    ON rollout_status (deployment_id, updated_at DESC);

CREATE TRIGGER rollout_status_set_updated_at
    BEFORE UPDATE ON rollout_status
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE rollout_status ENABLE ROW LEVEL SECURITY;
ALTER TABLE rollout_status FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON rollout_status
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
