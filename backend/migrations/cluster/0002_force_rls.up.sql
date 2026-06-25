-- SEC-CRIT-04: Force row-level security on cluster tables.

ALTER TABLE clusters FORCE ROW LEVEL SECURITY;
ALTER TABLE cluster_registration_tokens FORCE ROW LEVEL SECURITY;
ALTER TABLE cluster_heartbeats FORCE ROW LEVEL SECURITY;
