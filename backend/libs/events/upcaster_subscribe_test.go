package events

import (
	"context"
	"encoding/json"
	"testing"
)

// TestSubscribe_AppliesUpcastersBeforeHandler verifies that a subscriber with a
// registered upcaster chain delivers the latest schema version to the handler,
// migrating older versions automatically.
func TestSubscribe_AppliesUpcastersBeforeHandler(t *testing.T) {
	reg := NewRegistry()
	// v1 -> v2 renames "name" to "fullName".
	reg.Register("auth.user.created", 1, func(e Envelope) (Envelope, error) {
		var p map[string]any
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return Envelope{}, err
		}
		if v, ok := p["name"]; ok {
			p["fullName"] = v
			delete(p, "name")
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return Envelope{}, err
		}
		e.Version = 2
		e.Payload = raw
		return e, nil
	})

	broker := NewMemoryBroker("evt")
	ctx := context.Background()

	var gotVersion int
	var gotFullName string
	_, err := broker.Subscribe(ctx, SubscriptionOptions{
		Durable:       "consumer",
		FilterSubject: "evt.auth.>",
		Upcasters:     reg,
	}, func(_ context.Context, e Envelope) error {
		gotVersion = e.Version
		var p map[string]any
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		gotFullName, _ = p["fullName"].(string)
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Publish a v1 event.
	e, err := New("auth.user.created", 1, "platform", map[string]any{"userId": "u-1", "name": "Ada"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := broker.Publish(ctx, e); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if gotVersion != 2 {
		t.Errorf("handler saw version %d, want 2 (upcast applied)", gotVersion)
	}
	if gotFullName != "Ada" {
		t.Errorf("handler saw fullName %q, want %q", gotFullName, "Ada")
	}
}

func TestCatalogName(t *testing.T) {
	e, _ := New("tenant.organization.created", 1, "org-1", nil)
	if got := CanonicalName(e); got != "tenant.organization.created.v1" {
		t.Errorf("CanonicalName = %q", got)
	}
	if got := CatalogName("auth.user.created", 2); got != "auth.user.created.v2" {
		t.Errorf("CatalogName = %q", got)
	}
}
