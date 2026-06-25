package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// DeadLetter wraps an event that exhausted its retries, capturing why and how
// many times delivery was attempted. It is published to the DLQ stream for
// inspection, alerting, and manual replay.
type DeadLetter struct {
	Envelope     Envelope  `json:"envelope"`
	OrigSubject  string    `json:"origSubject"`
	Consumer     string    `json:"consumer"`
	Reason       string    `json:"reason"`
	Attempts     int       `json:"attempts"`
	DeadLettered time.Time `json:"deadLetteredAt"`
}

// dlqSubject derives the dead-letter subject for an event, e.g.
// "dlq.deployment.succeeded.v1".
func dlqSubject(e Envelope) string {
	return fmt.Sprintf("%s.%s.v%d", dlqSubjectPrefix, e.Type, e.Version)
}

// publishDeadLetter writes a dead-letter record to the DLQ stream.
func (c *Client) publishDeadLetter(ctx context.Context, dl DeadLetter) error {
	data, err := json.Marshal(dl)
	if err != nil {
		return fmt.Errorf("events: marshal dead letter: %w", err)
	}
	subject := dlqSubject(dl.Envelope)
	// Use the original EventID as Msg-Id so duplicate dead-letters collapse.
	if _, err := c.js.Publish(ctx, subject, data, jetstream.WithMsgID("dlq-"+dl.Envelope.EventID)); err != nil {
		return fmt.Errorf("events: publish dead letter %s: %w", subject, err)
	}
	return nil
}

// deadLetterRaw dead-letters a message whose body could not be parsed as an
// Envelope. The raw bytes are preserved for inspection under a fixed subject.
func (c *Client) deadLetterRaw(ctx context.Context, consumer, origSubject string, raw []byte, attempts int, reason string) error {
	record := map[string]any{
		"consumer":       consumer,
		"origSubject":    origSubject,
		"reason":         reason,
		"attempts":       attempts,
		"raw":            string(raw),
		"deadLetteredAt": time.Now().UTC(),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("events: marshal raw dead letter: %w", err)
	}
	subject := dlqSubjectPrefix + "._unparseable"
	if _, err := c.js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("events: publish raw dead letter: %w", err)
	}
	return nil
}
