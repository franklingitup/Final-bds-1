-- SEC-CRIT-04: Force row-level security on audit tables.

ALTER TABLE audit_logs FORCE ROW LEVEL SECURITY;
