-- Observability service schema: metrics aggregation, log metadata, and dashboard configs.

-- ---------------------------------------------------------------------------
-- Metric Definitions
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS metric_definitions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    name            TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    description     TEXT,
    unit            TEXT NOT NULL DEFAULT '',           -- bytes, ms, percent, count, etc.
    metric_type     TEXT NOT NULL DEFAULT 'gauge',      -- gauge | counter | histogram | summary
    
    -- Labels/dimensions
    label_keys      JSONB NOT NULL DEFAULT '[]'::jsonb,
    
    -- Aggregation
    aggregation     TEXT NOT NULL DEFAULT 'avg',        -- avg | sum | min | max | last | rate
    
    -- Source
    source_type     TEXT NOT NULL DEFAULT 'prometheus', -- prometheus | custom | computed
    prometheus_query TEXT,                              -- PromQL query for Prometheus metrics
    
    -- Thresholds for alerts
    warning_threshold   DOUBLE PRECISION,
    critical_threshold  DOUBLE PRECISION,
    threshold_direction TEXT DEFAULT 'above',           -- above | below
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, name)
);

CREATE INDEX IF NOT EXISTS metric_definitions_org_idx
    ON metric_definitions (org_id);

CREATE TRIGGER metric_definitions_set_updated_at
    BEFORE UPDATE ON metric_definitions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE metric_definitions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON metric_definitions
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Metric Samples (recent aggregated data for quick queries)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS metric_samples (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    -- Reference
    metric_name     TEXT NOT NULL,
    resource_type   TEXT NOT NULL,                      -- cluster | deployment | application | node | pod
    resource_id     UUID NOT NULL,
    
    -- Labels
    labels          JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- Value
    value           DOUBLE PRECISION NOT NULL,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    -- Aggregation period
    period_start    TIMESTAMPTZ NOT NULL,
    period_end      TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS metric_samples_resource_idx
    ON metric_samples (org_id, resource_type, resource_id, metric_name, timestamp DESC);

CREATE INDEX IF NOT EXISTS metric_samples_time_idx
    ON metric_samples (timestamp DESC);

-- Partition by time for efficient cleanup (simplified - in production use partitioning)
CREATE INDEX IF NOT EXISTS metric_samples_cleanup_idx
    ON metric_samples (timestamp) WHERE timestamp < now() - INTERVAL '7 days';

ALTER TABLE metric_samples ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON metric_samples
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Log Streams (metadata about log sources)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS log_streams (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    -- Source identification
    stream_name     TEXT NOT NULL,
    resource_type   TEXT NOT NULL,                      -- cluster | deployment | application | pod
    resource_id     UUID NOT NULL,
    
    -- Loki labels
    labels          JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- Stream info
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    log_count       BIGINT NOT NULL DEFAULT 0,
    bytes_ingested  BIGINT NOT NULL DEFAULT 0,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'active',     -- active | inactive | archived
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, stream_name)
);

CREATE INDEX IF NOT EXISTS log_streams_resource_idx
    ON log_streams (org_id, resource_type, resource_id);

CREATE INDEX IF NOT EXISTS log_streams_status_idx
    ON log_streams (status) WHERE status = 'active';

CREATE TRIGGER log_streams_set_updated_at
    BEFORE UPDATE ON log_streams
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE log_streams ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON log_streams
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Log Queries (saved queries for quick access)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS log_queries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    name            TEXT NOT NULL,
    description     TEXT,
    
    -- Query
    logql_query     TEXT NOT NULL,                      -- LogQL query for Loki
    
    -- Filters
    resource_type   TEXT,
    resource_id     UUID,
    
    -- Sharing
    is_shared       BOOLEAN NOT NULL DEFAULT false,
    created_by      UUID,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS log_queries_org_idx
    ON log_queries (org_id, created_at DESC);

CREATE TRIGGER log_queries_set_updated_at
    BEFORE UPDATE ON log_queries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE log_queries ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON log_queries
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Dashboards
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS dashboards (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    name            TEXT NOT NULL,
    description     TEXT,
    
    -- Layout and panels
    panels          JSONB NOT NULL DEFAULT '[]'::jsonb,
    variables       JSONB NOT NULL DEFAULT '[]'::jsonb,
    
    -- Time range defaults
    default_time_range  TEXT NOT NULL DEFAULT '1h',     -- 15m | 1h | 6h | 24h | 7d | 30d
    refresh_interval    TEXT NOT NULL DEFAULT '30s',    -- 10s | 30s | 1m | 5m | off
    
    -- Sharing
    is_shared       BOOLEAN NOT NULL DEFAULT false,
    is_default      BOOLEAN NOT NULL DEFAULT false,
    
    -- Scope
    resource_type   TEXT,                               -- cluster | deployment | application | null (global)
    resource_id     UUID,
    
    created_by      UUID,
    version         BIGINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS dashboards_org_idx
    ON dashboards (org_id, created_at DESC);

CREATE INDEX IF NOT EXISTS dashboards_resource_idx
    ON dashboards (org_id, resource_type, resource_id) WHERE resource_type IS NOT NULL;

CREATE TRIGGER dashboards_set_updated_at
    BEFORE UPDATE ON dashboards
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE dashboards ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON dashboards
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Health Checks
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS health_checks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    -- Target
    resource_type   TEXT NOT NULL,                      -- cluster | deployment | application | service
    resource_id     UUID NOT NULL,
    check_name      TEXT NOT NULL,
    
    -- Check config
    check_type      TEXT NOT NULL DEFAULT 'http',       -- http | tcp | dns | prometheus
    endpoint        TEXT,
    interval_seconds INT NOT NULL DEFAULT 30,
    timeout_seconds INT NOT NULL DEFAULT 5,
    
    -- Current status
    status          TEXT NOT NULL DEFAULT 'unknown',    -- healthy | degraded | unhealthy | unknown
    last_check_at   TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    failure_count   INT NOT NULL DEFAULT 0,
    
    -- Response details
    response_time_ms INT,
    error_message   TEXT,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, resource_type, resource_id, check_name)
);

CREATE INDEX IF NOT EXISTS health_checks_resource_idx
    ON health_checks (org_id, resource_type, resource_id);

CREATE INDEX IF NOT EXISTS health_checks_status_idx
    ON health_checks (status) WHERE status IN ('degraded', 'unhealthy');

CREATE TRIGGER health_checks_set_updated_at
    BEFORE UPDATE ON health_checks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE health_checks ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON health_checks
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Observability Events (platform events for correlation)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS observability_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    -- Event info
    event_type      TEXT NOT NULL,                      -- deployment | scale | restart | alert | incident
    severity        TEXT NOT NULL DEFAULT 'info',       -- info | warning | error | critical
    title           TEXT NOT NULL,
    description     TEXT,
    
    -- Resource
    resource_type   TEXT NOT NULL,
    resource_id     UUID NOT NULL,
    
    -- Metadata
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- Time
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS observability_events_resource_idx
    ON observability_events (org_id, resource_type, resource_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS observability_events_time_idx
    ON observability_events (org_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS observability_events_type_idx
    ON observability_events (org_id, event_type, occurred_at DESC);

ALTER TABLE observability_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON observability_events
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Alert Rules
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS alert_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    name            TEXT NOT NULL,
    description     TEXT,
    
    -- Query
    query_type      TEXT NOT NULL DEFAULT 'promql',     -- promql | logql | metric
    query           TEXT NOT NULL,
    
    -- Condition
    condition       TEXT NOT NULL DEFAULT 'gt',         -- gt | lt | eq | ne | absent
    threshold       DOUBLE PRECISION NOT NULL,
    for_duration    TEXT NOT NULL DEFAULT '5m',         -- How long condition must be true
    
    -- Severity
    severity        TEXT NOT NULL DEFAULT 'warning',    -- info | warning | error | critical
    
    -- Scope
    resource_type   TEXT,
    resource_id     UUID,
    
    -- Status
    enabled         BOOLEAN NOT NULL DEFAULT true,
    last_evaluated  TIMESTAMPTZ,
    current_state   TEXT NOT NULL DEFAULT 'ok',         -- ok | pending | firing | resolved
    
    -- Notification
    notification_channels JSONB NOT NULL DEFAULT '[]'::jsonb,
    
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS alert_rules_org_idx
    ON alert_rules (org_id, enabled) WHERE enabled = true;

CREATE TRIGGER alert_rules_set_updated_at
    BEFORE UPDATE ON alert_rules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE alert_rules ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON alert_rules
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Alert History
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS alert_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    rule_id         UUID NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    
    -- State change
    previous_state  TEXT NOT NULL,
    current_state   TEXT NOT NULL,
    
    -- Details
    value           DOUBLE PRECISION,
    labels          JSONB NOT NULL DEFAULT '{}'::jsonb,
    annotations     JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    fired_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS alert_history_rule_idx
    ON alert_history (rule_id, fired_at DESC);

CREATE INDEX IF NOT EXISTS alert_history_org_idx
    ON alert_history (org_id, fired_at DESC);

ALTER TABLE alert_history ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON alert_history
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
