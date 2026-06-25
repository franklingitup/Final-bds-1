-- Project service schema: status column + project memberships with RLS.
--
-- The `projects` table is already defined in tenant/0001_init.up.sql as it is
-- the tenant's core resource; this migration extends it with a status column
-- and memberships managed by the project service.

-- Add status column to projects (idempotent).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'projects' AND column_name = 'status'
    ) THEN
        ALTER TABLE projects ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS project_members (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL,
    role       TEXT NOT NULL,                       -- admin | developer | viewer
    added_by   UUID,
    version    BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, user_id)
);

-- Keyset pagination index (created_at DESC, id DESC).
CREATE INDEX IF NOT EXISTS project_members_project_created_idx
    ON project_members (project_id, created_at DESC, id DESC);

-- Query by user across projects within an org.
CREATE INDEX IF NOT EXISTS project_members_org_user_idx
    ON project_members (org_id, user_id);

CREATE TRIGGER project_members_set_updated_at
    BEFORE UPDATE ON project_members
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Row-level security keyed on org_id for tenant isolation.
ALTER TABLE project_members ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON project_members
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
