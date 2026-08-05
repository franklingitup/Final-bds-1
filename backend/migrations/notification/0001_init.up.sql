-- Notification service schema: channels, templates, preferences, and delivery tracking.

-- ---------------------------------------------------------------------------
-- Notification Channels (configured endpoints for sending notifications)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_channels (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    name            TEXT NOT NULL,
    channel_type    TEXT NOT NULL,                      -- email | slack | webhook | in_app
    
    -- Channel-specific configuration (encrypted)
    config          JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- Status
    enabled         BOOLEAN NOT NULL DEFAULT true,
    verified        BOOLEAN NOT NULL DEFAULT false,
    verified_at     TIMESTAMPTZ,
    
    -- Metadata
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, name)
);

CREATE INDEX IF NOT EXISTS notification_channels_org_idx
    ON notification_channels (org_id, enabled);

CREATE TRIGGER notification_channels_set_updated_at
    BEFORE UPDATE ON notification_channels
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE notification_channels ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notification_channels
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Notification Templates
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_templates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID REFERENCES organizations(id) ON DELETE CASCADE,  -- NULL = system template
    
    name            TEXT NOT NULL,
    event_type      TEXT NOT NULL,                      -- deployment.completed | deployment.failed | etc.
    
    -- Template content
    subject         TEXT NOT NULL,                      -- For email subject / notification title
    body_text       TEXT NOT NULL,                      -- Plain text version
    body_html       TEXT,                               -- HTML version (for email)
    
    -- Slack-specific
    slack_blocks    JSONB,                              -- Slack Block Kit JSON
    
    -- Variables available in template
    variables       JSONB NOT NULL DEFAULT '[]'::jsonb,
    
    -- Status
    is_default      BOOLEAN NOT NULL DEFAULT false,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (COALESCE(org_id, '00000000-0000-0000-0000-000000000000'::uuid), name)
);

CREATE INDEX IF NOT EXISTS notification_templates_event_idx
    ON notification_templates (event_type, is_default);

CREATE TRIGGER notification_templates_set_updated_at
    BEFORE UPDATE ON notification_templates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- System templates don't use RLS (org_id is NULL)
ALTER TABLE notification_templates ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notification_templates
    USING (org_id IS NULL OR org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- User Notification Preferences
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_preferences (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL,
    
    -- Event type preferences
    event_type      TEXT NOT NULL,                      -- deployment.* | alert.* | * (all)
    
    -- Channel preferences
    email_enabled   BOOLEAN NOT NULL DEFAULT true,
    slack_enabled   BOOLEAN NOT NULL DEFAULT true,
    webhook_enabled BOOLEAN NOT NULL DEFAULT true,
    in_app_enabled  BOOLEAN NOT NULL DEFAULT true,
    
    -- Filtering
    severity_filter TEXT[] NOT NULL DEFAULT ARRAY['info', 'warning', 'error', 'critical'],
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, user_id, event_type)
);

CREATE INDEX IF NOT EXISTS notification_preferences_user_idx
    ON notification_preferences (org_id, user_id);

CREATE TRIGGER notification_preferences_set_updated_at
    BEFORE UPDATE ON notification_preferences
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE notification_preferences ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notification_preferences
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Notifications (individual notification records)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notifications (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    -- Event info
    event_type      TEXT NOT NULL,
    event_id        UUID,                               -- Reference to source event
    
    -- Content
    title           TEXT NOT NULL,
    body            TEXT NOT NULL,
    severity        TEXT NOT NULL DEFAULT 'info',       -- info | warning | error | critical
    
    -- Target
    resource_type   TEXT,
    resource_id     UUID,
    resource_name   TEXT,
    
    -- Metadata
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'pending',    -- pending | sending | sent | failed
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS notifications_org_idx
    ON notifications (org_id, created_at DESC);

CREATE INDEX IF NOT EXISTS notifications_event_idx
    ON notifications (event_type, created_at DESC);

CREATE INDEX IF NOT EXISTS notifications_status_idx
    ON notifications (status) WHERE status IN ('pending', 'sending');

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notifications
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Notification Deliveries (per-channel delivery attempts)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_deliveries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    notification_id UUID NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    channel_id      UUID REFERENCES notification_channels(id) ON DELETE SET NULL,
    
    -- Channel info
    channel_type    TEXT NOT NULL,
    recipient       TEXT NOT NULL,                      -- email address, webhook URL, slack channel
    
    -- Delivery status
    status          TEXT NOT NULL DEFAULT 'pending',    -- pending | sending | delivered | failed | dlq
    
    -- Timing
    scheduled_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    
    -- Retry tracking
    attempt_count   INT NOT NULL DEFAULT 0,
    max_attempts    INT NOT NULL DEFAULT 3,
    next_retry_at   TIMESTAMPTZ,
    
    -- Response/Error
    response_code   INT,
    response_body   TEXT,
    error_message   TEXT,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS notification_deliveries_notification_idx
    ON notification_deliveries (notification_id);

CREATE INDEX IF NOT EXISTS notification_deliveries_status_idx
    ON notification_deliveries (status, next_retry_at) 
    WHERE status IN ('pending', 'failed');

CREATE INDEX IF NOT EXISTS notification_deliveries_channel_idx
    ON notification_deliveries (channel_id, status);

CREATE TRIGGER notification_deliveries_set_updated_at
    BEFORE UPDATE ON notification_deliveries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE notification_deliveries ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notification_deliveries
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Dead Letter Queue (failed deliveries after all retries)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_dlq (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    delivery_id     UUID NOT NULL REFERENCES notification_deliveries(id) ON DELETE CASCADE,
    notification_id UUID NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    
    -- Original delivery info
    channel_type    TEXT NOT NULL,
    recipient       TEXT NOT NULL,
    
    -- Failure info
    failure_reason  TEXT NOT NULL,
    last_error      TEXT,
    attempt_count   INT NOT NULL,
    
    -- For replay
    payload         JSONB NOT NULL,
    
    -- Status
    status          TEXT NOT NULL DEFAULT 'failed',     -- failed | replayed | discarded
    replayed_at     TIMESTAMPTZ,
    discarded_at    TIMESTAMPTZ,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS notification_dlq_org_idx
    ON notification_dlq (org_id, created_at DESC);

CREATE INDEX IF NOT EXISTS notification_dlq_status_idx
    ON notification_dlq (status) WHERE status = 'failed';

ALTER TABLE notification_dlq ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notification_dlq
    USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- Webhook Subscriptions (outbound webhooks)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    
    name            TEXT NOT NULL,
    url             TEXT NOT NULL,
    
    -- Events to subscribe to
    event_types     JSONB NOT NULL DEFAULT '["*"]'::jsonb,
    
    -- Security
    secret          BYTEA NOT NULL,                     -- HMAC secret (encrypted)
    
    -- Headers to include
    headers         JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- Status
    enabled         BOOLEAN NOT NULL DEFAULT true,
    
    -- Stats
    last_triggered_at TIMESTAMPTZ,
    success_count   BIGINT NOT NULL DEFAULT 0,
    failure_count   BIGINT NOT NULL DEFAULT 0,
    
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (org_id, name)
);

CREATE INDEX IF NOT EXISTS webhook_subscriptions_org_idx
    ON webhook_subscriptions (org_id, enabled);

CREATE TRIGGER webhook_subscriptions_set_updated_at
    BEFORE UPDATE ON webhook_subscriptions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE webhook_subscriptions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON webhook_subscriptions
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
