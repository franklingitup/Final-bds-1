package observability

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseTimeRange(t *testing.T) {
	tests := []struct {
		name      string
		start     string
		end       string
		wantErr   bool
	}{
		{
			name:    "duration format",
			start:   "1h",
			end:     "",
			wantErr: false,
		},
		{
			name:    "RFC3339 format",
			start:   "2024-01-01T00:00:00Z",
			end:     "2024-01-01T01:00:00Z",
			wantErr: false,
		},
		{
			name:    "empty defaults to 1h",
			start:   "",
			end:     "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parseTimeRange(tt.start, tt.end)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTimeRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if start.After(end) {
					t.Errorf("start %v should be before end %v", start, end)
				}
			}
		})
	}
}

func TestAggregateHealth(t *testing.T) {
	tests := []struct {
		name   string
		checks []HealthCheck
		want   string
	}{
		{
			name:   "empty checks",
			checks: []HealthCheck{},
			want:   HealthUnknown,
		},
		{
			name: "all healthy",
			checks: []HealthCheck{
				{Status: HealthHealthy},
				{Status: HealthHealthy},
			},
			want: HealthHealthy,
		},
		{
			name: "one degraded",
			checks: []HealthCheck{
				{Status: HealthHealthy},
				{Status: HealthDegraded},
			},
			want: HealthDegraded,
		},
		{
			name: "one unhealthy",
			checks: []HealthCheck{
				{Status: HealthHealthy},
				{Status: HealthUnhealthy},
			},
			want: HealthUnhealthy,
		},
		{
			name: "unhealthy takes precedence",
			checks: []HealthCheck{
				{Status: HealthDegraded},
				{Status: HealthUnhealthy},
			},
			want: HealthUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := aggregateHealth(tt.checks)
			if got != tt.want {
				t.Errorf("aggregateHealth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildStreamName(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name:   "namespace and app",
			labels: map[string]string{"namespace": "default", "app": "myapp"},
			want:   "default/myapp",
		},
		{
			name:   "namespace and pod",
			labels: map[string]string{"namespace": "default", "pod": "mypod-123"},
			want:   "default/mypod-123",
		},
		{
			name:   "only namespace",
			labels: map[string]string{"namespace": "default"},
			want:   "default/unknown",
		},
		{
			name:   "only app",
			labels: map[string]string{"app": "myapp"},
			want:   "myapp",
		},
		{
			name:   "empty labels",
			labels: map[string]string{},
			want:   "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildStreamName(tt.labels)
			if got != tt.want {
				t.Errorf("buildStreamName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildQuery(t *testing.T) {
	tests := []struct {
		template string
		args     []interface{}
		want     string
	}{
		{
			template: StandardQueries["cluster_cpu_usage"],
			args:     []interface{}{"cluster-1"},
			want:     `sum(rate(container_cpu_usage_seconds_total{cluster="cluster-1"}[5m])) * 100`,
		},
		{
			template: StandardQueries["cluster_pod_count"],
			args:     []interface{}{"my-cluster"},
			want:     `count(kube_pod_info{cluster="my-cluster"})`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.want[:20], func(t *testing.T) {
			got := BuildQuery(tt.template, tt.args...)
			if got != tt.want {
				t.Errorf("BuildQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildLogQuery(t *testing.T) {
	tests := []struct {
		template string
		args     []interface{}
		want     string
	}{
		{
			template: StandardLogQueries["deployment_logs"],
			args:     []interface{}{"default", "myapp"},
			want:     `{namespace="default",app="myapp"}`,
		},
		{
			template: StandardLogQueries["pod_logs"],
			args:     []interface{}{"kube-system", "coredns-abc123"},
			want:     `{namespace="kube-system",pod="coredns-abc123"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.want[:20], func(t *testing.T) {
			got := BuildLogQuery(tt.template, tt.args...)
			if got != tt.want {
				t.Errorf("BuildLogQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogQueryBuilder(t *testing.T) {
	tests := []struct {
		name string
		fn   func() string
		want string
	}{
		{
			name: "simple selector",
			fn: func() string {
				return NewLogQueryBuilder(`{app="myapp"}`).Build()
			},
			want: `{app="myapp"}`,
		},
		{
			name: "with contains filter",
			fn: func() string {
				return NewLogQueryBuilder(`{app="myapp"}`).Filter("contains", "error").Build()
			},
			want: `{app="myapp"} |= "error"`,
		},
		{
			name: "with json parser",
			fn: func() string {
				return NewLogQueryBuilder(`{app="myapp"}`).JSON().Build()
			},
			want: `{app="myapp"} | json`,
		},
		{
			name: "with multiple filters",
			fn: func() string {
				return NewLogQueryBuilder(`{app="myapp"}`).
					Filter("contains", "error").
					Filter("not_contains", "debug").
					Build()
			},
			want: `{app="myapp"} |= "error" != "debug"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn()
			if got != tt.want {
				t.Errorf("LogQueryBuilder = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	// Resource types
	if ResourceCluster != "cluster" {
		t.Errorf("ResourceCluster = %q", ResourceCluster)
	}
	if ResourceDeployment != "deployment" {
		t.Errorf("ResourceDeployment = %q", ResourceDeployment)
	}

	// Metric types
	if MetricGauge != "gauge" {
		t.Errorf("MetricGauge = %q", MetricGauge)
	}
	if MetricCounter != "counter" {
		t.Errorf("MetricCounter = %q", MetricCounter)
	}

	// Health status
	if HealthHealthy != "healthy" {
		t.Errorf("HealthHealthy = %q", HealthHealthy)
	}
	if HealthUnhealthy != "unhealthy" {
		t.Errorf("HealthUnhealthy = %q", HealthUnhealthy)
	}

	// Alert states
	if AlertOK != "ok" {
		t.Errorf("AlertOK = %q", AlertOK)
	}
	if AlertFiring != "firing" {
		t.Errorf("AlertFiring = %q", AlertFiring)
	}
}

func TestToMetricDefinitionView(t *testing.T) {
	now := time.Now()
	desc := "CPU usage percentage"
	def := &MetricDefinition{
		ID:          "def-123",
		Name:        "cpu_usage",
		DisplayName: "CPU Usage",
		Description: &desc,
		Unit:        "percent",
		MetricType:  MetricGauge,
		Aggregation: AggAvg,
		SourceType:  "prometheus",
		CreatedAt:   now,
	}

	view := ToMetricDefinitionView(def)

	if view.ID != "def-123" {
		t.Errorf("ID = %q, want %q", view.ID, "def-123")
	}
	if view.Name != "cpu_usage" {
		t.Errorf("Name = %q, want %q", view.Name, "cpu_usage")
	}
	if view.DisplayName != "CPU Usage" {
		t.Errorf("DisplayName = %q, want %q", view.DisplayName, "CPU Usage")
	}
	if view.Unit != "percent" {
		t.Errorf("Unit = %q, want %q", view.Unit, "percent")
	}
}

func TestToDashboardView(t *testing.T) {
	now := time.Now()
	panels := []DashboardPanel{{ID: "p1", Title: "Test", Type: "graph"}}
	panelsJSON, _ := json.Marshal(panels)

	dash := &Dashboard{
		ID:               "dash-123",
		OrgID:            "org-123",
		Name:             "My Dashboard",
		Panels:           panelsJSON,
		Variables:        []byte("[]"),
		DefaultTimeRange: "1h",
		RefreshInterval:  "30s",
		IsDefault:        true,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	view := ToDashboardView(dash)

	if view.ID != "dash-123" {
		t.Errorf("ID = %q, want %q", view.ID, "dash-123")
	}
	if view.Name != "My Dashboard" {
		t.Errorf("Name = %q, want %q", view.Name, "My Dashboard")
	}
	if len(view.Panels) != 1 {
		t.Errorf("Panels count = %d, want 1", len(view.Panels))
	}
	if !view.IsDefault {
		t.Error("IsDefault should be true")
	}
}

func TestToHealthCheckView(t *testing.T) {
	now := time.Now()
	responseTime := 50
	check := &HealthCheck{
		ID:             "hc-123",
		ResourceType:   ResourceCluster,
		ResourceID:     "cluster-1",
		CheckName:      "api",
		CheckType:      "http",
		Status:         HealthHealthy,
		LastCheckAt:    &now,
		ResponseTimeMs: &responseTime,
		FailureCount:   0,
	}

	view := ToHealthCheckView(check)

	if view.ID != "hc-123" {
		t.Errorf("ID = %q, want %q", view.ID, "hc-123")
	}
	if view.Status != HealthHealthy {
		t.Errorf("Status = %q, want %q", view.Status, HealthHealthy)
	}
	if view.ResponseTimeMs == nil || *view.ResponseTimeMs != 50 {
		t.Error("ResponseTimeMs should be 50")
	}
	if view.LastCheckAt == nil {
		t.Error("LastCheckAt should be set")
	}
}

func TestToEventView(t *testing.T) {
	now := time.Now()
	event := &ObservabilityEvent{
		ID:           "evt-123",
		EventType:    "deployment",
		Severity:     SeverityInfo,
		Title:        "Deployment started",
		ResourceType: ResourceDeployment,
		ResourceID:   "dep-1",
		Metadata:     []byte(`{"version":"v1.2.3"}`),
		OccurredAt:   now,
	}

	view := ToEventView(event)

	if view.ID != "evt-123" {
		t.Errorf("ID = %q, want %q", view.ID, "evt-123")
	}
	if view.EventType != "deployment" {
		t.Errorf("EventType = %q, want %q", view.EventType, "deployment")
	}
	if view.Severity != SeverityInfo {
		t.Errorf("Severity = %q, want %q", view.Severity, SeverityInfo)
	}
	if view.Metadata == nil {
		t.Error("Metadata should be parsed")
	}
}

func TestToAlertRuleView(t *testing.T) {
	now := time.Now()
	channels := []string{"slack", "email"}
	channelsJSON, _ := json.Marshal(channels)

	rule := &AlertRule{
		ID:                   "rule-123",
		Name:                 "High CPU",
		QueryType:            "promql",
		Query:                "cpu_usage > 80",
		Condition:            "gt",
		Threshold:            80,
		ForDuration:          "5m",
		Severity:             SeverityWarning,
		Enabled:              true,
		CurrentState:         AlertOK,
		NotificationChannels: channelsJSON,
		CreatedAt:            now,
	}

	view := ToAlertRuleView(rule)

	if view.ID != "rule-123" {
		t.Errorf("ID = %q, want %q", view.ID, "rule-123")
	}
	if view.Name != "High CPU" {
		t.Errorf("Name = %q, want %q", view.Name, "High CPU")
	}
	if view.Threshold != 80 {
		t.Errorf("Threshold = %f, want 80", view.Threshold)
	}
	if len(view.Channels) != 2 {
		t.Errorf("Channels count = %d, want 2", len(view.Channels))
	}
}

func TestToLogStreamView(t *testing.T) {
	now := time.Now()
	labels := map[string]string{"namespace": "default", "app": "myapp"}
	labelsJSON, _ := json.Marshal(labels)

	stream := &LogStream{
		ID:            "stream-123",
		StreamName:    "default/myapp",
		ResourceType:  ResourceApplication,
		ResourceID:    "app-1",
		Labels:        labelsJSON,
		LogCount:      1000,
		BytesIngested: 50000,
		Status:        "active",
		LastSeenAt:    now,
	}

	view := ToLogStreamView(stream)

	if view.ID != "stream-123" {
		t.Errorf("ID = %q, want %q", view.ID, "stream-123")
	}
	if view.StreamName != "default/myapp" {
		t.Errorf("StreamName = %q, want %q", view.StreamName, "default/myapp")
	}
	if view.LogCount != 1000 {
		t.Errorf("LogCount = %d, want 1000", view.LogCount)
	}
	if view.Labels["app"] != "myapp" {
		t.Error("Labels should be parsed")
	}
}

func TestGridPosition(t *testing.T) {
	pos := GridPosition{X: 0, Y: 0, W: 6, H: 4}

	if pos.X != 0 || pos.Y != 0 {
		t.Errorf("Position = (%d, %d), want (0, 0)", pos.X, pos.Y)
	}
	if pos.W != 6 || pos.H != 4 {
		t.Errorf("Size = (%d, %d), want (6, 4)", pos.W, pos.H)
	}
}

func TestMarshalJSON(t *testing.T) {
	data := map[string]string{"key": "value"}
	result := marshalJSON(data)

	var parsed map[string]string
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Errorf("marshalJSON() produced invalid JSON: %v", err)
	}
	if parsed["key"] != "value" {
		t.Errorf("parsed key = %q, want %q", parsed["key"], "value")
	}
}

func TestMarshalJSON_Nil(t *testing.T) {
	result := marshalJSON(nil)
	if string(result) != "{}" {
		t.Errorf("marshalJSON(nil) = %q, want %q", string(result), "{}")
	}
}
