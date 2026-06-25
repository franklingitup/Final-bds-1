package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/logger"
)

// Handler processes a consumed event. It MUST be idempotent: at-least-once
// delivery means the same EventID may be delivered more than once. Returning an
// error triggers retry with backoff; after the retry budget is exhausted the
// event is dead-lettered.
type Handler func(ctx context.Context, e Envelope) error

// SubscriptionOptions configures a durable consumer.
type SubscriptionOptions struct {
	// Durable is the stable consumer name. Required. Multiple instances sharing
	// a Durable form a queue group (work is load-balanced across them).
	Durable string
	// FilterSubject restricts which events are delivered, e.g.
	// "evt.deployment.>". Defaults to all events ("<prefix>.>").
	FilterSubject string
	// Retry governs redelivery and dead-lettering. Zero value uses defaults.
	Retry RetryPolicy
	// MaxAckPending caps in-flight unacknowledged messages (0 = server default).
	MaxAckPending int
	// Upcasters, when set, is applied to every consumed event before the
	// handler runs, migrating older schema versions to the latest registered
	// version. This lets handlers always operate on the current contract. An
	// event whose version cannot be upcast is dead-lettered.
	Upcasters *Registry
}

// Subscription is a running consumer that can be stopped.
type Subscription interface {
	// Stop halts delivery. In-flight handlers are allowed to finish.
	Stop()
}

// Subscriber creates durable subscriptions.
type Subscriber interface {
	Subscribe(ctx context.Context, opts SubscriptionOptions, h Handler) (Subscription, error)
}

// Subscribe creates (or updates) a durable JetStream consumer and begins
// delivering events to h. Retry backoff and dead-lettering are handled
// automatically per opts.Retry.
func (c *Client) Subscribe(ctx context.Context, opts SubscriptionOptions, h Handler) (Subscription, error) {
	if opts.Durable == "" {
		return nil, fmt.Errorf("events: subscription requires a durable name")
	}
	policy := opts.Retry.normalized()
	filter := opts.FilterSubject
	if filter == "" {
		filter = c.cfg.SubjectPrefix + ".>"
	}

	cons, err := c.js.CreateOrUpdateConsumer(ctx, c.cfg.Stream, jetstream.ConsumerConfig{
		Durable:       opts.Durable,
		FilterSubject: filter,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    policy.MaxAttempts,
		BackOff:       policy.backoffSchedule(),
		MaxAckPending: opts.MaxAckPending,
	})
	if err != nil {
		return nil, fmt.Errorf("events: create consumer %q: %w", opts.Durable, err)
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		c.handleMessage(ctx, opts.Durable, policy, opts.Upcasters, h, msg)
	})
	if err != nil {
		return nil, fmt.Errorf("events: start consume %q: %w", opts.Durable, err)
	}
	return &natsSubscription{cc: cc}, nil
}

func (c *Client) handleMessage(ctx context.Context, durable string, policy RetryPolicy, reg *Registry, h Handler, msg jetstream.Msg) {
	attempt := 1
	if meta, err := msg.Metadata(); err == nil {
		attempt = int(meta.NumDelivered)
	}

	var env Envelope
	if err := json.Unmarshal(msg.Data(), &env); err != nil {
		// Unparseable (poison) message: dead-letter the raw bytes and terminate
		// so it is never redelivered.
		_ = c.deadLetterRaw(ctx, durable, msg.Subject(), msg.Data(), attempt, "unmarshal: "+err.Error())
		_ = msg.Term()
		return
	}

	// Apply registered upcasters so the handler always sees the latest schema
	// version. An unrecoverable upcast is a poison message: dead-letter and
	// terminate rather than retry forever.
	if reg != nil {
		upgraded, err := reg.Upgrade(env)
		if err != nil {
			_ = c.deadLetterRaw(ctx, durable, msg.Subject(), msg.Data(), attempt, "upcast: "+err.Error())
			_ = msg.Term()
			return
		}
		env = upgraded
	}

	hErr := h(c.contextFromEnvelope(ctx, env), env)
	if hErr == nil {
		_ = msg.Ack()
		return
	}

	c.log.Warn("event handler failed",
		"consumer", durable, "type", env.Type, "eventId", env.EventID,
		"attempt", attempt, "maxAttempts", policy.MaxAttempts, "error", hErr)

	if attempt >= policy.MaxAttempts {
		dl := DeadLetter{
			Envelope:     env,
			OrigSubject:  msg.Subject(),
			Consumer:     durable,
			Reason:       hErr.Error(),
			Attempts:     attempt,
			DeadLettered: time.Now().UTC(),
		}
		if err := c.publishDeadLetter(ctx, dl); err != nil {
			// Could not dead-letter: keep the message for another attempt rather
			// than silently dropping it.
			c.log.Error("failed to dead-letter event", "eventId", env.EventID, "error", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Term()
		return
	}

	_ = msg.NakWithDelay(policy.backoff(attempt))
}

// contextFromEnvelope restores correlation and tenant context for the handler so
// downstream logging and authorization see the originating request's metadata.
func (c *Client) contextFromEnvelope(ctx context.Context, e Envelope) context.Context {
	if e.CorrelationID != "" {
		ctx = logger.WithCorrelationID(ctx, e.CorrelationID)
	}
	if e.OrgID != "" {
		ctx = authz.WithOrg(ctx, e.OrgID)
	}
	return ctx
}

type natsSubscription struct {
	cc jetstream.ConsumeContext
}

func (s *natsSubscription) Stop() { s.cc.Stop() }

var _ Subscriber = (*Client)(nil)
var _ Subscription = (*natsSubscription)(nil)
