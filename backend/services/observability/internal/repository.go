package observability

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// TenantRunner runs a function within a tenant-scoped transaction.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// DashboardStore persists dashboards.
type DashboardStore interface {
	Create(ctx context.Context, d *Dashboard) error
	GetByID(ctx context.Context, id string) (*Dashboard, error)
	List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[Dashboard], error)
	ListByResource(ctx context.Context, resourceType, resourceID string) ([]Dashboard, error)
	Update(ctx context.Context, d *Dashboard) error
	Delete(ctx context.Context, id string) error
}

// HealthCheckStore persists health checks.
type HealthCheckStore interface {
	Upsert(ctx context.Context, h *HealthCheck) error
	GetByID(ctx context.Context, id string) (*HealthCheck, error)
	GetByResource(ctx context.Context, resourceType, resourceID, checkName string) (*HealthCheck, error)
	List(ctx context.Context, orgID string) ([]HealthCheck, error)
	ListByResource(ctx context.Context, resourceType, resourceID string) ([]HealthCheck, error)
	ListUnhealthy(ctx context.Context, orgID string) ([]HealthCheck, error)
	Delete(ctx context.Context, id string) error
}

// EventStore persists observability events.
type EventStore interface {
	Create(ctx context.Context, e *ObservabilityEvent) error
	GetByID(ctx context.Context, id string) (*ObservabilityEvent, error)
	List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[ObservabilityEvent], error)
	ListByResource(ctx context.Context, resourceType, resourceID string, req database.PageRequest) (database.Page[ObservabilityEvent], error)
	ListByType(ctx context.Context, orgID, eventType string, req database.PageRequest) (database.Page[ObservabilityEvent], error)
}

// AlertRuleStore persists alert rules.
type AlertRuleStore interface {
	Create(ctx context.Context, r *AlertRule) error
	GetByID(ctx context.Context, id string) (*AlertRule, error)
	List(ctx context.Context, orgID string) ([]AlertRule, error)
	ListEnabled(ctx context.Context, orgID string) ([]AlertRule, error)
	Update(ctx context.Context, r *AlertRule) error
	UpdateState(ctx context.Context, id, state string) error
	Delete(ctx context.Context, id string) error
}

// AlertHistoryStore persists alert history.
type AlertHistoryStore interface {
	Create(ctx context.Context, h *AlertHistory) error
	ListByRule(ctx context.Context, ruleID string, req database.PageRequest) (database.Page[AlertHistory], error)
	ListFiring(ctx context.Context, orgID string) ([]AlertHistory, error)
}

// LogStreamStore persists log stream metadata.
type LogStreamStore interface {
	Upsert(ctx context.Context, s *LogStream) error
	GetByID(ctx context.Context, id string) (*LogStream, error)
	List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[LogStream], error)
	ListByResource(ctx context.Context, resourceType, resourceID string) ([]LogStream, error)
	UpdateStats(ctx context.Context, id string, logCount, bytesIngested int64) error
}

// MetricSampleStore persists metric samples.
type MetricSampleStore interface {
	Create(ctx context.Context, s *MetricSample) error
	ListByResource(ctx context.Context, resourceType, resourceID, metricName string, limit int) ([]MetricSample, error)
	DeleteOld(ctx context.Context, before int) error // Delete samples older than N days
}

// ----------------------------------------------------------------------------
// Dashboard Repository
// ----------------------------------------------------------------------------

type dashboardRepo struct{ db *database.DB }

func NewDashboardStore(db *database.DB) DashboardStore { return &dashboardRepo{db: db} }

func (r *dashboardRepo) Create(ctx context.Context, d *Dashboard) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if len(d.Panels) == 0 {
		d.Panels = []byte("[]")
	}
	if len(d.Variables) == 0 {
		d.Variables = []byte("[]")
	}
	if d.DefaultTimeRange == "" {
		d.DefaultTimeRange = "1h"
	}
	if d.RefreshInterval == "" {
		d.RefreshInterval = "30s"
	}

	const sql = `
INSERT INTO dashboards (id, org_id, name, description, panels, variables, 
    default_time_range, refresh_interval, is_shared, is_default, 
    resource_type, resource_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING created_at, updated_at, version`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		d.ID, d.OrgID, d.Name, d.Description, d.Panels, d.Variables,
		d.DefaultTimeRange, d.RefreshInterval, d.IsShared, d.IsDefault,
		d.ResourceType, d.ResourceID, d.CreatedBy)
	return database.MapError(row.Scan(&d.CreatedAt, &d.UpdatedAt, &d.Version))
}

func (r *dashboardRepo) GetByID(ctx context.Context, id string) (*Dashboard, error) {
	d, err := database.QueryOne[Dashboard](ctx, r.db.Conn(ctx),
		"SELECT * FROM dashboards WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *dashboardRepo) List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[Dashboard], error) {
	req = req.Normalize()
	items, err := database.QueryAll[Dashboard](ctx, r.db.Conn(ctx),
		"SELECT * FROM dashboards WHERE org_id = $1 ORDER BY is_default DESC, name ASC LIMIT $2",
		orgID, req.Limit+1)
	if err != nil {
		return database.Page[Dashboard]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[Dashboard]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[Dashboard]{Items: items}, nil
}

func (r *dashboardRepo) ListByResource(ctx context.Context, resourceType, resourceID string) ([]Dashboard, error) {
	return database.QueryAll[Dashboard](ctx, r.db.Conn(ctx),
		`SELECT * FROM dashboards 
		 WHERE resource_type = $1 AND resource_id = $2 
		 ORDER BY is_default DESC, name ASC`,
		resourceType, resourceID)
}

func (r *dashboardRepo) Update(ctx context.Context, d *Dashboard) error {
	const sql = `
UPDATE dashboards
SET name = $1, description = $2, panels = $3, variables = $4,
    default_time_range = $5, refresh_interval = $6, is_shared = $7, is_default = $8,
    version = version + 1, updated_at = now()
WHERE id = $9 AND version = $10`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		d.Name, d.Description, d.Panels, d.Variables,
		d.DefaultTimeRange, d.RefreshInterval, d.IsShared, d.IsDefault,
		d.ID, d.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	d.Version++
	return nil
}

func (r *dashboardRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM dashboards WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Health Check Repository
// ----------------------------------------------------------------------------

type healthCheckRepo struct{ db *database.DB }

func NewHealthCheckStore(db *database.DB) HealthCheckStore { return &healthCheckRepo{db: db} }

func (r *healthCheckRepo) Upsert(ctx context.Context, h *HealthCheck) error {
	if h.ID == "" {
		h.ID = uuid.NewString()
	}
	if h.CheckType == "" {
		h.CheckType = "http"
	}
	if h.IntervalSeconds == 0 {
		h.IntervalSeconds = 30
	}
	if h.TimeoutSeconds == 0 {
		h.TimeoutSeconds = 5
	}
	if h.Status == "" {
		h.Status = HealthUnknown
	}

	const sql = `
INSERT INTO health_checks (id, org_id, resource_type, resource_id, check_name, check_type,
    endpoint, interval_seconds, timeout_seconds, status, last_check_at, 
    last_success_at, last_failure_at, failure_count, response_time_ms, error_message)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (org_id, resource_type, resource_id, check_name) DO UPDATE SET
    status = EXCLUDED.status,
    last_check_at = EXCLUDED.last_check_at,
    last_success_at = CASE WHEN EXCLUDED.status = 'healthy' THEN now() ELSE health_checks.last_success_at END,
    last_failure_at = CASE WHEN EXCLUDED.status != 'healthy' THEN now() ELSE health_checks.last_failure_at END,
    failure_count = CASE WHEN EXCLUDED.status = 'healthy' THEN 0 ELSE health_checks.failure_count + 1 END,
    response_time_ms = EXCLUDED.response_time_ms,
    error_message = EXCLUDED.error_message,
    updated_at = now()
RETURNING id, created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		h.ID, h.OrgID, h.ResourceType, h.ResourceID, h.CheckName, h.CheckType,
		h.Endpoint, h.IntervalSeconds, h.TimeoutSeconds, h.Status, h.LastCheckAt,
		h.LastSuccessAt, h.LastFailureAt, h.FailureCount, h.ResponseTimeMs, h.ErrorMessage)
	return database.MapError(row.Scan(&h.ID, &h.CreatedAt, &h.UpdatedAt))
}

func (r *healthCheckRepo) GetByID(ctx context.Context, id string) (*HealthCheck, error) {
	h, err := database.QueryOne[HealthCheck](ctx, r.db.Conn(ctx),
		"SELECT * FROM health_checks WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (r *healthCheckRepo) GetByResource(ctx context.Context, resourceType, resourceID, checkName string) (*HealthCheck, error) {
	h, err := database.QueryOne[HealthCheck](ctx, r.db.Conn(ctx),
		"SELECT * FROM health_checks WHERE resource_type = $1 AND resource_id = $2 AND check_name = $3",
		resourceType, resourceID, checkName)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (r *healthCheckRepo) List(ctx context.Context, orgID string) ([]HealthCheck, error) {
	return database.QueryAll[HealthCheck](ctx, r.db.Conn(ctx),
		"SELECT * FROM health_checks WHERE org_id = $1 ORDER BY resource_type, check_name", orgID)
}

func (r *healthCheckRepo) ListByResource(ctx context.Context, resourceType, resourceID string) ([]HealthCheck, error) {
	return database.QueryAll[HealthCheck](ctx, r.db.Conn(ctx),
		"SELECT * FROM health_checks WHERE resource_type = $1 AND resource_id = $2 ORDER BY check_name",
		resourceType, resourceID)
}

func (r *healthCheckRepo) ListUnhealthy(ctx context.Context, orgID string) ([]HealthCheck, error) {
	return database.QueryAll[HealthCheck](ctx, r.db.Conn(ctx),
		"SELECT * FROM health_checks WHERE org_id = $1 AND status IN ('degraded', 'unhealthy') ORDER BY updated_at DESC",
		orgID)
}

func (r *healthCheckRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM health_checks WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Event Repository
// ----------------------------------------------------------------------------

type eventRepo struct{ db *database.DB }

func NewEventStore(db *database.DB) EventStore { return &eventRepo{db: db} }

func (r *eventRepo) Create(ctx context.Context, e *ObservabilityEvent) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.Severity == "" {
		e.Severity = SeverityInfo
	}
	if len(e.Metadata) == 0 {
		e.Metadata = []byte("{}")
	}

	const sql = `
INSERT INTO observability_events (id, org_id, event_type, severity, title, description,
    resource_type, resource_id, metadata, occurred_at, resolved_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING created_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		e.ID, e.OrgID, e.EventType, e.Severity, e.Title, e.Description,
		e.ResourceType, e.ResourceID, e.Metadata, e.OccurredAt, e.ResolvedAt)
	return database.MapError(row.Scan(&e.CreatedAt))
}

func (r *eventRepo) GetByID(ctx context.Context, id string) (*ObservabilityEvent, error) {
	e, err := database.QueryOne[ObservabilityEvent](ctx, r.db.Conn(ctx),
		"SELECT * FROM observability_events WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *eventRepo) List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[ObservabilityEvent], error) {
	req = req.Normalize()
	items, err := database.QueryAll[ObservabilityEvent](ctx, r.db.Conn(ctx),
		"SELECT * FROM observability_events WHERE org_id = $1 ORDER BY occurred_at DESC LIMIT $2",
		orgID, req.Limit+1)
	if err != nil {
		return database.Page[ObservabilityEvent]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[ObservabilityEvent]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[ObservabilityEvent]{Items: items}, nil
}

func (r *eventRepo) ListByResource(ctx context.Context, resourceType, resourceID string, req database.PageRequest) (database.Page[ObservabilityEvent], error) {
	req = req.Normalize()
	items, err := database.QueryAll[ObservabilityEvent](ctx, r.db.Conn(ctx),
		`SELECT * FROM observability_events 
		 WHERE resource_type = $1 AND resource_id = $2 
		 ORDER BY occurred_at DESC LIMIT $3`,
		resourceType, resourceID, req.Limit+1)
	if err != nil {
		return database.Page[ObservabilityEvent]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[ObservabilityEvent]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[ObservabilityEvent]{Items: items}, nil
}

func (r *eventRepo) ListByType(ctx context.Context, orgID, eventType string, req database.PageRequest) (database.Page[ObservabilityEvent], error) {
	req = req.Normalize()
	items, err := database.QueryAll[ObservabilityEvent](ctx, r.db.Conn(ctx),
		"SELECT * FROM observability_events WHERE org_id = $1 AND event_type = $2 ORDER BY occurred_at DESC LIMIT $3",
		orgID, eventType, req.Limit+1)
	if err != nil {
		return database.Page[ObservabilityEvent]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[ObservabilityEvent]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[ObservabilityEvent]{Items: items}, nil
}

// ----------------------------------------------------------------------------
// Alert Rule Repository
// ----------------------------------------------------------------------------

type alertRuleRepo struct{ db *database.DB }

func NewAlertRuleStore(db *database.DB) AlertRuleStore { return &alertRuleRepo{db: db} }

func (r *alertRuleRepo) Create(ctx context.Context, a *AlertRule) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	if a.QueryType == "" {
		a.QueryType = "promql"
	}
	if a.Condition == "" {
		a.Condition = "gt"
	}
	if a.ForDuration == "" {
		a.ForDuration = "5m"
	}
	if a.Severity == "" {
		a.Severity = SeverityWarning
	}
	if a.CurrentState == "" {
		a.CurrentState = AlertOK
	}
	if len(a.NotificationChannels) == 0 {
		a.NotificationChannels = []byte("[]")
	}

	const sql = `
INSERT INTO alert_rules (id, org_id, name, description, query_type, query, 
    condition, threshold, for_duration, severity, resource_type, resource_id,
    enabled, current_state, notification_channels, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		a.ID, a.OrgID, a.Name, a.Description, a.QueryType, a.Query,
		a.Condition, a.Threshold, a.ForDuration, a.Severity, a.ResourceType, a.ResourceID,
		a.Enabled, a.CurrentState, a.NotificationChannels, a.CreatedBy)
	return database.MapError(row.Scan(&a.CreatedAt, &a.UpdatedAt))
}

func (r *alertRuleRepo) GetByID(ctx context.Context, id string) (*AlertRule, error) {
	a, err := database.QueryOne[AlertRule](ctx, r.db.Conn(ctx),
		"SELECT * FROM alert_rules WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *alertRuleRepo) List(ctx context.Context, orgID string) ([]AlertRule, error) {
	return database.QueryAll[AlertRule](ctx, r.db.Conn(ctx),
		"SELECT * FROM alert_rules WHERE org_id = $1 ORDER BY name", orgID)
}

func (r *alertRuleRepo) ListEnabled(ctx context.Context, orgID string) ([]AlertRule, error) {
	return database.QueryAll[AlertRule](ctx, r.db.Conn(ctx),
		"SELECT * FROM alert_rules WHERE org_id = $1 AND enabled = true ORDER BY name", orgID)
}

func (r *alertRuleRepo) Update(ctx context.Context, a *AlertRule) error {
	const sql = `
UPDATE alert_rules
SET name = $1, description = $2, query_type = $3, query = $4, condition = $5,
    threshold = $6, for_duration = $7, severity = $8, enabled = $9, 
    notification_channels = $10, updated_at = now()
WHERE id = $11`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		a.Name, a.Description, a.QueryType, a.Query, a.Condition,
		a.Threshold, a.ForDuration, a.Severity, a.Enabled,
		a.NotificationChannels, a.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *alertRuleRepo) UpdateState(ctx context.Context, id, state string) error {
	const sql = `UPDATE alert_rules SET current_state = $1, last_evaluated = now(), updated_at = now() WHERE id = $2`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, state, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *alertRuleRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM alert_rules WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Alert History Repository
// ----------------------------------------------------------------------------

type alertHistoryRepo struct{ db *database.DB }

func NewAlertHistoryStore(db *database.DB) AlertHistoryStore { return &alertHistoryRepo{db: db} }

func (r *alertHistoryRepo) Create(ctx context.Context, h *AlertHistory) error {
	if h.ID == "" {
		h.ID = uuid.NewString()
	}
	if len(h.Labels) == 0 {
		h.Labels = []byte("{}")
	}
	if len(h.Annotations) == 0 {
		h.Annotations = []byte("{}")
	}

	const sql = `
INSERT INTO alert_history (id, org_id, rule_id, previous_state, current_state, value, labels, annotations, fired_at, resolved_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := r.db.Conn(ctx).Exec(ctx, sql,
		h.ID, h.OrgID, h.RuleID, h.PreviousState, h.CurrentState, h.Value, h.Labels, h.Annotations, h.FiredAt, h.ResolvedAt)
	return database.MapError(err)
}

func (r *alertHistoryRepo) ListByRule(ctx context.Context, ruleID string, req database.PageRequest) (database.Page[AlertHistory], error) {
	req = req.Normalize()
	items, err := database.QueryAll[AlertHistory](ctx, r.db.Conn(ctx),
		"SELECT * FROM alert_history WHERE rule_id = $1 ORDER BY fired_at DESC LIMIT $2",
		ruleID, req.Limit+1)
	if err != nil {
		return database.Page[AlertHistory]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[AlertHistory]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[AlertHistory]{Items: items}, nil
}

func (r *alertHistoryRepo) ListFiring(ctx context.Context, orgID string) ([]AlertHistory, error) {
	return database.QueryAll[AlertHistory](ctx, r.db.Conn(ctx),
		"SELECT * FROM alert_history WHERE org_id = $1 AND current_state = 'firing' AND resolved_at IS NULL ORDER BY fired_at DESC",
		orgID)
}

// ----------------------------------------------------------------------------
// Log Stream Repository
// ----------------------------------------------------------------------------

type logStreamRepo struct{ db *database.DB }

func NewLogStreamStore(db *database.DB) LogStreamStore { return &logStreamRepo{db: db} }

func (r *logStreamRepo) Upsert(ctx context.Context, s *LogStream) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if len(s.Labels) == 0 {
		s.Labels = []byte("{}")
	}
	if s.Status == "" {
		s.Status = "active"
	}

	const sql = `
INSERT INTO log_streams (id, org_id, stream_name, resource_type, resource_id, labels, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (org_id, stream_name) DO UPDATE SET
    last_seen_at = now(),
    status = 'active',
    updated_at = now()
RETURNING id, first_seen_at, last_seen_at, created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		s.ID, s.OrgID, s.StreamName, s.ResourceType, s.ResourceID, s.Labels, s.Status)
	return database.MapError(row.Scan(&s.ID, &s.FirstSeenAt, &s.LastSeenAt, &s.CreatedAt, &s.UpdatedAt))
}

func (r *logStreamRepo) GetByID(ctx context.Context, id string) (*LogStream, error) {
	s, err := database.QueryOne[LogStream](ctx, r.db.Conn(ctx),
		"SELECT * FROM log_streams WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *logStreamRepo) List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[LogStream], error) {
	req = req.Normalize()
	items, err := database.QueryAll[LogStream](ctx, r.db.Conn(ctx),
		"SELECT * FROM log_streams WHERE org_id = $1 ORDER BY last_seen_at DESC LIMIT $2",
		orgID, req.Limit+1)
	if err != nil {
		return database.Page[LogStream]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[LogStream]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[LogStream]{Items: items}, nil
}

func (r *logStreamRepo) ListByResource(ctx context.Context, resourceType, resourceID string) ([]LogStream, error) {
	return database.QueryAll[LogStream](ctx, r.db.Conn(ctx),
		"SELECT * FROM log_streams WHERE resource_type = $1 AND resource_id = $2 ORDER BY stream_name",
		resourceType, resourceID)
}

func (r *logStreamRepo) UpdateStats(ctx context.Context, id string, logCount, bytesIngested int64) error {
	const sql = `
UPDATE log_streams
SET log_count = log_count + $1, bytes_ingested = bytes_ingested + $2, last_seen_at = now(), updated_at = now()
WHERE id = $3`

	_, err := r.db.Conn(ctx).Exec(ctx, sql, logCount, bytesIngested, id)
	return database.MapError(err)
}

// ----------------------------------------------------------------------------
// Metric Sample Repository
// ----------------------------------------------------------------------------

type metricSampleRepo struct{ db *database.DB }

func NewMetricSampleStore(db *database.DB) MetricSampleStore { return &metricSampleRepo{db: db} }

func (r *metricSampleRepo) Create(ctx context.Context, s *MetricSample) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if len(s.Labels) == 0 {
		s.Labels = []byte("{}")
	}

	const sql = `
INSERT INTO metric_samples (id, org_id, metric_name, resource_type, resource_id, labels, value, timestamp, period_start, period_end)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := r.db.Conn(ctx).Exec(ctx, sql,
		s.ID, s.OrgID, s.MetricName, s.ResourceType, s.ResourceID, s.Labels, s.Value, s.Timestamp, s.PeriodStart, s.PeriodEnd)
	return database.MapError(err)
}

func (r *metricSampleRepo) ListByResource(ctx context.Context, resourceType, resourceID, metricName string, limit int) ([]MetricSample, error) {
	if limit <= 0 {
		limit = 100
	}
	return database.QueryAll[MetricSample](ctx, r.db.Conn(ctx),
		`SELECT * FROM metric_samples 
		 WHERE resource_type = $1 AND resource_id = $2 AND metric_name = $3
		 ORDER BY timestamp DESC LIMIT $4`,
		resourceType, resourceID, metricName, limit)
}

func (r *metricSampleRepo) DeleteOld(ctx context.Context, olderThanDays int) error {
	const sql = `DELETE FROM metric_samples WHERE timestamp < now() - ($1 || ' days')::interval`
	_, err := r.db.Conn(ctx).Exec(ctx, sql, olderThanDays)
	return database.MapError(err)
}

// Helper to marshal data to JSON
func marshalJSON(v any) json.RawMessage {
	if v == nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(v)
	return b
}
