package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"github.com/bdsplatform/platform/backend/libs/config"
	libmw "github.com/bdsplatform/platform/backend/libs/middleware"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/auth"
)

const revTestSigningKey = "revocation-test-key-please-change"

// fakeRevocationStore is a test seam for revocationStore. It records the ids it
// was asked about and returns a configurable set of revoked ids or an error.
type fakeRevocationStore struct {
	mu      sync.Mutex
	revoked map[string]bool
	err     error
	calls   int
	lastIDs []string
}

func newFakeRevocationStore() *fakeRevocationStore {
	return &fakeRevocationStore{revoked: map[string]bool{}}
}

func (f *fakeRevocationStore) AnyRevoked(_ context.Context, ids ...string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastIDs = append([]string(nil), ids...)
	if f.err != nil {
		return false, f.err
	}
	for _, id := range ids {
		if id != "" && f.revoked[id] {
			return true, nil
		}
	}
	return false, nil
}

func signRevTokenWithClaims(claims jwt.Claims) string {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := tok.SignedString([]byte(revTestSigningKey))
	return signed
}

func signUserAccessToken(userID, jti, sid string) string {
	return signRevTokenWithClaims(jwt.MapClaims{
		"sub":   userID,
		"email": userID + "@example.com",
		"iss":   "bdsplatform-auth",
		"jti":   jti,
		"sid":   sid,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(15 * time.Minute).Unix(),
	})
}

func signServiceAccountToken(saID, jti, orgID string) string {
	return signRevTokenWithClaims(jwt.MapClaims{
		"sub":    saID,
		"org_id": orgID,
		"iss":    "bdsplatform-auth",
		"jti":    jti,
		"iat":    time.Now().Unix(),
		"exp":    time.Now().Add(time.Hour).Unix(),
	})
}

// --- RevocationChecker unit tests --------------------------------------------

func TestRevocationChecker_NotRevoked(t *testing.T) {
	store := newFakeRevocationStore()
	rc := NewRevocationChecker(store, nil)

	id := auth.Identity{UserID: "u1", JTI: "jti-1", SessionID: "sess-1"}
	if rc.Revoked(context.Background(), "req-1", id) {
		t.Fatal("expected not revoked")
	}
	// It must check both the jti and the session id.
	if len(store.lastIDs) != 2 || store.lastIDs[0] != "jti-1" || store.lastIDs[1] != "sess-1" {
		t.Fatalf("expected [jti-1 sess-1] checked, got %v", store.lastIDs)
	}
}

func TestRevocationChecker_RevokedBySession(t *testing.T) {
	store := newFakeRevocationStore()
	store.revoked["sess-1"] = true // logout/refresh revokes the session, not the jti
	rc := NewRevocationChecker(store, nil)

	id := auth.Identity{UserID: "u1", JTI: "jti-1", SessionID: "sess-1"}
	if !rc.Revoked(context.Background(), "req-1", id) {
		t.Fatal("expected revoked when session is revoked")
	}
}

func TestRevocationChecker_RevokedByJTI(t *testing.T) {
	store := newFakeRevocationStore()
	store.revoked["jti-1"] = true
	rc := NewRevocationChecker(store, nil)

	id := auth.Identity{UserID: "u1", JTI: "jti-1", SessionID: "sess-1"}
	if !rc.Revoked(context.Background(), "req-1", id) {
		t.Fatal("expected revoked when jti is revoked")
	}
}

func TestRevocationChecker_StoreErrorFailsOpen(t *testing.T) {
	store := newFakeRevocationStore()
	store.err = errors.New("redis down")
	rc := NewRevocationChecker(store, nil)

	id := auth.Identity{UserID: "u1", JTI: "jti-1", SessionID: "sess-1"}
	if rc.Revoked(context.Background(), "req-1", id) {
		t.Fatal("expected fail-open (not revoked) on store error")
	}
}

func TestRevocationChecker_ServiceAccountChecksJTIOnly(t *testing.T) {
	store := newFakeRevocationStore()
	rc := NewRevocationChecker(store, nil)

	// Service account: no session id. Empty ids are dropped by the store, so only
	// the jti is meaningfully checked, and it is not revoked by default.
	id := auth.Identity{Type: auth.TokenTypeServiceAccount, UserID: "sa1", JTI: "sa-jti-1"}
	if rc.Revoked(context.Background(), "req-1", id) {
		t.Fatal("service account token must remain valid unless explicitly revoked")
	}

	store.revoked["sa-jti-1"] = true
	if !rc.Revoked(context.Background(), "req-1", id) {
		t.Fatal("explicit jti revocation must apply to service account tokens")
	}
}

func TestNewRevocationChecker_NilStorePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil store")
		}
	}()
	NewRevocationChecker(nil, nil)
}

func TestRevocationChecker_Concurrent(t *testing.T) {
	store := newFakeRevocationStore()
	store.revoked["sess-revoked"] = true
	rc := NewRevocationChecker(store, nil)

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			// Half the callers present a revoked session, half a live one.
			sid := "sess-live"
			want := false
			if i%2 == 0 {
				sid = "sess-revoked"
				want = true
			}
			id := auth.Identity{UserID: "u", JTI: "jti", SessionID: sid}
			if got := rc.Revoked(context.Background(), "req", id); got != want {
				t.Errorf("i=%d sid=%s: got %v want %v", i, sid, got, want)
			}
		}(i)
	}
	wg.Wait()
}

// --- Gateway middleware integration tests ------------------------------------

func newAuthTestApp(revoked map[string]bool, storeErr error) *fiber.App {
	validator := auth.NewValidator(config.AuthConfig{JWTSigningKey: revTestSigningKey})
	store := newFakeRevocationStore()
	store.err = storeErr
	for k, v := range revoked {
		store.revoked[k] = v
	}
	rc := NewRevocationChecker(store, nil)

	app := fiber.New(fiber.Config{ErrorHandler: libmw.ErrorHandler()})
	app.Use(RequestID())
	app.Use(Authentication(validator, rc))
	app.Get("/protected", func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}

func doGet(t *testing.T, app *fiber.App, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestGatewayAuth_ValidTokenPasses(t *testing.T) {
	app := newAuthTestApp(nil, nil)
	token := signUserAccessToken("u1", "jti-1", "sess-1")
	if got := doGet(t, app, token); got != http.StatusOK {
		t.Fatalf("expected 200 for a valid, non-revoked token, got %d", got)
	}
}

func TestGatewayAuth_RevokedSessionRejected(t *testing.T) {
	app := newAuthTestApp(map[string]bool{"sess-1": true}, nil)
	token := signUserAccessToken("u1", "jti-1", "sess-1")
	if got := doGet(t, app, token); got != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a revoked session, got %d", got)
	}
}

func TestGatewayAuth_RevokedJTIRejected(t *testing.T) {
	app := newAuthTestApp(map[string]bool{"jti-1": true}, nil)
	token := signUserAccessToken("u1", "jti-1", "sess-1")
	if got := doGet(t, app, token); got != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a revoked jti, got %d", got)
	}
}

func TestGatewayAuth_StoreErrorFailsOpen(t *testing.T) {
	app := newAuthTestApp(map[string]bool{"sess-1": true}, errors.New("redis down"))
	token := signUserAccessToken("u1", "jti-1", "sess-1")
	// Even though sess-1 is "revoked", the store errors, so the gateway fails open.
	if got := doGet(t, app, token); got != http.StatusOK {
		t.Fatalf("expected 200 (fail-open) on store error, got %d", got)
	}
}

func TestGatewayAuth_InvalidSignatureRejectedBeforeRevocation(t *testing.T) {
	app := newAuthTestApp(nil, nil)
	// Signed with a different key: must be rejected by signature validation.
	bad := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "u1", "iss": "bdsplatform-auth",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, _ := bad.SignedString([]byte("some-other-key"))
	if got := doGet(t, app, signed); got != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad signature, got %d", got)
	}
}

func TestGatewayAuth_ServiceAccountUnaffected(t *testing.T) {
	app := newAuthTestApp(nil, nil)
	token := signServiceAccountToken("sa1", "sa-jti-1", "org-1")
	if got := doGet(t, app, token); got != http.StatusOK {
		t.Fatalf("expected 200 for a non-revoked service account token, got %d", got)
	}
}
