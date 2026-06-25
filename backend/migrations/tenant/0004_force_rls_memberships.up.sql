-- SEC-CRIT-04: Force row-level security on membership tables.

ALTER TABLE organization_members FORCE ROW LEVEL SECURITY;
ALTER TABLE organization_invitations FORCE ROW LEVEL SECURITY;
