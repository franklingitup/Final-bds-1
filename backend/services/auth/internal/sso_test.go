package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Minimal IdP metadata sufficient for samlsp.ParseMetadata (no network).
const testIDPMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com/metadata">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
      Location="https://idp.example.com/sso/redirect"/>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
      Location="https://idp.example.com/sso/post"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

func testSPKeyPair(t *testing.T) (certPEM string, keyPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-sp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}

func testSSOConfig(t *testing.T, orgID string, version int64) *OrgSSOConfig {
	t.Helper()
	certPEM, keyPEM := testSPKeyPair(t)
	xml := testIDPMetadata
	return &OrgSSOConfig{
		TenantModel: database.TenantModel{
			Model: database.Model{ID: "cfg-" + orgID, Version: version},
			OrgID: orgID,
		},
		IDPMetadataXML:   &xml,
		IDPEntityID:      "https://idp.example.com/metadata",
		AttributeEmail:   "email",
		DefaultRole:      "member",
		Enabled:          true,
		SPCertificatePEM: &certPEM,
		SPPrivateKeyPEM:  keyPEM,
	}
}

func TestSSOProviderManager_CachesByOrgAndVersion(t *testing.T) {
	mgr, err := NewSSOProviderManager(SSOProviderManagerOpts{
		PublicBaseURL: "https://api.example.com",
	})
	if err != nil {
		t.Fatalf("NewSSOProviderManager: %v", err)
	}

	cfg := testSSOConfig(t, "org-1", 1)
	ctx := context.Background()

	sp1, err := mgr.Get(ctx, cfg)
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	sp2, err := mgr.Get(ctx, cfg)
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if sp1 != sp2 {
		t.Fatal("expected cached ServiceProvider pointer to be reused for same org/version")
	}

	cfg.Version = 2
	sp3, err := mgr.Get(ctx, cfg)
	if err != nil {
		t.Fatalf("Get after version bump: %v", err)
	}
	if sp3 == sp1 {
		t.Fatal("expected new ServiceProvider after config version change")
	}
}

func TestSSOProviderManager_Invalidate(t *testing.T) {
	mgr, err := NewSSOProviderManager(SSOProviderManagerOpts{
		PublicBaseURL: "https://api.example.com",
	})
	if err != nil {
		t.Fatalf("NewSSOProviderManager: %v", err)
	}

	cfg := testSSOConfig(t, "org-2", 1)
	ctx := context.Background()

	sp1, err := mgr.Get(ctx, cfg)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	mgr.Invalidate(cfg.OrgID)
	sp2, err := mgr.Get(ctx, cfg)
	if err != nil {
		t.Fatalf("Get after invalidate: %v", err)
	}
	if sp1 == sp2 {
		t.Fatal("expected rebuild after Invalidate")
	}
}

func TestSSOProviderManager_PerOrgIsolation(t *testing.T) {
	mgr, err := NewSSOProviderManager(SSOProviderManagerOpts{
		PublicBaseURL: "https://api.example.com",
	})
	if err != nil {
		t.Fatalf("NewSSOProviderManager: %v", err)
	}

	ctx := context.Background()
	a := testSSOConfig(t, "org-a", 1)
	b := testSSOConfig(t, "org-b", 1)

	spA, err := mgr.Get(ctx, a)
	if err != nil {
		t.Fatalf("Get A: %v", err)
	}
	spB, err := mgr.Get(ctx, b)
	if err != nil {
		t.Fatalf("Get B: %v", err)
	}
	if spA == spB {
		t.Fatal("expected distinct ServiceProviders per org")
	}
	if spA.AcsURL.Path == spB.AcsURL.Path {
		t.Fatalf("ACS paths should include org id; got both %s", spA.AcsURL.Path)
	}
	if want := "/v1/auth/sso/org-a/acs"; spA.AcsURL.Path != want {
		t.Fatalf("ACS path = %q, want %q", spA.AcsURL.Path, want)
	}
	if want := "/v1/auth/sso/org-a/metadata"; spA.MetadataURL.Path != want {
		t.Fatalf("metadata path = %q, want %q", spA.MetadataURL.Path, want)
	}
}

func TestSSOProviderManager_RequiresPublicBaseURL(t *testing.T) {
	if _, err := NewSSOProviderManager(SSOProviderManagerOpts{}); err == nil {
		t.Fatal("expected error for empty public base URL")
	}
}

func TestSSOKeyEncryptor_RoundTrip(t *testing.T) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	enc, err := NewSSOKeyEncryptorFromBytes(raw)
	if err != nil {
		t.Fatalf("NewSSOKeyEncryptorFromBytes: %v", err)
	}
	plain := []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----")
	ct, err := enc.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	out, err := enc.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(out) != string(plain) {
		t.Fatal("round-trip mismatch")
	}
}

// memorySSOConfigStore simulates Postgres row-level atomicity for TrySetSPKeys
// so concurrent ensureSPKeys races can be tested without a live database.
type memorySSOConfigStore struct {
	mu          sync.Mutex
	byOrg       map[string]*OrgSSOConfig
	setWins     atomic.Int64
	trySetCalls atomic.Int64
}

func newMemorySSOConfigStore(seed *OrgSSOConfig) *memorySSOConfigStore {
	s := &memorySSOConfigStore{byOrg: make(map[string]*OrgSSOConfig)}
	if seed != nil {
		cp := *seed
		if seed.IDPMetadataXML != nil {
			xml := *seed.IDPMetadataXML
			cp.IDPMetadataXML = &xml
		}
		if seed.SPCertificatePEM != nil {
			c := *seed.SPCertificatePEM
			cp.SPCertificatePEM = &c
		}
		if seed.SPPrivateKeyPEM != nil {
			cp.SPPrivateKeyPEM = append([]byte(nil), seed.SPPrivateKeyPEM...)
		}
		s.byOrg[seed.OrgID] = &cp
	}
	return s
}

func (s *memorySSOConfigStore) clone(c *OrgSSOConfig) *OrgSSOConfig {
	if c == nil {
		return nil
	}
	out := *c
	if c.IDPMetadataXML != nil {
		xml := *c.IDPMetadataXML
		out.IDPMetadataXML = &xml
	}
	if c.IDPMetadataURL != nil {
		u := *c.IDPMetadataURL
		out.IDPMetadataURL = &u
	}
	if c.SPCertificatePEM != nil {
		pem := *c.SPCertificatePEM
		out.SPCertificatePEM = &pem
	}
	if c.SPPrivateKeyPEM != nil {
		out.SPPrivateKeyPEM = append([]byte(nil), c.SPPrivateKeyPEM...)
	}
	return &out
}

func (s *memorySSOConfigStore) Upsert(_ context.Context, c *OrgSSOConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.clone(c)
	if existing, ok := s.byOrg[c.OrgID]; ok {
		cp.Version = existing.Version + 1
		// Preserve SP keys on upsert (matches Postgres Upsert behaviour).
		if cp.SPCertificatePEM == nil {
			cp.SPCertificatePEM = existing.SPCertificatePEM
			cp.SPPrivateKeyPEM = existing.SPPrivateKeyPEM
		}
	} else if cp.Version == 0 {
		cp.Version = 1
	}
	s.byOrg[c.OrgID] = cp
	c.Version = cp.Version
	c.ID = cp.ID
	return nil
}

func (s *memorySSOConfigStore) GetByOrgID(_ context.Context, orgID string) (*OrgSSOConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.byOrg[orgID]
	if !ok {
		return nil, apperrors.NotFound("sso config not found")
	}
	return s.clone(c), nil
}

func (s *memorySSOConfigStore) DeleteByOrgID(_ context.Context, orgID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byOrg[orgID]; !ok {
		return apperrors.NotFound("sso config not found")
	}
	delete(s.byOrg, orgID)
	return nil
}

func (s *memorySSOConfigStore) UpdateSPKeys(_ context.Context, orgID string, certPEM string, privateKeyPEM []byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.byOrg[orgID]
	if !ok {
		return 0, apperrors.NotFound("sso config not found")
	}
	pem := certPEM
	c.SPCertificatePEM = &pem
	c.SPPrivateKeyPEM = append([]byte(nil), privateKeyPEM...)
	c.Version++
	return c.Version, nil
}

func (s *memorySSOConfigStore) TrySetSPKeys(_ context.Context, orgID string, certPEM string, privateKeyPEM []byte) (int64, bool, error) {
	s.trySetCalls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.byOrg[orgID]
	if !ok {
		return 0, false, nil
	}
	if c.SPCertificatePEM != nil && *c.SPCertificatePEM != "" {
		return 0, false, nil
	}
	pem := certPEM
	c.SPCertificatePEM = &pem
	c.SPPrivateKeyPEM = append([]byte(nil), privateKeyPEM...)
	c.Version++
	s.setWins.Add(1)
	return c.Version, true, nil
}

func TestSSOProviderManager_ConcurrentKeyGeneration_SingleWinner(t *testing.T) {
	orgID := "org-race"
	xml := testIDPMetadata
	seed := &OrgSSOConfig{
		TenantModel: database.TenantModel{
			Model: database.Model{ID: "cfg-race", Version: 1},
			OrgID: orgID,
		},
		IDPMetadataXML: &xml,
		IDPEntityID:    "https://idp.example.com/metadata",
		AttributeEmail: "email",
		DefaultRole:    "member",
		Enabled:        true,
		// Intentionally no SP keypair — concurrent Gets must generate once.
	}
	store := newMemorySSOConfigStore(seed)

	mgr, err := NewSSOProviderManager(SSOProviderManagerOpts{
		PublicBaseURL: "https://api.example.com",
		Keys:          store,
	})
	if err != nil {
		t.Fatalf("NewSSOProviderManager: %v", err)
	}

	const goroutines = 16
	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		certs    = make([]string, goroutines)
		errs     = make([]error, goroutines)
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			// Each caller starts from a keyless config snapshot (like concurrent
			// metadata + login requests that both loaded before any write).
			cfg := &OrgSSOConfig{
				TenantModel: database.TenantModel{
					Model: database.Model{ID: seed.ID, Version: seed.Version},
					OrgID: orgID,
				},
				IDPMetadataXML: &xml,
				IDPEntityID:    seed.IDPEntityID,
				AttributeEmail: seed.AttributeEmail,
				DefaultRole:    seed.DefaultRole,
				Enabled:        true,
			}
			sp, err := mgr.Get(context.Background(), cfg)
			errs[i] = err
			if err == nil && sp != nil && sp.Certificate != nil {
				certs[i] = string(pem.EncodeToMemory(&pem.Block{
					Type:  "CERTIFICATE",
					Bytes: sp.Certificate.Raw,
				}))
			}
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	if got := store.setWins.Load(); got != 1 {
		t.Fatalf("TrySetSPKeys succeeded %d times, want exactly 1", got)
	}

	persisted, err := store.GetByOrgID(context.Background(), orgID)
	if err != nil {
		t.Fatalf("GetByOrgID: %v", err)
	}
	if !hasSPKeyPair(persisted) {
		t.Fatal("expected one persisted SP keypair")
	}
	want := *persisted.SPCertificatePEM
	for i, got := range certs {
		if got != want {
			t.Fatalf("goroutine %d used a different certificate than the persisted winner", i)
		}
	}
}

func TestSSOProviderManager_InvalidateReusesPersistedKeypair(t *testing.T) {
	orgID := "org-invalidate"
	certPEM, keyPEM := testSPKeyPair(t)
	xml := testIDPMetadata
	seed := &OrgSSOConfig{
		TenantModel: database.TenantModel{
			Model: database.Model{ID: "cfg-inv", Version: 1},
			OrgID: orgID,
		},
		IDPMetadataXML:   &xml,
		IDPEntityID:      "https://idp.example.com/metadata",
		AttributeEmail:   "email",
		DefaultRole:      "member",
		Enabled:          true,
		SPCertificatePEM: &certPEM,
		SPPrivateKeyPEM:  keyPEM,
	}
	store := newMemorySSOConfigStore(seed)

	mgr, err := NewSSOProviderManager(SSOProviderManagerOpts{
		PublicBaseURL: "https://api.example.com",
		Keys:          store,
	})
	if err != nil {
		t.Fatalf("NewSSOProviderManager: %v", err)
	}

	ctx := context.Background()
	cfgWithKeys := testSSOConfig(t, orgID, 1)
	// Align with the persisted seed material so the first Get caches that identity.
	cfgWithKeys.SPCertificatePEM = &certPEM
	cfgWithKeys.SPPrivateKeyPEM = append([]byte(nil), keyPEM...)
	cfgWithKeys.IDPMetadataXML = &xml

	sp1, err := mgr.Get(ctx, cfgWithKeys)
	if err != nil {
		t.Fatalf("initial Get: %v", err)
	}
	beforeCert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: sp1.Certificate.Raw}))
	beforePub := sp1.Key.Public()

	mgr.Invalidate(orgID)

	// Simulate an admin config edit handoff: Invalidate ran, and the next Get
	// receives a cfg snapshot that omits SP key fields (metadata-only update).
	cfgWithoutKeys := &OrgSSOConfig{
		TenantModel: database.TenantModel{
			Model: database.Model{ID: seed.ID, Version: seed.Version},
			OrgID: orgID,
		},
		IDPMetadataXML: &xml,
		IDPEntityID:    seed.IDPEntityID,
		AttributeEmail: "mail", // unrelated mapping change
		DefaultRole:    seed.DefaultRole,
		Enabled:        true,
	}
	trySetsBefore := store.trySetCalls.Load()
	sp2, err := mgr.Get(ctx, cfgWithoutKeys)
	if err != nil {
		t.Fatalf("Get after Invalidate: %v", err)
	}

	afterCert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: sp2.Certificate.Raw}))
	if afterCert != beforeCert {
		t.Fatal("Invalidate+rebuild changed the SP certificate; persisted keypair must be reused")
	}
	if !publicKeysEqual(sp2.Key.Public(), beforePub) {
		t.Fatal("Invalidate+rebuild changed the SP public key")
	}
	if store.trySetCalls.Load() != trySetsBefore {
		t.Fatalf("ensureSPKeys attempted TrySetSPKeys after Invalidate; persisted keys must be loaded from the store, not regenerated")
	}
	if store.setWins.Load() != 0 {
		t.Fatalf("TrySetSPKeys succeeded %d times after Invalidate; want 0", store.setWins.Load())
	}

	persisted, err := store.GetByOrgID(ctx, orgID)
	if err != nil {
		t.Fatalf("GetByOrgID: %v", err)
	}
	if *persisted.SPCertificatePEM != certPEM {
		t.Fatal("persisted certificate PEM changed after Invalidate rebuild")
	}
	if string(persisted.SPPrivateKeyPEM) != string(keyPEM) {
		t.Fatal("persisted private key PEM changed after Invalidate rebuild")
	}
}

func publicKeysEqual(a, b interface{}) bool {
	ak, ok1 := a.(*rsa.PublicKey)
	bk, ok2 := b.(*rsa.PublicKey)
	if !ok1 || !ok2 {
		return false
	}
	return ak.N.Cmp(bk.N) == 0 && ak.E == bk.E
}
