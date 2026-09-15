DELETE FROM sso_exchange_codes;

ALTER TABLE sso_exchange_codes DROP COLUMN IF EXISTS user_id;
ALTER TABLE sso_exchange_codes ADD COLUMN IF NOT EXISTS encrypted_payload BYTEA NOT NULL;
