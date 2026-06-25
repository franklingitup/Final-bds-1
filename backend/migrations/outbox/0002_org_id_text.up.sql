-- Widen outbox.org_id from UUID to TEXT.
--
-- The outbox is shared infrastructure across services. Tenant-scoped events use
-- a UUID org id, but identity/system events (Auth) are not tied to a single
-- tenant and use a logical org such as 'platform'. Storing org_id as TEXT lets
-- the outbox carry both without losing the envelope's orgId. The relay never
-- joins on this column; it is metadata for filtering and auditing only.

ALTER TABLE outbox
    ALTER COLUMN org_id TYPE TEXT USING org_id::text;
