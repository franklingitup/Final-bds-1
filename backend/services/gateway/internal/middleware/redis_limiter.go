package middleware

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/ratelimit"
)

// HeaderRetryAfter is the standard header advertising how long a client should
// wait before retrying a rate-limited request.
const HeaderRetryAfter = "Retry-After"

// redisAllower is the subset of ratelimit.RedisLimiter behavior the middleware
// depends on. *ratelimit.RedisLimiter satisfies it. Defining the seam here
// keeps the middleware unit-testable without a live Redis.
type redisAllower interface {
	Allow(ctx context.Context, key string, cfg ratelimit.Config) (*ratelimit.Result, error)
}

// RedisRateLimiter is a distributed, gateway rate limiter backed by Redis. It
// enforces the same per-user/per-IP keying and X-RateLimit-* headers as the
// in-memory limiter, but shares state across gateway replicas.
//
// If the Redis backend errors, the limiter fails open (allows the request) so
// that a Redis outage degrades enforcement rather than availability. Fail-open
// decisions are counted and logged.
type RedisRateLimiter struct {
	allower redisAllower
	cfg     ratelimit.Config
	keyFunc func(*fiber.Ctx) string
	log     *slog.Logger
}

// NewRedisRateLimiter builds a Redis-backed limiter. requestsPerMinute and
// burstSize mirror the in-memory limiter's configuration; keyFunc defaults to
// the shared per-user/per-IP key function when nil.
func NewRedisRateLimiter(client *ratelimit.RedisLimiter, requestsPerMinute, burstSize int, log *slog.Logger) *RedisRateLimiter {
	return newRedisRateLimiter(client, requestsPerMinute, burstSize, log)
}

// newRedisRateLimiter is the internal constructor accepting the redisAllower
// seam so tests can inject a fake backend.
func newRedisRateLimiter(allower redisAllower, requestsPerMinute, burstSize int, log *slog.Logger) *RedisRateLimiter {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 60
	}
	if burstSize < 0 {
		burstSize = 0
	}
	if log == nil {
		log = slog.Default()
	}
	return &RedisRateLimiter{
		allower: allower,
		cfg: ratelimit.Config{
			RequestsPerWindow: requestsPerMinute,
			WindowSize:        time.Minute,
			BurstSize:         burstSize,
			KeyPrefix:         "gateway:",
		},
		keyFunc: defaultKeyFunc,
		log:     log,
	}
}

// Middleware returns the Fiber handler enforcing distributed rate limits.
func (rl *RedisRateLimiter) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := rl.keyFunc(c)

		res, err := rl.allower.Allow(c.UserContext(), key, rl.cfg)
		if err != nil {
			// Fail open: a Redis failure must not take the platform down.
			rl.log.WarnContext(c.UserContext(), "rate limiter backend error, allowing request",
				slog.String("error", err.Error()),
				slog.String("key", key),
			)
			rateLimitDecisions.WithLabelValues("redis", "error").Inc()
			return c.Next()
		}

		c.Set(HeaderRateLimitLimit, strconv.Itoa(res.Limit))
		c.Set(HeaderRateLimitRemain, strconv.Itoa(max0(res.Remaining)))
		c.Set(HeaderRateLimitReset, strconv.FormatInt(time.Now().Add(res.ResetAfter).Unix(), 10))

		if !res.Allowed {
			retryAfter := int(res.RetryAfter.Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			c.Set(HeaderRetryAfter, strconv.Itoa(retryAfter))
			rateLimitDecisions.WithLabelValues("redis", "blocked").Inc()
			return apperrors.RateLimited("rate limit exceeded")
		}

		rateLimitDecisions.WithLabelValues("redis", "allowed").Inc()
		return c.Next()
	}
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
