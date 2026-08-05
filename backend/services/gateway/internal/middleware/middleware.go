// Package middleware provides gateway-specific middleware for authentication,
// rate limiting, and request enrichment.
package middleware

import (
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/bdsplatform/platform/backend/libs/authz"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/auth"
)

// rateLimitDecisions counts rate limit decisions labeled by backend
// implementation ("memory" or "redis") and outcome ("allowed", "blocked",
// "error"). The "error" outcome indicates the limiter backend failed and the
// request was allowed through (fail-open).
var rateLimitDecisions = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gateway_rate_limit_decisions_total",
		Help: "Total rate limit decisions by backend and outcome.",
	},
	[]string{"backend", "outcome"},
)

// Limiter is the behavior the gateway router requires from a rate limiter.
// Both the in-memory RateLimiter and the Redis-backed RedisRateLimiter
// satisfy it, allowing the backend to be selected at startup without changing
// the router.
type Limiter interface {
	Middleware() fiber.Handler
}

// Header names used by the gateway.
const (
	HeaderRequestID       = "X-Request-ID"
	HeaderCorrelationID   = "X-Correlation-ID"
	HeaderOrgID           = "X-Org-ID"
	HeaderUserID          = "X-User-ID"
	HeaderUserEmail       = "X-User-Email"
	HeaderTokenType       = "X-Token-Type"
	HeaderRateLimitLimit  = "X-RateLimit-Limit"
	HeaderRateLimitRemain = "X-RateLimit-Remaining"
	HeaderRateLimitReset  = "X-RateLimit-Reset"
)

// Locals keys for storing values in Fiber context.
const (
	LocalIdentity  = "gateway_identity"
	LocalRequestID = "request_id"
)

// RequestID ensures every request has a unique request ID.
func RequestID() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Get(HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(HeaderRequestID, id)
		c.Locals(LocalRequestID, id)

		// Also set correlation ID if not present.
		if c.Get(HeaderCorrelationID) == "" {
			c.Set(HeaderCorrelationID, id)
		}

		return c.Next()
	}
}

// Authentication validates the Bearer token and stores the identity in context.
// OPTIONS (preflight) requests are always allowed through without authentication.
//
// When revoker is non-nil, a revocation check runs after signature validation:
// a signature-valid token whose JTI or owning session has been revoked is
// rejected with 401. A nil revoker disables the check (signature-only), matching
// the gateway's pre-revocation behavior.
func Authentication(validator *auth.Validator, revoker *RevocationChecker) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Skip authentication for CORS preflight requests.
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		authHeader := c.Get(fiber.HeaderAuthorization)
		token := auth.ExtractBearerToken(authHeader)
		if token == "" {
			return auth.ErrNoToken
		}

		identity, err := validator.ValidateToken(token)
		if err != nil {
			return err
		}

		if revoker != nil && revoker.Revoked(c.UserContext(), GetRequestID(c), identity) {
			return auth.ErrTokenRevoked
		}

		// Store identity in locals and context.
		c.Locals(LocalIdentity, identity)
		c.SetUserContext(auth.WithIdentity(c.UserContext(), identity))
		c.SetUserContext(authz.WithPrincipal(c.UserContext(), authz.Principal{
			UserID: identity.UserID,
			OrgID:  identity.OrgID,
		}))

		// Forward identity headers to downstream services via request headers.
		c.Request().Header.Set(HeaderUserID, identity.UserID)
		if identity.Email != "" {
			c.Request().Header.Set(HeaderUserEmail, identity.Email)
		}
		c.Request().Header.Set(HeaderTokenType, string(identity.Type))

		return c.Next()
	}
}

// OptionalAuthentication validates the Bearer token if present but doesn't require it.
// A present-but-revoked token is rejected (a revoker must not be bypassable by
// routing through an optional-auth path).
func OptionalAuthentication(validator *auth.Validator, revoker *RevocationChecker) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := auth.ExtractBearerToken(c.Get(fiber.HeaderAuthorization))
		if token == "" {
			return c.Next()
		}

		identity, err := validator.ValidateToken(token)
		if err != nil {
			// Invalid token is still an error even for optional auth.
			return err
		}

		if revoker != nil && revoker.Revoked(c.UserContext(), GetRequestID(c), identity) {
			return auth.ErrTokenRevoked
		}

		c.Locals(LocalIdentity, identity)
		c.SetUserContext(auth.WithIdentity(c.UserContext(), identity))
		c.Request().Header.Set(HeaderUserID, identity.UserID)
		if identity.Email != "" {
			c.Request().Header.Set(HeaderUserEmail, identity.Email)
		}
		c.Request().Header.Set(HeaderTokenType, string(identity.Type))

		return c.Next()
	}
}

// OrgScope extracts the organization ID from the path and validates access.
func OrgScope() fiber.Handler {
	return func(c *fiber.Ctx) error {
		orgID := c.Params("orgId")
		if orgID == "" {
			return c.Next()
		}

		// Get identity from locals.
		identity, ok := c.Locals(LocalIdentity).(auth.Identity)
		if !ok {
			return auth.ErrNoToken
		}

		// For service accounts, validate org matches.
		if identity.Type == auth.TokenTypeServiceAccount {
			if identity.OrgID != "" && identity.OrgID != orgID {
				return auth.ErrOrgMismatch
			}
		}

		// Forward org ID to downstream services via request header.
		c.Request().Header.Set(HeaderOrgID, orgID)
		c.SetUserContext(authz.WithOrg(c.UserContext(), orgID))

		return c.Next()
	}
}

// ProjectScope extracts the project ID from the path for downstream authorization.
func ProjectScope() fiber.Handler {
	return func(c *fiber.Ctx) error {
		projectID := c.Params("projectId")
		if projectID == "" {
			return c.Next()
		}

		// The downstream service handles project-level authorization.
		// We just ensure the identity is present.
		if _, ok := c.Locals(LocalIdentity).(auth.Identity); !ok {
			return auth.ErrNoToken
		}

		return c.Next()
	}
}

// RequireScope ensures the service account has the required scope.
func RequireScope(scope string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		identity, ok := c.Locals(LocalIdentity).(auth.Identity)
		if !ok {
			return auth.ErrNoToken
		}

		// User tokens have implicit full scope.
		if identity.Type == auth.TokenTypeUser {
			return c.Next()
		}

		// Service accounts need explicit scope.
		if !identity.HasScope(scope) {
			return auth.ErrInsufficientScope
		}

		return c.Next()
	}
}

// RateLimiterConfig configures the rate limiter.
type RateLimiterConfig struct {
	RequestsPerMinute int
	BurstSize         int
	KeyFunc           func(*fiber.Ctx) string
}

// DefaultRateLimiterConfig returns sensible defaults.
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
		KeyFunc:           defaultKeyFunc,
	}
}

func defaultKeyFunc(c *fiber.Ctx) string {
	// Use user ID if authenticated, otherwise IP.
	if identity, ok := c.Locals(LocalIdentity).(auth.Identity); ok {
		return "user:" + identity.UserID
	}
	return "ip:" + c.IP()
}

// rateLimitEntry tracks rate limit state for a single key.
type rateLimitEntry struct {
	tokens    float64
	lastCheck time.Time
}

// RateLimiter implements a token bucket rate limiter.
type RateLimiter struct {
	config  RateLimiterConfig
	entries map[string]*rateLimitEntry
	mu      sync.Mutex
}

// NewRateLimiter creates a rate limiter with the given config.
func NewRateLimiter(config RateLimiterConfig) *RateLimiter {
	return &RateLimiter{
		config:  config,
		entries: make(map[string]*rateLimitEntry),
	}
}

// Middleware returns the rate limiting middleware.
func (rl *RateLimiter) Middleware() fiber.Handler {
	refillRate := float64(rl.config.RequestsPerMinute) / 60.0 // tokens per second

	return func(c *fiber.Ctx) error {
		key := rl.config.KeyFunc(c)

		rl.mu.Lock()
		entry, exists := rl.entries[key]
		if !exists {
			entry = &rateLimitEntry{
				tokens:    float64(rl.config.BurstSize),
				lastCheck: time.Now(),
			}
			rl.entries[key] = entry
		}

		// Refill tokens.
		now := time.Now()
		elapsed := now.Sub(entry.lastCheck).Seconds()
		entry.tokens += elapsed * refillRate
		if entry.tokens > float64(rl.config.BurstSize) {
			entry.tokens = float64(rl.config.BurstSize)
		}
		entry.lastCheck = now

		// Check if request can proceed.
		if entry.tokens < 1 {
			remaining := int(entry.tokens)
			resetTime := time.Duration((1-entry.tokens)/refillRate) * time.Second
			rl.mu.Unlock()

			c.Set(HeaderRateLimitLimit, itoa(rl.config.RequestsPerMinute))
			c.Set(HeaderRateLimitRemain, itoa(remaining))
			c.Set(HeaderRateLimitReset, itoa(int(resetTime.Seconds())))

			rateLimitDecisions.WithLabelValues("memory", "blocked").Inc()
			return apperrors.RateLimited("rate limit exceeded")
		}

		// Consume token.
		entry.tokens--
		remaining := int(entry.tokens)
		rl.mu.Unlock()

		c.Set(HeaderRateLimitLimit, itoa(rl.config.RequestsPerMinute))
		c.Set(HeaderRateLimitRemain, itoa(remaining))

		rateLimitDecisions.WithLabelValues("memory", "allowed").Inc()
		return c.Next()
	}
}

// Cleanup removes stale entries from the rate limiter.
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for key, entry := range rl.entries {
		if entry.lastCheck.Before(cutoff) {
			delete(rl.entries, key)
		}
	}
}

func itoa(i int) string {
	if i < 0 {
		i = 0
	}
	return strconv.Itoa(i)
}

// GetIdentity retrieves the identity from Fiber locals.
func GetIdentity(c *fiber.Ctx) (auth.Identity, bool) {
	identity, ok := c.Locals(LocalIdentity).(auth.Identity)
	return identity, ok
}

// GetRequestID retrieves the request ID from Fiber locals.
func GetRequestID(c *fiber.Ctx) string {
	id, _ := c.Locals(LocalRequestID).(string)
	return id
}
