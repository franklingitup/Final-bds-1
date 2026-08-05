package security

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestRevocationList spins up an in-memory Redis (miniredis) and returns a
// TokenRevocationList wired to it plus the server handle so tests can advance
// its clock for TTL assertions.
func newTestRevocationList(t *testing.T) (*TokenRevocationList, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewTokenRevocationList(client, "revoked:"), mr
}

func TestTokenRevocationList_RevokeAndIsRevoked(t *testing.T) {
	rl, _ := newTestRevocationList(t)
	ctx := context.Background()

	revoked, err := rl.IsRevoked(ctx, "jti-1")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("expected not revoked before Revoke")
	}

	if err := rl.Revoke(ctx, "jti-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	revoked, err = rl.IsRevoked(ctx, "jti-1")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if !revoked {
		t.Fatal("expected revoked after Revoke")
	}
}

func TestTokenRevocationList_RevokeExpiredIsNoop(t *testing.T) {
	rl, _ := newTestRevocationList(t)
	ctx := context.Background()

	// An already-expired token needs no marker: it fails validation on its own.
	if err := rl.Revoke(ctx, "jti-old", time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revoked, err := rl.IsRevoked(ctx, "jti-old")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("expected no marker written for an already-expired token")
	}
}

func TestTokenRevocationList_TTLMatchesExpiry(t *testing.T) {
	rl, mr := newTestRevocationList(t)
	ctx := context.Background()

	if err := rl.Revoke(ctx, "sess-1", time.Now().Add(30*time.Second)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	ttl := mr.TTL("revoked:sess-1")
	if ttl <= 0 || ttl > 31*time.Second {
		t.Fatalf("expected TTL close to 30s, got %v", ttl)
	}

	// Once the marker's TTL elapses, the session is no longer treated as revoked
	// (the access token bound to it has itself expired by then).
	mr.FastForward(31 * time.Second)
	revoked, err := rl.IsRevoked(ctx, "sess-1")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("expected marker to expire with the token TTL")
	}
}

func TestTokenRevocationList_AnyRevoked(t *testing.T) {
	rl, _ := newTestRevocationList(t)
	ctx := context.Background()

	// Neither present.
	got, err := rl.AnyRevoked(ctx, "jti-x", "sess-x")
	if err != nil {
		t.Fatalf("AnyRevoked: %v", err)
	}
	if got {
		t.Fatal("expected false when neither id is revoked")
	}

	// Only the session is revoked (the common logout/refresh case: the access
	// token's own jti is never written, only its sid).
	if err := rl.Revoke(ctx, "sess-x", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, err = rl.AnyRevoked(ctx, "jti-x", "sess-x")
	if err != nil {
		t.Fatalf("AnyRevoked: %v", err)
	}
	if !got {
		t.Fatal("expected true when the session id is revoked")
	}
}

func TestTokenRevocationList_AnyRevoked_NoIDsSkipsRedis(t *testing.T) {
	rl, mr := newTestRevocationList(t)
	ctx := context.Background()

	// With only empty ids there is nothing to check; it must not error and must
	// not touch Redis (so it works even when Redis is down).
	mr.Close()
	got, err := rl.AnyRevoked(ctx, "", "")
	if err != nil {
		t.Fatalf("AnyRevoked with empty ids: %v", err)
	}
	if got {
		t.Fatal("expected false with no ids")
	}
}

func TestTokenRevocationList_AnyRevoked_StoreErrorPropagates(t *testing.T) {
	rl, mr := newTestRevocationList(t)
	ctx := context.Background()

	// Simulate a Redis outage; the store must surface the error so the caller can
	// decide its fail-open/closed policy (the gateway fails open).
	mr.Close()
	if _, err := rl.AnyRevoked(ctx, "jti-1"); err == nil {
		t.Fatal("expected an error when Redis is unreachable")
	}
}

func TestTokenRevocationList_ConcurrentRevokeAndCheck(t *testing.T) {
	rl, _ := newTestRevocationList(t)
	ctx := context.Background()

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("jti-%d", i)
			if err := rl.Revoke(ctx, id, time.Now().Add(time.Minute)); err != nil {
				t.Errorf("Revoke %s: %v", id, err)
				return
			}
			revoked, err := rl.AnyRevoked(ctx, id)
			if err != nil {
				t.Errorf("AnyRevoked %s: %v", id, err)
				return
			}
			if !revoked {
				t.Errorf("expected %s revoked after concurrent Revoke", id)
			}
		}(i)
	}
	wg.Wait()
}
