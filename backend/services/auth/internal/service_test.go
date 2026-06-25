package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// ----------------------------------------------------------------------------
// In-memory fakes
// ----------------------------------------------------------------------------

type fakeTx struct{}

func (fakeTx) Tx(ctx context.Context, fn database.TxFunc) error { return fn(ctx) }

// fakeOutbox records enqueued envelopes so tests can assert on emitted events.
type fakeOutbox struct{ records []events.Envelope }

func (o *fakeOutbox) Enqueue(_ context.Context, e events.Envelope) error {
	o.records = append(o.records, e)
	return nil
}
func (o *fakeOutbox) FetchUnpublished(context.Context, int) ([]events.OutboxRecord, error) {
	return nil, nil
}
func (o *fakeOutbox) MarkPublished(context.Context, []string) error { return nil }

func (o *fakeOutbox) find(eventType string) (events.Envelope, bool) {
	for _, e := range o.records {
		if e.Type == eventType {
			return e, true
		}
	}
	return events.Envelope{}, false
}

// fakeNotifier captures the plaintext one-time tokens handed to the secure
// delivery channel, standing in for the Notification Service.
type fakeNotifier struct{ byPurpose map[string]TokenDelivery }

func newFakeNotifier() *fakeNotifier { return &fakeNotifier{byPurpose: map[string]TokenDelivery{}} }

func (n *fakeNotifier) DeliverToken(_ context.Context, d TokenDelivery) {
	n.byPurpose[d.Purpose] = d
}

type fakeUserStore struct {
	byID    map[string]*User
	byEmail map[string]*User
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{byID: map[string]*User{}, byEmail: map[string]*User{}}
}

func (f *fakeUserStore) Create(_ context.Context, u *User) error {
	if _, ok := f.byEmail[u.Email]; ok {
		return apperrors.Conflict("email exists")
	}
	u.ID = uuid.NewString()
	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	u.Version = 1
	f.byID[u.ID] = u
	f.byEmail[u.Email] = u
	return nil
}

func (f *fakeUserStore) GetByID(_ context.Context, id string) (*User, error) {
	if u, ok := f.byID[id]; ok {
		return u, nil
	}
	return nil, apperrors.NotFound("user")
}

func (f *fakeUserStore) GetByEmail(_ context.Context, email string) (*User, error) {
	if u, ok := f.byEmail[email]; ok {
		return u, nil
	}
	return nil, apperrors.NotFound("user")
}

func (f *fakeUserStore) Update(_ context.Context, u *User) error {
	if _, ok := f.byID[u.ID]; !ok {
		return apperrors.NotFound("user")
	}
	u.Version++
	f.byID[u.ID] = u
	f.byEmail[u.Email] = u
	return nil
}

type fakeSessionStore struct {
	byHash map[string]*RefreshToken
	byID   map[string]*RefreshToken
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{byHash: map[string]*RefreshToken{}, byID: map[string]*RefreshToken{}}
}

func (f *fakeSessionStore) Create(_ context.Context, t *RefreshToken) error {
	t.ID = uuid.NewString()
	t.CreatedAt = time.Now()
	f.byHash[t.TokenHash] = t
	f.byID[t.ID] = t
	return nil
}

func (f *fakeSessionStore) GetByHash(_ context.Context, hash string) (*RefreshToken, error) {
	if t, ok := f.byHash[hash]; ok {
		return t, nil
	}
	return nil, apperrors.NotFound("session")
}

func (f *fakeSessionStore) Revoke(_ context.Context, id string, replacedBy *string) error {
	if t, ok := f.byID[id]; ok && t.RevokedAt == nil {
		now := time.Now()
		t.RevokedAt = &now
		t.ReplacedBy = replacedBy
	}
	return nil
}

func (f *fakeSessionStore) RevokeAllForUser(_ context.Context, userID string) error {
	now := time.Now()
	for _, t := range f.byID {
		if t.UserID == userID && t.RevokedAt == nil {
			t.RevokedAt = &now
		}
	}
	return nil
}

type fakeOTTStore struct {
	byHash map[string]*OneTimeToken
	byID   map[string]*OneTimeToken
}

func newFakeOTTStore() *fakeOTTStore {
	return &fakeOTTStore{byHash: map[string]*OneTimeToken{}, byID: map[string]*OneTimeToken{}}
}

func (f *fakeOTTStore) Create(_ context.Context, t *OneTimeToken) error {
	t.ID = uuid.NewString()
	t.CreatedAt = time.Now()
	f.byHash[t.TokenHash] = t
	f.byID[t.ID] = t
	return nil
}

func (f *fakeOTTStore) GetByHash(_ context.Context, hash string) (*OneTimeToken, error) {
	if t, ok := f.byHash[hash]; ok {
		return t, nil
	}
	return nil, apperrors.NotFound("token")
}

func (f *fakeOTTStore) MarkUsed(_ context.Context, id string) error {
	if t, ok := f.byID[id]; ok && t.UsedAt == nil {
		now := time.Now()
		t.UsedAt = &now
	}
	return nil
}

func (f *fakeOTTStore) InvalidateForUser(_ context.Context, userID, purpose string) error {
	now := time.Now()
	for _, t := range f.byID {
		if t.UserID == userID && t.Purpose == purpose && t.UsedAt == nil {
			t.UsedAt = &now
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// Test harness
// ----------------------------------------------------------------------------

type testEnv struct {
	svc      *Service
	users    *fakeUserStore
	sessions *fakeSessionStore
	otts     *fakeOTTStore
	outbox   *fakeOutbox
	notifier *fakeNotifier
	issuer   *JWTIssuer
	now      time.Time
}

func newTestEnv() *testEnv {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	users := newFakeUserStore()
	sessions := newFakeSessionStore()
	otts := newFakeOTTStore()
	outbox := &fakeOutbox{}
	notifier := newFakeNotifier()
	issuer := NewJWTIssuer(testAuthConfig())

	svc := NewService(Deps{
		Users:         users,
		Sessions:      sessions,
		OneTimeTokens: otts,
		Tx:            fakeTx{},
		JWT:           issuer,
		Outbox:        outbox,
		Notifier:      notifier,
		Auth:          testAuthConfig(),
		Now:           func() time.Time { return now },
	})
	return &testEnv{svc: svc, users: users, sessions: sessions, otts: otts, outbox: outbox, notifier: notifier, issuer: issuer, now: now}
}

// tokenForPurpose returns the plaintext token delivered out-of-band for a
// purpose. Tokens never appear on the event stream, only via the secure
// delivery channel captured by fakeNotifier.
func (e *testEnv) tokenForPurpose(t *testing.T, purpose string) string {
	t.Helper()
	d, ok := e.notifier.byPurpose[purpose]
	if !ok || d.Token == "" {
		t.Fatalf("no delivered token for purpose %q", purpose)
	}
	return d.Token
}

func mustSignup(t *testing.T, e *testEnv, email string) *TokenPair {
	t.Helper()
	pair, err := e.svc.Signup(context.Background(), SignupRequest{
		Email: email, Password: "password123", Name: "Test User",
	}, RequestMeta{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	return pair
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestSignupAndVerifyEmail(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()

	pair := mustSignup(t, e, "user@example.com")
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected token pair")
	}
	if pair.User == nil || pair.User.Email != "user@example.com" {
		t.Fatalf("unexpected user profile: %+v", pair.User)
	}

	if _, ok := e.outbox.find(EventUserCreated); !ok {
		t.Error("expected auth.user.created event in outbox")
	}
	if _, ok := e.outbox.find(EventEmailVerificationRequested); !ok {
		t.Error("expected email verification requested event in outbox")
	}

	token := e.tokenForPurpose(t, PurposeEmailVerification)
	if err := e.svc.VerifyEmail(ctx, token); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	if u := e.users.byEmail["user@example.com"]; !u.EmailVerified {
		t.Error("expected email to be verified")
	}
	if _, ok := e.outbox.find(EventEmailVerified); !ok {
		t.Error("expected auth.user.email.verified event in outbox")
	}
}

func TestSignup_DuplicateEmail(t *testing.T) {
	e := newTestEnv()
	mustSignup(t, e, "dup@example.com")
	_, err := e.svc.Signup(context.Background(), SignupRequest{
		Email: "dup@example.com", Password: "password123", Name: "Again",
	}, RequestMeta{})
	if err != errEmailTaken {
		t.Errorf("expected errEmailTaken, got %v", err)
	}
}

func TestSignup_Validation(t *testing.T) {
	e := newTestEnv()
	if _, err := e.svc.Signup(context.Background(), SignupRequest{Email: "bad", Password: "password123", Name: "x"}, RequestMeta{}); err == nil {
		t.Error("expected invalid email error")
	}
	if _, err := e.svc.Signup(context.Background(), SignupRequest{Email: "ok@example.com", Password: "short", Name: "x"}, RequestMeta{}); err == nil {
		t.Error("expected weak password error")
	}
}

func TestLoginSuccess(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()
	mustSignup(t, e, "login@example.com")

	pair, err := e.svc.Login(ctx, LoginRequest{Email: "login@example.com", Password: "password123"}, RequestMeta{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	claims, err := e.issuer.Verify(pair.AccessToken)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if claims.Email != "login@example.com" {
		t.Errorf("claims email = %s", claims.Email)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	e := newTestEnv()
	mustSignup(t, e, "wrong@example.com")
	_, err := e.svc.Login(context.Background(), LoginRequest{Email: "wrong@example.com", Password: "nope"}, RequestMeta{})
	if err != errInvalidCredentials {
		t.Errorf("expected errInvalidCredentials, got %v", err)
	}
	if e.users.byEmail["wrong@example.com"].FailedLoginAttempts != 1 {
		t.Error("expected failed attempt to be recorded")
	}
}

func TestLogin_Lockout(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()
	mustSignup(t, e, "lock@example.com")

	for i := 0; i < maxFailedLogins; i++ {
		_, _ = e.svc.Login(ctx, LoginRequest{Email: "lock@example.com", Password: "bad"}, RequestMeta{})
	}
	// Correct password is now rejected because the account is locked.
	_, err := e.svc.Login(ctx, LoginRequest{Email: "lock@example.com", Password: "password123"}, RequestMeta{})
	if err != errAccountLocked {
		t.Errorf("expected errAccountLocked, got %v", err)
	}
}

func TestLogin_MFA(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()
	mustSignup(t, e, "mfa@example.com")
	user := e.users.byEmail["mfa@example.com"]

	setup, err := e.svc.SetupMFA(ctx, user.ID)
	if err != nil {
		t.Fatalf("setup mfa: %v", err)
	}
	if _, ok := e.outbox.find(EventMFASetupStarted); !ok {
		t.Error("expected auth.mfa.setup.started event in outbox")
	}
	code, _ := totpCodeAt(setup.Secret, e.now)
	if err := e.svc.EnableMFA(ctx, user.ID, code); err != nil {
		t.Fatalf("enable mfa: %v", err)
	}
	if _, ok := e.outbox.find(EventMFAEnabled); !ok {
		t.Error("expected auth.mfa.enabled event in outbox")
	}

	// Login without a code is rejected.
	if _, err := e.svc.Login(ctx, LoginRequest{Email: "mfa@example.com", Password: "password123"}, RequestMeta{}); err != errMFARequired {
		t.Errorf("expected errMFARequired, got %v", err)
	}
	// Login with a valid code succeeds.
	code, _ = totpCodeAt(setup.Secret, e.now)
	if _, err := e.svc.Login(ctx, LoginRequest{Email: "mfa@example.com", Password: "password123", MFACode: code}, RequestMeta{}); err != nil {
		t.Errorf("expected MFA login to succeed, got %v", err)
	}
}

func TestDisableMFAEmitsEvent(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()
	mustSignup(t, e, "mfa-off@example.com")
	user := e.users.byEmail["mfa-off@example.com"]

	setup, err := e.svc.SetupMFA(ctx, user.ID)
	if err != nil {
		t.Fatalf("setup mfa: %v", err)
	}
	code, _ := totpCodeAt(setup.Secret, e.now)
	if err := e.svc.EnableMFA(ctx, user.ID, code); err != nil {
		t.Fatalf("enable mfa: %v", err)
	}
	code, _ = totpCodeAt(setup.Secret, e.now)
	if err := e.svc.DisableMFA(ctx, user.ID, code); err != nil {
		t.Fatalf("disable mfa: %v", err)
	}
	if _, ok := e.outbox.find(EventMFADisabled); !ok {
		t.Error("expected auth.mfa.disabled event in outbox")
	}
	if user.MFAEnabled || user.MFASecret != nil {
		t.Error("expected MFA to be disabled and secret cleared")
	}
}

func TestRefreshRotation(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()
	pair := mustSignup(t, e, "refresh@example.com")

	rotated, err := e.svc.Refresh(ctx, RefreshRequest{RefreshToken: pair.RefreshToken}, RequestMeta{})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if rotated.RefreshToken == pair.RefreshToken {
		t.Error("expected a new refresh token after rotation")
	}
	if _, ok := e.outbox.find(EventTokenRotated); !ok {
		t.Error("expected auth.token.rotated event in outbox")
	}
	// The old refresh token is now revoked.
	if _, err := e.svc.Refresh(ctx, RefreshRequest{RefreshToken: pair.RefreshToken}, RequestMeta{}); err != errTokenRevoked {
		t.Errorf("expected errTokenRevoked for old token, got %v", err)
	}
}

func TestLogout(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()
	pair := mustSignup(t, e, "logout@example.com")

	if err := e.svc.Logout(ctx, LogoutRequest{RefreshToken: pair.RefreshToken}); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := e.svc.Refresh(ctx, RefreshRequest{RefreshToken: pair.RefreshToken}, RequestMeta{}); err != errTokenRevoked {
		t.Errorf("expected revoked token after logout, got %v", err)
	}
}

func TestPasswordResetFlow(t *testing.T) {
	e := newTestEnv()
	ctx := context.Background()
	mustSignup(t, e, "reset@example.com")

	if err := e.svc.RequestPasswordReset(ctx, "reset@example.com"); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	token := e.tokenForPurpose(t, PurposePasswordResetMsg)

	if err := e.svc.ConfirmPasswordReset(ctx, token, "new-password-456"); err != nil {
		t.Fatalf("confirm reset: %v", err)
	}

	// Old password no longer works; new password does.
	if _, err := e.svc.Login(ctx, LoginRequest{Email: "reset@example.com", Password: "password123"}, RequestMeta{}); err != errInvalidCredentials {
		t.Errorf("expected old password to fail, got %v", err)
	}
	if _, err := e.svc.Login(ctx, LoginRequest{Email: "reset@example.com", Password: "new-password-456"}, RequestMeta{}); err != nil {
		t.Errorf("expected new password to work, got %v", err)
	}
}

func TestRequestPasswordReset_UnknownEmailIsNoop(t *testing.T) {
	e := newTestEnv()
	if err := e.svc.RequestPasswordReset(context.Background(), "nobody@example.com"); err != nil {
		t.Errorf("unknown email should not error, got %v", err)
	}
}
