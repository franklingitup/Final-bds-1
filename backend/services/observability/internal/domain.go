// Package observability ingests logs/metrics from agents and serves query APIs.
package observability

import (
	"encoding/json"
	"time"
)

// Resource types.
const (
	ResourceCluster     = "cluster"
	ResourceDeployment  = "deployment"
	ResourceApplication = "application"
	ResourceNode        = "node"
	ResourcePod         = "pod"
	ResourceService     = "service"
)

// Metric types.
const (
	MetricGauge     = "gauge"
	MetricCounter   = "counter"
	MetricHistogram = "histogram"
	MetricSummary   = "summary"
)

// Aggregation types.
const (
	AggAvg  = "avg"
	AggSum  = "sum"
	AggMin  = "min"
	AggMax  = "max"
	AggLast = "last"
	AggRate = "rate"
)

// Health status.
const (
	HealthHealthy   = "healthy"
	HealthDegraded  = "degraded"
	HealthUnhealthy = "unhealthy"
	HealthUnknown   = "unknown"
)

// Event severity.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityError    = "error"
	SeverityCritical = "critical"
)

// Alert states.
const (
	AlertOK       = "ok"
	AlertPending  = "pending"
	AlertFiring   = "firing"
	AlertResolved = "resolved"
)

// ----------------------------------------------------------------------------
// Metric Definitions
// ----------------------------------------------------------------------------

// MetricDefinition defines a metric to track.
type MetricDefinition struct {
	ID                 string          `db:"id"`
	OrgID              string          `db:"org_id"`
	Name               string          `db:"name"`
	DisplayName        string          `db:"display_name"`
	Description        *string         `db:"description"`
	Unit               string          `db:"unit"`
	MetricType         string          `db:"metric_type"`
	LabelKeys          json.RawMessage `db:"label_keys"`
	Aggregation        string          `db:"aggregation"`
	SourceType         string          `db:"source_type"`
	PrometheusQuery    *string         `db:"prometheus_query"`
	WarningThreshold   *float64        `db:"warning_threshold"`
	CriticalThreshold  *float64        `db:"critical_threshold"`
	ThresholdDirection *string         `db:"threshold_direction"`
	CreatedAt          time.Time       `db:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at"`
}

// MetricSample is a recorded metric value.
type MetricSample struct {
	ID           string          `db:"id"`
	OrgID        string          `db:"org_id"`
	MetricName   string          `db:"metric_name"`
	ResourceType string          `db:"resource_type"`
	ResourceID   string          `db:"resource_id"`
	Labels       json.RawMessage `db:"labels"`
	Value        float64         `db:"value"`
	Timestamp    time.Time       `db:"timestamp"`
	PeriodStart  time.Time       `db:"period_start"`
	PeriodEnd    time.Time       `db:"period_end"`
}

// ----------------------------------------------------------------------------
// Log Streams
// ----------------------------------------------------------------------------

// LogStream represents a source of logs.
type LogStream struct {
	ID            string          `db:"id"`
	OrgID         string          `db:"org_id"`
	StreamName    string          `db:"stream_name"`
	ResourceType  string          `db:"resource_type"`
	ResourceID    string          `db:"resource_id"`
	Labels        json.RawMessage `db:"labels"`
	FirstSeenAt   time.Time       `db:"first_seen_at"`
	LastSeenAt    time.Time       `db:"last_seen_at"`
	LogCount      int64           `db:"log_count"`
	BytesIngested int64           `db:"bytes_ingested"`
	Status        string          `db:"status"`
	CreatedAt     time.Time       `db:"created_at"`
	UpdatedAt     time.Time       `db:"updated_at"`
}

// LogQuery is a saved log query.
type LogQuery struct {
	ID           string    `db:"id"`
	OrgID        string    `db:"org_id"`
	Name         string    `db:"name"`
	Description  *string   `db:"description"`
	LogQLQuery   string    `db:"logql_query"`
	ResourceType *string   `db:"resource_type"`
	ResourceID   *string   `db:"resource_id"`
	IsShared     bool      `db:"is_shared"`
	CreatedBy    *string   `db:"created_by"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

// ----------------------------------------------------------------------------
// Dashboards
// ----------------------------------------------------------------------------

// Dashboard is a configurable metrics/logs dashboard.
type Dashboard struct {
	ID               string          `db:"id"`
	OrgID            string          `db:"org_id"`
	Name             string          `db:"name"`
	Description      *string         `db:"description"`
	Panels           json.RawMessage `db:"panels"`
	Variables        json.RawMessage `db:"variables"`
	DefaultTimeRange string          `db:"default_time_range"`
	RefreshInterval  string          `db:"refresh_interval"`
	IsShared         bool            `db:"is_shared"`
	IsDefault        bool            `db:"is_default"`
	ResourceType     *string         `db:"resource_type"`
	ResourceID       *string         `db:"resource_id"`
	CreatedBy        *string         `db:"created_by"`
	Version          int64           `db:"version"`
	CreatedAt        time.Time       `db:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
}

// DashboardPanel defines a panel in a dashboard.
type DashboardPanel struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Type        string          `json:"type"` // graph | stat | table | logs | gauge
	GridPos     GridPosition    `json:"gridPos"`
	DataSource  string          `json:"dataSource"` // prometheus | loki
	Query       string          `json:"query"`
	Legend      *string         `json:"legend,omitempty"`
	Unit        *string         `json:"unit,omitempty"`
	Thresholds  []Threshold     `json:"thresholds,omitempty"`
	Options     json.RawMessage `json:"options,omitempty"`
}

// GridPosition defines panel position in a grid.
type GridPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Threshold defines visual thresholds.
type Threshold struct {
	Value float64 `json:"value"`
	Color string  `json:"color"`
}

// ----------------------------------------------------------------------------
// Health Checks
// ----------------------------------------------------------------------------

// HealthCheck tracks health status of a resource.
type HealthCheck struct {
	ID              string     `db:"id"`
	OrgID           string     `db:"org_id"`
	ResourceType    string     `db:"resource_type"`
	ResourceID      string     `db:"resource_id"`
	CheckName       string     `db:"check_name"`
	CheckType       string     `db:"check_type"`
	Endpoint        *string    `db:"endpoint"`
	IntervalSeconds int        `db:"interval_seconds"`
	TimeoutSeconds  int        `db:"timeout_seconds"`
	Status          string     `db:"status"`
	LastCheckAt     *time.Time `db:"last_check_at"`
	LastSuccessAt   *time.Time `db:"last_success_at"`
	LastFailureAt   *time.Time `db:"last_failure_at"`
	FailureCount    int        `db:"failure_count"`
	ResponseTimeMs  *int       `db:"response_time_ms"`
	ErrorMessage    *string    `db:"error_message"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
}

// ----------------------------------------------------------------------------
// Events
// ----------------------------------------------------------------------------

// ObservabilityEvent is a platform event for correlation.
type ObservabilityEvent struct {
	ID           string          `db:"id"`
	OrgID        string          `db:"org_id"`
	EventType    string          `db:"event_type"`
	Severity     string          `db:"severity"`
	Title        string          `db:"title"`
	Description  *string         `db:"description"`
	ResourceType string          `db:"resource_type"`
	ResourceID   string          `db:"resource_id"`
	Metadata     json.RawMessage `db:"metadata"`
	OccurredAt   time.Time       `db:"occurred_at"`
	ResolvedAt   *time.Time      `db:"resolved_at"`
	CreatedAt    time.Time       `db:"created_at"`
}

// ----------------------------------------------------------------------------
// Alerts
// ----------------------------------------------------------------------------

// AlertRule defines an alerting rule.
type AlertRule struct {
	ID                   string          `db:"id"`
	OrgID                string          `db:"org_id"`
	Name                 string          `db:"name"`
	Description          *string         `db:"description"`
	QueryType            string          `db:"query_type"`
	Query                string          `db:"query"`
	Condition            string          `db:"condition"`
	Threshold            float64         `db:"threshold"`
	ForDuration          string          `db:"for_duration"`
	Severity             string          `db:"severity"`
	ResourceType         *string         `db:"resource_type"`
	ResourceID           *string         `db:"resource_id"`
	Enabled              bool            `db:"enabled"`
	LastEvaluated        *time.Time      `db:"last_evaluated"`
	CurrentState         string          `db:"current_state"`
	NotificationChannels json.RawMessage `db:"notification_channels"`
	CreatedBy            *string         `db:"created_by"`
	CreatedAt            time.Time       `db:"created_at"`
	UpdatedAt            time.Time       `db:"updated_at"`
}

// AlertHistory records alert state changes.
type AlertHistory struct {
	ID            string          `db:"id"`
	OrgID         string          `db:"org_id"`
	RuleID        string          `db:"rule_id"`
	PreviousState string          `db:"previous_state"`
	CurrentState  string          `db:"current_state"`
	Value         *float64        `db:"value"`
	Labels        json.RawMessage `db:"labels"`
	Annotations   json.RawMessage `db:"annotations"`
	FiredAt       time.Time       `db:"fired_at"`
	ResolvedAt    *time.Time      `db:"resolved_at"`
}

// ----------------------------------------------------------------------------
// Request DTOs
// ----------------------------------------------------------------------------

// CreateDashboardRequest is the request to create a dashboard.
type CreateDashboardRequest struct {
	Name             string           `json:"name"`
	Description      *string          `json:"description,omitempty"`
	Panels           []DashboardPanel `json:"panels"`
	Variables        []DashboardVar   `json:"variables,omitempty"`
	DefaultTimeRange string           `json:"defaultTimeRange,omitempty"`
	RefreshInterval  string           `json:"refreshInterval,omitempty"`
	ResourceType     *string          `json:"resourceType,omitempty"`
	ResourceID       *string          `json:"resourceId,omitempty"`
}

// DashboardVar defines a dashboard variable.
type DashboardVar struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Type    string   `json:"type"` // query | custom | constant
	Query   string   `json:"query,omitempty"`
	Options []string `json:"options,omitempty"`
	Current string   `json:"current,omitempty"`
}

// MetricsQueryRequest is the request to query metrics.
type MetricsQueryRequest struct {
	Query      string `json:"query"`
	Start      string `json:"start"`
	End        string `json:"end"`
	Step       string `json:"step,omitempty"`
	ResourceID string `json:"resourceId,omitempty"`
}

// LogsQueryRequest is the request to query logs.
type LogsQueryRequest struct {
	Query      string `json:"query"`
	Start      string `json:"start"`
	End        string `json:"end"`
	Limit      int    `json:"limit,omitempty"`
	Direction  string `json:"direction,omitempty"` // forward | backward
	ResourceID string `json:"resourceId,omitempty"`
}

// CreateAlertRuleRequest is the request to create an alert rule.
type CreateAlertRuleRequest struct {
	Name         string   `json:"name"`
	Description  *string  `json:"description,omitempty"`
	QueryType    string   `json:"queryType"`
	Query        string   `json:"query"`
	Condition    string   `json:"condition"`
	Threshold    float64  `json:"threshold"`
	ForDuration  string   `json:"forDuration,omitempty"`
	Severity     string   `json:"severity,omitempty"`
	ResourceType *string  `json:"resourceType,omitempty"`
	ResourceID   *string  `json:"resourceId,omitempty"`
	Channels     []string `json:"channels,omitempty"`
}

// IngestMetricsRequest is the request from agents to ingest metrics.
type IngestMetricsRequest struct {
	ClusterID string             `json:"clusterId"`
	Metrics   []IngestMetricData `json:"metrics"`
}

// IngestMetricData is a single metric data point.
type IngestMetricData struct {
	Name         string            `json:"name"`
	ResourceType string            `json:"resourceType"`
	ResourceID   string            `json:"resourceId"`
	Labels       map[string]string `json:"labels,omitempty"`
	Value        float64           `json:"value"`
	Timestamp    int64             `json:"timestamp,omitempty"` // Unix ms
}

// IngestLogsRequest is the request from agents to ingest logs.
type IngestLogsRequest struct {
	ClusterID string           `json:"clusterId"`
	Streams   []IngestLogEntry `json:"streams"`
}

// IngestLogEntry is a batch of log lines.
type IngestLogEntry struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"` // [[timestamp, line], ...]
}

// ReportHealthRequest is the request to report health status.
type ReportHealthRequest struct {
	ResourceType   string  `json:"resourceType"`
	ResourceID     string  `json:"resourceId"`
	CheckName      string  `json:"checkName"`
	Status         string  `json:"status"`
	ResponseTimeMs *int    `json:"responseTimeMs,omitempty"`
	ErrorMessage   *string `json:"errorMessage,omitempty"`
}

// ----------------------------------------------------------------------------
// View Models
// ----------------------------------------------------------------------------

// MetricDefinitionView is the API response for a metric definition.
type MetricDefinitionView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Description *string  `json:"description,omitempty"`
	Unit        string   `json:"unit"`
	MetricType  string   `json:"metricType"`
	Aggregation string   `json:"aggregation"`
	SourceType  string   `json:"sourceType"`
	CreatedAt   string   `json:"createdAt"`
}

func ToMetricDefinitionView(m *MetricDefinition) MetricDefinitionView {
	return MetricDefinitionView{
		ID:          m.ID,
		Name:        m.Name,
		DisplayName: m.DisplayName,
		Description: m.Description,
		Unit:        m.Unit,
		MetricType:  m.MetricType,
		Aggregation: m.Aggregation,
		SourceType:  m.SourceType,
		CreatedAt:   m.CreatedAt.Format(time.RFC3339),
	}
}

// DashboardView is the API response for a dashboard.
type DashboardView struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Description      *string          `json:"description,omitempty"`
	Panels           []DashboardPanel `json:"panels"`
	Variables        []DashboardVar   `json:"variables"`
	DefaultTimeRange string           `json:"defaultTimeRange"`
	RefreshInterval  string           `json:"refreshInterval"`
	IsShared         bool             `json:"isShared"`
	IsDefault        bool             `json:"isDefault"`
	ResourceType     *string          `json:"resourceType,omitempty"`
	ResourceID       *string          `json:"resourceId,omitempty"`
	Version          int64            `json:"version"`
	CreatedAt        string           `json:"createdAt"`
	UpdatedAt        string           `json:"updatedAt"`
}

func ToDashboardView(d *Dashboard) DashboardView {
	var panels []DashboardPanel
	var variables []DashboardVar
	_ = json.Unmarshal(d.Panels, &panels)
	_ = json.Unmarshal(d.Variables, &variables)

	return DashboardView{
		ID:               d.ID,
		Name:             d.Name,
		Description:      d.Description,
		Panels:           panels,
		Variables:        variables,
		DefaultTimeRange: d.DefaultTimeRange,
		RefreshInterval:  d.RefreshInterval,
		IsShared:         d.IsShared,
		IsDefault:        d.IsDefault,
		ResourceType:     d.ResourceType,
		ResourceID:       d.ResourceID,
		Version:          d.Version,
		CreatedAt:        d.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        d.UpdatedAt.Format(time.RFC3339),
	}
}

// HealthCheckView is the API response for a health check.
type HealthCheckView struct {
	ID             string  `json:"id"`
	ResourceType   string  `json:"resourceType"`
	ResourceID     string  `json:"resourceId"`
	CheckName      string  `json:"checkName"`
	CheckType      string  `json:"checkType"`
	Status         string  `json:"status"`
	LastCheckAt    *string `json:"lastCheckAt,omitempty"`
	ResponseTimeMs *int    `json:"responseTimeMs,omitempty"`
	ErrorMessage   *string `json:"errorMessage,omitempty"`
	FailureCount   int     `json:"failureCount"`
}

func ToHealthCheckView(h *HealthCheck) HealthCheckView {
	view := HealthCheckView{
		ID:             h.ID,
		ResourceType:   h.ResourceType,
		ResourceID:     h.ResourceID,
		CheckName:      h.CheckName,
		CheckType:      h.CheckType,
		Status:         h.Status,
		ResponseTimeMs: h.ResponseTimeMs,
		ErrorMessage:   h.ErrorMessage,
		FailureCount:   h.FailureCount,
	}
	if h.LastCheckAt != nil {
		s := h.LastCheckAt.Format(time.RFC3339)
		view.LastCheckAt = &s
	}
	return view
}

// EventView is the API response for an observability event.
type EventView struct {
	ID           string                 `json:"id"`
	EventType    string                 `json:"eventType"`
	Severity     string                 `json:"severity"`
	Title        string                 `json:"title"`
	Description  *string                `json:"description,omitempty"`
	ResourceType string                 `json:"resourceType"`
	ResourceID   string                 `json:"resourceId"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	OccurredAt   string                 `json:"occurredAt"`
	ResolvedAt   *string                `json:"resolvedAt,omitempty"`
}

func ToEventView(e *ObservabilityEvent) EventView {
	view := EventView{
		ID:           e.ID,
		EventType:    e.EventType,
		Severity:     e.Severity,
		Title:        e.Title,
		Description:  e.Description,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		OccurredAt:   e.OccurredAt.Format(time.RFC3339),
	}
	if len(e.Metadata) > 0 {
		_ = json.Unmarshal(e.Metadata, &view.Metadata)
	}
	if e.ResolvedAt != nil {
		s := e.ResolvedAt.Format(time.RFC3339)
		view.ResolvedAt = &s
	}
	return view
}

// AlertRuleView is the API response for an alert rule.
type AlertRuleView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  *string  `json:"description,omitempty"`
	QueryType    string   `json:"queryType"`
	Query        string   `json:"query"`
	Condition    string   `json:"condition"`
	Threshold    float64  `json:"threshold"`
	ForDuration  string   `json:"forDuration"`
	Severity     string   `json:"severity"`
	Enabled      bool     `json:"enabled"`
	CurrentState string   `json:"currentState"`
	Channels     []string `json:"channels"`
	CreatedAt    string   `json:"createdAt"`
}

func ToAlertRuleView(a *AlertRule) AlertRuleView {
	var channels []string
	_ = json.Unmarshal(a.NotificationChannels, &channels)

	return AlertRuleView{
		ID:           a.ID,
		Name:         a.Name,
		Description:  a.Description,
		QueryType:    a.QueryType,
		Query:        a.Query,
		Condition:    a.Condition,
		Threshold:    a.Threshold,
		ForDuration:  a.ForDuration,
		Severity:     a.Severity,
		Enabled:      a.Enabled,
		CurrentState: a.CurrentState,
		Channels:     channels,
		CreatedAt:    a.CreatedAt.Format(time.RFC3339),
	}
}

// LogStreamView is the API response for a log stream.
type LogStreamView struct {
	ID            string            `json:"id"`
	StreamName    string            `json:"streamName"`
	ResourceType  string            `json:"resourceType"`
	ResourceID    string            `json:"resourceId"`
	Labels        map[string]string `json:"labels"`
	LogCount      int64             `json:"logCount"`
	BytesIngested int64             `json:"bytesIngested"`
	Status        string            `json:"status"`
	LastSeenAt    string            `json:"lastSeenAt"`
}

func ToLogStreamView(l *LogStream) LogStreamView {
	var labels map[string]string
	_ = json.Unmarshal(l.Labels, &labels)

	return LogStreamView{
		ID:            l.ID,
		StreamName:    l.StreamName,
		ResourceType:  l.ResourceType,
		ResourceID:    l.ResourceID,
		Labels:        labels,
		LogCount:      l.LogCount,
		BytesIngested: l.BytesIngested,
		Status:        l.Status,
		LastSeenAt:    l.LastSeenAt.Format(time.RFC3339),
	}
}

// MetricsResponse is the response for a metrics query.
type MetricsResponse struct {
	Status string        `json:"status"`
	Data   MetricsResult `json:"data"`
}

// MetricsResult contains the metrics query result.
type MetricsResult struct {
	ResultType string             `json:"resultType"` // matrix | vector | scalar
	Result     []MetricSeriesData `json:"result"`
}

// MetricSeriesData is a single metric series.
type MetricSeriesData struct {
	Metric map[string]string `json:"metric"`
	Values [][]interface{}   `json:"values"` // [[timestamp, value], ...]
}

// LogsResponse is the response for a logs query.
type LogsResponse struct {
	Status string     `json:"status"`
	Data   LogsResult `json:"data"`
}

// LogsResult contains the logs query result.
type LogsResult struct {
	ResultType string           `json:"resultType"` // streams
	Result     []LogStreamData  `json:"result"`
	Stats      *LogsQueryStats  `json:"stats,omitempty"`
}

// LogStreamData is a single log stream.
type LogStreamData struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"` // [[timestamp, line], ...]
}

// LogsQueryStats contains query statistics.
type LogsQueryStats struct {
	Summary LogsStatsSummary `json:"summary"`
}

// LogsStatsSummary contains query timing stats.
type LogsStatsSummary struct {
	BytesProcessed int64   `json:"bytesProcessedPerSecond"`
	LinesProcessed int64   `json:"linesProcessedPerSecond"`
	TotalBytes     int64   `json:"totalBytesProcessed"`
	TotalLines     int64   `json:"totalLinesProcessed"`
	ExecTime       float64 `json:"execTime"`
}

// ResourceMetrics contains aggregated metrics for a resource.
type ResourceMetrics struct {
	ResourceType string                  `json:"resourceType"`
	ResourceID   string                  `json:"resourceId"`
	ResourceName string                  `json:"resourceName,omitempty"`
	Metrics      map[string]MetricValue  `json:"metrics"`
	Health       string                  `json:"health"`
	UpdatedAt    string                  `json:"updatedAt"`
}

// MetricValue is a single metric value with metadata.
type MetricValue struct {
	Value     float64  `json:"value"`
	Unit      string   `json:"unit,omitempty"`
	Trend     *string  `json:"trend,omitempty"` // up | down | stable
	Change    *float64 `json:"change,omitempty"`
	Threshold *string  `json:"threshold,omitempty"` // ok | warning | critical
}

// OverviewStats contains platform-wide statistics.
type OverviewStats struct {
	TotalClusters     int     `json:"totalClusters"`
	HealthyClusters   int     `json:"healthyClusters"`
	TotalDeployments  int     `json:"totalDeployments"`
	ActiveDeployments int     `json:"activeDeployments"`
	TotalAlerts       int     `json:"totalAlerts"`
	FiringAlerts      int     `json:"firingAlerts"`
	ErrorRate         float64 `json:"errorRate"`
	RequestRate       float64 `json:"requestRate"`
}
