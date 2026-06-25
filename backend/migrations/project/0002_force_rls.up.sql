-- SEC-CRIT-04: Force row-level security on project tables.

ALTER TABLE project_members FORCE ROW LEVEL SECURITY;
