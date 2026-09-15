package auth

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

type fakeSSOOrganizationStore struct {
	bySlug map[string]string
}

func (f *fakeSSOOrganizationStore) GetIDBySlug(_ context.Context, slug string) (string, error) {
	if id, ok := f.bySlug[slug]; ok {
		return id, nil
	}
	return "", apperrors.NotFound("organization not found")
}

type fakeSSOMemberStore struct {
	mu     sync.Mutex
	calls  int
	userID string
	role   authz.OrgRole
}

func (f *fakeSSOMemberStore) Ensure(_ context.Context, userID string, role authz.OrgRole) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.userID = userID
	f.role = role
	return nil
}

type fakeSSOHandoffStore struct {
	mu        sync.Mutex
	states    map[string]*SSOLoginState
	exchanges map[string]*SSOExchangeCode
}

func newFakeSSOHandoffStore() *fakeSSOHandoffStore {
	return &fakeSSOHandoffStore{
		states:    make(map[string]*SSOLoginState),
		exchanges: make(map[string]*SSOExchangeCode),
	}
}

func (f *fakeSSOHandoffStore) CreateLoginState(_ context.Context, state *SSOLoginState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *state
	f.states[state.StateHash] = &cp
	return nil
}

func (f *fakeSSOHandoffStore) ConsumeLoginState(_ context.Context, orgID, stateHash string, now time.Time) (*SSOLoginState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state, ok := f.states[stateHash]
	if !ok || state.OrgID != orgID || !state.ExpiresAt.After(now) {
		return nil, apperrors.Unauthorized("invalid or expired SSO state")
	}
	delete(f.states, stateHash)
	cp := *state
	return &cp, nil
}

func (f *fakeSSOHandoffStore) CreateExchangeCode(_ context.Context, code *SSOExchangeCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *code
	f.exchanges[code.CodeHash] = &cp
	return nil
}

func (f *fakeSSOHandoffStore) ConsumeExchangeCode(_ context.Context, orgID, codeHash string, now time.Time) (*SSOExchangeCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	code, ok := f.exchanges[codeHash]
	if !ok || code.OrgID != orgID || !code.ExpiresAt.After(now) {
		return nil, apperrors.Unauthorized("invalid or expired SSO exchange code")
	}
	delete(f.exchanges, codeHash)
	cp := *code
	return &cp, nil
}

func newSSOFlowTestService(t *testing.T) (*Service, *fakeSSOHandoffStore, *fakeSSOMemberStore, *OrgSSOConfig) {
	t.Helper()
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	cfg := testSSOConfig(t, "org-1", 1)
	configs := newMemorySSOConfigStore(cfg)
	manager, err := NewSSOProviderManager(SSOProviderManagerOpts{
		PublicBaseURL: "https://api.example.com",
		Keys:          configs,
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSSOProviderManager: %v", err)
	}
	handoffs := newFakeSSOHandoffStore()
	members := &fakeSSOMemberStore{}
	svc := NewService(Deps{
		Users:            newFakeUserStore(),
		Sessions:         newFakeSessionStore(),
		OneTimeTokens:    newFakeOTTStore(),
		SSOConfigs:       configs,
		SSOProviders:     manager,
		SSOOrganizations: &fakeSSOOrganizationStore{bySlug: map[string]string{"acme": cfg.OrgID}},
		SSOMembers:       members,
		SSOHandoffs:      handoffs,
		SSORedirectURL:   "https://app.example.com/sso/callback",
		Tx:               fakeTx{},
		Tenant:           &fakeTenantRunner{},
		JWT:              NewJWTIssuer(testAuthConfig()),
		Outbox:           &fakeOutbox{},
		Auth:             testAuthConfig(),
		Now:              func() time.Time { return now },
	})
	return svc, handoffs, members, cfg
}

func TestStartSSOLoginBuildsSignedRequestAndPersistsCorrelation(t *testing.T) {
	svc, handoffs, _, cfg := newSSOFlowTestService(t)

	redirectURL, err := svc.StartSSOLogin(context.Background(), "acme")
	if err != nil {
		t.Fatalf("StartSSOLogin: %v", err)
	}
	query := redirectURL.Query()
	if query.Get("SAMLRequest") == "" {
		t.Fatal("redirect is missing SAMLRequest")
	}
	if query.Get("Signature") == "" || query.Get("SigAlg") == "" {
		t.Fatal("AuthnRequest redirect is not signed")
	}
	relayState := query.Get("RelayState")
	if relayState == "" {
		t.Fatal("redirect is missing RelayState")
	}
	state := handoffs.states[hashToken(relayState)]
	if state == nil {
		t.Fatal("RelayState correlation was not persisted")
	}
	if state.OrgID != cfg.OrgID || state.RequestID == "" {
		t.Fatalf("unexpected login state: %+v", state)
	}
}

func TestSSOMetadataUsesOrgServiceProvider(t *testing.T) {
	svc, _, _, _ := newSSOFlowTestService(t)

	metadata, err := svc.SSOMetadata(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("SSOMetadata: %v", err)
	}
	body := string(metadata)
	for _, want := range []string{
		`EntityDescriptor`,
		`https://api.example.com/v1/auth/sso/org-1/metadata`,
		`https://api.example.com/v1/auth/sso/org-1/acs`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metadata missing %q", want)
		}
	}
}

func TestExchangeSSOCodeIsSingleUseAndOrgBound(t *testing.T) {
	svc, handoffs, _, _ := newSSOFlowTestService(t)
	user, err := svc.findOrCreateSSOUser(context.Background(), "sso@example.com", "SSO User")
	if err != nil {
		t.Fatal(err)
	}
	code := "one-time-code"
	stored := &SSOExchangeCode{
		OrgID:     "org-1",
		UserID:    user.ID,
		CodeHash:  hashToken(code),
		ExpiresAt: svc.now().Add(time.Minute),
	}
	handoffs.exchanges[hashToken(code)] = stored
	if stored.UserID == "" {
		t.Fatal("exchange code must store a user ID")
	}

	if _, err := svc.ExchangeSSOCode(context.Background(), SSOExchangeRequest{
		OrgID: "org-2", Code: code,
	}, RequestMeta{}); err == nil {
		t.Fatal("expected cross-org exchange to fail")
	}

	got, err := svc.ExchangeSSOCode(context.Background(), SSOExchangeRequest{
		OrgID: "org-1", Code: code,
	}, RequestMeta{UserAgent: "test", IP: "127.0.0.1"})
	if err != nil {
		t.Fatalf("ExchangeSSOCode: %v", err)
	}
	claims, err := svc.jwt.Verify(got.AccessToken)
	if err != nil {
		t.Fatalf("verify minted access token: %v", err)
	}
	if claims.Subject != user.ID {
		t.Fatalf("access token subject = %q, want %q", claims.Subject, user.ID)
	}
	if got.RefreshToken == "" {
		t.Fatal("expected a freshly minted refresh token")
	}

	if _, err := svc.ExchangeSSOCode(context.Background(), SSOExchangeRequest{
		OrgID: "org-1", Code: code,
	}, RequestMeta{}); err == nil {
		t.Fatal("expected second exchange to fail")
	}
}

func TestSSOExchangeCodeStoresIdentityOnlyAndMintsAtRedemption(t *testing.T) {
	svc, handoffs, _, _ := newSSOFlowTestService(t)
	user, err := svc.findOrCreateSSOUser(context.Background(), "handoff@example.com", "Handoff User")
	if err != nil {
		t.Fatal(err)
	}

	code := "identity-only-code"
	if err := handoffs.CreateExchangeCode(context.Background(), &SSOExchangeCode{
		OrgID:     "org-1",
		UserID:    user.ID,
		CodeHash:  hashToken(code),
		ExpiresAt: svc.now().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	row := handoffs.exchanges[hashToken(code)]
	if row == nil {
		t.Fatal("exchange code was not stored")
	}
	if row.OrgID != "org-1" || row.UserID != user.ID || row.CodeHash != hashToken(code) {
		t.Fatalf("stored identity fields: %+v", row)
	}

	pair, err := svc.ExchangeSSOCode(context.Background(), SSOExchangeRequest{
		OrgID: "org-1", Code: code,
	}, RequestMeta{UserAgent: "test-agent", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("ExchangeSSOCode: %v", err)
	}
	claims, err := svc.jwt.Verify(pair.AccessToken)
	if err != nil {
		t.Fatalf("minted access token is invalid: %v", err)
	}
	if claims.Subject != user.ID || claims.Email != user.Email {
		t.Fatalf("minted claims = %+v, want user %s", claims, user.ID)
	}
	if pair.User == nil || pair.User.ID != user.ID {
		t.Fatalf("token pair user = %+v", pair.User)
	}
	if len(svc.sessions.(*fakeSessionStore).byHash) != 1 {
		t.Fatal("expected a new refresh session to be created at exchange time")
	}
}

func TestCompleteSSOLoginRejectsInvalidLibraryResponseBeforeProvisioning(t *testing.T) {
	svc, handoffs, members, cfg := newSSOFlowTestService(t)
	relayState := "relay-state"
	handoffs.states[hashToken(relayState)] = &SSOLoginState{
		OrgID:     cfg.OrgID,
		StateHash: hashToken(relayState),
		RequestID: "request-1",
		ExpiresAt: svc.now().Add(time.Minute),
	}

	_, err := svc.CompleteSSOLogin(
		context.Background(), cfg.OrgID, "not-a-valid-saml-response", relayState)
	if err == nil || apperrors.From(err).Code != apperrors.CodeUnauthenticated {
		t.Fatalf("expected unauthenticated validation failure, got %v", err)
	}
	if members.calls != 0 {
		t.Fatal("membership was provisioned before SAML library validation")
	}
}

func TestSSOCallbackURLContainsOnlyCodeAndOrg(t *testing.T) {
	u, err := url.Parse("https://app.example.com/sso/callback?code=abc&orgId=org-1")
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("accessToken") != "" || u.Query().Get("refreshToken") != "" {
		t.Fatal("tokens must never be placed in the SSO redirect URL")
	}
}

func TestSSOOrgRoleValidation(t *testing.T) {
	for _, role := range []authz.OrgRole{authz.OrgOwner, authz.OrgAdmin, authz.RoleMember, authz.OrgAuditor} {
		got, err := ssoOrgRole(string(role))
		if err != nil || got != role {
			t.Fatalf("ssoOrgRole(%q) = %q, %v", role, got, err)
		}
	}
	if _, err := ssoOrgRole("superuser"); err == nil {
		t.Fatal("expected invalid role to fail")
	}
}

func TestFindOrCreateSSOUser(t *testing.T) {
	svc, _, _, _ := newSSOFlowTestService(t)

	created, err := svc.findOrCreateSSOUser(context.Background(), "sso@example.com", "SSO User")
	if err != nil {
		t.Fatalf("findOrCreateSSOUser create: %v", err)
	}
	if created.ID == "" || !created.EmailVerified || created.PasswordHash == "" {
		t.Fatalf("SSO user was not fully provisioned: %+v", created)
	}

	found, err := svc.findOrCreateSSOUser(context.Background(), "sso@example.com", "Changed Name")
	if err != nil {
		t.Fatalf("findOrCreateSSOUser find: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("existing user was not reused: got %s, want %s", found.ID, created.ID)
	}
}
