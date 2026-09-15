-- Short-lived, single-use state used by the SAML browser flow.
-- Login state binds an ACS response to the org and AuthnRequest that initiated it.
CREATE TABLE IF NOT EXISTS sso_login_states (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL,
    state_hash  TEXT NOT NULL UNIQUE,
    request_id  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sso_login_states_expiry_idx
    ON sso_login_states (expires_at);

ALTER TABLE sso_login_states ENABLE ROW LEVEL SECURITY;
ALTER TABLE sso_login_states FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sso_login_states
    USING (org_id = current_setting('app.current_org_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.current_org_id', true)::uuid);

-- The ACS cannot place access or refresh tokens in the browser redirect URL.
-- It encrypts the token pair and stores it behind a short-lived one-time code;
-- the frontend redeems that code immediately after the redirect.
CREATE TABLE IF NOT EXISTS sso_exchange_codes (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id            UUID NOT NULL,
    code_hash         TEXT NOT NULL UNIQUE,
    encrypted_payload BYTEA NOT NULL,
    expires_at        TIMESTAMPTZ NOT NULL,
    used_at           TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sso_exchange_codes_expiry_idx
    ON sso_exchange_codes (expires_at);

ALTER TABLE sso_exchange_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE sso_exchange_codes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sso_exchange_codes
    USING (org_id = current_setting('app.current_org_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.current_org_id', true)::uuid);
