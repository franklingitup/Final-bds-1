package events

import (
	"context"
	"testing"
)

type samplePayload struct {
	RevisionID string `json:"revisionId"`
	Replicas   int    `json:"replicas"`
}

func TestNew_BuildsValidEnvelope(t *testing.T) {
	e, err := New("deployment.succeeded", 1, "org-1",
		samplePayload{RevisionID: "r-1", Replicas: 3},
		WithCorrelationID("corr-1"),
		WithActor(Actor{Type: "user", ID: "u-1"}),
		WithResource(Resource{Type: "deployment", ID: "d-1"}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e.EventID == "" {
		t.Error("expected generated event ID")
	}
	if e.Version != 1 || e.Type != "deployment.succeeded" || e.OrgID != "org-1" {
		t.Errorf("unexpected envelope %+v", e)
	}
	if e.CorrelationID != "corr-1" || e.Actor.ID != "u-1" || e.Resource.ID != "d-1" {
		t.Errorf("options not applied: %+v", e)
	}
	if e.OccurredAt.IsZero() {
		t.Error("expected timestamp set")
	}

	got, err := DecodePayload[samplePayload](e)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got.RevisionID != "r-1" || got.Replicas != 3 {
		t.Errorf("payload round-trip = %+v", got)
	}
}

func TestNew_RejectsInvalid(t *testing.T) {
	if _, err := New("", 1, "org", nil); err == nil {
		t.Error("expected error for empty type")
	}
	if _, err := New("x", 0, "org", nil); err == nil {
		t.Error("expected error for version < 1")
	}
	if _, err := New("x", 1, "", nil); err == nil {
		t.Error("expected error for empty orgId")
	}
}

func TestMemoryBroker_DispatchesToMatchingSubscribers(t *testing.T) {
	broker := NewMemoryBroker("evt")
	ctx := context.Background()

	var deploymentHits, allHits int
	_, _ = broker.Subscribe(ctx, SubscriptionOptions{Durable: "d", FilterSubject: "evt.deployment.>"},
		func(_ context.Context, _ Envelope) error { deploymentHits++; return nil })
	_, _ = broker.Subscribe(ctx, SubscriptionOptions{Durable: "a", FilterSubject: "evt.>"},
		func(_ context.Context, _ Envelope) error { allHits++; return nil })

	dep, _ := New("deployment.succeeded", 1, "org", nil)
	if err := broker.Publish(ctx, dep); err != nil {
		t.Fatalf("publish: %v", err)
	}
	clu, _ := New("cluster.registered", 1, "org", nil)
	if err := broker.Publish(ctx, clu); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if deploymentHits != 1 {
		t.Errorf("deployment subscriber hits = %d, want 1", deploymentHits)
	}
	if allHits != 2 {
		t.Errorf("catch-all subscriber hits = %d, want 2", allHits)
	}
	if len(broker.Published()) != 2 {
		t.Errorf("published log = %d, want 2", len(broker.Published()))
	}
}

func TestSubjectMatches(t *testing.T) {
	cases := []struct {
		filter, subject string
		want            bool
	}{
		{"evt.>", "evt.deployment.succeeded.v1", true},
		{"evt.deployment.>", "evt.deployment.succeeded.v1", true},
		{"evt.deployment.>", "evt.cluster.registered.v1", false},
		{"evt.*.succeeded.v1", "evt.deployment.succeeded.v1", true},
		{"evt.*.succeeded.v1", "evt.deployment.failed.v1", false},
		{"evt.a.b", "evt.a.b", true},
		{"evt.a.b", "evt.a.b.c", false},
		{"evt.>", "evt", false},
	}
	for _, c := range cases {
		if got := subjectMatches(c.filter, c.subject); got != c.want {
			t.Errorf("subjectMatches(%q, %q) = %v, want %v", c.filter, c.subject, got, c.want)
		}
	}
}

// Compile-time assertions for the framework's core interfaces.
var (
	_ Publisher  = (*MemoryBroker)(nil)
	_ Subscriber = (*MemoryBroker)(nil)
	_ Publisher  = (*natsPublisher)(nil)
	_ Subscriber = (*Client)(nil)
)
