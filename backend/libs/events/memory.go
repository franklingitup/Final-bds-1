package events

import (
	"context"
	"strings"
	"sync"
)

// MemoryBroker is an in-process Publisher/Subscriber for tests and local use. It
// dispatches synchronously and supports NATS-style subject wildcards (`*` and
// `>`) so subscription filters behave like the real broker. It does not
// implement retry or dead-lettering; a handler error surfaces from Publish.
type MemoryBroker struct {
	prefix string

	mu        sync.RWMutex
	subs      []memorySub
	published []Envelope
}

type memorySub struct {
	filter    string
	handler   Handler
	upcasters *Registry
}

// NewMemoryBroker returns an in-memory broker using the given subject prefix.
func NewMemoryBroker(prefix string) *MemoryBroker {
	if prefix == "" {
		prefix = "evt"
	}
	return &MemoryBroker{prefix: prefix}
}

// Publish validates the event, records it, and dispatches it to every matching
// subscriber. If a handler returns an error, Publish returns it.
func (b *MemoryBroker) Publish(ctx context.Context, e Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	subject := Subject(b.prefix, e)

	b.mu.RLock()
	b.published = append(b.published, e)
	matched := make([]memorySub, 0, len(b.subs))
	for _, s := range b.subs {
		if subjectMatches(s.filter, subject) {
			matched = append(matched, s)
		}
	}
	b.mu.RUnlock()

	for _, s := range matched {
		delivered := e
		if s.upcasters != nil {
			upgraded, err := s.upcasters.Upgrade(e)
			if err != nil {
				return err
			}
			delivered = upgraded
		}
		if err := s.handler(ctx, delivered); err != nil {
			return err
		}
	}
	return nil
}

// Subscribe registers a handler for events whose subject matches the filter.
// FilterSubject defaults to all events ("<prefix>.>").
func (b *MemoryBroker) Subscribe(_ context.Context, opts SubscriptionOptions, h Handler) (Subscription, error) {
	filter := opts.FilterSubject
	if filter == "" {
		filter = b.prefix + ".>"
	}
	b.mu.Lock()
	b.subs = append(b.subs, memorySub{filter: filter, handler: h, upcasters: opts.Upcasters})
	b.mu.Unlock()
	return memoryNoopSubscription{}, nil
}

// Published returns a copy of all events published so far (test helper).
func (b *MemoryBroker) Published() []Envelope {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]Envelope(nil), b.published...)
}

type memoryNoopSubscription struct{}

func (memoryNoopSubscription) Stop() {}

// subjectMatches reports whether a NATS subject matches a filter that may
// contain `*` (single token) and `>` (one-or-more trailing tokens) wildcards.
func subjectMatches(filter, subject string) bool {
	f := strings.Split(filter, ".")
	s := strings.Split(subject, ".")

	for i, token := range f {
		if token == ">" {
			return i < len(s) // `>` matches one or more remaining tokens
		}
		if i >= len(s) {
			return false
		}
		if token == "*" {
			continue
		}
		if token != s[i] {
			return false
		}
	}
	return len(f) == len(s)
}

var (
	_ Publisher  = (*MemoryBroker)(nil)
	_ Subscriber = (*MemoryBroker)(nil)
)
