-- Domain service schema: custom domains, DNS verification, and TLS certificates.

-- ---------------------------------------------------------------------------
-- Custom Domains
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS domains (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    deployment_id   UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    
    -- Domain info
    domain          TEXT NOT NULL,
    subdomain       TEXT,                     -- Optional subdomain (www, api, etc.)
    full_domain     TEXT NOT NULL,            -- Computed: subdomain.domain or just domain
    
    -- Verification
    verification_status TEXT NOT NULL DEFAULT 'pending',  -- pending | verifying | verified | failed
    verification_token  TEXT NOT NULL,
    verification_method TEXT NOT NULL DEFAULT 'dns_txt',  -- dns_txt | dns_cname | http
    verified_at     TIMESTAMPTZ,
    last_check_at   TIMESTAMPTZ,
    verification_error TEXT,
    
    -- DNS records (for user to configure)
    dns_txt_name    TEXT NOT NULL,            -- _bdsplatform-verify.domain
    dns_txt_value   TEXT NOT NULL,            -- verification token
    dns_cname_target TEXT,                    -- For CNAME verification
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending | active | suspended | deleted
    is_primary      BOOLEAN NOT NULL DEFAULT false,
    
    created_by      UUID,
    version         BIGINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, full_domain)
);

CREATE INDEX IF NOT EXISTS domains_org_idx
    ON domains (org_id, created_at DESC);

CREATE INDEX IF NOT EXISTS domains_deployment_idx
    ON domains (deployment_id);

CREATE INDEX IF NOT EXISTS domains_verification_idx
    ON domains (verification_status) WHERE verification_status IN ('pending', 'verifying');

CREATE TRIGGER domains_set_updated_at
    BEFORE UPDATE ON domains
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE domains ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON domains
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- TLS Certificates
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS certificates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    domain_id       UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    
    -- Certificate info
    common_name     TEXT NOT NULL,
    san_domains     JSONB NOT NULL DEFAULT '[]'::jsonb,  -- Subject Alternative Names
    issuer          TEXT NOT NULL DEFAULT 'letsencrypt',
    
    -- Certificate data (encrypted)
    certificate_pem BYTEA,                    -- Encrypted PEM certificate chain
    private_key_pem BYTEA,                    -- Encrypted PEM private key
    
    -- Validity
    issued_at       TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending | issuing | active | expired | revoked | failed
    last_renewal_at TIMESTAMPTZ,
    renewal_error   TEXT,
    
    -- ACME tracking
    acme_order_url  TEXT,
    acme_cert_url   TEXT,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (domain_id)  -- One certificate per domain
);

CREATE INDEX IF NOT EXISTS certificates_org_idx
    ON certificates (org_id);

CREATE INDEX IF NOT EXISTS certificates_domain_idx
    ON certificates (domain_id);

CREATE INDEX IF NOT EXISTS certificates_expiry_idx
    ON certificates (expires_at) WHERE status = 'active';

CREATE TRIGGER certificates_set_updated_at
    BEFORE UPDATE ON certificates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE certificates ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON certificates
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- ACME Challenges (for Let's Encrypt HTTP-01 challenges)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS acme_challenges (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    domain_id       UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    
    -- Challenge info
    challenge_type  TEXT NOT NULL DEFAULT 'http-01',  -- http-01 | dns-01
    token           TEXT NOT NULL,
    key_auth        TEXT NOT NULL,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending | valid | invalid | expired
    validated_at    TIMESTAMPTZ,
    
    expires_at      TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '1 hour'),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS acme_challenges_token_idx
    ON acme_challenges (token);

CREATE INDEX IF NOT EXISTS acme_challenges_domain_idx
    ON acme_challenges (domain_id);

ALTER TABLE acme_challenges ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON acme_challenges
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Ingress Records (Kubernetes Ingress tracking)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ingress_records (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    domain_id       UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    cluster_id      UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    
    -- Ingress info
    ingress_name    TEXT NOT NULL,
    namespace       TEXT NOT NULL,
    ingress_class   TEXT NOT NULL DEFAULT 'nginx',
    
    -- Routing
    service_name    TEXT NOT NULL,
    service_port    INT NOT NULL,
    path            TEXT NOT NULL DEFAULT '/',
    path_type       TEXT NOT NULL DEFAULT 'Prefix',  -- Prefix | Exact | ImplementationSpecific
    
    -- TLS
    tls_secret_name TEXT,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending | synced | failed
    last_synced_at  TIMESTAMPTZ,
    sync_error      TEXT,
    
    -- Manifest tracking
    manifest_hash   TEXT,
    generation      BIGINT NOT NULL DEFAULT 1,
    observed_generation BIGINT DEFAULT 0,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (domain_id, cluster_id)
);

CREATE INDEX IF NOT EXISTS ingress_records_cluster_idx
    ON ingress_records (cluster_id, status);

CREATE INDEX IF NOT EXISTS ingress_records_domain_idx
    ON ingress_records (domain_id);

CREATE TRIGGER ingress_records_set_updated_at
    BEFORE UPDATE ON ingress_records
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE ingress_records ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ingress_records
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Domain Events (audit log)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS domain_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    domain_id       UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    
    event_type      TEXT NOT NULL,            -- created | verified | cert_issued | cert_renewed | deleted
    message         TEXT NOT NULL,
    details         JSONB,
    
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS domain_events_domain_idx
    ON domain_events (domain_id, created_at DESC);

ALTER TABLE domain_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON domain_events
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
