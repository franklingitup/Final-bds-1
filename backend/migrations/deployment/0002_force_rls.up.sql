-- SEC-CRIT-04: Force row-level security on deployment tables.

ALTER TABLE applications FORCE ROW LEVEL SECURITY;
ALTER TABLE deployments FORCE ROW LEVEL SECURITY;
ALTER TABLE releases FORCE ROW LEVEL SECURITY;
