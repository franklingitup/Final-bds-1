-- Reverse secrets service schema migration

-- Drop RLS policies first
DROP POLICY IF EXISTS tenant_isolation ON secret_access_logs;
DROP POLICY IF EXISTS tenant_isolation ON secrets;

-- Drop indexes
DROP INDEX IF EXISTS secret_access_logs_org_idx;
DROP INDEX IF EXISTS secret_access_logs_secret_idx;
DROP INDEX IF EXISTS secrets_org_created_idx;
DROP INDEX IF EXISTS secrets_project_name_idx;
DROP INDEX IF EXISTS secrets_project_id_idx;
DROP INDEX IF EXISTS secrets_org_id_idx;
DROP INDEX IF EXISTS secrets_project_name_unique;

-- Drop trigger
DROP TRIGGER IF EXISTS secrets_set_updated_at ON secrets;

-- Drop tables
DROP TABLE IF EXISTS secret_access_logs;
DROP TABLE IF EXISTS secrets;
