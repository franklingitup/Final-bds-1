-- Argo CD Applications: the GitOps binding for a deployment.
--
-- When a deployment is managed by GitOps, the Deployment Service owns one Argo
-- CD Application per deployment. This table stores the desired source (git repo,
-- path, revision, tool), the destination cluster/namespace, the sync policy, and
-- the last observed Argo CD status (sync/health/operation/revision). A background
-- monitor reconciles this row to Argo CD and mirrors Argo CD's status back here.
--
-- Additive and backward compatible: no existing table is modified. Deployments
-- without an argo_applications row keep the pre-existing agent-driven behaviour.

CREATE TABLE IF NOT EXISTS argo_applications (
    deployment_id     UUID PRIMARY KEY REFERENCES deployments(id) ON DELETE CASCADE,
    org_id            UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    -- Argo CD Application name (unique across the Argo CD instance) and project.
    app_name          TEXT NOT NULL,
    project           TEXT NOT NULL DEFAULT 'default',

    -- Desired source.
    repo_url          TEXT NOT NULL,
    path              TEXT NOT NULL DEFAULT '.',
    target_revision   TEXT NOT NULL DEFAULT 'HEAD',
    -- directory | helm | kustomize
    source_type       TEXT NOT NULL DEFAULT 'directory',

    -- Destination cluster and namespace.
    dest_server       TEXT NOT NULL DEFAULT 'https://kubernetes.default.svc',
    dest_namespace    TEXT NOT NULL,

    -- Sync policy.
    auto_sync         BOOLEAN NOT NULL DEFAULT true,
    self_heal         BOOLEAN NOT NULL DEFAULT true,
    prune             BOOLEAN NOT NULL DEFAULT true,

    -- Observed Argo CD status (mirrored by the monitor).
    -- sync_status:   Synced | OutOfSync | Unknown
    -- health_status: Healthy | Progressing | Degraded | Suspended | Missing | Unknown
    sync_status       TEXT NOT NULL DEFAULT 'Unknown',
    health_status     TEXT NOT NULL DEFAULT 'Unknown',
    operation_phase   TEXT NOT NULL DEFAULT '',
    synced_revision   TEXT NOT NULL DEFAULT '',
    -- True when Argo CD reports OutOfSync or a degraded resource tree.
    drift             BOOLEAN NOT NULL DEFAULT false,
    observed_at       TIMESTAMPTZ,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- One Argo CD Application name per tenant.
    UNIQUE (org_id, app_name)
);

CREATE INDEX IF NOT EXISTS argo_applications_org_idx
    ON argo_applications (org_id);

-- The monitor scans applications ordered by staleness of the last observation.
CREATE INDEX IF NOT EXISTS argo_applications_observed_idx
    ON argo_applications (observed_at NULLS FIRST);

CREATE TRIGGER argo_applications_set_updated_at
    BEFORE UPDATE ON argo_applications
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE argo_applications ENABLE ROW LEVEL SECURITY;
ALTER TABLE argo_applications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON argo_applications
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
