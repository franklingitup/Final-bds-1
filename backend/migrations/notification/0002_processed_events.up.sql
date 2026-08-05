-- Deployment -> Notification consumer idempotency ledger.
--
-- Records which domain events a durable notification consumer has already
-- processed, so that at-least-once event redelivery is idempotent: a
-- redelivered deployment.succeeded/failed event must not create a duplicate
-- notification and its fan-out of deliveries. The marker row is written in the
-- same transaction as the notification it guards, so both commit atomically.

CREATE TABLE IF NOT EXISTS notification_processed_events (
    consumer     TEXT        NOT NULL,           -- durable consumer name
    event_id     TEXT        NOT NULL,           -- source event envelope id
    org_id       UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (consumer, event_id)
);

CREATE INDEX IF NOT EXISTS notification_processed_events_org_idx
    ON notification_processed_events (org_id, processed_at DESC);

ALTER TABLE notification_processed_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notification_processed_events
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
