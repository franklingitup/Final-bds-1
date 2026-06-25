-- Revert FORCE ROW LEVEL SECURITY on membership tables
ALTER TABLE organization_members NO FORCE ROW LEVEL SECURITY;
ALTER TABLE organization_invitations NO FORCE ROW LEVEL SECURITY;
