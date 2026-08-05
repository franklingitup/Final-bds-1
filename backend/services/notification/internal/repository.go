package notification

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// TenantRunner runs a function within a tenant-scoped transaction.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// ChannelStore persists notification channels.
type ChannelStore interface {
	Create(ctx context.Context, c *NotificationChannel) error
	GetByID(ctx context.Context, id string) (*NotificationChannel, error)
	GetByName(ctx context.Context, name string) (*NotificationChannel, error)
	List(ctx context.Context, orgID string) ([]NotificationChannel, error)
	ListEnabled(ctx context.Context, orgID string) ([]NotificationChannel, error)
	Update(ctx context.Context, c *NotificationChannel) error
	Delete(ctx context.Context, id string) error
}

// TemplateStore persists notification templates.
type TemplateStore interface {
	Create(ctx context.Context, t *NotificationTemplate) error
	GetByID(ctx context.Context, id string) (*NotificationTemplate, error)
	GetByEventType(ctx context.Context, orgID, eventType string) (*NotificationTemplate, error)
	GetDefault(ctx context.Context, eventType string) (*NotificationTemplate, error)
	List(ctx context.Context, orgID string) ([]NotificationTemplate, error)
	Update(ctx context.Context, t *NotificationTemplate) error
	Delete(ctx context.Context, id string) error
}

// PreferenceStore persists user preferences.
type PreferenceStore interface {
	Upsert(ctx context.Context, p *NotificationPreference) error
	Get(ctx context.Context, orgID, userID, eventType string) (*NotificationPreference, error)
	ListByUser(ctx context.Context, orgID, userID string) ([]NotificationPreference, error)
	Delete(ctx context.Context, id string) error
}

// NotificationStore persists notifications.
type NotificationStore interface {
	Create(ctx context.Context, n *Notification) error
	GetByID(ctx context.Context, id string) (*Notification, error)
	List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[Notification], error)
	UpdateStatus(ctx context.Context, id, status string) error
}

// DeliveryStore persists delivery attempts.
type DeliveryStore interface {
	Create(ctx context.Context, d *NotificationDelivery) error
	GetByID(ctx context.Context, id string) (*NotificationDelivery, error)
	ListByNotification(ctx context.Context, notificationID string) ([]NotificationDelivery, error)
	ListPending(ctx context.Context, limit int) ([]NotificationDelivery, error)
	ListRetryable(ctx context.Context, limit int) ([]NotificationDelivery, error)
	Update(ctx context.Context, d *NotificationDelivery) error
	UpdateStatus(ctx context.Context, id, status string, errMsg *string) error
}

// DLQStore persists dead letter queue items.
type DLQStore interface {
	Create(ctx context.Context, d *NotificationDLQ) error
	GetByID(ctx context.Context, id string) (*NotificationDLQ, error)
	List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[NotificationDLQ], error)
	ListFailed(ctx context.Context, orgID string, limit int) ([]NotificationDLQ, error)
	UpdateStatus(ctx context.Context, id, status string) error
}

// WebhookStore persists webhook subscriptions.
type WebhookStore interface {
	Create(ctx context.Context, w *WebhookSubscription) error
	GetByID(ctx context.Context, id string) (*WebhookSubscription, error)
	List(ctx context.Context, orgID string) ([]WebhookSubscription, error)
	ListByEventType(ctx context.Context, orgID, eventType string) ([]WebhookSubscription, error)
	Update(ctx context.Context, w *WebhookSubscription) error
	IncrementStats(ctx context.Context, id string, success bool) error
	Delete(ctx context.Context, id string) error
}

// ----------------------------------------------------------------------------
// Channel Repository
// ----------------------------------------------------------------------------

type channelRepo struct{ db *database.DB }

func NewChannelStore(db *database.DB) ChannelStore { return &channelRepo{db: db} }

func (r *channelRepo) Create(ctx context.Context, c *NotificationChannel) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if len(c.Config) == 0 {
		c.Config = []byte("{}")
	}

	const sql = `
INSERT INTO notification_channels (id, org_id, name, channel_type, config, enabled, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		c.ID, c.OrgID, c.Name, c.ChannelType, c.Config, c.Enabled, c.CreatedBy)
	return database.MapError(row.Scan(&c.CreatedAt, &c.UpdatedAt))
}

func (r *channelRepo) GetByID(ctx context.Context, id string) (*NotificationChannel, error) {
	c, err := database.QueryOne[NotificationChannel](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_channels WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *channelRepo) GetByName(ctx context.Context, name string) (*NotificationChannel, error) {
	c, err := database.QueryOne[NotificationChannel](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_channels WHERE name = $1", name)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *channelRepo) List(ctx context.Context, orgID string) ([]NotificationChannel, error) {
	return database.QueryAll[NotificationChannel](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_channels WHERE org_id = $1 ORDER BY name", orgID)
}

func (r *channelRepo) ListEnabled(ctx context.Context, orgID string) ([]NotificationChannel, error) {
	return database.QueryAll[NotificationChannel](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_channels WHERE org_id = $1 AND enabled = true ORDER BY name", orgID)
}

func (r *channelRepo) Update(ctx context.Context, c *NotificationChannel) error {
	const sql = `
UPDATE notification_channels
SET name = $1, config = $2, enabled = $3, verified = $4, verified_at = $5, updated_at = now()
WHERE id = $6`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		c.Name, c.Config, c.Enabled, c.Verified, c.VerifiedAt, c.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *channelRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM notification_channels WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Template Repository
// ----------------------------------------------------------------------------

type templateRepo struct{ db *database.DB }

func NewTemplateStore(db *database.DB) TemplateStore { return &templateRepo{db: db} }

func (r *templateRepo) Create(ctx context.Context, t *NotificationTemplate) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if len(t.Variables) == 0 {
		t.Variables = []byte("[]")
	}

	const sql = `
INSERT INTO notification_templates (id, org_id, name, event_type, subject, body_text, body_html, slack_blocks, variables, is_default, enabled)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		t.ID, t.OrgID, t.Name, t.EventType, t.Subject, t.BodyText, t.BodyHTML, t.SlackBlocks, t.Variables, t.IsDefault, t.Enabled)
	return database.MapError(row.Scan(&t.CreatedAt, &t.UpdatedAt))
}

func (r *templateRepo) GetByID(ctx context.Context, id string) (*NotificationTemplate, error) {
	t, err := database.QueryOne[NotificationTemplate](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_templates WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *templateRepo) GetByEventType(ctx context.Context, orgID, eventType string) (*NotificationTemplate, error) {
	t, err := database.QueryOne[NotificationTemplate](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_templates WHERE org_id = $1 AND event_type = $2 AND enabled = true ORDER BY is_default DESC LIMIT 1",
		orgID, eventType)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *templateRepo) GetDefault(ctx context.Context, eventType string) (*NotificationTemplate, error) {
	t, err := database.QueryOne[NotificationTemplate](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_templates WHERE org_id IS NULL AND event_type = $1 AND is_default = true", eventType)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *templateRepo) List(ctx context.Context, orgID string) ([]NotificationTemplate, error) {
	return database.QueryAll[NotificationTemplate](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_templates WHERE org_id = $1 OR (org_id IS NULL AND is_default = true) ORDER BY event_type, name", orgID)
}

func (r *templateRepo) Update(ctx context.Context, t *NotificationTemplate) error {
	const sql = `
UPDATE notification_templates
SET name = $1, subject = $2, body_text = $3, body_html = $4, slack_blocks = $5, variables = $6, enabled = $7, updated_at = now()
WHERE id = $8`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		t.Name, t.Subject, t.BodyText, t.BodyHTML, t.SlackBlocks, t.Variables, t.Enabled, t.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *templateRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM notification_templates WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Preference Repository
// ----------------------------------------------------------------------------

type preferenceRepo struct{ db *database.DB }

func NewPreferenceStore(db *database.DB) PreferenceStore { return &preferenceRepo{db: db} }

func (r *preferenceRepo) Upsert(ctx context.Context, p *NotificationPreference) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if len(p.SeverityFilter) == 0 {
		p.SeverityFilter = []string{SeverityInfo, SeverityWarning, SeverityError, SeverityCritical}
	}

	const sql = `
INSERT INTO notification_preferences (id, org_id, user_id, event_type, email_enabled, slack_enabled, webhook_enabled, in_app_enabled, severity_filter)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (org_id, user_id, event_type) DO UPDATE SET
    email_enabled = EXCLUDED.email_enabled,
    slack_enabled = EXCLUDED.slack_enabled,
    webhook_enabled = EXCLUDED.webhook_enabled,
    in_app_enabled = EXCLUDED.in_app_enabled,
    severity_filter = EXCLUDED.severity_filter,
    updated_at = now()
RETURNING id, created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		p.ID, p.OrgID, p.UserID, p.EventType, p.EmailEnabled, p.SlackEnabled, p.WebhookEnabled, p.InAppEnabled, p.SeverityFilter)
	return database.MapError(row.Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt))
}

func (r *preferenceRepo) Get(ctx context.Context, orgID, userID, eventType string) (*NotificationPreference, error) {
	p, err := database.QueryOne[NotificationPreference](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_preferences WHERE org_id = $1 AND user_id = $2 AND event_type = $3",
		orgID, userID, eventType)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *preferenceRepo) ListByUser(ctx context.Context, orgID, userID string) ([]NotificationPreference, error) {
	return database.QueryAll[NotificationPreference](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_preferences WHERE org_id = $1 AND user_id = $2 ORDER BY event_type",
		orgID, userID)
}

func (r *preferenceRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM notification_preferences WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Notification Repository
// ----------------------------------------------------------------------------

type notificationRepo struct{ db *database.DB }

func NewNotificationStore(db *database.DB) NotificationStore { return &notificationRepo{db: db} }

func (r *notificationRepo) Create(ctx context.Context, n *Notification) error {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.Status == "" {
		n.Status = StatusPending
	}
	if n.Severity == "" {
		n.Severity = SeverityInfo
	}
	if len(n.Metadata) == 0 {
		n.Metadata = []byte("{}")
	}

	const sql = `
INSERT INTO notifications (id, org_id, event_type, event_id, title, body, severity, resource_type, resource_id, resource_name, metadata, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING created_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		n.ID, n.OrgID, n.EventType, n.EventID, n.Title, n.Body, n.Severity, n.ResourceType, n.ResourceID, n.ResourceName, n.Metadata, n.Status)
	return database.MapError(row.Scan(&n.CreatedAt))
}

func (r *notificationRepo) GetByID(ctx context.Context, id string) (*Notification, error) {
	n, err := database.QueryOne[Notification](ctx, r.db.Conn(ctx),
		"SELECT * FROM notifications WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *notificationRepo) List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[Notification], error) {
	req = req.Normalize()
	items, err := database.QueryAll[Notification](ctx, r.db.Conn(ctx),
		"SELECT * FROM notifications WHERE org_id = $1 ORDER BY created_at DESC LIMIT $2",
		orgID, req.Limit+1)
	if err != nil {
		return database.Page[Notification]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[Notification]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[Notification]{Items: items}, nil
}

func (r *notificationRepo) UpdateStatus(ctx context.Context, id, status string) error {
	const sql = `UPDATE notifications SET status = $1 WHERE id = $2`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Delivery Repository
// ----------------------------------------------------------------------------

type deliveryRepo struct{ db *database.DB }

func NewDeliveryStore(db *database.DB) DeliveryStore { return &deliveryRepo{db: db} }

func (r *deliveryRepo) Create(ctx context.Context, d *NotificationDelivery) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if d.Status == "" {
		d.Status = DeliveryPending
	}
	if d.MaxAttempts == 0 {
		d.MaxAttempts = 3
	}

	const sql = `
INSERT INTO notification_deliveries (id, org_id, notification_id, channel_id, channel_type, recipient, status, scheduled_at, max_attempts)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		d.ID, d.OrgID, d.NotificationID, d.ChannelID, d.ChannelType, d.Recipient, d.Status, d.ScheduledAt, d.MaxAttempts)
	return database.MapError(row.Scan(&d.CreatedAt, &d.UpdatedAt))
}

func (r *deliveryRepo) GetByID(ctx context.Context, id string) (*NotificationDelivery, error) {
	d, err := database.QueryOne[NotificationDelivery](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_deliveries WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *deliveryRepo) ListByNotification(ctx context.Context, notificationID string) ([]NotificationDelivery, error) {
	return database.QueryAll[NotificationDelivery](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_deliveries WHERE notification_id = $1 ORDER BY created_at", notificationID)
}

func (r *deliveryRepo) ListPending(ctx context.Context, limit int) ([]NotificationDelivery, error) {
	return database.QueryAll[NotificationDelivery](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_deliveries WHERE status = 'pending' ORDER BY scheduled_at ASC LIMIT $1", limit)
}

func (r *deliveryRepo) ListRetryable(ctx context.Context, limit int) ([]NotificationDelivery, error) {
	return database.QueryAll[NotificationDelivery](ctx, r.db.Conn(ctx),
		`SELECT * FROM notification_deliveries 
		 WHERE status = 'failed' AND attempt_count < max_attempts AND next_retry_at <= now()
		 ORDER BY next_retry_at ASC LIMIT $1`, limit)
}

func (r *deliveryRepo) Update(ctx context.Context, d *NotificationDelivery) error {
	const sql = `
UPDATE notification_deliveries
SET status = $1, sent_at = $2, delivered_at = $3, attempt_count = $4, next_retry_at = $5,
    response_code = $6, response_body = $7, error_message = $8, updated_at = now()
WHERE id = $9`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		d.Status, d.SentAt, d.DeliveredAt, d.AttemptCount, d.NextRetryAt,
		d.ResponseCode, d.ResponseBody, d.ErrorMessage, d.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *deliveryRepo) UpdateStatus(ctx context.Context, id, status string, errMsg *string) error {
	const sql = `UPDATE notification_deliveries SET status = $1, error_message = $2, updated_at = now() WHERE id = $3`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, errMsg, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// DLQ Repository
// ----------------------------------------------------------------------------

type dlqRepo struct{ db *database.DB }

func NewDLQStore(db *database.DB) DLQStore { return &dlqRepo{db: db} }

func (r *dlqRepo) Create(ctx context.Context, d *NotificationDLQ) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if d.Status == "" {
		d.Status = DLQFailed
	}

	const sql = `
INSERT INTO notification_dlq (id, org_id, delivery_id, notification_id, channel_type, recipient, failure_reason, last_error, attempt_count, payload, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING created_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		d.ID, d.OrgID, d.DeliveryID, d.NotificationID, d.ChannelType, d.Recipient, d.FailureReason, d.LastError, d.AttemptCount, d.Payload, d.Status)
	return database.MapError(row.Scan(&d.CreatedAt))
}

func (r *dlqRepo) GetByID(ctx context.Context, id string) (*NotificationDLQ, error) {
	d, err := database.QueryOne[NotificationDLQ](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_dlq WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *dlqRepo) List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[NotificationDLQ], error) {
	req = req.Normalize()
	items, err := database.QueryAll[NotificationDLQ](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_dlq WHERE org_id = $1 ORDER BY created_at DESC LIMIT $2",
		orgID, req.Limit+1)
	if err != nil {
		return database.Page[NotificationDLQ]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[NotificationDLQ]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[NotificationDLQ]{Items: items}, nil
}

func (r *dlqRepo) ListFailed(ctx context.Context, orgID string, limit int) ([]NotificationDLQ, error) {
	return database.QueryAll[NotificationDLQ](ctx, r.db.Conn(ctx),
		"SELECT * FROM notification_dlq WHERE org_id = $1 AND status = 'failed' ORDER BY created_at DESC LIMIT $2",
		orgID, limit)
}

func (r *dlqRepo) UpdateStatus(ctx context.Context, id, status string) error {
	var sql string
	if status == DLQReplayed {
		sql = `UPDATE notification_dlq SET status = $1, replayed_at = now() WHERE id = $2`
	} else if status == DLQDiscarded {
		sql = `UPDATE notification_dlq SET status = $1, discarded_at = now() WHERE id = $2`
	} else {
		sql = `UPDATE notification_dlq SET status = $1 WHERE id = $2`
	}

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Webhook Repository
// ----------------------------------------------------------------------------

type webhookRepo struct{ db *database.DB }

func NewWebhookStore(db *database.DB) WebhookStore { return &webhookRepo{db: db} }

func (r *webhookRepo) Create(ctx context.Context, w *WebhookSubscription) error {
	if w.ID == "" {
		w.ID = uuid.NewString()
	}
	if len(w.EventTypes) == 0 {
		w.EventTypes = []byte(`["*"]`)
	}
	if len(w.Headers) == 0 {
		w.Headers = []byte("{}")
	}

	const sql = `
INSERT INTO webhook_subscriptions (id, org_id, name, url, event_types, secret, headers, enabled, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		w.ID, w.OrgID, w.Name, w.URL, w.EventTypes, w.Secret, w.Headers, w.Enabled, w.CreatedBy)
	return database.MapError(row.Scan(&w.CreatedAt, &w.UpdatedAt))
}

func (r *webhookRepo) GetByID(ctx context.Context, id string) (*WebhookSubscription, error) {
	w, err := database.QueryOne[WebhookSubscription](ctx, r.db.Conn(ctx),
		"SELECT * FROM webhook_subscriptions WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *webhookRepo) List(ctx context.Context, orgID string) ([]WebhookSubscription, error) {
	return database.QueryAll[WebhookSubscription](ctx, r.db.Conn(ctx),
		"SELECT * FROM webhook_subscriptions WHERE org_id = $1 ORDER BY name", orgID)
}

func (r *webhookRepo) ListByEventType(ctx context.Context, orgID, eventType string) ([]WebhookSubscription, error) {
	return database.QueryAll[WebhookSubscription](ctx, r.db.Conn(ctx),
		`SELECT * FROM webhook_subscriptions 
		 WHERE org_id = $1 AND enabled = true 
		 AND (event_types @> $2::jsonb OR event_types @> '["*"]'::jsonb)
		 ORDER BY name`,
		orgID, fmt.Sprintf(`["%s"]`, eventType))
}

func (r *webhookRepo) Update(ctx context.Context, w *WebhookSubscription) error {
	const sql = `
UPDATE webhook_subscriptions
SET name = $1, url = $2, event_types = $3, headers = $4, enabled = $5, updated_at = now()
WHERE id = $6`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		w.Name, w.URL, w.EventTypes, w.Headers, w.Enabled, w.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *webhookRepo) IncrementStats(ctx context.Context, id string, success bool) error {
	var sql string
	if success {
		sql = `UPDATE webhook_subscriptions SET success_count = success_count + 1, last_triggered_at = now(), updated_at = now() WHERE id = $1`
	} else {
		sql = `UPDATE webhook_subscriptions SET failure_count = failure_count + 1, last_triggered_at = now(), updated_at = now() WHERE id = $1`
	}

	_, err := r.db.Conn(ctx).Exec(ctx, sql, id)
	return database.MapError(err)
}

func (r *webhookRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM webhook_subscriptions WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------------
// Processed Event Store (consumer idempotency ledger)
// ----------------------------------------------------------------------------

// ProcessedEventStore records which domain events a durable consumer has
// already handled, so at-least-once redelivery stays idempotent.
type ProcessedEventStore interface {
	// MarkProcessed records that (consumer, eventID) has been handled. It
	// returns true when the row was newly inserted (i.e. this is the first time
	// the event is seen) and false when it was already present (a duplicate).
	// It must be called inside the same tenant-scoped transaction as the work it
	// guards so the marker and the work commit atomically.
	MarkProcessed(ctx context.Context, consumer, eventID, orgID string) (bool, error)
}

type processedEventRepo struct{ db *database.DB }

// NewProcessedEventStore builds a Postgres-backed ProcessedEventStore.
func NewProcessedEventStore(db *database.DB) ProcessedEventStore { return &processedEventRepo{db: db} }

func (r *processedEventRepo) MarkProcessed(ctx context.Context, consumer, eventID, orgID string) (bool, error) {
	const sql = `
INSERT INTO notification_processed_events (consumer, event_id, org_id)
VALUES ($1, $2, $3)
ON CONFLICT (consumer, event_id) DO NOTHING`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, consumer, eventID, orgID)
	if err != nil {
		return false, database.MapError(err)
	}
	return tag.RowsAffected() > 0, nil
}

