package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// Channel is the interface for notification delivery channels.
type Channel interface {
	Send(ctx context.Context, msg *Message) error
	Test(ctx context.Context) error
}

// Message is the content to be sent.
type Message struct {
	To       string
	Subject  string
	BodyText string
	BodyHTML string
	Metadata map[string]interface{}
}

// ----------------------------------------------------------------------------
// Email Channel
// ----------------------------------------------------------------------------

// EmailChannel sends notifications via email.
type EmailChannel struct {
	cfg    EmailConfig
	client *http.Client
}

// NewEmailChannel creates a new email channel.
func NewEmailChannel(cfg EmailConfig) *EmailChannel {
	return &EmailChannel{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Send sends an email notification.
func (c *EmailChannel) Send(ctx context.Context, msg *Message) error {
	if c.cfg.SMTPHost == "" {
		return fmt.Errorf("SMTP host not configured")
	}

	from := c.cfg.FromAddress
	if c.cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", c.cfg.FromName, c.cfg.FromAddress)
	}

	// Build email headers
	var body bytes.Buffer
	body.WriteString(fmt.Sprintf("From: %s\r\n", from))
	body.WriteString(fmt.Sprintf("To: %s\r\n", msg.To))
	body.WriteString(fmt.Sprintf("Subject: %s\r\n", msg.Subject))
	body.WriteString("MIME-Version: 1.0\r\n")

	if msg.BodyHTML != "" {
		body.WriteString("Content-Type: multipart/alternative; boundary=boundary42\r\n\r\n")
		body.WriteString("--boundary42\r\n")
		body.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
		body.WriteString(msg.BodyText)
		body.WriteString("\r\n--boundary42\r\n")
		body.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
		body.WriteString(msg.BodyHTML)
		body.WriteString("\r\n--boundary42--")
	} else {
		body.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
		body.WriteString(msg.BodyText)
	}

	addr := fmt.Sprintf("%s:%d", c.cfg.SMTPHost, c.cfg.SMTPPort)

	var auth smtp.Auth
	if c.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", c.cfg.SMTPUser, c.cfg.SMTPPassword, c.cfg.SMTPHost)
	}

	err := smtp.SendMail(addr, auth, c.cfg.FromAddress, []string{msg.To}, body.Bytes())
	if err != nil {
		return fmt.Errorf("smtp send failed: %w", err)
	}

	return nil
}

// Test sends a test email.
func (c *EmailChannel) Test(ctx context.Context) error {
	return c.Send(ctx, &Message{
		To:       c.cfg.FromAddress,
		Subject:  "Test Notification",
		BodyText: "This is a test notification from BDS Platform.",
	})
}

// ----------------------------------------------------------------------------
// Slack Channel
// ----------------------------------------------------------------------------

// SlackChannel sends notifications via Slack webhooks.
type SlackChannel struct {
	cfg    SlackConfig
	client *http.Client
}

// NewSlackChannel creates a new Slack channel.
func NewSlackChannel(cfg SlackConfig) *SlackChannel {
	return &SlackChannel{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// SlackMessage is the Slack webhook payload.
type SlackMessage struct {
	Channel     string        `json:"channel,omitempty"`
	Username    string        `json:"username,omitempty"`
	IconEmoji   string        `json:"icon_emoji,omitempty"`
	Text        string        `json:"text,omitempty"`
	Attachments []interface{} `json:"attachments,omitempty"`
	Blocks      []interface{} `json:"blocks,omitempty"`
}

// Send sends a Slack notification.
func (c *SlackChannel) Send(ctx context.Context, msg *Message) error {
	if c.cfg.WebhookURL == "" {
		return fmt.Errorf("Slack webhook URL not configured")
	}

	payload := SlackMessage{
		Channel:   c.cfg.Channel,
		Username:  c.cfg.Username,
		IconEmoji: c.cfg.IconEmoji,
		Text:      msg.BodyText,
	}

	// Check if we have Slack blocks in metadata
	if blocks, ok := msg.Metadata["slack_blocks"].([]interface{}); ok {
		payload.Blocks = blocks
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.WebhookURL, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Test sends a test Slack message.
func (c *SlackChannel) Test(ctx context.Context) error {
	return c.Send(ctx, &Message{
		To:       c.cfg.Channel,
		BodyText: "🔔 Test notification from BDS Platform",
	})
}

// ----------------------------------------------------------------------------
// Webhook Channel
// ----------------------------------------------------------------------------

// WebhookChannel sends notifications via HTTP webhooks.
type WebhookChannel struct {
	cfg    WebhookConfig
	client *http.Client
}

// NewWebhookChannel creates a new webhook channel.
func NewWebhookChannel(cfg WebhookConfig) *WebhookChannel {
	return &WebhookChannel{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// WebhookPayload is the webhook request payload.
type WebhookPayload struct {
	Event     string                 `json:"event"`
	Timestamp string                 `json:"timestamp"`
	Title     string                 `json:"title"`
	Body      string                 `json:"body"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Send sends a webhook notification.
func (c *WebhookChannel) Send(ctx context.Context, msg *Message) error {
	if c.cfg.URL == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	payload := WebhookPayload{
		Event:     msg.Metadata["event_type"].(string),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Title:     msg.Subject,
		Body:      msg.BodyText,
		Metadata:  msg.Metadata,
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	method := c.cfg.Method
	if method == "" {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, c.cfg.URL, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// Add custom headers
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}

	// Add HMAC signature if secret is configured
	if c.cfg.Secret != "" {
		signature := computeHMAC(jsonBody, c.cfg.Secret)
		req.Header.Set("X-Signature-256", "sha256="+signature)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Test sends a test webhook.
func (c *WebhookChannel) Test(ctx context.Context) error {
	return c.Send(ctx, &Message{
		Subject:  "Test Notification",
		BodyText: "This is a test notification from BDS Platform.",
		Metadata: map[string]interface{}{
			"event_type": "test.notification",
		},
	})
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyWebhookSignature verifies an incoming webhook signature.
func VerifyWebhookSignature(payload []byte, signature, secret string) bool {
	signature = strings.TrimPrefix(signature, "sha256=")
	expected := computeHMAC(payload, secret)
	return hmac.Equal([]byte(signature), []byte(expected))
}
