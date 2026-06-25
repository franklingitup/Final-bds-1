-- SEC-CRIT-04: Force row-level security on all tenant-scoped tables.
-- This ensures RLS policies are enforced even for table owners.

ALTER TABLE organizations FORCE ROW LEVEL SECURITY;
ALTER TABLE projects FORCE ROW LEVEL SECURITY;
