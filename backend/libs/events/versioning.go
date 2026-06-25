package events

import (
	"fmt"
	"strconv"
	"strings"
)

// Subject derives the NATS subject for an event from its type and version, e.g.
// prefix "evt" + type "deployment.succeeded" v2 -> "evt.deployment.succeeded.v2".
// Including the version in the subject lets consumers bind to a specific schema
// version while a stream captures all versions via the "<prefix>.>" wildcard.
func Subject(prefix string, e Envelope) string {
	return fmt.Sprintf("%s.%s.v%d", prefix, e.Type, e.Version)
}

// SubjectFor builds a subject from an explicit type and version.
func SubjectFor(prefix, eventType string, version int) string {
	return fmt.Sprintf("%s.%s.v%d", prefix, eventType, version)
}

// CanonicalName returns the catalog name for an event: "<type>.v<version>", e.g.
// type "tenant.organization.created" v1 -> "tenant.organization.created.v1".
// The catalog name is derived, never stored as a first-class envelope field.
func CanonicalName(e Envelope) string {
	return CatalogName(e.Type, e.Version)
}

// CatalogName builds the canonical catalog name from an explicit type and
// version. It is the inverse of ParseSubject's (type, version) for the catalog
// namespace (no transport prefix).
func CatalogName(eventType string, version int) string {
	return fmt.Sprintf("%s.v%d", eventType, version)
}

// ParseSubject extracts the event type and version from a subject produced by
// Subject. It returns ok=false if the subject does not match the scheme.
func ParseSubject(prefix, subject string) (eventType string, version int, ok bool) {
	trimmed := strings.TrimPrefix(subject, prefix+".")
	if trimmed == subject {
		return "", 0, false
	}
	idx := strings.LastIndex(trimmed, ".v")
	if idx < 0 {
		return "", 0, false
	}
	v, err := strconv.Atoi(trimmed[idx+2:])
	if err != nil || v < 1 {
		return "", 0, false
	}
	return trimmed[:idx], v, true
}

// Upcaster migrates an envelope from one schema version to the next. It must
// return an envelope whose Version is exactly one greater than its input.
type Upcaster func(Envelope) (Envelope, error)

// Registry holds upcasters that progressively migrate older event versions to
// the current schema. Consumers call Upgrade before decoding so handlers only
// deal with the latest version.
type Registry struct {
	// keyed by "<type>:<fromVersion>"
	upcasters map[string]Upcaster
}

// NewRegistry returns an empty upcaster registry.
func NewRegistry() *Registry {
	return &Registry{upcasters: make(map[string]Upcaster)}
}

func upcasterKey(eventType string, fromVersion int) string {
	return eventType + ":" + strconv.Itoa(fromVersion)
}

// Register adds an upcaster that migrates eventType from fromVersion to
// fromVersion+1. Registering the same (type, version) twice panics, since that
// indicates a programming error in wiring the upcaster chain.
func (r *Registry) Register(eventType string, fromVersion int, up Upcaster) {
	key := upcasterKey(eventType, fromVersion)
	if _, exists := r.upcasters[key]; exists {
		panic(fmt.Sprintf("events: duplicate upcaster for %s", key))
	}
	r.upcasters[key] = up
}

// Upgrade applies the registered upcaster chain until no further upcaster exists
// for the envelope's (type, version), yielding the latest known version.
func (r *Registry) Upgrade(e Envelope) (Envelope, error) {
	for {
		up, ok := r.upcasters[upcasterKey(e.Type, e.Version)]
		if !ok {
			return e, nil
		}
		prevVersion := e.Version
		next, err := up(e)
		if err != nil {
			return Envelope{}, fmt.Errorf("events: upcast %s v%d: %w", e.Type, prevVersion, err)
		}
		if next.Version != prevVersion+1 {
			return Envelope{}, fmt.Errorf("events: upcaster for %s v%d must produce v%d, got v%d",
				e.Type, prevVersion, prevVersion+1, next.Version)
		}
		e = next
	}
}
