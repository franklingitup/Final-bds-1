-- Transactional outbox for the event framework (libs/events).
--
-- Producers INSERT here in the same transaction as their state change; the
-- relay (events.Relay) publishes pending rows to NATS and stamps published_at.
-- This table is infrastructure, not tenant data: the relay reads across all
-- orgs, so it does NOT enable row-level security.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS outbox (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id     UUID NOT NULL UNIQUE,
    event_type   TEXT NOT NULL,
    version      INT NOT NULL,
    org_id       UUID NOT NULL,
    envelope     JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

-- Drives the relay's "oldest unpublished first" scan; partial index keeps it
-- small as published rows accumulate.
CREATE INDEX IF NOT EXISTS outbox_unpublished_idx
    ON outbox (created_at)
    WHERE published_at IS NULL;
