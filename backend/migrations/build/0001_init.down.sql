-- Drop build service schema
DROP TABLE IF EXISTS build_queue CASCADE;
DROP TABLE IF EXISTS build_artifacts CASCADE;
DROP TABLE IF EXISTS build_logs CASCADE;
DROP TABLE IF EXISTS builds CASCADE;
DROP TABLE IF EXISTS git_repositories CASCADE;
