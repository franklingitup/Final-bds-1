package audit

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// supportedDomains is the set of event domains the audit service records. The
// consumer subscribes to every platform event but only persists events whose
// type begins with one of these domains; anything else is acknowledged and
// skipped so the trail stays focused on business/state-changing facts.
var supportedDomains = map[string]bool{
	"auth":       true,
	"tenant":     true,
	"project":    true,
	"cluster":    true,
	"deployment": true,
	"secret":     true,
}

// SupportedDomains returns the recorded domains (sorted-insensitive copy) for
// documentation and tests.
func SupportedDomains() []string {
	out := make([]string, 0, len(supportedDomains))
	for d := range supportedDomains {
		out = append(out, d)
	}
	return out
}

// domainOf returns the leading segment of a dotted event type, e.g.
// "auth.user.created" -> "auth".
func domainOf(eventType string) string {
	if i := strings.IndexByte(eventType, '.'); i >= 0 {
		return eventType[:i]
	}
	return eventType
}

// isSupportedDomain reports whether events of eventType should be recorded.
func isSupportedDomain(eventType string) bool {
	return supportedDomains[domainOf(eventType)]
}

// AuditLog is a single immutable audit record. It maps to the audit_logs table;
// db tags are scanned by name (RowToStructByNameLax).
type AuditLog struct {
	ID           string          `db:"id"`
	EventID      string          `db:"event_id"`
	EventType    string          `db:"event_type"`
	OrgID        string          `db:"org_id"`
	ActorID      *string         `db:"actor_id"`
	ResourceType *string         `db:"resource_type"`
	ResourceID   *string         `db:"resource_id"`
	OccurredAt   time.Time       `db:"occurred_at"`
	Payload      json.RawMessage `db:"payload"`
	CreatedAt    time.Time       `db:"created_at"`
}

// Cursor returns the keyset position used for list pagination. The list order is
// (created_at DESC, id DESC), the platform's canonical ordering.
func (a AuditLog) Cursor() database.Cursor {
	return database.Cursor{CreatedAt: a.CreatedAt, ID: a.ID}
}

// AuditFilter narrows an audit-log query. All fields are optional; zero values
// are ignored. Time bounds apply to the event occurrence time (occurred_at).
type AuditFilter struct {
	EventType    string
	Domain       string
	ActorID      string
	ResourceType string
	ResourceID   string
	From         *time.Time
	To           *time.Time
}

// AuditLogView is the API representation of an audit record. The occurrence time
// is exposed as `timestamp` per the service contract.
type AuditLogView struct {
	ID           string          `json:"id"`
	EventID      string          `json:"eventId"`
	EventType    string          `json:"eventType"`
	OrgID        string          `json:"organizationId"`
	ActorID      string          `json:"actorId,omitempty"`
	ResourceType string          `json:"resourceType,omitempty"`
	ResourceID   string          `json:"resourceId,omitempty"`
	Timestamp    time.Time       `json:"timestamp"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	RecordedAt   time.Time       `json:"recordedAt"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toAuditLogView(a AuditLog) AuditLogView {
	return AuditLogView{
		ID:           a.ID,
		EventID:      a.EventID,
		EventType:    a.EventType,
		OrgID:        a.OrgID,
		ActorID:      deref(a.ActorID),
		ResourceType: deref(a.ResourceType),
		ResourceID:   deref(a.ResourceID),
		Timestamp:    a.OccurredAt,
		Payload:      a.Payload,
		RecordedAt:   a.CreatedAt,
	}
}
