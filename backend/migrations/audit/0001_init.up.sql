-- Event-sourced, append-only audit log owned by the audit service.
--
-- The audit service consumes platform domain events (auth.*, tenant.*,
-- project.*, cluster.*, deployment.*, secret.*) and records one immutable row
-- per source event. Rows are immutable (a trigger blocks UPDATE/DELETE) and
-- unique per source event (event_id), so the framework's at-least-once delivery
-- is idempotent: a redelivered event is a no-op INSERT.
--
-- Tenant data is isolated with row-level security on org_id. org_id is TEXT (not
-- UUID) because identity events are emitted under the non-UUID "platform" org,
-- mirroring the outbox table (migrations/outbox/0002_org_id_text).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS audit_logs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id      TEXT NOT NULL,
    event_type    TEXT NOT NULL,
    org_id        TEXT NOT NULL,
    actor_id      TEXT,
    resource_type TEXT,
    resource_id   TEXT,
    occurred_at   TIMESTAMPTZ NOT NULL,
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Idempotency: a given source event is recorded at most once.
CREATE UNIQUE INDEX IF NOT EXISTS audit_logs_event_id_key
    ON audit_logs (event_id);

-- Query/filtering indexes. Listing is keyset-paginated by (created_at, id);
-- filters narrow by event type, actor, or resource within an org.
CREATE INDEX IF NOT EXISTS audit_logs_org_created_idx
    ON audit_logs (org_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS audit_logs_org_type_idx
    ON audit_logs (org_id, event_type, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_org_actor_idx
    ON audit_logs (org_id, actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_org_resource_idx
    ON audit_logs (org_id, resource_type, resource_id, created_at DESC);

-- Enforce append-only semantics: audit records are never mutated or removed.
CREATE OR REPLACE FUNCTION audit_logs_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_logs_block_mutation
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_immutable();

-- Tenant isolation. org_id is TEXT so the comparison is against the raw session
-- variable (no ::uuid cast), allowing both UUID orgs and the "platform" org.
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_logs
    USING (org_id = current_setting('app.current_org_id', true));
