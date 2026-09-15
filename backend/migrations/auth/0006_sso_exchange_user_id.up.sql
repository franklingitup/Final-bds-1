-- Exchange codes now map an opaque one-time code to a user identity.
-- Tokens are minted at redemption, so the encrypted TokenPair payload is gone.

DELETE FROM sso_exchange_codes;

ALTER TABLE sso_exchange_codes DROP COLUMN IF EXISTS encrypted_payload;
ALTER TABLE sso_exchange_codes ADD COLUMN IF NOT EXISTS user_id UUID NOT NULL;
