-- Per-organization SAML SSO configuration.
--
-- One enabled config per org (UNIQUE org_id). IdP metadata may be supplied as a
-- fetch URL and/or pasted XML; attribute_* columns map assertion attributes to
-- user fields (defaults match common Azure AD / ADFS claim URIs).
-- default_role is the OrgRole assigned on JIT provisioning when no membership exists.

CREATE TABLE IF NOT EXISTS org_sso_configs (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id               UUID NOT NULL,
    idp_metadata_url     TEXT,
    idp_metadata_xml     TEXT,
    idp_entity_id        TEXT NOT NULL DEFAULT '',
    sp_entity_id         TEXT NOT NULL DEFAULT '',
    attribute_email      TEXT NOT NULL DEFAULT 'http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress',
    attribute_first_name TEXT NOT NULL DEFAULT 'http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname',
    attribute_last_name  TEXT NOT NULL DEFAULT 'http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname',
    default_role         TEXT NOT NULL DEFAULT 'member',
    enabled              BOOLEAN NOT NULL DEFAULT false,
    version              BIGINT NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id),
    CONSTRAINT org_sso_configs_default_role_check
        CHECK (default_role IN ('owner', 'admin', 'member', 'auditor')),
    CONSTRAINT org_sso_configs_metadata_present_check
        CHECK (idp_metadata_url IS NOT NULL OR idp_metadata_xml IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS org_sso_configs_org_idx ON org_sso_configs (org_id);

CREATE TRIGGER org_sso_configs_set_updated_at
    BEFORE UPDATE ON org_sso_configs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE org_sso_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_sso_configs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON org_sso_configs
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
