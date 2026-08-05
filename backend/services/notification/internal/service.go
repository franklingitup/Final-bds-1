package notification

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
	Channels      ChannelStore
	Templates     TemplateStore
	Preferences   PreferenceStore
	Notifications NotificationStore
	Deliveries    DeliveryStore
	DLQ           DLQStore
	Webhooks      WebhookStore
	OrgMembers    authz.OrgMemberStore
	Tenant        TenantRunner
	Logger        *slog.Logger
}

// Service implements notification logic.
type Service struct {
	channels      ChannelStore
	templates     TemplateStore
	preferences   PreferenceStore
	notifications NotificationStore
	deliveries    DeliveryStore
	dlq           DLQStore
	webhooks      WebhookStore
	orgMembers    authz.OrgMemberStore
	tenant        TenantRunner
	authSvc       *authz.AuthorizationService
	templateEngine *TemplateEngine
	log           *slog.Logger
}

// NewService creates a new notification service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}

	return &Service{
		channels:       d.Channels,
		templates:      d.Templates,
		preferences:    d.Preferences,
		notifications:  d.Notifications,
		deliveries:     d.Deliveries,
		dlq:            d.DLQ,
		webhooks:       d.Webhooks,
		orgMembers:     d.OrgMembers,
		tenant:         d.Tenant,
		authSvc:        authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		templateEngine: NewTemplateEngine(),
		log:            d.Logger,
	}
}

// ----------------------------------------------------------------------------
// Channels
// ----------------------------------------------------------------------------

// CreateChannel creates a notification channel.
func (s *Service) CreateChannel(ctx context.Context, orgID, userID string, req CreateChannelRequest) (*NotificationChannel, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return nil, err
	}

	if req.Name == "" {
		return nil, apperrors.Validation("name is required")
	}
	if req.ChannelType == "" {
		return nil, apperrors.Validation("channelType is required")
	}

	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		return nil, apperrors.Validation("invalid config")
	}

	channel := &NotificationChannel{
		OrgID:       orgID,
		Name:        req.Name,
		ChannelType: req.ChannelType,
		Config:      configJSON,
		Enabled:     true,
		CreatedBy:   &userID,
	}

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.channels.Create(ctx, channel)
	})
	if err != nil {
		return nil, err
	}

	return channel, nil
}

// GetChannel returns a channel by ID.
func (s *Service) GetChannel(ctx context.Context, orgID, userID, channelID string) (*NotificationChannel, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var channel *NotificationChannel
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		channel, err = s.channels.GetByID(ctx, channelID)
		return err
	})
	return channel, err
}

// ListChannels returns channels for an organization.
func (s *Service) ListChannels(ctx context.Context, orgID, userID string) ([]NotificationChannel, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var channels []NotificationChannel
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		channels, err = s.channels.List(ctx, orgID)
		return err
	})
	return channels, err
}

// UpdateChannel updates a channel.
func (s *Service) UpdateChannel(ctx context.Context, orgID, userID, channelID string, req UpdateChannelRequest) (*NotificationChannel, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return nil, err
	}

	var channel *NotificationChannel
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		channel, err = s.channels.GetByID(ctx, channelID)
		if err != nil {
			return err
		}

		if req.Name != nil {
			channel.Name = *req.Name
		}
		if req.Config != nil {
			channel.Config, _ = json.Marshal(req.Config)
		}
		if req.Enabled != nil {
			channel.Enabled = *req.Enabled
		}

		return s.channels.Update(ctx, channel)
	})
	return channel, err
}

// DeleteChannel deletes a channel.
func (s *Service) DeleteChannel(ctx context.Context, orgID, userID, channelID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.channels.Delete(ctx, channelID)
	})
}

// TestChannel tests a channel by sending a test message.
func (s *Service) TestChannel(ctx context.Context, orgID, userID, channelID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return err
	}

	var channel *NotificationChannel
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		channel, err = s.channels.GetByID(ctx, channelID)
		return err
	})
	if err != nil {
		return err
	}

	ch, err := s.createChannelInstance(channel)
	if err != nil {
		return err
	}

	if err := ch.Test(ctx); err != nil {
		return apperrors.Internal(fmt.Sprintf("channel test failed: %v", err))
	}

	// Mark as verified
	now := time.Now()
	channel.Verified = true
	channel.VerifiedAt = &now

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.channels.Update(ctx, channel)
	})
}

// ----------------------------------------------------------------------------
// Preferences
// ----------------------------------------------------------------------------

// GetPreferences returns user preferences.
func (s *Service) GetPreferences(ctx context.Context, orgID, userID string) ([]NotificationPreference, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var prefs []NotificationPreference
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		prefs, err = s.preferences.ListByUser(ctx, orgID, userID)
		return err
	})
	return prefs, err
}

// UpdatePreference updates a user preference.
func (s *Service) UpdatePreference(ctx context.Context, orgID, userID string, req UpdatePreferenceRequest) (*NotificationPreference, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	pref := &NotificationPreference{
		OrgID:     orgID,
		UserID:    userID,
		EventType: req.EventType,
	}

	// Set defaults
	pref.EmailEnabled = true
	pref.SlackEnabled = true
	pref.WebhookEnabled = true
	pref.InAppEnabled = true
	pref.SeverityFilter = []string{SeverityInfo, SeverityWarning, SeverityError, SeverityCritical}

	// Apply updates
	if req.EmailEnabled != nil {
		pref.EmailEnabled = *req.EmailEnabled
	}
	if req.SlackEnabled != nil {
		pref.SlackEnabled = *req.SlackEnabled
	}
	if req.WebhookEnabled != nil {
		pref.WebhookEnabled = *req.WebhookEnabled
	}
	if req.InAppEnabled != nil {
		pref.InAppEnabled = *req.InAppEnabled
	}
	if len(req.SeverityFilter) > 0 {
		pref.SeverityFilter = req.SeverityFilter
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.preferences.Upsert(ctx, pref)
	})
	if err != nil {
		return nil, err
	}

	return pref, nil
}

// ----------------------------------------------------------------------------
// Notifications
// ----------------------------------------------------------------------------

// SendNotification sends a notification.
func (s *Service) SendNotification(ctx context.Context, orgID string, req SendNotificationRequest) (*Notification, error) {
	if req.EventType == "" {
		return nil, apperrors.Validation("eventType is required")
	}
	if req.Title == "" {
		return nil, apperrors.Validation("title is required")
	}

	severity := req.Severity
	if severity == "" {
		severity = SeverityInfo
	}

	metadataJSON, _ := json.Marshal(req.Metadata)

	notif := &Notification{
		OrgID:        orgID,
		EventType:    req.EventType,
		Title:        req.Title,
		Body:         req.Body,
		Severity:     severity,
		ResourceType: req.ResourceType,
		ResourceID:   req.ResourceID,
		ResourceName: req.ResourceName,
		Metadata:     metadataJSON,
		Status:       StatusPending,
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.dispatchTx(ctx, notif)
	})
	if err != nil {
		return nil, err
	}

	return notif, nil
}

// dispatchTx persists a notification and fans out one pending delivery per
// enabled channel and per subscribed webhook. It MUST be called inside a
// tenant-scoped transaction (TenantRunner.WithTenant) so that RLS applies and
// the notification plus all of its deliveries commit atomically; the delivery
// worker performs the actual sends afterwards. Both the API path
// (SendNotification) and the event consumer reuse this so their fan-out
// behaviour stays identical.
func (s *Service) dispatchTx(ctx context.Context, notif *Notification) error {
	if err := s.notifications.Create(ctx, notif); err != nil {
		return err
	}

	channels, err := s.channels.ListEnabled(ctx, notif.OrgID)
	if err != nil {
		return err
	}
	for i := range channels {
		ch := channels[i]
		delivery := &NotificationDelivery{
			OrgID:          notif.OrgID,
			NotificationID: notif.ID,
			ChannelID:      &ch.ID,
			ChannelType:    ch.ChannelType,
			Recipient:      s.getRecipientForChannel(&ch),
			Status:         DeliveryPending,
			ScheduledAt:    time.Now(),
			MaxAttempts:    3,
		}
		if err := s.deliveries.Create(ctx, delivery); err != nil {
			s.log.Warn("failed to create delivery", "error", err, "channel", ch.Name)
		}
	}

	webhooks, err := s.webhooks.ListByEventType(ctx, notif.OrgID, notif.EventType)
	if err == nil {
		for i := range webhooks {
			wh := webhooks[i]
			delivery := &NotificationDelivery{
				OrgID:          notif.OrgID,
				NotificationID: notif.ID,
				ChannelType:    ChannelWebhook,
				Recipient:      wh.URL,
				Status:         DeliveryPending,
				ScheduledAt:    time.Now(),
				MaxAttempts:    3,
			}
			if err := s.deliveries.Create(ctx, delivery); err != nil {
				s.log.Warn("failed to create webhook delivery", "error", err, "webhook", wh.Name)
			}
		}
	}

	return nil
}

// ListNotifications returns notifications for an organization.
func (s *Service) ListNotifications(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[Notification], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Notification]{}, err
	}

	var result database.Page[Notification]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		result, err = s.notifications.List(ctx, orgID, page)
		return err
	})
	return result, err
}

// GetNotification returns a notification by ID.
func (s *Service) GetNotification(ctx context.Context, orgID, userID, notificationID string) (*Notification, []NotificationDelivery, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, nil, err
	}

	var notif *Notification
	var deliveries []NotificationDelivery
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		notif, err = s.notifications.GetByID(ctx, notificationID)
		if err != nil {
			return err
		}
		deliveries, err = s.deliveries.ListByNotification(ctx, notificationID)
		return err
	})
	return notif, deliveries, err
}

// ----------------------------------------------------------------------------
// Webhooks
// ----------------------------------------------------------------------------

// CreateWebhook creates a webhook subscription.
func (s *Service) CreateWebhook(ctx context.Context, orgID, userID string, req CreateWebhookRequest) (*WebhookSubscription, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return nil, err
	}

	if req.Name == "" {
		return nil, apperrors.Validation("name is required")
	}
	if req.URL == "" {
		return nil, apperrors.Validation("url is required")
	}

	eventTypesJSON, _ := json.Marshal(req.EventTypes)
	headersJSON, _ := json.Marshal(req.Headers)

	// Generate secret
	secret := generateWebhookSecret()

	webhook := &WebhookSubscription{
		OrgID:      orgID,
		Name:       req.Name,
		URL:        req.URL,
		EventTypes: eventTypesJSON,
		Secret:     secret,
		Headers:    headersJSON,
		Enabled:    true,
		CreatedBy:  &userID,
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.webhooks.Create(ctx, webhook)
	})
	if err != nil {
		return nil, err
	}

	return webhook, nil
}

// ListWebhooks returns webhooks for an organization.
func (s *Service) ListWebhooks(ctx context.Context, orgID, userID string) ([]WebhookSubscription, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var webhooks []WebhookSubscription
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		webhooks, err = s.webhooks.List(ctx, orgID)
		return err
	})
	return webhooks, err
}

// DeleteWebhook deletes a webhook subscription.
func (s *Service) DeleteWebhook(ctx context.Context, orgID, userID, webhookID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.webhooks.Delete(ctx, webhookID)
	})
}

// ----------------------------------------------------------------------------
// DLQ
// ----------------------------------------------------------------------------

// ListDLQ returns DLQ items.
func (s *Service) ListDLQ(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[NotificationDLQ], error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return database.Page[NotificationDLQ]{}, err
	}

	var result database.Page[NotificationDLQ]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		result, err = s.dlq.List(ctx, orgID, page)
		return err
	})
	return result, err
}

// ReplayDLQ replays DLQ items.
func (s *Service) ReplayDLQ(ctx context.Context, orgID, userID string, req ReplayDLQRequest) (int, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return 0, err
	}

	var count int
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var items []NotificationDLQ
		var err error

		if len(req.IDs) > 0 {
			// Replay specific items
			for _, id := range req.IDs {
				item, err := s.dlq.GetByID(ctx, id)
				if err != nil {
					continue
				}
				items = append(items, *item)
			}
		} else {
			// Replay all failed items
			items, err = s.dlq.ListFailed(ctx, orgID, 100)
			if err != nil {
				return err
			}
		}

		for _, item := range items {
			// Create new delivery
			delivery := &NotificationDelivery{
				OrgID:          orgID,
				NotificationID: item.NotificationID,
				ChannelType:    item.ChannelType,
				Recipient:      item.Recipient,
				Status:         DeliveryPending,
				ScheduledAt:    time.Now(),
				MaxAttempts:    3,
			}

			if err := s.deliveries.Create(ctx, delivery); err != nil {
				continue
			}

			// Mark DLQ item as replayed
			_ = s.dlq.UpdateStatus(ctx, item.ID, DLQReplayed)
			count++
		}

		return nil
	})

	return count, err
}

// DiscardDLQ discards DLQ items.
func (s *Service) DiscardDLQ(ctx context.Context, orgID, userID string, ids []string) (int, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return 0, err
	}

	var count int
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		for _, id := range ids {
			if err := s.dlq.UpdateStatus(ctx, id, DLQDiscarded); err == nil {
				count++
			}
		}
		return nil
	})

	return count, err
}

// ----------------------------------------------------------------------------
// Worker Methods
// ----------------------------------------------------------------------------

// ProcessPendingDeliveries processes pending deliveries.
func (s *Service) ProcessPendingDeliveries(ctx context.Context, limit int) (int, error) {
	deliveries, err := s.deliveries.ListPending(ctx, limit)
	if err != nil {
		return 0, err
	}

	processed := 0
	for _, d := range deliveries {
		if err := s.processDelivery(ctx, &d); err != nil {
			s.log.Warn("failed to process delivery", "error", err, "id", d.ID)
		} else {
			processed++
		}
	}

	return processed, nil
}

// ProcessRetries processes failed deliveries ready for retry.
func (s *Service) ProcessRetries(ctx context.Context, limit int) (int, error) {
	deliveries, err := s.deliveries.ListRetryable(ctx, limit)
	if err != nil {
		return 0, err
	}

	processed := 0
	for _, d := range deliveries {
		if err := s.processDelivery(ctx, &d); err != nil {
			s.log.Warn("failed to process retry", "error", err, "id", d.ID)
		} else {
			processed++
		}
	}

	return processed, nil
}

func (s *Service) processDelivery(ctx context.Context, d *NotificationDelivery) error {
	// Get notification
	notif, err := s.notifications.GetByID(ctx, d.NotificationID)
	if err != nil {
		return err
	}

	// Get channel
	var channel *NotificationChannel
	if d.ChannelID != nil {
		channel, _ = s.channels.GetByID(ctx, *d.ChannelID)
	}

	// Build message
	msg := &Message{
		To:       d.Recipient,
		Subject:  notif.Title,
		BodyText: notif.Body,
		Metadata: map[string]interface{}{
			"event_type":      notif.EventType,
			"notification_id": notif.ID,
		},
	}

	// Create channel instance
	var ch Channel
	if channel != nil {
		ch, err = s.createChannelInstance(channel)
		if err != nil {
			return s.handleDeliveryFailure(ctx, d, err)
		}
	} else if d.ChannelType == ChannelWebhook {
		ch = NewWebhookChannel(WebhookConfig{URL: d.Recipient})
	} else {
		return s.handleDeliveryFailure(ctx, d, fmt.Errorf("no channel configured"))
	}

	// Attempt delivery
	now := time.Now()
	d.SentAt = &now
	d.AttemptCount++
	d.Status = DeliverySending

	if err := s.deliveries.Update(ctx, d); err != nil {
		return err
	}

	if err := ch.Send(ctx, msg); err != nil {
		return s.handleDeliveryFailure(ctx, d, err)
	}

	// Mark as delivered
	d.Status = DeliveryDelivered
	d.DeliveredAt = &now
	return s.deliveries.Update(ctx, d)
}

func (s *Service) handleDeliveryFailure(ctx context.Context, d *NotificationDelivery, err error) error {
	errMsg := err.Error()
	d.ErrorMessage = &errMsg
	d.Status = DeliveryFailed

	if d.AttemptCount >= d.MaxAttempts {
		// Move to DLQ
		d.Status = DeliveryDLQ

		dlqItem := &NotificationDLQ{
			OrgID:          d.OrgID,
			DeliveryID:     d.ID,
			NotificationID: d.NotificationID,
			ChannelType:    d.ChannelType,
			Recipient:      d.Recipient,
			FailureReason:  "max retries exceeded",
			LastError:      &errMsg,
			AttemptCount:   d.AttemptCount,
			Payload:        []byte("{}"),
			Status:         DLQFailed,
		}

		if err := s.dlq.Create(ctx, dlqItem); err != nil {
			s.log.Error("failed to create DLQ entry", "error", err)
		}
	} else {
		// Schedule retry with exponential backoff
		backoff := time.Duration(d.AttemptCount*d.AttemptCount) * time.Minute
		nextRetry := time.Now().Add(backoff)
		d.NextRetryAt = &nextRetry
	}

	return s.deliveries.Update(ctx, d)
}

func (s *Service) createChannelInstance(c *NotificationChannel) (Channel, error) {
	switch c.ChannelType {
	case ChannelEmail:
		var cfg EmailConfig
		if err := json.Unmarshal(c.Config, &cfg); err != nil {
			return nil, err
		}
		return NewEmailChannel(cfg), nil

	case ChannelSlack:
		var cfg SlackConfig
		if err := json.Unmarshal(c.Config, &cfg); err != nil {
			return nil, err
		}
		return NewSlackChannel(cfg), nil

	case ChannelWebhook:
		var cfg WebhookConfig
		if err := json.Unmarshal(c.Config, &cfg); err != nil {
			return nil, err
		}
		return NewWebhookChannel(cfg), nil

	default:
		return nil, fmt.Errorf("unsupported channel type: %s", c.ChannelType)
	}
}

func (s *Service) getRecipientForChannel(c *NotificationChannel) string {
	switch c.ChannelType {
	case ChannelEmail:
		var cfg EmailConfig
		json.Unmarshal(c.Config, &cfg)
		return cfg.FromAddress
	case ChannelSlack:
		var cfg SlackConfig
		json.Unmarshal(c.Config, &cfg)
		return cfg.Channel
	case ChannelWebhook:
		var cfg WebhookConfig
		json.Unmarshal(c.Config, &cfg)
		return cfg.URL
	default:
		return c.Name
	}
}

// Helper to generate webhook secret
func generateWebhookSecret() []byte {
	// In production, use crypto/rand
	return []byte(fmt.Sprintf("whsec_%d", time.Now().UnixNano()))
}
