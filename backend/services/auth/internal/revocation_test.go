package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
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

// ----------------------------------------------------------------------------
// API Token Revocation Tests
// ----------------------------------------------------------------------------

// fakeAPITokenStore implements APITokenStore for testing API token revocation.
type fakeAPITokenStore struct {
	tokens map[string]*APIToken
}

func newFakeAPITokenStore() *fakeAPITokenStore {
	return &fakeAPITokenStore{tokens: map[string]*APIToken{}}
}

func (f *fakeAPITokenStore) Create(_ context.Context, t *APIToken) error {
	t.ID = "tok-" + t.TokenHash[:8]
	t.CreatedAt = time.Now()
	f.tokens[t.ID] = t
	return nil
}

func (f *fakeAPITokenStore) ListByOrg(_ context.Context, req database.PageRequest) (database.Page[APIToken], error) {
	var items []APIToken
	for _, t := range f.tokens {
		items = append(items, *t)
	}
	return database.Page[APIToken]{Items: items}, nil
}

func (f *fakeAPITokenStore) Revoke(_ context.Context, id string) (*APIToken, error) {
	t, ok := f.tokens[id]
	if !ok || t.RevokedAt != nil {
		return nil, errors.New("api token not found")
	}
	now := time.Now()
	t.RevokedAt = &now
	return t, nil
}

// fakeServiceAccountStore implements ServiceAccountStore for testing.
type fakeServiceAccountStore struct {
	accounts map[string]*ServiceAccount
}

func newFakeServiceAccountStore() *fakeServiceAccountStore {
	return &fakeServiceAccountStore{accounts: map[string]*ServiceAccount{}}
}

func (f *fakeServiceAccountStore) Create(_ context.Context, sa *ServiceAccount) error {
	sa.ID = "sa-" + sa.Name[:8]
	sa.CreatedAt = time.Now()
	f.accounts[sa.ID] = sa
	return nil
}

func (f *fakeServiceAccountStore) GetByID(_ context.Context, id string) (*ServiceAccount, error) {
	if sa, ok := f.accounts[id]; ok {
		return sa, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeServiceAccountStore) List(_ context.Context, req database.PageRequest) (database.Page[ServiceAccount], error) {
	var items []ServiceAccount
	for _, sa := range f.accounts {
		items = append(items, *sa)
	}
	return database.Page[ServiceAccount]{Items: items}, nil
}

func (f *fakeServiceAccountStore) Delete(_ context.Context, id string) error {
	delete(f.accounts, id)
	return nil
}

// fakeTenantRunnerForTokens implements TenantRunner for token tests.
type fakeTenantRunnerForTokens struct{}

func (f fakeTenantRunnerForTokens) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	return fn(ctx)
}

// newAPITokenRevokerEnv builds a service wired with a fake revoker and API token stores.
func newAPITokenRevokerEnv(rev TokenRevoker) (*Service, *fakeAPITokenStore, *fakeRevoker, time.Time) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	apiTokens := newFakeAPITokenStore()
	serviceAccounts := newFakeServiceAccountStore()

	// Seed a service account so we can create tokens.
	serviceAccounts.accounts["sa-test1234"] = &ServiceAccount{
		TenantModel: database.TenantModel{Model: database.Model{ID: "sa-test1234"}},
		Name:        "test-sa",
		Status:      "active",
	}

	var fakeRev *fakeRevoker
	if rev != nil {
		fakeRev = rev.(*fakeRevoker)
	}

	svc := NewService(Deps{
		Users:           newFakeUserStore(),
		Sessions:        newFakeSessionStore(),
		OneTimeTokens:   newFakeOTTStore(),
		ServiceAccounts: serviceAccounts,
		APITokens:       apiTokens,
		Tx:              fakeTx{},
		Tenant:          fakeTenantRunnerForTokens{},
		JWT:             NewJWTIssuer(testAuthConfig()),
		Outbox:          &fakeOutbox{},
		Notifier:        newFakeNotifier(),
		Revoker:         rev,
		Auth:            testAuthConfig(),
		Now:             func() time.Time { return now },
	})
	return svc, apiTokens, fakeRev, now
}

func TestRevokeAPITokenPushesToCache(t *testing.T) {
	rev := newFakeRevoker()
	svc, apiTokens, _, now := newAPITokenRevokerEnv(rev)
	ctx := context.Background()
	orgID := "org-test"

	// Create an API token manually in the fake store with a known JTI.
	jti := "jti-" + time.Now().Format("20060102150405")
	expiry := now.Add(24 * time.Hour)
	token := &APIToken{
		TenantModel:      database.TenantModel{Model: database.Model{ID: "tok-testrev"}},
		ServiceAccountID: "sa-test1234",
		Name:             "test-token",
		Prefix:           "jwt_abc",
		TokenHash:        jti, // JTI is stored in TokenHash
		Scopes:           []string{"read"},
		ExpiresAt:        &expiry,
	}
	token.OrgID = orgID
	apiTokens.tokens["tok-testrev"] = token

	// Revoke the token.
	if err := svc.RevokeAPIToken(ctx, orgID, "user-123", "tok-testrev"); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}

	// Verify the JTI was pushed to the revoker.
	if !rev.isRevoked(jti) {
		t.Fatal("expected API token JTI to be revoked in cache")
	}

	// Verify the expiry matches the token's expiry.
	cachedExpiry, ok := rev.expiryOf(jti)
	if !ok {
		t.Fatal("expected expiry to be recorded")
	}
	if !cachedExpiry.Equal(expiry) {
		t.Errorf("cached expiry = %v, want %v", cachedExpiry, expiry)
	}
}

func TestRevokeAPITokenWithoutExpiryUsesLongFallback(t *testing.T) {
	rev := newFakeRevoker()
	svc, apiTokens, _, now := newAPITokenRevokerEnv(rev)
	ctx := context.Background()
	orgID := "org-test"

	// Create an API token without an expiry (non-expiring token).
	jti := "jti-noexp-" + time.Now().Format("20060102150405")
	token := &APIToken{
		TenantModel:      database.TenantModel{Model: database.Model{ID: "tok-noexp"}},
		ServiceAccountID: "sa-test1234",
		Name:             "non-expiring-token",
		Prefix:           "jwt_noexp",
		TokenHash:        jti,
		Scopes:           []string{"read"},
		ExpiresAt:        nil, // No expiry
	}
	token.OrgID = orgID
	apiTokens.tokens["tok-noexp"] = token

	// Revoke the token.
	if err := svc.RevokeAPIToken(ctx, orgID, "user-123", "tok-noexp"); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}

	// Verify the JTI was pushed to the revoker with a 1-year fallback.
	cachedExpiry, ok := rev.expiryOf(jti)
	if !ok {
		t.Fatal("expected JTI to be revoked in cache")
	}
	expectedFallback := now.Add(24 * time.Hour * 365)
	if !cachedExpiry.Equal(expectedFallback) {
		t.Errorf("cached expiry = %v, want 1-year fallback %v", cachedExpiry, expectedFallback)
	}
}

func TestRevokeAPITokenSucceedsWhenRevokerErrors(t *testing.T) {
	rev := newFakeRevoker()
	rev.err = errors.New("redis down")
	svc, apiTokens, _, _ := newAPITokenRevokerEnv(rev)
	ctx := context.Background()
	orgID := "org-test"

	jti := "jti-err-test"
	token := &APIToken{
		TenantModel:      database.TenantModel{Model: database.Model{ID: "tok-err"}},
		ServiceAccountID: "sa-test1234",
		Name:             "error-test-token",
		Prefix:           "jwt_err",
		TokenHash:        jti,
		Scopes:           []string{"read"},
	}
	token.OrgID = orgID
	apiTokens.tokens["tok-err"] = token

	// RevokeAPIToken should succeed even when the revoker fails (best-effort).
	if err := svc.RevokeAPIToken(ctx, orgID, "user-123", "tok-err"); err != nil {
		t.Fatalf("RevokeAPIToken must not fail on revoker error, got %v", err)
	}

	// The token should still be marked as revoked in the database.
	if token.RevokedAt == nil {
		t.Error("expected token to be marked revoked in database")
	}
}

func TestRevokeAPITokenWithoutRevoker(t *testing.T) {
	svc, apiTokens, _, _ := newAPITokenRevokerEnv(nil)
	ctx := context.Background()
	orgID := "org-test"

	jti := "jti-nil-revoker"
	token := &APIToken{
		TenantModel:      database.TenantModel{Model: database.Model{ID: "tok-nil"}},
		ServiceAccountID: "sa-test1234",
		Name:             "nil-revoker-token",
		Prefix:           "jwt_nil",
		TokenHash:        jti,
		Scopes:           []string{"read"},
	}
	token.OrgID = orgID
	apiTokens.tokens["tok-nil"] = token

	// RevokeAPIToken should succeed when no revoker is configured.
	if err := svc.RevokeAPIToken(ctx, orgID, "user-123", "tok-nil"); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}

	// The token should still be marked as revoked in the database.
	if token.RevokedAt == nil {
		t.Error("expected token to be marked revoked in database")
	}
}
