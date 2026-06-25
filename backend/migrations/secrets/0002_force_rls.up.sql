-- SEC-CRIT-04: Force row-level security on secrets tables.

ALTER TABLE secrets FORCE ROW LEVEL SECURITY;
ALTER TABLE secret_access_logs FORCE ROW LEVEL SECURITY;
