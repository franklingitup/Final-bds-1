// Package internalapi is a small HTTP client for service-to-service calls on
// the private network. It talks to backend services directly, not through the
// API gateway.
package internalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout bounds a single internal lookup so a down dependency cannot
// stall the caller's request indefinitely.
const DefaultTimeout = 3 * time.Second

// HeaderInternalToken is the optional shared-secret header for internal routes.
const HeaderInternalToken = "X-Internal-Token"

// UserSummary is the public display projection of a user. It never includes
// secrets (password hashes, tokens, MFA material).
type UserSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// UserLookup is the lookup surface used by tenant and audit services so tests
// can substitute a fake without spinning up HTTP.
type UserLookup interface {
	GetUsers(ctx context.Context, ids []string) (map[string]UserSummary, error)
}

// AuthClient calls the auth service's internal user-lookup API.
type AuthClient struct {
	baseURL       string
	InternalToken string
	http          *http.Client
}

// NewAuthClient builds a client for the auth service at baseURL
// (e.g. http://localhost:8081). The HTTP timeout defaults to DefaultTimeout.
func NewAuthClient(baseURL string) *AuthClient {
	return &AuthClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{Timeout: DefaultTimeout},
	}
}

type usersResponse struct {
	Items []UserSummary `json:"items"`
}

// GetUsers resolves a batch of user IDs to public display info in one call.
// Missing IDs are omitted from the result rather than treated as an error.
// A transport or non-2xx response is returned so callers can degrade.
func (c *AuthClient) GetUsers(ctx context.Context, ids []string) (map[string]UserSummary, error) {
	out := make(map[string]UserSummary)
	if c == nil || c.baseURL == "" || len(ids) == 0 {
		return out, nil
	}

	unique := uniqueNonEmpty(ids)
	if len(unique) == 0 {
		return out, nil
	}

	u, err := url.Parse(c.baseURL + "/internal/users")
	if err != nil {
		return nil, fmt.Errorf("internalapi: parse url: %w", err)
	}
	q := u.Query()
	q.Set("ids", strings.Join(unique, ","))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("internalapi: new request: %w", err)
	}
	if c.InternalToken != "" {
		req.Header.Set(HeaderInternalToken, c.InternalToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("internalapi: get users: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("internalapi: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("internalapi: get users: status %d", resp.StatusCode)
	}

	var parsed usersResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("internalapi: decode users: %w", err)
	}
	for _, u := range parsed.Items {
		if u.ID == "" {
			continue
		}
		out[u.ID] = u
	}
	return out, nil
}

func uniqueNonEmpty(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
