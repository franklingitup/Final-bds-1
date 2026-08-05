package notification

import (
	"encoding/json"
	"testing"
	"time"
)

func TestChannelConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"ChannelEmail", ChannelEmail, "email"},
		{"ChannelSlack", ChannelSlack, "slack"},
		{"ChannelWebhook", ChannelWebhook, "webhook"},
		{"ChannelInApp", ChannelInApp, "in_app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.constant)
			}
		})
	}
}

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"StatusPending", StatusPending, "pending"},
		{"StatusSending", StatusSending, "sending"},
		{"StatusSent", StatusSent, "sent"},
		{"StatusFailed", StatusFailed, "failed"},
		{"StatusDLQ", StatusDLQ, "dlq"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.constant)
			}
		})
	}
}

func TestSeverityConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"SeverityInfo", SeverityInfo, "info"},
		{"SeverityWarning", SeverityWarning, "warning"},
		{"SeverityError", SeverityError, "error"},
		{"SeverityCritical", SeverityCritical, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.constant)
			}
		})
	}
}

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"EventDeploymentStarted", EventDeploymentStarted, "deployment.started"},
		{"EventDeploymentCompleted", EventDeploymentCompleted, "deployment.completed"},
		{"EventDeploymentFailed", EventDeploymentFailed, "deployment.failed"},
		{"EventBuildCompleted", EventBuildCompleted, "build.completed"},
		{"EventBuildFailed", EventBuildFailed, "build.failed"},
		{"EventAlertFiring", EventAlertFiring, "alert.firing"},
		{"EventAlertResolved", EventAlertResolved, "alert.resolved"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.constant)
			}
		})
	}
}

func TestToChannelView(t *testing.T) {
	now := time.Now()
	verifiedAt := now.Add(-time.Hour)

	channel := &NotificationChannel{
		ID:          "ch-123",
		OrgID:       "org-456",
		Name:        "Test Channel",
		ChannelType: ChannelEmail,
		Config:      json.RawMessage(`{"smtpHost": "smtp.example.com"}`),
		Enabled:     true,
		Verified:    true,
		VerifiedAt:  &verifiedAt,
		CreatedAt:   now,
	}

	view := ToChannelView(channel)

	if view.ID != "ch-123" {
		t.Errorf("expected ID ch-123, got %s", view.ID)
	}
	if view.Name != "Test Channel" {
		t.Errorf("expected name Test Channel, got %s", view.Name)
	}
	if view.ChannelType != ChannelEmail {
		t.Errorf("expected type email, got %s", view.ChannelType)
	}
	if !view.Enabled {
		t.Error("expected enabled true")
	}
	if !view.Verified {
		t.Error("expected verified true")
	}
	if view.VerifiedAt == nil {
		t.Error("expected verifiedAt to be set")
	}
}

func TestToNotificationView(t *testing.T) {
	resourceType := "deployment"
	resourceID := "dep-123"
	resourceName := "my-app"

	notif := &Notification{
		ID:           "notif-123",
		OrgID:        "org-456",
		EventType:    EventDeploymentCompleted,
		Title:        "Deployment Complete",
		Body:         "Your deployment succeeded",
		Severity:     SeverityInfo,
		ResourceType: &resourceType,
		ResourceID:   &resourceID,
		ResourceName: &resourceName,
		Metadata:     json.RawMessage(`{"version": "1.0.0"}`),
		Status:       StatusSent,
		CreatedAt:    time.Now(),
	}

	view := ToNotificationView(notif)

	if view.ID != "notif-123" {
		t.Errorf("expected ID notif-123, got %s", view.ID)
	}
	if view.EventType != EventDeploymentCompleted {
		t.Errorf("expected event type %s, got %s", EventDeploymentCompleted, view.EventType)
	}
	if view.Title != "Deployment Complete" {
		t.Errorf("expected title Deployment Complete, got %s", view.Title)
	}
	if view.Status != StatusSent {
		t.Errorf("expected status %s, got %s", StatusSent, view.Status)
	}
	if view.ResourceType == nil || *view.ResourceType != "deployment" {
		t.Error("expected resource type deployment")
	}
}

func TestToDeliveryView(t *testing.T) {
	now := time.Now()
	errMsg := "connection timeout"

	delivery := &NotificationDelivery{
		ID:           "del-123",
		ChannelType:  ChannelEmail,
		Recipient:    "user@example.com",
		Status:       DeliveryFailed,
		AttemptCount: 3,
		SentAt:       &now,
		ErrorMessage: &errMsg,
	}

	view := ToDeliveryView(delivery)

	if view.ID != "del-123" {
		t.Errorf("expected ID del-123, got %s", view.ID)
	}
	if view.ChannelType != ChannelEmail {
		t.Errorf("expected channel type email, got %s", view.ChannelType)
	}
	if view.Recipient != "user@example.com" {
		t.Errorf("expected recipient user@example.com, got %s", view.Recipient)
	}
	if view.Status != DeliveryFailed {
		t.Errorf("expected status %s, got %s", DeliveryFailed, view.Status)
	}
	if view.AttemptCount != 3 {
		t.Errorf("expected attempt count 3, got %d", view.AttemptCount)
	}
	if view.ErrorMessage == nil || *view.ErrorMessage != errMsg {
		t.Error("expected error message")
	}
}

func TestToDLQItemView(t *testing.T) {
	lastError := "max retries exceeded"

	dlq := &NotificationDLQ{
		ID:            "dlq-123",
		ChannelType:   ChannelWebhook,
		Recipient:     "https://example.com/webhook",
		FailureReason: "timeout",
		LastError:     &lastError,
		AttemptCount:  5,
		Status:        DLQFailed,
		CreatedAt:     time.Now(),
	}

	view := ToDLQItemView(dlq)

	if view.ID != "dlq-123" {
		t.Errorf("expected ID dlq-123, got %s", view.ID)
	}
	if view.ChannelType != ChannelWebhook {
		t.Errorf("expected channel type webhook, got %s", view.ChannelType)
	}
	if view.FailureReason != "timeout" {
		t.Errorf("expected failure reason timeout, got %s", view.FailureReason)
	}
	if view.Status != DLQFailed {
		t.Errorf("expected status %s, got %s", DLQFailed, view.Status)
	}
}

func TestToWebhookView(t *testing.T) {
	now := time.Now()

	webhook := &WebhookSubscription{
		ID:              "wh-123",
		OrgID:           "org-456",
		Name:            "My Webhook",
		URL:             "https://example.com/webhook",
		EventTypes:      json.RawMessage(`["deployment.completed", "deployment.failed"]`),
		Headers:         json.RawMessage(`{"X-Custom": "header"}`),
		Enabled:         true,
		LastTriggeredAt: &now,
		SuccessCount:    100,
		FailureCount:    5,
		CreatedAt:       now,
	}

	view := ToWebhookView(webhook)

	if view.ID != "wh-123" {
		t.Errorf("expected ID wh-123, got %s", view.ID)
	}
	if view.Name != "My Webhook" {
		t.Errorf("expected name My Webhook, got %s", view.Name)
	}
	if view.URL != "https://example.com/webhook" {
		t.Errorf("expected URL https://example.com/webhook, got %s", view.URL)
	}
	if len(view.EventTypes) != 2 {
		t.Errorf("expected 2 event types, got %d", len(view.EventTypes))
	}
	if view.SuccessCount != 100 {
		t.Errorf("expected success count 100, got %d", view.SuccessCount)
	}
	if view.FailureCount != 5 {
		t.Errorf("expected failure count 5, got %d", view.FailureCount)
	}
}

func TestToPreferenceView(t *testing.T) {
	pref := &NotificationPreference{
		ID:             "pref-123",
		EventType:      "deployment.*",
		EmailEnabled:   true,
		SlackEnabled:   false,
		WebhookEnabled: true,
		InAppEnabled:   true,
		SeverityFilter: []string{SeverityError, SeverityCritical},
	}

	view := ToPreferenceView(pref)

	if view.ID != "pref-123" {
		t.Errorf("expected ID pref-123, got %s", view.ID)
	}
	if view.EventType != "deployment.*" {
		t.Errorf("expected event type deployment.*, got %s", view.EventType)
	}
	if !view.EmailEnabled {
		t.Error("expected email enabled")
	}
	if view.SlackEnabled {
		t.Error("expected slack disabled")
	}
	if len(view.SeverityFilter) != 2 {
		t.Errorf("expected 2 severity filters, got %d", len(view.SeverityFilter))
	}
}

func TestTemplateEngine_Render(t *testing.T) {
	engine := NewTemplateEngine()

	data := &RenderData{
		DeploymentName: "my-app",
		Environment:    "production",
		Version:        "v1.2.3",
		ClusterName:    "prod-cluster",
		Namespace:      "default",
		DashboardURL:   "https://dashboard.example.com/deployments/123",
	}

	template := "Deployment {{.DeploymentName}} to {{.Environment}} completed!"
	result, err := engine.Render(template, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Deployment my-app to production completed!"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestTemplateEngine_RenderSubject(t *testing.T) {
	engine := NewTemplateEngine()

	data := &RenderData{
		DeploymentName: "my-app",
		Severity:       SeverityError,
	}

	template := "Alert: {{.DeploymentName}} - {{.Severity | upper}}"
	result, err := engine.RenderSubject(template, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Alert: my-app - ERROR"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestDefaultTemplates_DeploymentCompleted(t *testing.T) {
	tmpl, ok := DefaultTemplates[EventDeploymentCompleted]
	if !ok {
		t.Fatal("deployment.completed template not found")
	}

	if tmpl.Subject == "" {
		t.Error("subject should not be empty")
	}
	if tmpl.BodyText == "" {
		t.Error("body text should not be empty")
	}
	if tmpl.BodyHTML == "" {
		t.Error("body HTML should not be empty")
	}
}

func TestDefaultTemplates_DeploymentFailed(t *testing.T) {
	tmpl, ok := DefaultTemplates[EventDeploymentFailed]
	if !ok {
		t.Fatal("deployment.failed template not found")
	}

	if tmpl.Subject == "" {
		t.Error("subject should not be empty")
	}
	if tmpl.BodyText == "" {
		t.Error("body text should not be empty")
	}
	if tmpl.BodyHTML == "" {
		t.Error("body HTML should not be empty")
	}
}

func TestSeverityEmoji(t *testing.T) {
	tests := []struct {
		severity string
		expected string
	}{
		{SeverityInfo, "ℹ️"},
		{SeverityWarning, "⚠️"},
		{SeverityError, "❌"},
		{SeverityCritical, "🔴"},
		{"unknown", "📣"},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			result := severityEmoji(tt.severity)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		length   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello"},
		{"", 5, ""},
		{"abc", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncate(tt.length, tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	payload := []byte(`{"event": "test"}`)
	secret := "my-secret"

	signature := "sha256=" + computeHMAC(payload, secret)

	if !VerifyWebhookSignature(payload, signature, secret) {
		t.Error("valid signature should verify")
	}

	if VerifyWebhookSignature(payload, "sha256=invalid", secret) {
		t.Error("invalid signature should not verify")
	}

	if VerifyWebhookSignature(payload, signature, "wrong-secret") {
		t.Error("wrong secret should not verify")
	}
}

func TestComputeHMAC(t *testing.T) {
	payload := []byte("test payload")
	secret := "secret"

	result := computeHMAC(payload, secret)

	if result == "" {
		t.Error("HMAC should not be empty")
	}

	// Same input should produce same output
	result2 := computeHMAC(payload, secret)
	if result != result2 {
		t.Error("HMAC should be deterministic")
	}

	// Different payload should produce different output
	result3 := computeHMAC([]byte("different"), secret)
	if result == result3 {
		t.Error("different payload should produce different HMAC")
	}
}

func TestDefaultWorkerConfig(t *testing.T) {
	cfg := DefaultWorkerConfig()

	if !cfg.Enabled {
		t.Error("worker should be enabled by default")
	}
	if cfg.PollInterval == 0 {
		t.Error("poll interval should not be zero")
	}
	if cfg.BatchSize == 0 {
		t.Error("batch size should not be zero")
	}
	if cfg.RetryInterval == 0 {
		t.Error("retry interval should not be zero")
	}
}
