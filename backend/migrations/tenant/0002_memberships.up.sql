-- Memberships and invitations for the tenant service.
--
-- Both tables are tenant-owned: they carry org_id and enable row-level security
-- keyed on the per-transaction `app.current_org_id` session variable, matching
-- the convention established for organizations/projects in 0001.

CREATE EXTENSION IF NOT EXISTS citext;

-- ---------------------------------------------------------------------------
-- Organization members
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS organization_members (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL,
    role       TEXT NOT NULL,                       -- owner | admin | developer | viewer
    status     TEXT NOT NULL DEFAULT 'active',      -- active | suspended
    invited_by UUID,
    version    BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, user_id)
);

CREATE INDEX IF NOT EXISTS organization_members_org_created_idx
    ON organization_members (org_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS organization_members_user_idx
    ON organization_members (user_id);

CREATE TRIGGER organization_members_set_updated_at
    BEFORE UPDATE ON organization_members
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE organization_members ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organization_members
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Organization invitations
--
-- Acceptance is a capability-based, cross-tenant flow keyed on token_hash:
-- the invited user is not yet a member, so the token itself authorizes the
-- lookup. RLS remains a backstop for the org-scoped reads/writes. See
-- docs/06-security-design.md section 3.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS organization_invitations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email       CITEXT NOT NULL,
    role        TEXT NOT NULL,                      -- admin | developer | viewer
    token_hash  TEXT NOT NULL UNIQUE,
    status      TEXT NOT NULL DEFAULT 'pending',    -- pending | accepted | revoked | expired
    invited_by  UUID,
    expires_at  TIMESTAMPTZ NOT NULL,
    accepted_by UUID,
    accepted_at TIMESTAMPTZ,
    version     BIGINT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- At most one pending invitation per (org, email).
CREATE UNIQUE INDEX IF NOT EXISTS organization_invitations_pending_idx
    ON organization_invitations (org_id, email)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS organization_invitations_org_created_idx
    ON organization_invitations (org_id, created_at DESC, id DESC);

CREATE TRIGGER organization_invitations_set_updated_at
    BEFORE UPDATE ON organization_invitations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE organization_invitations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organization_invitations
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
