// Package ratelimit provides distributed rate limiting using Redis.
package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter implements distributed rate limiting using Redis with a sliding window algorithm.
type RedisLimiter struct {
	client    redis.UniversalClient
	keyPrefix string
}

// Config holds rate limiter configuration.
type Config struct {
	// RequestsPerWindow is the maximum number of requests allowed in the window.
	RequestsPerWindow int
	// WindowSize is the duration of the rate limit window.
	WindowSize time.Duration
	// BurstSize allows temporary bursts above the limit (0 = no burst).
	BurstSize int
	// KeyPrefix is prepended to all Redis keys.
	KeyPrefix string
}

// DefaultConfig returns sensible defaults (60 req/min with burst of 10).
func DefaultConfig() Config {
	return Config{
		RequestsPerWindow: 60,
		WindowSize:        time.Minute,
		BurstSize:         10,
		KeyPrefix:         "ratelimit:",
	}
}

// Result contains the outcome of a rate limit check.
type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	ResetAfter time.Duration
	RetryAfter time.Duration
}

// NewRedisLimiter creates a new Redis-backed rate limiter.
func NewRedisLimiter(client redis.UniversalClient, keyPrefix string) *RedisLimiter {
	if keyPrefix == "" {
		keyPrefix = "ratelimit:"
	}
	return &RedisLimiter{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// slidingWindowScript implements a sliding window rate limiter in Lua.
// This runs atomically in Redis.
var slidingWindowScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local burst = tonumber(ARGV[4])

-- Remove old entries outside the window
redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)

-- Count current requests in window
local count = redis.call('ZCARD', key)

-- Calculate effective limit (base + burst)
local effective_limit = limit + burst

if count < effective_limit then
    -- Add new request
    redis.call('ZADD', key, now, now .. ':' .. math.random(1000000))
    redis.call('EXPIRE', key, math.ceil(window / 1000) + 1)
    return {1, effective_limit, effective_limit - count - 1, window}
else
    -- Get oldest entry to calculate retry time
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    local retry_after = 0
    if oldest and #oldest >= 2 then
        retry_after = oldest[2] + window - now
    end
    return {0, effective_limit, 0, retry_after}
end
`)

// Allow checks if a request should be allowed for the given key.
func (rl *RedisLimiter) Allow(ctx context.Context, key string, cfg Config) (*Result, error) {
	redisKey := rl.keyPrefix + key
	now := time.Now().UnixMilli()
	windowMs := cfg.WindowSize.Milliseconds()

	result, err := slidingWindowScript.Run(ctx, rl.client, []string{redisKey},
		now, windowMs, cfg.RequestsPerWindow, cfg.BurstSize).Slice()
	if err != nil {
		return nil, fmt.Errorf("rate limit script error: %w", err)
	}

	allowed := result[0].(int64) == 1
	limit := int(result[1].(int64))
	remaining := int(result[2].(int64))
	retryAfterMs := result[3].(int64)

	res := &Result{
		Allowed:    allowed,
		Limit:      limit,
		Remaining:  remaining,
		ResetAfter: cfg.WindowSize,
	}

	if !allowed {
		res.RetryAfter = time.Duration(retryAfterMs) * time.Millisecond
	}

	return res, nil
}

// Reset clears the rate limit for a key.
func (rl *RedisLimiter) Reset(ctx context.Context, key string) error {
	return rl.client.Del(ctx, rl.keyPrefix+key).Err()
}

// TokenBucketLimiter implements a token bucket algorithm for smoother rate limiting.
type TokenBucketLimiter struct {
	client    redis.UniversalClient
	keyPrefix string
}

// NewTokenBucketLimiter creates a new token bucket rate limiter.
func NewTokenBucketLimiter(client redis.UniversalClient, keyPrefix string) *TokenBucketLimiter {
	if keyPrefix == "" {
		keyPrefix = "tokenbucket:"
	}
	return &TokenBucketLimiter{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// tokenBucketScript implements token bucket in Lua.
var tokenBucketScript = redis.NewScript(`
local tokens_key = KEYS[1]
local timestamp_key = KEYS[2]
local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local fill_time = capacity / rate
local ttl = math.ceil(fill_time * 2)

local last_tokens = tonumber(redis.call('GET', tokens_key))
if last_tokens == nil then
    last_tokens = capacity
end

local last_refreshed = tonumber(redis.call('GET', timestamp_key))
if last_refreshed == nil then
    last_refreshed = 0
end

local delta = math.max(0, now - last_refreshed)
local filled_tokens = math.min(capacity, last_tokens + (delta * rate))
local allowed = filled_tokens >= requested
local new_tokens = filled_tokens

if allowed then
    new_tokens = filled_tokens - requested
end

redis.call('SETEX', tokens_key, ttl, new_tokens)
redis.call('SETEX', timestamp_key, ttl, now)

return {allowed and 1 or 0, new_tokens, capacity}
`)

// TokenBucketConfig holds token bucket configuration.
type TokenBucketConfig struct {
	// Rate is tokens added per second.
	Rate float64
	// Capacity is the maximum number of tokens.
	Capacity int
	// KeyPrefix is prepended to all Redis keys.
	KeyPrefix string
}

// Allow checks if a request should be allowed using token bucket algorithm.
func (tb *TokenBucketLimiter) Allow(ctx context.Context, key string, cfg TokenBucketConfig) (*Result, error) {
	tokensKey := tb.keyPrefix + key + ":tokens"
	timestampKey := tb.keyPrefix + key + ":ts"
	now := float64(time.Now().UnixNano()) / 1e9

	result, err := tokenBucketScript.Run(ctx, tb.client,
		[]string{tokensKey, timestampKey},
		cfg.Rate, cfg.Capacity, now, 1).Slice()
	if err != nil {
		return nil, fmt.Errorf("token bucket script error: %w", err)
	}

	allowed := result[0].(int64) == 1
	remaining := int(result[1].(int64))
	capacity := int(result[2].(int64))

	res := &Result{
		Allowed:   allowed,
		Limit:     capacity,
		Remaining: remaining,
	}

	if !allowed {
		res.RetryAfter = time.Duration(float64(time.Second) / cfg.Rate)
	}

	return res, nil
}

// MultiTierLimiter implements tiered rate limiting (e.g., per-user and per-IP).
type MultiTierLimiter struct {
	limiter *RedisLimiter
	tiers   []Tier
}

// Tier defines a rate limit tier.
type Tier struct {
	Name              string
	KeyFunc           func(ctx context.Context, identifier string) string
	RequestsPerWindow int
	WindowSize        time.Duration
	BurstSize         int
}

// NewMultiTierLimiter creates a multi-tier rate limiter.
func NewMultiTierLimiter(client redis.UniversalClient, tiers []Tier) *MultiTierLimiter {
	return &MultiTierLimiter{
		limiter: NewRedisLimiter(client, "multitier:"),
		tiers:   tiers,
	}
}

// Allow checks all tiers and returns the most restrictive result.
func (mt *MultiTierLimiter) Allow(ctx context.Context, identifier string) (*Result, error) {
	var mostRestrictive *Result

	for _, tier := range mt.tiers {
		key := tier.KeyFunc(ctx, identifier)
		cfg := Config{
			RequestsPerWindow: tier.RequestsPerWindow,
			WindowSize:        tier.WindowSize,
			BurstSize:         tier.BurstSize,
			KeyPrefix:         tier.Name + ":",
		}

		result, err := mt.limiter.Allow(ctx, key, cfg)
		if err != nil {
			return nil, fmt.Errorf("tier %s: %w", tier.Name, err)
		}

		if !result.Allowed {
			return result, nil
		}

		if mostRestrictive == nil || result.Remaining < mostRestrictive.Remaining {
			mostRestrictive = result
		}
	}

	return mostRestrictive, nil
}

// DefaultTiers returns common rate limit tiers.
func DefaultTiers(userIDFromCtx func(context.Context) string) []Tier {
	return []Tier{
		{
			Name: "global",
			KeyFunc: func(ctx context.Context, identifier string) string {
				return "global"
			},
			RequestsPerWindow: 10000,
			WindowSize:        time.Second,
			BurstSize:         1000,
		},
		{
			Name: "per-ip",
			KeyFunc: func(ctx context.Context, identifier string) string {
				return "ip:" + identifier
			},
			RequestsPerWindow: 100,
			WindowSize:        time.Minute,
			BurstSize:         20,
		},
		{
			Name: "per-user",
			KeyFunc: func(ctx context.Context, identifier string) string {
				if userID := userIDFromCtx(ctx); userID != "" {
					return "user:" + userID
				}
				return "anon:" + identifier
			},
			RequestsPerWindow: 300,
			WindowSize:        time.Minute,
			BurstSize:         50,
		},
	}
}

// ParseRedisURL parses a Redis URL and returns a client.
func ParseRedisURL(url string) (redis.UniversalClient, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opts), nil
}

// KeyForIP returns a rate limit key for an IP address.
func KeyForIP(ip string) string {
	return "ip:" + ip
}

// KeyForUser returns a rate limit key for a user ID.
func KeyForUser(userID string) string {
	return "user:" + userID
}

// KeyForOrg returns a rate limit key for an organization.
func KeyForOrg(orgID string) string {
	return "org:" + orgID
}

// KeyForEndpoint returns a rate limit key for a specific endpoint.
func KeyForEndpoint(method, path, identifier string) string {
	return "endpoint:" + method + ":" + path + ":" + identifier
}

// FormatHeaders returns rate limit headers as a map.
func FormatHeaders(r *Result) map[string]string {
	headers := map[string]string{
		"X-RateLimit-Limit":     strconv.Itoa(r.Limit),
		"X-RateLimit-Remaining": strconv.Itoa(r.Remaining),
		"X-RateLimit-Reset":     strconv.FormatInt(time.Now().Add(r.ResetAfter).Unix(), 10),
	}
	if r.RetryAfter > 0 {
		headers["Retry-After"] = strconv.Itoa(int(r.RetryAfter.Seconds()))
	}
	return headers
}
