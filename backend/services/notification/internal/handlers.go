package notification

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler implements HTTP handlers for the notification service.
type Handler struct {
	svc *Service
}

// NewHandler creates a new handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Identity represents an authenticated user.
type Identity struct {
	UserID string
	Email  string
}

func extractIdentity(c *fiber.Ctx) (*Identity, error) {
	id, ok := c.Locals("identity").(*Identity)
	if !ok || id == nil {
		return nil, apperrors.Unauthenticated("authentication required")
	}
	return id, nil
}

// ----------------------------------------------------------------------------
// Channels
// ----------------------------------------------------------------------------

// CreateChannel handles POST /organizations/:orgId/channels.
func (h *Handler) CreateChannel(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")

	var req CreateChannelRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	channel, err := h.svc.CreateChannel(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToChannelView(channel))
}

// ListChannels handles GET /organizations/:orgId/channels.
func (h *Handler) ListChannels(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")

	channels, err := h.svc.ListChannels(c.Context(), orgID, id.UserID)
	if err != nil {
		return err
	}

	views := make([]ChannelView, len(channels))
	for i, ch := range channels {
		views[i] = ToChannelView(&ch)
	}

	return c.JSON(fiber.Map{"channels": views})
}

// GetChannel handles GET /organizations/:orgId/channels/:channelId.
func (h *Handler) GetChannel(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")
	channelID := c.Params("channelId")

	channel, err := h.svc.GetChannel(c.Context(), orgID, id.UserID, channelID)
	if err != nil {
		return err
	}

	return c.JSON(ToChannelView(channel))
}

// UpdateChannel handles PATCH /organizations/:orgId/channels/:channelId.
func (h *Handler) UpdateChannel(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")
	channelID := c.Params("channelId")

	var req UpdateChannelRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	channel, err := h.svc.UpdateChannel(c.Context(), orgID, id.UserID, channelID, req)
	if err != nil {
		return err
	}

	return c.JSON(ToChannelView(channel))
}

// DeleteChannel handles DELETE /organizations/:orgId/channels/:channelId.
func (h *Handler) DeleteChannel(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")
	channelID := c.Params("channelId")

	if err := h.svc.DeleteChannel(c.Context(), orgID, id.UserID, channelID); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// TestChannel handles POST /organizations/:orgId/channels/:channelId/test.
func (h *Handler) TestChannel(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")
	channelID := c.Params("channelId")

	if err := h.svc.TestChannel(c.Context(), orgID, id.UserID, channelID); err != nil {
		return err
	}

	return c.JSON(fiber.Map{"message": "test sent successfully"})
}

// ----------------------------------------------------------------------------
// Preferences
// ----------------------------------------------------------------------------

// GetPreferences handles GET /organizations/:orgId/preferences.
func (h *Handler) GetPreferences(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")

	prefs, err := h.svc.GetPreferences(c.Context(), orgID, id.UserID)
	if err != nil {
		return err
	}

	views := make([]PreferenceView, len(prefs))
	for i, p := range prefs {
		views[i] = ToPreferenceView(&p)
	}

	return c.JSON(fiber.Map{"preferences": views})
}

// UpdatePreference handles PUT /organizations/:orgId/preferences.
func (h *Handler) UpdatePreference(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")

	var req UpdatePreferenceRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	pref, err := h.svc.UpdatePreference(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.JSON(ToPreferenceView(pref))
}

// ----------------------------------------------------------------------------
// Notifications
// ----------------------------------------------------------------------------

// SendNotification handles POST /organizations/:orgId/notifications.
func (h *Handler) SendNotification(c *fiber.Ctx) error {
	orgID := c.Params("orgId")

	var req SendNotificationRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	notif, err := h.svc.SendNotification(c.Context(), orgID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToNotificationView(notif))
}

// ListNotifications handles GET /organizations/:orgId/notifications.
func (h *Handler) ListNotifications(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")

	page := database.PageRequest{
		Cursor: c.Query("cursor"),
		Limit:  c.QueryInt("limit", 50),
	}

	result, err := h.svc.ListNotifications(c.Context(), orgID, id.UserID, page)
	if err != nil {
		return err
	}

	views := make([]NotificationView, len(result.Items))
	for i, n := range result.Items {
		views[i] = ToNotificationView(&n)
	}

	return c.JSON(fiber.Map{
		"notifications": views,
		"nextCursor":    result.NextCursor,
	})
}

// GetNotification handles GET /organizations/:orgId/notifications/:notificationId.
func (h *Handler) GetNotification(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")
	notificationID := c.Params("notificationId")

	notif, deliveries, err := h.svc.GetNotification(c.Context(), orgID, id.UserID, notificationID)
	if err != nil {
		return err
	}

	deliveryViews := make([]DeliveryView, len(deliveries))
	for i, d := range deliveries {
		deliveryViews[i] = ToDeliveryView(&d)
	}

	return c.JSON(fiber.Map{
		"notification": ToNotificationView(notif),
		"deliveries":   deliveryViews,
	})
}

// ----------------------------------------------------------------------------
// Webhooks
// ----------------------------------------------------------------------------

// CreateWebhook handles POST /organizations/:orgId/webhooks.
func (h *Handler) CreateWebhook(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")

	var req CreateWebhookRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	webhook, err := h.svc.CreateWebhook(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToWebhookView(webhook))
}

// ListWebhooks handles GET /organizations/:orgId/webhooks.
func (h *Handler) ListWebhooks(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")

	webhooks, err := h.svc.ListWebhooks(c.Context(), orgID, id.UserID)
	if err != nil {
		return err
	}

	views := make([]WebhookView, len(webhooks))
	for i, w := range webhooks {
		views[i] = ToWebhookView(&w)
	}

	return c.JSON(fiber.Map{"webhooks": views})
}

// DeleteWebhook handles DELETE /organizations/:orgId/webhooks/:webhookId.
func (h *Handler) DeleteWebhook(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")
	webhookID := c.Params("webhookId")

	if err := h.svc.DeleteWebhook(c.Context(), orgID, id.UserID, webhookID); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Dead Letter Queue
// ----------------------------------------------------------------------------

// ListDLQ handles GET /organizations/:orgId/dlq.
func (h *Handler) ListDLQ(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")

	page := database.PageRequest{
		Cursor: c.Query("cursor"),
		Limit:  c.QueryInt("limit", 50),
	}

	result, err := h.svc.ListDLQ(c.Context(), orgID, id.UserID, page)
	if err != nil {
		return err
	}

	views := make([]DLQItemView, len(result.Items))
	for i, d := range result.Items {
		views[i] = ToDLQItemView(&d)
	}

	return c.JSON(fiber.Map{
		"items":      views,
		"nextCursor": result.NextCursor,
	})
}

// ReplayDLQ handles POST /organizations/:orgId/dlq/replay.
func (h *Handler) ReplayDLQ(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")

	var req ReplayDLQRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	count, err := h.svc.ReplayDLQ(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"replayed": count})
}

// DiscardDLQ handles POST /organizations/:orgId/dlq/discard.
func (h *Handler) DiscardDLQ(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := c.Params("orgId")

	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	count, err := h.svc.DiscardDLQ(c.Context(), orgID, id.UserID, req.IDs)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"discarded": count})
}
