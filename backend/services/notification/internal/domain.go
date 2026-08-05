// Package notification delivers email/webhook/Slack/in-app notifications.
package notification

import (
	"encoding/json"
	"time"
)

// Channel types.
const (
	ChannelEmail   = "email"
	ChannelSlack   = "slack"
	ChannelWebhook = "webhook"
	ChannelInApp   = "in_app"
)

// Notification status.
const (
	StatusPending  = "pending"
	StatusSending  = "sending"
	StatusSent     = "sent"
	StatusFailed   = "failed"
	StatusDLQ      = "dlq"
)

// Delivery status.
const (
	DeliveryPending   = "pending"
	DeliverySending   = "sending"
	DeliveryDelivered = "delivered"
	DeliveryFailed    = "failed"
	DeliveryDLQ       = "dlq"
)

// Severity levels.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityError    = "error"
	SeverityCritical = "critical"
)

// Event types.
const (
	EventDeploymentStarted   = "deployment.started"
	EventDeploymentCompleted = "deployment.completed"
	EventDeploymentFailed    = "deployment.failed"
	EventDeploymentRolledBack = "deployment.rolled_back"
	EventBuildStarted        = "build.started"
	EventBuildCompleted      = "build.completed"
	EventBuildFailed         = "build.failed"
	EventAlertFiring         = "alert.firing"
	EventAlertResolved       = "alert.resolved"
	EventClusterHealthy      = "cluster.healthy"
	EventClusterUnhealthy    = "cluster.unhealthy"
	EventCertExpiring        = "certificate.expiring"
	EventInvitationSent      = "invitation.sent"
)

// DLQ status.
const (
	DLQFailed    = "failed"
	DLQReplayed  = "replayed"
	DLQDiscarded = "discarded"
)

// ----------------------------------------------------------------------------
// Notification Channel
// ----------------------------------------------------------------------------

// NotificationChannel is a configured notification endpoint.
type NotificationChannel struct {
	ID          string          `db:"id"`
	OrgID       string          `db:"org_id"`
	Name        string          `db:"name"`
	ChannelType string          `db:"channel_type"`
	Config      json.RawMessage `db:"config"`
	Enabled     bool            `db:"enabled"`
	Verified    bool            `db:"verified"`
	VerifiedAt  *time.Time      `db:"verified_at"`
	CreatedBy   *string         `db:"created_by"`
	CreatedAt   time.Time       `db:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

// EmailConfig is the configuration for email channels.
type EmailConfig struct {
	SMTPHost     string `json:"smtpHost"`
	SMTPPort     int    `json:"smtpPort"`
	SMTPUser     string `json:"smtpUser"`
	SMTPPassword string `json:"smtpPassword,omitempty"`
	FromAddress  string `json:"fromAddress"`
	FromName     string `json:"fromName"`
	UseTLS       bool   `json:"useTLS"`
}

// SlackConfig is the configuration for Slack channels.
type SlackConfig struct {
	WebhookURL string `json:"webhookUrl"`
	Channel    string `json:"channel,omitempty"`
	Username   string `json:"username,omitempty"`
	IconEmoji  string `json:"iconEmoji,omitempty"`
}

// WebhookConfig is the configuration for webhook channels.
type WebhookConfig struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
	Secret  string            `json:"secret,omitempty"`
}

// ----------------------------------------------------------------------------
// Notification Template
// ----------------------------------------------------------------------------

// NotificationTemplate defines notification content.
type NotificationTemplate struct {
	ID          string          `db:"id"`
	OrgID       *string         `db:"org_id"`
	Name        string          `db:"name"`
	EventType   string          `db:"event_type"`
	Subject     string          `db:"subject"`
	BodyText    string          `db:"body_text"`
	BodyHTML    *string         `db:"body_html"`
	SlackBlocks json.RawMessage `db:"slack_blocks"`
	Variables   json.RawMessage `db:"variables"`
	IsDefault   bool            `db:"is_default"`
	Enabled     bool            `db:"enabled"`
	CreatedAt   time.Time       `db:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

// TemplateVariable describes a variable available in templates.
type TemplateVariable struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Example     string `json:"example,omitempty"`
}

// ----------------------------------------------------------------------------
// User Preferences
// ----------------------------------------------------------------------------

// NotificationPreference is a user's notification preference.
type NotificationPreference struct {
	ID              string   `db:"id"`
	OrgID           string   `db:"org_id"`
	UserID          string   `db:"user_id"`
	EventType       string   `db:"event_type"`
	EmailEnabled    bool     `db:"email_enabled"`
	SlackEnabled    bool     `db:"slack_enabled"`
	WebhookEnabled  bool     `db:"webhook_enabled"`
	InAppEnabled    bool     `db:"in_app_enabled"`
	SeverityFilter  []string `db:"severity_filter"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

// ----------------------------------------------------------------------------
// Notification
// ----------------------------------------------------------------------------

// Notification is an individual notification record.
type Notification struct {
	ID           string          `db:"id"`
	OrgID        string          `db:"org_id"`
	EventType    string          `db:"event_type"`
	EventID      *string         `db:"event_id"`
	Title        string          `db:"title"`
	Body         string          `db:"body"`
	Severity     string          `db:"severity"`
	ResourceType *string         `db:"resource_type"`
	ResourceID   *string         `db:"resource_id"`
	ResourceName *string         `db:"resource_name"`
	Metadata     json.RawMessage `db:"metadata"`
	Status       string          `db:"status"`
	CreatedAt    time.Time       `db:"created_at"`
}

// ----------------------------------------------------------------------------
// Delivery
// ----------------------------------------------------------------------------

// NotificationDelivery tracks delivery attempts.
type NotificationDelivery struct {
	ID             string     `db:"id"`
	OrgID          string     `db:"org_id"`
	NotificationID string     `db:"notification_id"`
	ChannelID      *string    `db:"channel_id"`
	ChannelType    string     `db:"channel_type"`
	Recipient      string     `db:"recipient"`
	Status         string     `db:"status"`
	ScheduledAt    time.Time  `db:"scheduled_at"`
	SentAt         *time.Time `db:"sent_at"`
	DeliveredAt    *time.Time `db:"delivered_at"`
	AttemptCount   int        `db:"attempt_count"`
	MaxAttempts    int        `db:"max_attempts"`
	NextRetryAt    *time.Time `db:"next_retry_at"`
	ResponseCode   *int       `db:"response_code"`
	ResponseBody   *string    `db:"response_body"`
	ErrorMessage   *string    `db:"error_message"`
	CreatedAt      time.Time  `db:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at"`
}

// ----------------------------------------------------------------------------
// Dead Letter Queue
// ----------------------------------------------------------------------------

// NotificationDLQ is a failed delivery in the dead letter queue.
type NotificationDLQ struct {
	ID             string          `db:"id"`
	OrgID          string          `db:"org_id"`
	DeliveryID     string          `db:"delivery_id"`
	NotificationID string          `db:"notification_id"`
	ChannelType    string          `db:"channel_type"`
	Recipient      string          `db:"recipient"`
	FailureReason  string          `db:"failure_reason"`
	LastError      *string         `db:"last_error"`
	AttemptCount   int             `db:"attempt_count"`
	Payload        json.RawMessage `db:"payload"`
	Status         string          `db:"status"`
	ReplayedAt     *time.Time      `db:"replayed_at"`
	DiscardedAt    *time.Time      `db:"discarded_at"`
	CreatedAt      time.Time       `db:"created_at"`
}

// ----------------------------------------------------------------------------
// Webhook Subscription
// ----------------------------------------------------------------------------

// WebhookSubscription is an outbound webhook subscription.
type WebhookSubscription struct {
	ID              string          `db:"id"`
	OrgID           string          `db:"org_id"`
	Name            string          `db:"name"`
	URL             string          `db:"url"`
	EventTypes      json.RawMessage `db:"event_types"`
	Secret          []byte          `db:"secret"`
	Headers         json.RawMessage `db:"headers"`
	Enabled         bool            `db:"enabled"`
	LastTriggeredAt *time.Time      `db:"last_triggered_at"`
	SuccessCount    int64           `db:"success_count"`
	FailureCount    int64           `db:"failure_count"`
	CreatedBy       *string         `db:"created_by"`
	CreatedAt       time.Time       `db:"created_at"`
	UpdatedAt       time.Time       `db:"updated_at"`
}

// ----------------------------------------------------------------------------
// Request DTOs
// ----------------------------------------------------------------------------

// CreateChannelRequest is the request to create a channel.
type CreateChannelRequest struct {
	Name        string      `json:"name"`
	ChannelType string      `json:"channelType"`
	Config      interface{} `json:"config"`
}

// UpdateChannelRequest is the request to update a channel.
type UpdateChannelRequest struct {
	Name    *string     `json:"name,omitempty"`
	Config  interface{} `json:"config,omitempty"`
	Enabled *bool       `json:"enabled,omitempty"`
}

// CreateTemplateRequest is the request to create a template.
type CreateTemplateRequest struct {
	Name        string             `json:"name"`
	EventType   string             `json:"eventType"`
	Subject     string             `json:"subject"`
	BodyText    string             `json:"bodyText"`
	BodyHTML    *string            `json:"bodyHtml,omitempty"`
	SlackBlocks []interface{}      `json:"slackBlocks,omitempty"`
	Variables   []TemplateVariable `json:"variables,omitempty"`
}

// UpdatePreferenceRequest is the request to update preferences.
type UpdatePreferenceRequest struct {
	EventType       string   `json:"eventType"`
	EmailEnabled    *bool    `json:"emailEnabled,omitempty"`
	SlackEnabled    *bool    `json:"slackEnabled,omitempty"`
	WebhookEnabled  *bool    `json:"webhookEnabled,omitempty"`
	InAppEnabled    *bool    `json:"inAppEnabled,omitempty"`
	SeverityFilter  []string `json:"severityFilter,omitempty"`
}

// SendNotificationRequest is the request to send a notification.
type SendNotificationRequest struct {
	EventType    string                 `json:"eventType"`
	Title        string                 `json:"title"`
	Body         string                 `json:"body"`
	Severity     string                 `json:"severity,omitempty"`
	ResourceType *string                `json:"resourceType,omitempty"`
	ResourceID   *string                `json:"resourceId,omitempty"`
	ResourceName *string                `json:"resourceName,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Recipients   []string               `json:"recipients,omitempty"` // User IDs or email addresses
}

// CreateWebhookRequest is the request to create a webhook subscription.
type CreateWebhookRequest struct {
	Name       string            `json:"name"`
	URL        string            `json:"url"`
	EventTypes []string          `json:"eventTypes"`
	Headers    map[string]string `json:"headers,omitempty"`
}

// TestChannelRequest is the request to test a channel.
type TestChannelRequest struct {
	Message string `json:"message,omitempty"`
}

// ReplayDLQRequest is the request to replay DLQ items.
type ReplayDLQRequest struct {
	IDs []string `json:"ids,omitempty"` // Empty = replay all
}

// ----------------------------------------------------------------------------
// View Models
// ----------------------------------------------------------------------------

// ChannelView is the API response for a channel.
type ChannelView struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	ChannelType string      `json:"channelType"`
	Config      interface{} `json:"config"`
	Enabled     bool        `json:"enabled"`
	Verified    bool        `json:"verified"`
	VerifiedAt  *string     `json:"verifiedAt,omitempty"`
	CreatedAt   string      `json:"createdAt"`
}

func ToChannelView(c *NotificationChannel) ChannelView {
	var config interface{}
	_ = json.Unmarshal(c.Config, &config)

	view := ChannelView{
		ID:          c.ID,
		Name:        c.Name,
		ChannelType: c.ChannelType,
		Config:      config,
		Enabled:     c.Enabled,
		Verified:    c.Verified,
		CreatedAt:   c.CreatedAt.Format(time.RFC3339),
	}
	if c.VerifiedAt != nil {
		s := c.VerifiedAt.Format(time.RFC3339)
		view.VerifiedAt = &s
	}
	return view
}

// TemplateView is the API response for a template.
type TemplateView struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	EventType   string             `json:"eventType"`
	Subject     string             `json:"subject"`
	BodyText    string             `json:"bodyText"`
	BodyHTML    *string            `json:"bodyHtml,omitempty"`
	SlackBlocks []interface{}      `json:"slackBlocks,omitempty"`
	Variables   []TemplateVariable `json:"variables"`
	IsDefault   bool               `json:"isDefault"`
	Enabled     bool               `json:"enabled"`
	CreatedAt   string             `json:"createdAt"`
}

func ToTemplateView(t *NotificationTemplate) TemplateView {
	var blocks []interface{}
	var vars []TemplateVariable
	_ = json.Unmarshal(t.SlackBlocks, &blocks)
	_ = json.Unmarshal(t.Variables, &vars)

	return TemplateView{
		ID:          t.ID,
		Name:        t.Name,
		EventType:   t.EventType,
		Subject:     t.Subject,
		BodyText:    t.BodyText,
		BodyHTML:    t.BodyHTML,
		SlackBlocks: blocks,
		Variables:   vars,
		IsDefault:   t.IsDefault,
		Enabled:     t.Enabled,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
	}
}

// PreferenceView is the API response for a preference.
type PreferenceView struct {
	ID             string   `json:"id"`
	EventType      string   `json:"eventType"`
	EmailEnabled   bool     `json:"emailEnabled"`
	SlackEnabled   bool     `json:"slackEnabled"`
	WebhookEnabled bool     `json:"webhookEnabled"`
	InAppEnabled   bool     `json:"inAppEnabled"`
	SeverityFilter []string `json:"severityFilter"`
}

func ToPreferenceView(p *NotificationPreference) PreferenceView {
	return PreferenceView{
		ID:             p.ID,
		EventType:      p.EventType,
		EmailEnabled:   p.EmailEnabled,
		SlackEnabled:   p.SlackEnabled,
		WebhookEnabled: p.WebhookEnabled,
		InAppEnabled:   p.InAppEnabled,
		SeverityFilter: p.SeverityFilter,
	}
}

// NotificationView is the API response for a notification.
type NotificationView struct {
	ID           string                 `json:"id"`
	EventType    string                 `json:"eventType"`
	Title        string                 `json:"title"`
	Body         string                 `json:"body"`
	Severity     string                 `json:"severity"`
	ResourceType *string                `json:"resourceType,omitempty"`
	ResourceID   *string                `json:"resourceId,omitempty"`
	ResourceName *string                `json:"resourceName,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Status       string                 `json:"status"`
	CreatedAt    string                 `json:"createdAt"`
}

func ToNotificationView(n *Notification) NotificationView {
	var metadata map[string]interface{}
	_ = json.Unmarshal(n.Metadata, &metadata)

	return NotificationView{
		ID:           n.ID,
		EventType:    n.EventType,
		Title:        n.Title,
		Body:         n.Body,
		Severity:     n.Severity,
		ResourceType: n.ResourceType,
		ResourceID:   n.ResourceID,
		ResourceName: n.ResourceName,
		Metadata:     metadata,
		Status:       n.Status,
		CreatedAt:    n.CreatedAt.Format(time.RFC3339),
	}
}

// DeliveryView is the API response for a delivery.
type DeliveryView struct {
	ID           string  `json:"id"`
	ChannelType  string  `json:"channelType"`
	Recipient    string  `json:"recipient"`
	Status       string  `json:"status"`
	AttemptCount int     `json:"attemptCount"`
	SentAt       *string `json:"sentAt,omitempty"`
	ErrorMessage *string `json:"errorMessage,omitempty"`
}

func ToDeliveryView(d *NotificationDelivery) DeliveryView {
	view := DeliveryView{
		ID:           d.ID,
		ChannelType:  d.ChannelType,
		Recipient:    d.Recipient,
		Status:       d.Status,
		AttemptCount: d.AttemptCount,
		ErrorMessage: d.ErrorMessage,
	}
	if d.SentAt != nil {
		s := d.SentAt.Format(time.RFC3339)
		view.SentAt = &s
	}
	return view
}

// DLQItemView is the API response for a DLQ item.
type DLQItemView struct {
	ID            string  `json:"id"`
	ChannelType   string  `json:"channelType"`
	Recipient     string  `json:"recipient"`
	FailureReason string  `json:"failureReason"`
	LastError     *string `json:"lastError,omitempty"`
	AttemptCount  int     `json:"attemptCount"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"createdAt"`
}

func ToDLQItemView(d *NotificationDLQ) DLQItemView {
	return DLQItemView{
		ID:            d.ID,
		ChannelType:   d.ChannelType,
		Recipient:     d.Recipient,
		FailureReason: d.FailureReason,
		LastError:     d.LastError,
		AttemptCount:  d.AttemptCount,
		Status:        d.Status,
		CreatedAt:     d.CreatedAt.Format(time.RFC3339),
	}
}

// WebhookView is the API response for a webhook subscription.
type WebhookView struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	URL             string            `json:"url"`
	EventTypes      []string          `json:"eventTypes"`
	Headers         map[string]string `json:"headers,omitempty"`
	Enabled         bool              `json:"enabled"`
	LastTriggeredAt *string           `json:"lastTriggeredAt,omitempty"`
	SuccessCount    int64             `json:"successCount"`
	FailureCount    int64             `json:"failureCount"`
	CreatedAt       string            `json:"createdAt"`
}

func ToWebhookView(w *WebhookSubscription) WebhookView {
	var eventTypes []string
	var headers map[string]string
	_ = json.Unmarshal(w.EventTypes, &eventTypes)
	_ = json.Unmarshal(w.Headers, &headers)

	view := WebhookView{
		ID:           w.ID,
		Name:         w.Name,
		URL:          w.URL,
		EventTypes:   eventTypes,
		Headers:      headers,
		Enabled:      w.Enabled,
		SuccessCount: w.SuccessCount,
		FailureCount: w.FailureCount,
		CreatedAt:    w.CreatedAt.Format(time.RFC3339),
	}
	if w.LastTriggeredAt != nil {
		s := w.LastTriggeredAt.Format(time.RFC3339)
		view.LastTriggeredAt = &s
	}
	return view
}
