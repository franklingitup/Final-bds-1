package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeRevoker captures session/token revocations written to the shared store so
// tests can assert the auth service records them on logout and refresh rotation.
type fakeRevoker struct {
	mu      sync.Mutex
	revoked map[string]time.Time // id -> expiresAt
	err     error
	calls   int
}

func newFakeRevoker() *fakeRevoker { return &fakeRevoker{revoked: map[string]time.Time{}} }

func (f *fakeRevoker) Revoke(_ context.Context, tokenID string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.revoked[tokenID] = expiresAt
	return nil
}

func (f *fakeRevoker) isRevoked(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.revoked[id]
	return ok
}

func (f *fakeRevoker) expiryOf(id string) (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	exp, ok := f.revoked[id]
	return exp, ok
}

// newRevokerEnv builds a service wired with a fake revoker, reusing the in-memory
// fakes from service_test.go.
func newRevokerEnv(rev TokenRevoker) (*Service, *fakeSessionStore, time.Time) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	sessions := newFakeSessionStore()
	svc := NewService(Deps{
		Users:         newFakeUserStore(),
		Sessions:      sessions,
		OneTimeTokens: newFakeOTTStore(),
		Tx:            fakeTx{},
		JWT:           NewJWTIssuer(testAuthConfig()),
		Outbox:        &fakeOutbox{},
		Notifier:      newFakeNotifier(),
		Revoker:       rev,
		Auth:          testAuthConfig(),
		Now:           func() time.Time { return now },
	})
	return svc, sessions, now
}

func signupUser(t *testing.T, svc *Service, email string) *TokenPair {
	t.Helper()
	pair, err := svc.Signup(context.Background(), SignupRequest{
		Email: email, Password: "password123", Name: "Test User",
	}, RequestMeta{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	return pair
}

// The access token must carry the sid of the session issued alongside it, so
// revoking that session invalidates the access token at the gateway.
func TestIssuedAccessTokenCarriesSessionID(t *testing.T) {
	svc, sessions, _ := newRevokerEnv(newFakeRevoker())
	pair := signupUser(t, svc, "sid@example.com")

	claims, err := svc.jwt.Verify(pair.AccessToken)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.SID == "" {
		t.Fatal("expected access token to carry a sid claim")
	}
	sess, err := sessions.GetByHash(context.Background(), hashToken(pair.RefreshToken))
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if claims.SID != sess.ID {
		t.Fatalf("sid claim %q does not match session id %q", claims.SID, sess.ID)
	}
}

func TestLogoutRevokesSessionInCache(t *testing.T) {
	rev := newFakeRevoker()
	svc, sessions, now := newRevokerEnv(rev)
	pair := signupUser(t, svc, "logout@example.com")

	sess, _ := sessions.GetByHash(context.Background(), hashToken(pair.RefreshToken))

	if err := svc.Logout(context.Background(), LogoutRequest{RefreshToken: pair.RefreshToken}); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if !rev.isRevoked(sess.ID) {
		t.Fatal("expected session revoked in cache after logout")
	}
	// TTL must match the access-token lifetime.
	exp, _ := rev.expiryOf(sess.ID)
	if want := now.Add(testAuthConfig().AccessTTL); !exp.Equal(want) {
		t.Fatalf("revocation expiry = %v, want %v (now + AccessTTL)", exp, want)
	}
}

func TestRefreshRevokesOldSessionInCache(t *testing.T) {
	rev := newFakeRevoker()
	svc, sessions, _ := newRevokerEnv(rev)
	pair := signupUser(t, svc, "refresh@example.com")

	oldSess, _ := sessions.GetByHash(context.Background(), hashToken(pair.RefreshToken))

	rotated, err := svc.Refresh(context.Background(), RefreshRequest{RefreshToken: pair.RefreshToken}, RequestMeta{})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !rev.isRevoked(oldSess.ID) {
		t.Fatal("expected old session revoked in cache after refresh rotation")
	}
	// The new session (and its access token) must NOT be revoked.
	newSess, _ := sessions.GetByHash(context.Background(), hashToken(rotated.RefreshToken))
	if rev.isRevoked(newSess.ID) {
		t.Fatal("did not expect the freshly issued session to be revoked")
	}
}

// A revocation-store failure is best-effort: logout/refresh must still succeed
// (the durable DB session revocation already happened).
func TestLogoutSucceedsWhenRevokerErrors(t *testing.T) {
	rev := newFakeRevoker()
	rev.err = errors.New("redis down")
	svc, _, _ := newRevokerEnv(rev)
	pair := signupUser(t, svc, "err@example.com")

	if err := svc.Logout(context.Background(), LogoutRequest{RefreshToken: pair.RefreshToken}); err != nil {
		t.Fatalf("logout must not fail on revoker error, got %v", err)
	}
	// The DB session is still revoked, so a subsequent refresh is rejected.
	if _, err := svc.Refresh(context.Background(), RefreshRequest{RefreshToken: pair.RefreshToken}, RequestMeta{}); err != errTokenRevoked {
		t.Fatalf("expected errTokenRevoked after logout, got %v", err)
	}
}

// A nil revoker (no Redis configured) must not break logout/refresh.
func TestLogoutWithoutRevoker(t *testing.T) {
	svc, _, _ := newRevokerEnv(nil)
	pair := signupUser(t, svc, "norevoker@example.com")
	if err := svc.Logout(context.Background(), LogoutRequest{RefreshToken: pair.RefreshToken}); err != nil {
		t.Fatalf("logout: %v", err)
	}
}

func TestConcurrentLogoutsAreSafe(t *testing.T) {
	rev := newFakeRevoker()
	svc, sessions, _ := newRevokerEnv(rev)

	const n = 50
	pairs := make([]*TokenPair, n)
	for i := 0; i < n; i++ {
		pairs[i] = signupUser(t, svc, fmtEmail(i))
	}

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if err := svc.Logout(context.Background(), LogoutRequest{RefreshToken: pairs[i].RefreshToken}); err != nil {
				t.Errorf("logout %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		sess, _ := sessions.GetByHash(context.Background(), hashToken(pairs[i].RefreshToken))
		if !rev.isRevoked(sess.ID) {
			t.Errorf("session %d not revoked in cache", i)
		}
	}
}

func fmtEmail(i int) string {
	return "user" + string(rune('a'+i%26)) + string(rune('0'+i/26)) + "@example.com"
}
