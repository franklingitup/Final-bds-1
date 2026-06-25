package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// Publisher publishes events to the broker. Implementations are safe for
// concurrent use.
type Publisher interface {
	// Publish validates and publishes the event. Delivery is at-least-once;
	// the event's ID is used for broker-side deduplication.
	Publish(ctx context.Context, e Envelope) error
}

// natsPublisher publishes envelopes to JetStream.
type natsPublisher struct {
	js     jetstream.JetStream
	prefix string
}

// NewPublisher returns a JetStream-backed Publisher bound to the client's
// configured subject prefix.
func (c *Client) NewPublisher() Publisher {
	return &natsPublisher{js: c.js, prefix: c.cfg.SubjectPrefix}
}

// Publish marshals the envelope and publishes it to its versioned subject. The
// JetStream Msg-Id is set to the EventID so re-publishing the same event within
// the stream's duplicate window is deduplicated (idempotent publish).
func (p *natsPublisher) Publish(ctx context.Context, e Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("events: marshal envelope: %w", err)
	}
	subject := Subject(p.prefix, e)
	if _, err := p.js.Publish(ctx, subject, data, jetstream.WithMsgID(e.EventID)); err != nil {
		return fmt.Errorf("events: publish %s: %w", subject, err)
	}
	return nil
}

var _ Publisher = (*natsPublisher)(nil)
