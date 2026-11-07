-- Reverse Phase 1: remove environments and promotion requests.
DROP TABLE IF EXISTS promotion_requests;
ALTER TABLE deployments DROP COLUMN IF EXISTS environment_id;
DROP TABLE IF EXISTS environments;
