-- Drop GitHub integration schema
DROP TABLE IF EXISTS github_oauth_states CASCADE;
DROP TABLE IF EXISTS github_webhook_deliveries CASCADE;
DROP TABLE IF EXISTS github_webhooks CASCADE;
DROP TABLE IF EXISTS github_repositories CASCADE;
DROP TABLE IF EXISTS github_connections CASCADE;
