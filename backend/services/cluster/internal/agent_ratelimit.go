package cluster

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/ratelimit"
)

// RegistrationRateLimiter returns a Fiber middleware that rate-limits agent
// registration attempts by IP address. This provides defense-in-depth against
// brute-force attacks on registration tokens, in addition to the gateway's
// global rate limiting.
//
// The limiter is optional: when nil (Redis not configured), requests are allowed
// through with a warning log. This matches the existing codebase convention of
// fail-open on Redis unavailability.
func RegistrationRateLimiter(limiter *ratelimit.RedisLimiter, log *slog.Logger) fiber.Handler {
	if log == nil {
		log = slog.Default()
	}

	return func(c *fiber.Ctx) error {
		// If no limiter is configured, fail open (matches existing codebase pattern).
		if limiter == nil {
			return c.Next()
		}

		cfg := ratelimit.Config{
			RequestsPerWindow: 5,
			WindowSize:        time.Minute,
			BurstSize:         2,
			KeyPrefix:         "register:",
		}

		result, err := limiter.Allow(c.UserContext(), ratelimit.KeyForIP(c.IP()), cfg)
		if err != nil {
			// Fail open on Redis errors, consistent with gateway's existing behavior.
			log.WarnContext(c.UserContext(), "rate limiter error; allowing request",
				slog.String("error", err.Error()),
				slog.String("ip", c.IP()))
			return c.Next()
		}

		if !result.Allowed {
			return apperrors.RateLimited("too many registration attempts")
		}

		return c.Next()
	}
}
