package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	libmw "github.com/bdsplatform/platform/backend/libs/middleware"
	"github.com/bdsplatform/platform/backend/libs/ratelimit"
)

// fakeAllower is a controllable redisAllower for tests.
type fakeAllower struct {
	mu     sync.Mutex
	result *ratelimit.Result
	err    error
	calls  int32
	keys   []string
}

func (f *fakeAllower) Allow(_ context.Context, key string, _ ratelimit.Config) (*ratelimit.Result, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	f.keys = append(f.keys, key)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func newRedisTestApp(rl *RedisRateLimiter) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: libmw.ErrorHandler()})
	app.Use(rl.Middleware())
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}

func TestRedisRateLimiter_Allowed(t *testing.T) {
	fake := &fakeAllower{result: &ratelimit.Result{
		Allowed:    true,
		Limit:      70,
		Remaining:  69,
		ResetAfter: time.Minute,
	}}
	rl := newRedisRateLimiter(fake, 60, 10, nil)
	app := newRedisTestApp(rl)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(HeaderRateLimitLimit); got != "70" {
		t.Errorf("expected limit header 70, got %q", got)
	}
	if got := resp.Header.Get(HeaderRateLimitRemain); got != "69" {
		t.Errorf("expected remaining header 69, got %q", got)
	}
}

func TestRedisRateLimiter_Blocked(t *testing.T) {
	fake := &fakeAllower{result: &ratelimit.Result{
		Allowed:    false,
		Limit:      70,
		Remaining:  0,
		RetryAfter: 5 * time.Second,
	}}
	rl := newRedisRateLimiter(fake, 60, 10, nil)
	app := newRedisTestApp(rl)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(HeaderRetryAfter); got != "5" {
		t.Errorf("expected Retry-After 5, got %q", got)
	}
	if got := resp.Header.Get(HeaderRateLimitRemain); got != "0" {
		t.Errorf("expected remaining 0, got %q", got)
	}
}

func TestRedisRateLimiter_FailsOpenOnError(t *testing.T) {
	fake := &fakeAllower{err: errors.New("redis unavailable")}
	rl := newRedisRateLimiter(fake, 60, 10, nil)
	app := newRedisTestApp(rl)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected fail-open 200 on backend error, got %d", resp.StatusCode)
	}
}

func TestRedisRateLimiter_BlockedRetryAfterMinimum(t *testing.T) {
	// A sub-second RetryAfter must still advertise at least 1 second.
	fake := &fakeAllower{result: &ratelimit.Result{
		Allowed:    false,
		Limit:      10,
		Remaining:  0,
		RetryAfter: 100 * time.Millisecond,
	}}
	rl := newRedisRateLimiter(fake, 60, 10, nil)
	app := newRedisTestApp(rl)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get(HeaderRetryAfter); got != "1" {
		t.Errorf("expected Retry-After clamped to 1, got %q", got)
	}
}

func TestRedisRateLimiter_Concurrent(t *testing.T) {
	fake := &fakeAllower{result: &ratelimit.Result{Allowed: true, Limit: 1000, Remaining: 999, ResetAfter: time.Minute}}
	rl := newRedisRateLimiter(fake, 60, 10, nil)
	app := newRedisTestApp(rl)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&fake.calls); got != n {
		t.Errorf("expected %d limiter calls, got %d", n, got)
	}
}
