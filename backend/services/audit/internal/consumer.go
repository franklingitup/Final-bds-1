package audit

import (
	"context"
	"log/slog"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// durableName is the stable JetStream consumer name. Instances sharing it form a
// queue group so audit recording scales horizontally without double-writing
// (the insert is idempotent regardless).
const durableName = "audit-recorder"

// Consumer subscribes to every platform event and records the supported domains
// into the audit log. It is the audit service's single ingestion path.
type Consumer struct {
	svc        *Service
	subscriber events.Subscriber
	prefix     string
	log        *slog.Logger
	sub        events.Subscription
}

// NewConsumer builds an audit Consumer. subjectPrefix is the NATS subject prefix
// (config NATS.SubjectPrefix, e.g. "evt"); the consumer filters on
// "<prefix>.>" to receive all events.
func NewConsumer(svc *Service, subscriber events.Subscriber, subjectPrefix string, log *slog.Logger) *Consumer {
	if log == nil {
		log = slog.Default()
	}
	return &Consumer{svc: svc, subscriber: subscriber, prefix: subjectPrefix, log: log}
}

// Start registers the durable subscription and begins recording events. The
// handler is idempotent: returning an error triggers the framework's retry and
// dead-letter handling, so a transient database failure is retried rather than
// dropped.
func (c *Consumer) Start(ctx context.Context) error {
	filter := ""
	if c.prefix != "" {
		filter = c.prefix + ".>"
	}
	sub, err := c.subscriber.Subscribe(ctx, events.SubscriptionOptions{
		Durable:       durableName,
		FilterSubject: filter,
	}, c.handle)
	if err != nil {
		return err
	}
	c.sub = sub
	return nil
}

// Stop halts delivery. In-flight handlers are allowed to finish.
func (c *Consumer) Stop() {
	if c.sub != nil {
		c.sub.Stop()
	}
}

func (c *Consumer) handle(ctx context.Context, e events.Envelope) error {
	inserted, err := c.svc.RecordEvent(ctx, e)
	if err != nil {
		// Returning the error lets the framework retry and eventually
		// dead-letter; do not swallow it.
		return err
	}
	if inserted {
		c.log.Debug("recorded audit event", "type", e.Type, "eventId", e.EventID, "orgId", e.OrgID)
	}
	return nil
}
