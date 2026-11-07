-- Phase 1: Environments and promotion requests.
--
-- Additive and non-breaking: all new columns are nullable or have defaults,
-- existing deployments gain environment_id = NULL (unbound), and the agent
-- desired-state query continues to produce Namespace="" for them exactly as
-- before — the agent's own fallback remains the source of truth for those
-- deployments until they are explicitly bound to an environment.

-- ---------------------------------------------------------------------------
-- Environments: named deployment targets within an application.
-- Each environment pins an application to a specific cluster + k8s namespace.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS environments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id)  ON DELETE CASCADE,
    application_id  UUID NOT NULL REFERENCES applications(id)  ON DELETE CASCADE,

    name            TEXT NOT NULL,      -- human label, e.g. "production", "staging"
    namespace       TEXT NOT NULL,      -- Kubernetes namespace this env deploys into
    cluster_id      UUID NOT NULL REFERENCES clusters(id) ON DELETE RESTRICT,
    order_index     INT  NOT NULL DEFAULT 0,  -- UI ordering within an application

    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (application_id, name)
);

CREATE INDEX IF NOT EXISTS environments_org_idx
    ON environments (org_id, created_at DESC);

CREATE INDEX IF NOT EXISTS environments_app_order_idx
    ON environments (application_id, order_index ASC, created_at ASC);

CREATE TRIGGER environments_set_updated_at
    BEFORE UPDATE ON environments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE environments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON environments
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Bind deployments to environments (nullable, additive).
-- NULL = unbound legacy deployment; the agent serves it with namespace="".
-- ---------------------------------------------------------------------------
ALTER TABLE deployments
    ADD COLUMN IF NOT EXISTS environment_id UUID
        REFERENCES environments(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS deployments_environment_idx
    ON deployments (environment_id, created_at DESC)
    WHERE environment_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Promotion requests: explicit approval gate for cross-environment promotion.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS promotion_requests (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id               UUID NOT NULL REFERENCES organizations(id)   ON DELETE CASCADE,

    -- Denormalized for simple per-application queries.
    application_id       UUID NOT NULL REFERENCES applications(id)    ON DELETE CASCADE,

    release_id           UUID NOT NULL REFERENCES releases(id)         ON DELETE RESTRICT,
    from_environment_id  UUID NOT NULL REFERENCES environments(id)     ON DELETE RESTRICT,
    to_environment_id    UUID NOT NULL REFERENCES environments(id)     ON DELETE RESTRICT,

    requested_by         UUID NOT NULL,

    -- Lifecycle: pending → approved/rejected; approved → applied.
    status               TEXT NOT NULL DEFAULT 'pending',

    approved_by          UUID,
    approved_at          TIMESTAMPTZ,
    applied_at           TIMESTAMPTZ,
    rejection_reason     TEXT,

    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS promotion_requests_org_idx
    ON promotion_requests (org_id, created_at DESC);

CREATE INDEX IF NOT EXISTS promotion_requests_app_idx
    ON promotion_requests (application_id, created_at DESC);

-- Fast lookup of pending approvals.
CREATE INDEX IF NOT EXISTS promotion_requests_pending_idx
    ON promotion_requests (org_id, status)
    WHERE status = 'pending';

CREATE TRIGGER promotion_requests_set_updated_at
    BEFORE UPDATE ON promotion_requests
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE promotion_requests ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON promotion_requests
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
