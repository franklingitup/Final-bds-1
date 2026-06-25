package database

import (
	"encoding/base64"
	"encoding/json"
	"time"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Pagination defaults. Lists are cursor (keyset) paginated as specified in
// docs/04-api-spec.md (`?limit=&cursor=`, responses carry `nextCursor`).
const (
	DefaultPageLimit = 20
	MaxPageLimit     = 100
)

// PageRequest is an incoming pagination request.
type PageRequest struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

// Normalize clamps Limit into [1, MaxPageLimit], applying the default when unset.
func (p PageRequest) Normalize() PageRequest {
	switch {
	case p.Limit <= 0:
		p.Limit = DefaultPageLimit
	case p.Limit > MaxPageLimit:
		p.Limit = MaxPageLimit
	}
	return p
}

// Page is a single page of results plus the cursor for the following page. When
// NextCursor is empty there are no further results.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// Cursor is a keyset position. (CreatedAt, ID) is a stable, unique ordering key
// matching the platform's list indexes which sort by `created_at DESC, id DESC`.
type Cursor struct {
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"id"`
}

// IsZero reports whether the cursor points at the start of the result set.
func (c Cursor) IsZero() bool { return c.ID == "" && c.CreatedAt.IsZero() }

// EncodeCursor serializes a keyset position into an opaque, URL-safe token.
func EncodeCursor(c Cursor) string {
	b, _ := json.Marshal(c) // Cursor has only encodable fields; cannot fail.
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor parses an opaque token produced by EncodeCursor. An empty token
// yields the zero Cursor (start of results). Malformed tokens are a validation
// error so the API returns 422 rather than 500.
func DecodeCursor(token string) (Cursor, error) {
	if token == "" {
		return Cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, apperrors.Validation("invalid pagination cursor")
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return Cursor{}, apperrors.Validation("invalid pagination cursor")
	}
	return c, nil
}

// BuildPage assembles a Page from a slice fetched with limit+1 semantics: when
// more than limit rows are returned, an extra row was read to detect a next
// page; it is trimmed and its predecessor's key becomes NextCursor.
func BuildPage[T any](items []T, limit int, key func(T) Cursor) Page[T] {
	if limit > 0 && len(items) > limit {
		trimmed := items[:limit]
		return Page[T]{
			Items:      trimmed,
			NextCursor: EncodeCursor(key(trimmed[limit-1])),
		}
	}
	return Page[T]{Items: items}
}
