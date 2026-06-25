-- Revert outbox.org_id back to UUID. This only succeeds if every existing
-- org_id value is a valid UUID (i.e. no non-tenant 'platform' rows remain).

ALTER TABLE outbox
    ALTER COLUMN org_id TYPE UUID USING org_id::uuid;
