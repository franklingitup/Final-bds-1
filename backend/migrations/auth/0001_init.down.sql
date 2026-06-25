DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS service_accounts;
DROP TABLE IF EXISTS one_time_tokens;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;
-- set_updated_at() is shared; leave it in place for other services.
