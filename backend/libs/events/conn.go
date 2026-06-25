package events

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/bdsplatform/platform/backend/libs/config"
)

// dlqSubjectPrefix namespaces dead-lettered events. The DLQ stream binds to
// "<dlqSubjectPrefix>.>".
const dlqSubjectPrefix = "dlq"

// Client owns the NATS connection and JetStream context and is the factory for
// publishers and subscribers. One Client per service is sufficient.
type Client struct {
	nc  *nats.Conn
	js  jetstream.JetStream
	cfg config.NATSConfig
	log *slog.Logger
}

// Connect dials NATS and initializes JetStream. The connection is configured to
// reconnect indefinitely so transient broker outages do not crash services.
func Connect(ctx context.Context, cfg config.NATSConfig, log *slog.Logger) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("events: NATS URL is required")
	}
	if log == nil {
		log = slog.Default()
	}

	nc, err := nats.Connect(cfg.URL,
		nats.Name("bdsplatform"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Warn("nats disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			log.Info("nats reconnected", "url", c.ConnectedUrl())
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("events: connect nats: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("events: init jetstream: %w", err)
	}

	return &Client{nc: nc, js: js, cfg: cfg, log: log}, nil
}

// JetStream exposes the underlying JetStream context for advanced use.
func (c *Client) JetStream() jetstream.JetStream { return c.js }

// EnsureStreams creates (or updates) the primary events stream and the
// dead-letter stream. Safe to call on every startup; it is idempotent.
func (c *Client) EnsureStreams(ctx context.Context) error {
	if _, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        c.cfg.Stream,
		Description: "Platform domain events",
		Subjects:    []string{c.cfg.SubjectPrefix + ".>"},
		Retention:   jetstream.LimitsPolicy,
		Storage:     jetstream.FileStorage,
		// Deduplicate re-published events (publisher sets Msg-Id = EventID).
		Duplicates: 2 * time.Minute,
		MaxAge:     7 * 24 * time.Hour,
	}); err != nil {
		return fmt.Errorf("events: ensure events stream: %w", err)
	}

	if _, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        c.cfg.DLQStream,
		Description: "Dead-lettered platform events",
		Subjects:    []string{dlqSubjectPrefix + ".>"},
		Retention:   jetstream.LimitsPolicy,
		Storage:     jetstream.FileStorage,
		MaxAge:      30 * 24 * time.Hour,
	}); err != nil {
		return fmt.Errorf("events: ensure dlq stream: %w", err)
	}
	return nil
}

// Close drains and closes the NATS connection. Draining flushes pending messages
// and lets in-flight handlers finish.
func (c *Client) Close() {
	if c.nc != nil && !c.nc.IsClosed() {
		_ = c.nc.Drain()
	}
}
