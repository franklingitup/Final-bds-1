-- Build -> Deployment consumer idempotency ledger.
--
-- Records which events a durable deployment consumer has already processed so
-- that at-least-once event redelivery is idempotent (a redelivered
-- build.succeeded event must not create duplicate releases). The row is written
-- in the same transaction as the release/deployment changes it guards, so the
-- dedup marker and the work commit atomically.

CREATE TABLE IF NOT EXISTS deployment_processed_events (
    consumer     TEXT        NOT NULL,           -- durable consumer name
    event_id     TEXT        NOT NULL,           -- source event envelope id
    org_id       UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (consumer, event_id)
);

CREATE INDEX IF NOT EXISTS deployment_processed_events_org_idx
    ON deployment_processed_events (org_id, processed_at DESC);

ALTER TABLE deployment_processed_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON deployment_processed_events
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
