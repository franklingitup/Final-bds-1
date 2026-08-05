-- Drop observability service schema
DROP TABLE IF EXISTS alert_history CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS observability_events CASCADE;
DROP TABLE IF EXISTS health_checks CASCADE;
DROP TABLE IF EXISTS dashboards CASCADE;
DROP TABLE IF EXISTS log_queries CASCADE;
DROP TABLE IF EXISTS log_streams CASCADE;
DROP TABLE IF EXISTS metric_samples CASCADE;
DROP TABLE IF EXISTS metric_definitions CASCADE;
