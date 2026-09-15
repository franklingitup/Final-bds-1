package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/crewjam/saml"

	"github.com/bdsplatform/platform/backend/libs/authz"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

type recordingSSOProviders struct {
	*SSOProviderManager
	mu          sync.Mutex
	invalidates []string
}

func (r *recordingSSOProviders) Invalidate(orgID string) {
	r.mu.Lock()
	r.invalidates = append(r.invalidates, orgID)
	r.mu.Unlock()
	r.SSOProviderManager.Invalidate(orgID)
}

func (r *recordingSSOProviders) invalidationCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.invalidates)
}

type countingSSOConfigStore struct {
	SSOConfigStore
	upserts atomic.Int64
}

func (c *countingSSOConfigStore) Upsert(ctx context.Context, cfg *OrgSSOConfig) error {
	c.upserts.Add(1)
	return c.SSOConfigStore.Upsert(ctx, cfg)
}

type staticOrgMemberStore struct {
	role authz.OrgRole
}

func (s *staticOrgMemberStore) GetOrgMember(_ context.Context, userID string) (*authz.OrgMember, error) {
	if userID == "" {
		return nil, apperrors.NotFound("member not found")
	}
	return &authz.OrgMember{
		OrgID:  "ignored",
		UserID: userID,
		Role:   s.role,
		Status: "active",
	}, nil
}

func newSSOAdminTestService(t *testing.T, role authz.OrgRole) (*Service, *countingSSOConfigStore, *recordingSSOProviders) {
	t.Helper()
	inner := newMemorySSOConfigStore(nil)
	configs := &countingSSOConfigStore{SSOConfigStore: inner}
	manager, err := NewSSOProviderManager(SSOProviderManagerOpts{
		PublicBaseURL: "https://api.example.com",
		Keys:          inner,
	})
	if err != nil {
		t.Fatalf("NewSSOProviderManager: %v", err)
	}
	providers := &recordingSSOProviders{SSOProviderManager: manager}
	svc := NewService(Deps{
		Users:            newFakeUserStore(),
		Sessions:         newFakeSessionStore(),
		OneTimeTokens:    newFakeOTTStore(),
		SSOConfigs:       configs,
		SSOProviders:     providers,
		SSOOrganizations: &fakeSSOOrganizationStore{bySlug: map[string]string{"acme": "org-1"}},
		SSOMembers:       &fakeSSOMemberStore{},
		SSOHandoffs:      newFakeSSOHandoffStore(),
		SSORedirectURL:   "https://app.example.com/sso/callback",
		OrgMembers:       &staticOrgMemberStore{role: role},
		Tx:               fakeTx{},
		Tenant:           &fakeTenantRunner{},
		JWT:              NewJWTIssuer(testAuthConfig()),
		Outbox:           &fakeOutbox{},
		Auth:             testAuthConfig(),
	})
	return svc, configs, providers
}

func TestSSOConfigRejectedForNonAdminMember(t *testing.T) {
	svc, configs, providers := newSSOAdminTestService(t, authz.RoleMember)
	ctx := context.Background()
	xml := testIDPMetadata
	req := UpsertSSOConfigRequest{IDPMetadataXML: &xml, Enabled: true}

	if _, err := svc.UpsertSSOConfig(ctx, "org-1", "user-1", req); err == nil || apperrors.From(err).Code != apperrors.CodeForbidden {
		t.Fatalf("PUT: expected forbidden, got %v", err)
	}
	if _, err := svc.GetSSOConfig(ctx, "org-1", "user-1"); err == nil || apperrors.From(err).Code != apperrors.CodeForbidden {
		t.Fatalf("GET: expected forbidden, got %v", err)
	}
	if err := svc.DeleteSSOConfig(ctx, "org-1", "user-1"); err == nil || apperrors.From(err).Code != apperrors.CodeForbidden {
		t.Fatalf("DELETE: expected forbidden, got %v", err)
	}
	if configs.upserts.Load() != 0 {
		t.Fatal("non-admin PUT must not upsert")
	}
	if providers.invalidationCount() != 0 {
		t.Fatal("non-admin must not invalidate the provider cache")
	}
}

func TestUpsertSSOConfigRejectsBothMetadataSources(t *testing.T) {
	svc, configs, _ := newSSOAdminTestService(t, authz.OrgOwner)
	url := "https://idp.example.com/metadata"
	xml := testIDPMetadata
	_, err := svc.UpsertSSOConfig(context.Background(), "org-1", "owner", UpsertSSOConfigRequest{
		IDPMetadataURL: &url,
		IDPMetadataXML: &xml,
	})
	if err == nil || apperrors.From(err).Code != apperrors.CodeValidationFailed {
		t.Fatalf("expected validation error, got %v", err)
	}
	if configs.upserts.Load() != 0 {
		t.Fatal("upsert must not run when both sources are set")
	}
}

func TestUpsertSSOConfigRejectsNeitherMetadataSource(t *testing.T) {
	svc, configs, _ := newSSOAdminTestService(t, authz.OrgOwner)
	_, err := svc.UpsertSSOConfig(context.Background(), "org-1", "owner", UpsertSSOConfigRequest{})
	if err == nil || apperrors.From(err).Code != apperrors.CodeValidationFailed {
		t.Fatalf("expected validation error, got %v", err)
	}
	if configs.upserts.Load() != 0 {
		t.Fatal("upsert must not run when neither source is set")
	}
}

func TestUpsertSSOConfigRejectsUnreachableMetadataURLBeforeWrite(t *testing.T) {
	svc, configs, providers := newSSOAdminTestService(t, authz.OrgOwner)
	hits := atomic.Int64{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)

	_, err := svc.UpsertSSOConfig(context.Background(), "org-1", "owner", UpsertSSOConfigRequest{
		IDPMetadataURL: &ts.URL,
		Enabled:        true,
	})
	if err == nil || apperrors.From(err).Code != apperrors.CodeValidationFailed {
		t.Fatalf("expected validation error, got %v", err)
	}
	if !strings.Contains(err.Error(), "could not fetch IdP metadata from the provided URL") {
		t.Fatalf("error should mention fetch failure, got %v", err)
	}
	if configs.upserts.Load() != 0 {
		t.Fatal("upsert must not run after a fetch failure")
	}
	if providers.invalidationCount() != 0 {
		t.Fatal("Invalidate must not run after a failed PUT")
	}
	if hits.Load() == 0 {
		t.Fatal("expected the metadata URL to be fetched")
	}
}

func TestUpsertSSOConfigRejectsMalformedXMLBeforeWrite(t *testing.T) {
	svc, configs, providers := newSSOAdminTestService(t, authz.OrgOwner)
	bad := "<not-valid-idp-metadata"
	_, err := svc.UpsertSSOConfig(context.Background(), "org-1", "owner", UpsertSSOConfigRequest{
		IDPMetadataXML: &bad,
	})
	if err == nil || apperrors.From(err).Code != apperrors.CodeValidationFailed {
		t.Fatalf("expected validation error, got %v", err)
	}
	if !strings.Contains(err.Error(), "could not parse IdP metadata XML") {
		t.Fatalf("error should mention parse failure, got %v", err)
	}
	if configs.upserts.Load() != 0 {
		t.Fatal("upsert must not run after XML parse failure")
	}
	if providers.invalidationCount() != 0 {
		t.Fatal("Invalidate must not run after a failed PUT")
	}
}

func TestUpsertSSOConfigRejectsInvalidDefaultRoleBeforeFetchOrWrite(t *testing.T) {
	svc, configs, providers := newSSOAdminTestService(t, authz.OrgOwner)
	hits := atomic.Int64{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testIDPMetadata))
	}))
	t.Cleanup(ts.Close)

	_, err := svc.UpsertSSOConfig(context.Background(), "org-1", "owner", UpsertSSOConfigRequest{
		IDPMetadataURL: &ts.URL,
		DefaultRole:    "superuser",
	})
	if err == nil || apperrors.From(err).Code != apperrors.CodeValidationFailed {
		t.Fatalf("expected validation error, got %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("metadata must not be fetched when defaultRole is invalid")
	}
	if configs.upserts.Load() != 0 {
		t.Fatal("upsert must not run when defaultRole is invalid")
	}
	if providers.invalidationCount() != 0 {
		t.Fatal("Invalidate must not run when defaultRole is invalid")
	}
}

func TestUpsertSSOConfigInvalidatesProviderCache(t *testing.T) {
	svc, _, providers := newSSOAdminTestService(t, authz.OrgOwner)
	ctx := context.Background()
	xml := testIDPMetadata

	first, err := svc.UpsertSSOConfig(ctx, "org-1", "owner", UpsertSSOConfigRequest{
		IDPMetadataXML: &xml,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("first PUT: %v", err)
	}
	if providers.invalidationCount() != 1 {
		t.Fatalf("Invalidate calls after first PUT = %d, want 1", providers.invalidationCount())
	}

	cfg, err := svc.ssoConfigs.GetByOrgID(ctx, "org-1")
	if err != nil {
		t.Fatal(err)
	}
	sp1, err := providers.Get(ctx, cfg)
	if err != nil {
		t.Fatalf("Get after first PUT: %v", err)
	}

	updatedXML := strings.Replace(testIDPMetadata, "https://idp.example.com/metadata", "https://idp-b.example.com/metadata", 1)
	second, err := svc.UpsertSSOConfig(ctx, "org-1", "owner", UpsertSSOConfigRequest{
		IDPMetadataXML: &updatedXML,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("second PUT: %v", err)
	}
	if providers.invalidationCount() != 2 {
		t.Fatalf("Invalidate calls after second PUT = %d, want 2", providers.invalidationCount())
	}
	if second.IDPEntityID == first.IDPEntityID {
		t.Fatal("expected IdP entity ID to change after metadata update")
	}

	cfg2, err := svc.ssoConfigs.GetByOrgID(ctx, "org-1")
	if err != nil {
		t.Fatal(err)
	}
	sp2, err := providers.Get(ctx, cfg2)
	if err != nil {
		t.Fatalf("Get after second PUT: %v", err)
	}
	if sp1 == sp2 {
		t.Fatal("expected a new ServiceProvider after Invalidate + metadata change")
	}
	if sp2.IDPMetadata.EntityID != "https://idp-b.example.com/metadata" {
		t.Fatalf("cached SP still has old IdP entity %q", sp2.IDPMetadata.EntityID)
	}
}

func TestGetSSOConfigOmitsPrivateKeyMaterial(t *testing.T) {
	svc, _, _ := newSSOAdminTestService(t, authz.OrgOwner)
	ctx := context.Background()
	xml := testIDPMetadata
	if _, err := svc.UpsertSSOConfig(ctx, "org-1", "owner", UpsertSSOConfigRequest{
		IDPMetadataXML: &xml,
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := svc.ssoConfigs.GetByOrgID(ctx, "org-1")
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEfake\n-----END RSA PRIVATE KEY-----")
	cert := "-----BEGIN CERTIFICATE-----\nMIIfake\n-----END CERTIFICATE-----"
	cfg.SPPrivateKeyPEM = key
	cfg.SPCertificatePEM = &cert
	if err := svc.ssoConfigs.Upsert(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	got, err := svc.GetSSOConfig(ctx, "org-1", "owner")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, leak := range []string{
		"sp_private_key_pem",
		"spPrivateKeyPem",
		"BEGIN RSA PRIVATE KEY",
		"MIIEfake",
	} {
		if strings.Contains(body, leak) {
			t.Fatalf("GET body leaked %q: %s", leak, body)
		}
	}
}

func TestGetSSOConfigEchoesURLAndXML(t *testing.T) {
	ctx := context.Background()

	t.Run("xml", func(t *testing.T) {
		svc, _, _ := newSSOAdminTestService(t, authz.OrgOwner)
		xml := testIDPMetadata
		if _, err := svc.UpsertSSOConfig(ctx, "org-1", "owner", UpsertSSOConfigRequest{
			IDPMetadataXML: &xml,
			Enabled:        true,
		}); err != nil {
			t.Fatal(err)
		}
		got, err := svc.GetSSOConfig(ctx, "org-1", "owner")
		if err != nil {
			t.Fatal(err)
		}
		if got.IDPMetadataXML == nil || *got.IDPMetadataXML != xml {
			t.Fatalf("expected stored XML echoed, got %+v", got.IDPMetadataXML)
		}
		if got.IDPMetadataURL != nil {
			t.Fatalf("XML config must not echo a URL, got %v", *got.IDPMetadataURL)
		}
		if got.SPMetadataURL != "https://api.example.com/v1/auth/sso/org-1/metadata" {
			t.Fatalf("SP metadata URL = %q", got.SPMetadataURL)
		}
	})

	t.Run("url", func(t *testing.T) {
		svc, _, _ := newSSOAdminTestService(t, authz.OrgOwner)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(testIDPMetadata))
		}))
		t.Cleanup(ts.Close)

		if _, err := svc.UpsertSSOConfig(ctx, "org-1", "owner", UpsertSSOConfigRequest{
			IDPMetadataURL: &ts.URL,
			Enabled:        true,
		}); err != nil {
			t.Fatal(err)
		}
		got, err := svc.GetSSOConfig(ctx, "org-1", "owner")
		if err != nil {
			t.Fatal(err)
		}
		if got.IDPMetadataURL == nil || *got.IDPMetadataURL != ts.URL {
			t.Fatalf("expected stored URL echoed, got %+v", got.IDPMetadataURL)
		}
		if got.IDPMetadataXML != nil {
			t.Fatalf("URL config must not echo XML, got %q", *got.IDPMetadataXML)
		}
	})
}

func TestDeleteSSOConfigInvalidatesAndRemovesLogin(t *testing.T) {
	svc, _, providers := newSSOAdminTestService(t, authz.OrgOwner)
	ctx := context.Background()
	xml := testIDPMetadata
	if _, err := svc.UpsertSSOConfig(ctx, "org-1", "owner", UpsertSSOConfigRequest{
		IDPMetadataXML: &xml,
		Enabled:        true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartSSOLogin(ctx, "acme"); err != nil {
		t.Fatalf("login should work while config exists: %v", err)
	}

	if err := svc.DeleteSSOConfig(ctx, "org-1", "owner"); err != nil {
		t.Fatalf("DeleteSSOConfig: %v", err)
	}
	if providers.invalidationCount() < 2 {
		t.Fatalf("expected Invalidate on PUT and DELETE, got %d", providers.invalidationCount())
	}

	if _, err := svc.GetSSOConfig(ctx, "org-1", "owner"); err == nil || apperrors.From(err).Code != apperrors.CodeNotFound {
		t.Fatalf("GET after DELETE: expected not found, got %v", err)
	}
	_, err := svc.StartSSOLogin(ctx, "acme")
	if err == nil || apperrors.From(err).Code != apperrors.CodeNotFound {
		t.Fatalf("login after DELETE: expected not found, got %v", err)
	}
}

var _ ssoProviderRuntime = (*recordingSSOProviders)(nil)
var _ ssoProviderRuntime = (*SSOProviderManager)(nil)

func TestUpsertSSOConfigStoresParsedEntityID(t *testing.T) {
	svc, _, _ := newSSOAdminTestService(t, authz.OrgOwner)
	xml := testIDPMetadata
	got, err := svc.UpsertSSOConfig(context.Background(), "org-1", "owner", UpsertSSOConfigRequest{
		IDPMetadataXML: &xml,
		DefaultRole:    "admin",
		Enabled:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IDPEntityID != "https://idp.example.com/metadata" {
		t.Fatalf("IDPEntityID = %q", got.IDPEntityID)
	}
	if got.DefaultRole != string(authz.OrgAdmin) {
		t.Fatalf("DefaultRole = %q", got.DefaultRole)
	}
	if got.AttributeEmail != defaultSSOEmailAttr {
		t.Fatalf("email attribute default = %q", got.AttributeEmail)
	}
}

func TestLoadIDPMetadataMatchesManagerPath(t *testing.T) {
	// Sanity: the admin PUT path calls the exported wrapper around the same
	// loadIDPMetadata used by SSOProviderManager.Get.
	mgr, err := NewSSOProviderManager(SSOProviderManagerOpts{PublicBaseURL: "https://api.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	xml := testIDPMetadata
	meta, err := mgr.LoadIDPMetadata(context.Background(), &OrgSSOConfig{IDPMetadataXML: &xml})
	if err != nil {
		t.Fatal(err)
	}
	if meta.EntityID == "" {
		t.Fatal("expected parsed EntityDescriptor")
	}
	var _ *saml.EntityDescriptor = meta
}
