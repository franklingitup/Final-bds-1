-- SEC-CRIT-04: Force row-level security on auth tenant-scoped tables.
-- Note: users and refresh_tokens are global tables, not tenant-scoped.

ALTER TABLE service_accounts FORCE ROW LEVEL SECURITY;
ALTER TABLE api_tokens FORCE ROW LEVEL SECURITY;
