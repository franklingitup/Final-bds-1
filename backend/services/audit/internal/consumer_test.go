package audit

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// TestConsumer_RecordsSupportedEvents drives the consumer end-to-end through the
// in-memory broker: published events are dispatched to the handler, which
// records the supported domains and skips the rest.
func TestConsumer_RecordsSupportedEvents(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv()
	broker := events.NewMemoryBroker("evt")

	consumer := NewConsumer(env.svc, broker, "evt", nil)
	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("start consumer: %v", err)
	}
	defer consumer.Stop()

	org := uuid.NewString()
	publish := func(eventType, orgID string) events.Envelope {
		e, err := events.New(eventType, 1, orgID, map[string]any{"x": 1},
			events.WithActor(events.Actor{Type: "user", ID: "actor-1"}),
			events.WithResource(events.Resource{Type: "r", ID: "1"}))
		if err != nil {
			t.Fatalf("new event: %v", err)
		}
		if err := broker.Publish(ctx, e); err != nil {
			t.Fatalf("publish %s: %v", eventType, err)
		}
		return e
	}

	tenantEvt := publish("tenant.organization.created", org)
	authEvt := publish("auth.user.created", "platform")
	publish("billing.invoice.issued", org) // unsupported domain

	if len(env.store.byEvent) != 2 {
		t.Fatalf("expected 2 recorded events, got %d", len(env.store.byEvent))
	}
	if _, ok := env.store.byEvent[tenantEvt.EventID]; !ok {
		t.Error("expected tenant event recorded")
	}
	if _, ok := env.store.byEvent[authEvt.EventID]; !ok {
		t.Error("expected auth event recorded")
	}
}

// TestConsumer_IdempotentRedelivery verifies a redelivered event does not
// duplicate the audit record.
func TestConsumer_IdempotentRedelivery(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv()
	broker := events.NewMemoryBroker("evt")

	consumer := NewConsumer(env.svc, broker, "evt", nil)
	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("start consumer: %v", err)
	}
	defer consumer.Stop()

	e, err := events.New("cluster.registered", 1, uuid.NewString(), map[string]any{"n": "c1"},
		events.WithResource(events.Resource{Type: "cluster", ID: "c1"}))
	if err != nil {
		t.Fatalf("new event: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := broker.Publish(ctx, e); err != nil {
			t.Fatalf("publish attempt %d: %v", i, err)
		}
	}
	if len(env.store.byEvent) != 1 {
		t.Errorf("expected 1 record after redelivery, got %d", len(env.store.byEvent))
	}
}
