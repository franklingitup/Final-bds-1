package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.RequestsPerWindow != 60 {
		t.Errorf("expected 60 requests per window, got %d", cfg.RequestsPerWindow)
	}
	if cfg.WindowSize != time.Minute {
		t.Errorf("expected 1 minute window, got %v", cfg.WindowSize)
	}
	if cfg.BurstSize != 10 {
		t.Errorf("expected burst size 10, got %d", cfg.BurstSize)
	}
	if cfg.KeyPrefix != "ratelimit:" {
		t.Errorf("expected prefix 'ratelimit:', got %s", cfg.KeyPrefix)
	}
}

func TestKeyForIP(t *testing.T) {
	key := KeyForIP("192.168.1.1")
	if key != "ip:192.168.1.1" {
		t.Errorf("expected 'ip:192.168.1.1', got %s", key)
	}
}

func TestKeyForUser(t *testing.T) {
	key := KeyForUser("user-123")
	if key != "user:user-123" {
		t.Errorf("expected 'user:user-123', got %s", key)
	}
}

func TestKeyForOrg(t *testing.T) {
	key := KeyForOrg("org-456")
	if key != "org:org-456" {
		t.Errorf("expected 'org:org-456', got %s", key)
	}
}

func TestKeyForEndpoint(t *testing.T) {
	key := KeyForEndpoint("POST", "/api/users", "user-123")
	expected := "endpoint:POST:/api/users:user-123"
	if key != expected {
		t.Errorf("expected %q, got %q", expected, key)
	}
}

func TestFormatHeaders(t *testing.T) {
	result := &Result{
		Allowed:    true,
		Limit:      100,
		Remaining:  95,
		ResetAfter: time.Minute,
	}

	headers := FormatHeaders(result)

	if headers["X-RateLimit-Limit"] != "100" {
		t.Errorf("unexpected limit header: %s", headers["X-RateLimit-Limit"])
	}
	if headers["X-RateLimit-Remaining"] != "95" {
		t.Errorf("unexpected remaining header: %s", headers["X-RateLimit-Remaining"])
	}
	if _, ok := headers["X-RateLimit-Reset"]; !ok {
		t.Error("expected reset header")
	}
}

func TestFormatHeadersWithRetryAfter(t *testing.T) {
	result := &Result{
		Allowed:    false,
		Limit:      100,
		Remaining:  0,
		ResetAfter: time.Minute,
		RetryAfter: 30 * time.Second,
	}

	headers := FormatHeaders(result)

	if headers["Retry-After"] != "30" {
		t.Errorf("unexpected retry-after header: %s", headers["Retry-After"])
	}
}

func TestResult(t *testing.T) {
	t.Run("Allowed", func(t *testing.T) {
		result := &Result{
			Allowed:   true,
			Limit:     100,
			Remaining: 50,
		}

		if !result.Allowed {
			t.Error("expected allowed to be true")
		}
	})

	t.Run("NotAllowed", func(t *testing.T) {
		result := &Result{
			Allowed:    false,
			Limit:      100,
			Remaining:  0,
			RetryAfter: 5 * time.Second,
		}

		if result.Allowed {
			t.Error("expected allowed to be false")
		}
		if result.RetryAfter != 5*time.Second {
			t.Errorf("unexpected retry after: %v", result.RetryAfter)
		}
	})
}

func TestTokenBucketConfig(t *testing.T) {
	cfg := TokenBucketConfig{
		Rate:      10.0,
		Capacity:  100,
		KeyPrefix: "bucket:",
	}

	if cfg.Rate != 10.0 {
		t.Errorf("expected rate 10.0, got %f", cfg.Rate)
	}
	if cfg.Capacity != 100 {
		t.Errorf("expected capacity 100, got %d", cfg.Capacity)
	}
}

func TestTier(t *testing.T) {
	tier := Tier{
		Name: "per-user",
		KeyFunc: func(ctx context.Context, identifier string) string {
			return "user:" + identifier
		},
		RequestsPerWindow: 100,
		WindowSize:        time.Minute,
		BurstSize:         20,
	}

	if tier.Name != "per-user" {
		t.Errorf("unexpected name: %s", tier.Name)
	}

	key := tier.KeyFunc(context.Background(), "test-user")
	if key != "user:test-user" {
		t.Errorf("unexpected key: %s", key)
	}
}

func TestDefaultTiers(t *testing.T) {
	userFunc := func(ctx context.Context) string {
		return "test-user"
	}

	tiers := DefaultTiers(userFunc)

	if len(tiers) != 3 {
		t.Errorf("expected 3 tiers, got %d", len(tiers))
	}

	// Check tier names
	tierNames := make(map[string]bool)
	for _, tier := range tiers {
		tierNames[tier.Name] = true
	}

	expectedTiers := []string{"global", "per-ip", "per-user"}
	for _, name := range expectedTiers {
		if !tierNames[name] {
			t.Errorf("missing tier: %s", name)
		}
	}
}

func TestConfig(t *testing.T) {
	cfg := Config{
		RequestsPerWindow: 100,
		WindowSize:        5 * time.Minute,
		BurstSize:         20,
		KeyPrefix:         "custom:",
	}

	if cfg.RequestsPerWindow != 100 {
		t.Errorf("unexpected requests per window: %d", cfg.RequestsPerWindow)
	}
	if cfg.WindowSize != 5*time.Minute {
		t.Errorf("unexpected window size: %v", cfg.WindowSize)
	}
	if cfg.BurstSize != 20 {
		t.Errorf("unexpected burst size: %d", cfg.BurstSize)
	}
	if cfg.KeyPrefix != "custom:" {
		t.Errorf("unexpected key prefix: %s", cfg.KeyPrefix)
	}
}

func TestSlidingWindowScript(t *testing.T) {
	// Just verify the script is not nil and compiles
	if slidingWindowScript == nil {
		t.Error("sliding window script is nil")
	}
}

func TestTokenBucketScript(t *testing.T) {
	// Just verify the script is not nil and compiles
	if tokenBucketScript == nil {
		t.Error("token bucket script is nil")
	}
}

func TestNewRedisLimiterDefaults(t *testing.T) {
	// Test with empty prefix (should use default)
	limiter := NewRedisLimiter(nil, "")
	if limiter.keyPrefix != "ratelimit:" {
		t.Errorf("expected default prefix, got %s", limiter.keyPrefix)
	}

	// Test with custom prefix
	limiter2 := NewRedisLimiter(nil, "custom:")
	if limiter2.keyPrefix != "custom:" {
		t.Errorf("expected custom prefix, got %s", limiter2.keyPrefix)
	}
}

func TestNewTokenBucketLimiterDefaults(t *testing.T) {
	// Test with empty prefix (should use default)
	limiter := NewTokenBucketLimiter(nil, "")
	if limiter.keyPrefix != "tokenbucket:" {
		t.Errorf("expected default prefix, got %s", limiter.keyPrefix)
	}

	// Test with custom prefix
	limiter2 := NewTokenBucketLimiter(nil, "custom:")
	if limiter2.keyPrefix != "custom:" {
		t.Errorf("expected custom prefix, got %s", limiter2.keyPrefix)
	}
}
