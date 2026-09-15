-- Persist per-org SAML Service Provider signing material.
-- The certificate is public (embedded in SP metadata); the private key is stored
-- encrypted at rest when CERTIFICATE_ENCRYPTION_KEY is configured (same AES-256-GCM
-- pattern as the domain service), or as raw PEM in local development.

ALTER TABLE org_sso_configs
    ADD COLUMN IF NOT EXISTS sp_certificate_pem  TEXT,
    ADD COLUMN IF NOT EXISTS sp_private_key_pem  BYTEA;
