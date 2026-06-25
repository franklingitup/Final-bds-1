-- Secrets service schema: secure project-scoped secret management.
--
-- Secrets are encrypted at rest using AES-256-GCM envelope encryption.
-- Plaintext values are NEVER stored; only encrypted_value and value_hash.

-- ---------------------------------------------------------------------------
-- Secrets
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS secrets (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id       UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name             VARCHAR(255) NOT NULL,
    description      TEXT,
    
    -- Encryption (AES-256-GCM envelope encryption)
    -- encrypted_value contains: nonce (12 bytes) || ciphertext || tag (16 bytes)
    encrypted_value  BYTEA NOT NULL,
    
    -- SHA-256 hash of the plaintext value for change detection
    -- Allows checking if a value changed without decryption
    value_hash       VARCHAR(128) NOT NULL,
    
    -- Versioning for optimistic concurrency and secret rotation tracking
    version          BIGINT NOT NULL DEFAULT 1,
    
    -- Audit fields
    created_by       UUID,
    updated_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    -- Soft delete support
    deleted_at       TIMESTAMPTZ
);

-- Unique name per project (excluding deleted secrets)
-- Note: PostgreSQL doesn't support WHERE in constraint definitions,
-- so we use a partial unique index instead
CREATE UNIQUE INDEX IF NOT EXISTS secrets_project_name_unique
    ON secrets (project_id, name)
    WHERE deleted_at IS NULL;

-- Primary query path: list secrets by org (RLS) and project
CREATE INDEX IF NOT EXISTS secrets_org_id_idx
    ON secrets (org_id);

CREATE INDEX IF NOT EXISTS secrets_project_id_idx
    ON secrets (project_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- Name lookup within project
CREATE INDEX IF NOT EXISTS secrets_project_name_idx
    ON secrets (project_id, name)
    WHERE deleted_at IS NULL;

-- Keyset pagination index
CREATE INDEX IF NOT EXISTS secrets_org_created_idx
    ON secrets (org_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

-- Auto-update updated_at timestamp
CREATE TRIGGER secrets_set_updated_at
    BEFORE UPDATE ON secrets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Row-Level Security: tenant isolation using org_id
ALTER TABLE secrets ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON secrets
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Secret Audit Trail (for tracking access and modifications)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS secret_access_logs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    secret_id        UUID NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    action           TEXT NOT NULL,  -- created | updated | deleted | accessed
    performed_by     UUID,
    performed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata         JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS secret_access_logs_secret_idx
    ON secret_access_logs (secret_id, performed_at DESC);

CREATE INDEX IF NOT EXISTS secret_access_logs_org_idx
    ON secret_access_logs (org_id, performed_at DESC);

ALTER TABLE secret_access_logs ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON secret_access_logs
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
