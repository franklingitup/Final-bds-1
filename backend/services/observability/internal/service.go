package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Deps holds service dependencies.
type Deps struct {
	Dashboards    DashboardStore
	HealthChecks  HealthCheckStore
	Events        EventStore
	AlertRules    AlertRuleStore
	AlertHistory  AlertHistoryStore
	LogStreams    LogStreamStore
	MetricSamples MetricSampleStore
	OrgMembers    authz.OrgMemberStore
	Tenant        TenantRunner
	Prometheus    *PrometheusClient
	Loki          *LokiClient
	Logger        *slog.Logger
}

// Service implements observability logic.
type Service struct {
	dashboards    DashboardStore
	healthChecks  HealthCheckStore
	events        EventStore
	alertRules    AlertRuleStore
	alertHistory  AlertHistoryStore
	logStreams    LogStreamStore
	metricSamples MetricSampleStore
	orgMembers    authz.OrgMemberStore
	tenant        TenantRunner
	prometheus    *PrometheusClient
	loki          *LokiClient
	authSvc       *authz.AuthorizationService
	log           *slog.Logger
}

// NewService creates a new observability service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}

	return &Service{
		dashboards:    d.Dashboards,
		healthChecks:  d.HealthChecks,
		events:        d.Events,
		alertRules:    d.AlertRules,
		alertHistory:  d.AlertHistory,
		logStreams:    d.LogStreams,
		metricSamples: d.MetricSamples,
		orgMembers:    d.OrgMembers,
		tenant:        d.Tenant,
		prometheus:    d.Prometheus,
		loki:          d.Loki,
		authSvc:       authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		log:           d.Logger,
	}
}

// ----------------------------------------------------------------------------
// Metrics Queries
// ----------------------------------------------------------------------------

// QueryMetrics queries Prometheus for metrics.
func (s *Service) QueryMetrics(ctx context.Context, orgID, userID string, req MetricsQueryRequest) (*MetricsResponse, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	if s.prometheus == nil {
		return nil, apperrors.Internal("prometheus not configured")
	}

	start, end, err := parseTimeRange(req.Start, req.End)
	if err != nil {
		return nil, apperrors.Validation(err.Error())
	}

	return s.prometheus.QueryRange(ctx, req.Query, start, end, req.Step)
}

// QueryInstantMetrics queries Prometheus for instant metrics.
func (s *Service) QueryInstantMetrics(ctx context.Context, orgID, userID, query string) (*MetricsResponse, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	if s.prometheus == nil {
		return nil, apperrors.Internal("prometheus not configured")
	}

	return s.prometheus.Query(ctx, query, time.Now())
}

// GetResourceMetrics returns aggregated metrics for a resource.
func (s *Service) GetResourceMetrics(ctx context.Context, orgID, userID, resourceType, resourceID string) (*ResourceMetrics, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	metrics := &ResourceMetrics{
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Metrics:      make(map[string]MetricValue),
		Health:       HealthUnknown,
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}

	// Get health status
	var checks []HealthCheck
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		checks, err = s.healthChecks.ListByResource(ctx, resourceType, resourceID)
		return err
	})
	if err == nil && len(checks) > 0 {
		metrics.Health = aggregateHealth(checks)
	}

	// If Prometheus is available, fetch real metrics
	if s.prometheus != nil {
		s.fetchResourceMetricsFromPrometheus(ctx, metrics, resourceType, resourceID)
	}

	return metrics, nil
}

func (s *Service) fetchResourceMetricsFromPrometheus(ctx context.Context, metrics *ResourceMetrics, resourceType, resourceID string) {
	switch resourceType {
	case ResourceCluster:
		s.fetchClusterMetrics(ctx, metrics, resourceID)
	case ResourceDeployment:
		s.fetchDeploymentMetrics(ctx, metrics, resourceID)
	case ResourceApplication:
		s.fetchApplicationMetrics(ctx, metrics, resourceID)
	}
}

func (s *Service) fetchClusterMetrics(ctx context.Context, metrics *ResourceMetrics, clusterID string) {
	queries := map[string]string{
		"cpu_usage":    BuildQuery(StandardQueries["cluster_cpu_usage"], clusterID),
		"memory_usage": BuildQuery(StandardQueries["cluster_memory_usage"], clusterID, clusterID),
		"pod_count":    BuildQuery(StandardQueries["cluster_pod_count"], clusterID),
		"node_count":   BuildQuery(StandardQueries["cluster_node_count"], clusterID),
	}

	for name, query := range queries {
		resp, err := s.prometheus.Query(ctx, query, time.Now())
		if err != nil {
			continue
		}
		if len(resp.Data.Result) > 0 && len(resp.Data.Result[0].Values) > 0 {
			if val, ok := resp.Data.Result[0].Values[0][1].(string); ok {
				var v float64
				fmt.Sscanf(val, "%f", &v)
				metrics.Metrics[name] = MetricValue{Value: v}
			}
		}
	}
}

func (s *Service) fetchDeploymentMetrics(ctx context.Context, metrics *ResourceMetrics, deploymentID string) {
	// Simplified - in production, would need to resolve deployment name and namespace
}

func (s *Service) fetchApplicationMetrics(ctx context.Context, metrics *ResourceMetrics, appID string) {
	// Simplified - in production, would query with app labels
}

// ----------------------------------------------------------------------------
// Logs Queries
// ----------------------------------------------------------------------------

// QueryLogs queries Loki for logs.
func (s *Service) QueryLogs(ctx context.Context, orgID, userID string, req LogsQueryRequest) (*LogsResponse, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	if s.loki == nil {
		return nil, apperrors.Internal("loki not configured")
	}

	start, end, err := parseTimeRange(req.Start, req.End)
	if err != nil {
		return nil, apperrors.Validation(err.Error())
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	return s.loki.QueryRange(ctx, req.Query, limit, start, end, "", req.Direction)
}

// GetLogStreams returns log stream metadata.
func (s *Service) GetLogStreams(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[LogStream], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[LogStream]{}, err
	}

	var result database.Page[LogStream]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		result, err = s.logStreams.List(ctx, orgID, page)
		return err
	})
	return result, err
}

// ----------------------------------------------------------------------------
// Dashboards
// ----------------------------------------------------------------------------

// CreateDashboard creates a new dashboard.
func (s *Service) CreateDashboard(ctx context.Context, orgID, userID string, req CreateDashboardRequest) (*Dashboard, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionReadLogs); err != nil {
		return nil, err
	}

	if req.Name == "" {
		return nil, apperrors.Validation("name is required")
	}

	panelsJSON, _ := json.Marshal(req.Panels)
	varsJSON, _ := json.Marshal(req.Variables)

	dash := &Dashboard{
		OrgID:            orgID,
		Name:             req.Name,
		Description:      req.Description,
		Panels:           panelsJSON,
		Variables:        varsJSON,
		DefaultTimeRange: req.DefaultTimeRange,
		RefreshInterval:  req.RefreshInterval,
		ResourceType:     req.ResourceType,
		ResourceID:       req.ResourceID,
		CreatedBy:        &userID,
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.dashboards.Create(ctx, dash)
	})
	if err != nil {
		return nil, err
	}

	return dash, nil
}

// GetDashboard returns a dashboard by ID.
func (s *Service) GetDashboard(ctx context.Context, orgID, userID, dashboardID string) (*Dashboard, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var dash *Dashboard
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		dash, err = s.dashboards.GetByID(ctx, dashboardID)
		return err
	})
	return dash, err
}

// ListDashboards returns dashboards for an organization.
func (s *Service) ListDashboards(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[Dashboard], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Dashboard]{}, err
	}

	var result database.Page[Dashboard]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		result, err = s.dashboards.List(ctx, orgID, page)
		return err
	})
	return result, err
}

// UpdateDashboard updates a dashboard.
func (s *Service) UpdateDashboard(ctx context.Context, orgID, userID, dashboardID string, req CreateDashboardRequest) (*Dashboard, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionReadLogs); err != nil {
		return nil, err
	}

	var dash *Dashboard
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		dash, err = s.dashboards.GetByID(ctx, dashboardID)
		if err != nil {
			return err
		}

		dash.Name = req.Name
		dash.Description = req.Description
		dash.Panels, _ = json.Marshal(req.Panels)
		dash.Variables, _ = json.Marshal(req.Variables)
		if req.DefaultTimeRange != "" {
			dash.DefaultTimeRange = req.DefaultTimeRange
		}
		if req.RefreshInterval != "" {
			dash.RefreshInterval = req.RefreshInterval
		}

		return s.dashboards.Update(ctx, dash)
	})
	return dash, err
}

// DeleteDashboard deletes a dashboard.
func (s *Service) DeleteDashboard(ctx context.Context, orgID, userID, dashboardID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionReadLogs); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.dashboards.Delete(ctx, dashboardID)
	})
}

// ----------------------------------------------------------------------------
// Health Checks
// ----------------------------------------------------------------------------

// GetHealthSummary returns health summary for an organization.
func (s *Service) GetHealthSummary(ctx context.Context, orgID, userID string) (map[string]interface{}, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var checks []HealthCheck
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		checks, err = s.healthChecks.List(ctx, orgID)
		return err
	})
	if err != nil {
		return nil, err
	}

	summary := map[string]interface{}{
		"total":     len(checks),
		"healthy":   0,
		"degraded":  0,
		"unhealthy": 0,
		"unknown":   0,
	}

	for _, c := range checks {
		switch c.Status {
		case HealthHealthy:
			summary["healthy"] = summary["healthy"].(int) + 1
		case HealthDegraded:
			summary["degraded"] = summary["degraded"].(int) + 1
		case HealthUnhealthy:
			summary["unhealthy"] = summary["unhealthy"].(int) + 1
		default:
			summary["unknown"] = summary["unknown"].(int) + 1
		}
	}

	return summary, nil
}

// GetResourceHealth returns health checks for a resource.
func (s *Service) GetResourceHealth(ctx context.Context, orgID, userID, resourceType, resourceID string) ([]HealthCheck, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var checks []HealthCheck
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		checks, err = s.healthChecks.ListByResource(ctx, resourceType, resourceID)
		return err
	})
	return checks, err
}

// ReportHealth reports health status from an agent.
func (s *Service) ReportHealth(ctx context.Context, orgID string, req ReportHealthRequest) error {
	now := time.Now()

	check := &HealthCheck{
		OrgID:          orgID,
		ResourceType:   req.ResourceType,
		ResourceID:     req.ResourceID,
		CheckName:      req.CheckName,
		Status:         req.Status,
		LastCheckAt:    &now,
		ResponseTimeMs: req.ResponseTimeMs,
		ErrorMessage:   req.ErrorMessage,
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.healthChecks.Upsert(ctx, check)
	})
}

// ----------------------------------------------------------------------------
// Events
// ----------------------------------------------------------------------------

// ListEvents returns observability events.
func (s *Service) ListEvents(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[ObservabilityEvent], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[ObservabilityEvent]{}, err
	}

	var result database.Page[ObservabilityEvent]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		result, err = s.events.List(ctx, orgID, page)
		return err
	})
	return result, err
}

// CreateEvent creates an observability event.
func (s *Service) CreateEvent(ctx context.Context, orgID string, event *ObservabilityEvent) error {
	event.OrgID = orgID
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now()
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.events.Create(ctx, event)
	})
}

// ----------------------------------------------------------------------------
// Alert Rules
// ----------------------------------------------------------------------------

// CreateAlertRule creates an alert rule.
func (s *Service) CreateAlertRule(ctx context.Context, orgID, userID string, req CreateAlertRuleRequest) (*AlertRule, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionReadLogs); err != nil {
		return nil, err
	}

	if req.Name == "" {
		return nil, apperrors.Validation("name is required")
	}
	if req.Query == "" {
		return nil, apperrors.Validation("query is required")
	}

	channelsJSON, _ := json.Marshal(req.Channels)

	rule := &AlertRule{
		OrgID:                orgID,
		Name:                 req.Name,
		Description:          req.Description,
		QueryType:            req.QueryType,
		Query:                req.Query,
		Condition:            req.Condition,
		Threshold:            req.Threshold,
		ForDuration:          req.ForDuration,
		Severity:             req.Severity,
		ResourceType:         req.ResourceType,
		ResourceID:           req.ResourceID,
		Enabled:              true,
		NotificationChannels: channelsJSON,
		CreatedBy:            &userID,
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.alertRules.Create(ctx, rule)
	})
	if err != nil {
		return nil, err
	}

	return rule, nil
}

// ListAlertRules returns alert rules.
func (s *Service) ListAlertRules(ctx context.Context, orgID, userID string) ([]AlertRule, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var rules []AlertRule
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		rules, err = s.alertRules.List(ctx, orgID)
		return err
	})
	return rules, err
}

// GetFiringAlerts returns currently firing alerts.
func (s *Service) GetFiringAlerts(ctx context.Context, orgID, userID string) ([]AlertHistory, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var alerts []AlertHistory
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		alerts, err = s.alertHistory.ListFiring(ctx, orgID)
		return err
	})
	return alerts, err
}

// ----------------------------------------------------------------------------
// Agent Ingest
// ----------------------------------------------------------------------------

// IngestMetrics ingests metrics from an agent.
func (s *Service) IngestMetrics(ctx context.Context, orgID string, req IngestMetricsRequest) error {
	now := time.Now()

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		for _, m := range req.Metrics {
			ts := now
			if m.Timestamp > 0 {
				ts = time.UnixMilli(m.Timestamp)
			}

			sample := &MetricSample{
				OrgID:        orgID,
				MetricName:   m.Name,
				ResourceType: m.ResourceType,
				ResourceID:   m.ResourceID,
				Labels:       marshalJSON(m.Labels),
				Value:        m.Value,
				Timestamp:    ts,
				PeriodStart:  ts.Add(-time.Minute),
				PeriodEnd:    ts,
			}

			if err := s.metricSamples.Create(ctx, sample); err != nil {
				s.log.Warn("failed to store metric sample", "error", err, "metric", m.Name)
			}
		}
		return nil
	})
}

// IngestLogs ingests logs from an agent.
func (s *Service) IngestLogs(ctx context.Context, orgID string, req IngestLogsRequest) error {
	// Forward to Loki if available
	if s.loki != nil {
		if err := s.loki.Push(ctx, req.Streams); err != nil {
			s.log.Warn("failed to push logs to loki", "error", err)
		}
	}

	// Track log streams
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		for _, entry := range req.Streams {
			streamName := buildStreamName(entry.Stream)
			resourceType := entry.Stream["resource_type"]
			resourceID := entry.Stream["resource_id"]
			if resourceType == "" {
				resourceType = "unknown"
			}

			stream := &LogStream{
				OrgID:        orgID,
				StreamName:   streamName,
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Labels:       marshalJSON(entry.Stream),
			}

			if err := s.logStreams.Upsert(ctx, stream); err != nil {
				s.log.Warn("failed to upsert log stream", "error", err, "stream", streamName)
			}
		}
		return nil
	})
}

// ----------------------------------------------------------------------------
// Overview
// ----------------------------------------------------------------------------

// GetOverview returns platform overview statistics.
func (s *Service) GetOverview(ctx context.Context, orgID, userID string) (*OverviewStats, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	stats := &OverviewStats{}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Get health check counts
		checks, _ := s.healthChecks.List(ctx, orgID)
		for _, c := range checks {
			if c.ResourceType == ResourceCluster {
				stats.TotalClusters++
				if c.Status == HealthHealthy {
					stats.HealthyClusters++
				}
			} else if c.ResourceType == ResourceDeployment {
				stats.TotalDeployments++
				if c.Status == HealthHealthy {
					stats.ActiveDeployments++
				}
			}
		}

		// Get alert counts
		rules, _ := s.alertRules.List(ctx, orgID)
		stats.TotalAlerts = len(rules)

		firing, _ := s.alertHistory.ListFiring(ctx, orgID)
		stats.FiringAlerts = len(firing)

		return nil
	})

	return stats, err
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func parseTimeRange(startStr, endStr string) (time.Time, time.Time, error) {
	now := time.Now()

	// Parse start time
	start := now.Add(-1 * time.Hour)
	if startStr != "" {
		if d, err := time.ParseDuration(startStr); err == nil {
			start = now.Add(-d)
		} else if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		}
	}

	// Parse end time
	end := now
	if endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		}
	}

	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("start time cannot be after end time")
	}

	return start, end, nil
}

func aggregateHealth(checks []HealthCheck) string {
	if len(checks) == 0 {
		return HealthUnknown
	}

	hasUnhealthy := false
	hasDegraded := false

	for _, c := range checks {
		switch c.Status {
		case HealthUnhealthy:
			hasUnhealthy = true
		case HealthDegraded:
			hasDegraded = true
		}
	}

	if hasUnhealthy {
		return HealthUnhealthy
	}
	if hasDegraded {
		return HealthDegraded
	}
	return HealthHealthy
}

func buildStreamName(labels map[string]string) string {
	name := ""
	if ns := labels["namespace"]; ns != "" {
		name += ns + "/"
	}
	if app := labels["app"]; app != "" {
		name += app
	} else if pod := labels["pod"]; pod != "" {
		name += pod
	} else if container := labels["container"]; container != "" {
		name += container
	} else {
		name += "unknown"
	}
	return name
}
