// Package events is the platform's reusable, NATS/JetStream-backed event
// framework. It provides a versioned event envelope, a JetStream publisher and
// subscriber with retry and dead-letter handling, a transactional outbox with a
// relay, and an in-memory broker for tests.
//
// Design notes:
//   - Delivery is at-least-once. Handlers MUST be idempotent (deduplicate on
//     EventID). The publisher sets the JetStream Msg-Id to EventID so publishes
//     are deduplicated within the stream's duplicate window.
//   - Events carry tenant (OrgID), correlation, and W3C trace context so async
//     consumers can restore the originating request context.
//   - This package contains no business events; services define their own event
//     types and payloads on top of it.
package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Actor identifies who or what triggered an event.
type Actor struct {
	Type string `json:"type"` // user | agent | system
	ID   string `json:"id"`
}

// Resource identifies the entity an event concerns.
type Resource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Envelope is the canonical, versioned event structure published to the broker.
// Payload holds the event-specific body; use DecodePayload to read it into a
// typed struct.
type Envelope struct {
	EventID       string          `json:"eventId"`
	Type          string          `json:"type"`    // dotted name, e.g. "deployment.succeeded"
	Version       int             `json:"version"` // schema version of Type, >= 1
	OrgID         string          `json:"orgId"`
	CorrelationID string          `json:"correlationId,omitempty"`
	TraceParent   string          `json:"traceparent,omitempty"` // W3C traceparent
	OccurredAt    time.Time       `json:"occurredAt"`
	Actor         Actor           `json:"actor"`
	Resource      Resource        `json:"resource"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// Option customizes an Envelope at construction time.
type Option func(*Envelope)

// WithCorrelationID sets the correlation ID linking the event to a request.
func WithCorrelationID(id string) Option { return func(e *Envelope) { e.CorrelationID = id } }

// WithTraceParent sets the W3C traceparent for cross-service trace continuity.
func WithTraceParent(tp string) Option { return func(e *Envelope) { e.TraceParent = tp } }

// WithActor sets the actor that triggered the event.
func WithActor(a Actor) Option { return func(e *Envelope) { e.Actor = a } }

// WithResource sets the resource the event concerns.
func WithResource(r Resource) Option { return func(e *Envelope) { e.Resource = r } }

// New builds an event envelope with a generated ID and current timestamp. The
// payload is marshaled to JSON; pass nil for events without a body.
func New(eventType string, version int, orgID string, payload any, opts ...Option) (Envelope, error) {
	e := Envelope{
		EventID:    uuid.NewString(),
		Type:       eventType,
		Version:    version,
		OrgID:      orgID,
		OccurredAt: time.Now().UTC(),
	}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, fmt.Errorf("events: marshal payload: %w", err)
		}
		e.Payload = raw
	}
	for _, opt := range opts {
		opt(&e)
	}
	if err := e.Validate(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// Validate verifies the envelope carries the fields required for routing and
// auditing.
func (e Envelope) Validate() error {
	switch {
	case e.EventID == "":
		return fmt.Errorf("events: envelope missing eventId")
	case e.Type == "":
		return fmt.Errorf("events: envelope missing type")
	case e.Version < 1:
		return fmt.Errorf("events: envelope version must be >= 1")
	case e.OrgID == "":
		return fmt.Errorf("events: envelope missing orgId")
	}
	return nil
}

// DecodePayload unmarshals an envelope's payload into a value of type T.
func DecodePayload[T any](e Envelope) (T, error) {
	var v T
	if len(e.Payload) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(e.Payload, &v); err != nil {
		return v, fmt.Errorf("events: decode payload for %s v%d: %w", e.Type, e.Version, err)
	}
	return v, nil
}
