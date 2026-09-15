ALTER TABLE org_sso_configs
    DROP COLUMN IF EXISTS sp_private_key_pem,
    DROP COLUMN IF EXISTS sp_certificate_pem;
