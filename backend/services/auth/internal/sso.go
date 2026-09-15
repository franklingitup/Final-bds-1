package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	dsig "github.com/russellhaering/goxmldsig"
)

// URL path conventions for SAML endpoints (Phase 3 registers these routes).
const (
	ssoACSPathFormat      = "/v1/auth/sso/%s/acs"
	ssoMetadataPathFormat = "/v1/auth/sso/%s/metadata"
)

// SSOProviderManager builds and caches one crewjam saml.ServiceProvider per org.
type SSOProviderManager struct {
	publicBaseURL url.URL
	httpClient    *http.Client
	encryptor     *SSOKeyEncryptor // nil => store/load SP private keys as plaintext PEM
	keys          spKeyPersister   // nil => cannot generate+persist missing keys
	now           func() time.Time

	mu    sync.RWMutex
	cache map[string]*cachedServiceProvider
}

type spKeyPersister interface {
	TrySetSPKeys(ctx context.Context, orgID string, certPEM string, privateKeyPEM []byte) (version int64, ok bool, err error)
	GetByOrgID(ctx context.Context, orgID string) (*OrgSSOConfig, error)
}

type cachedServiceProvider struct {
	sp      *saml.ServiceProvider
	version int64
}

// SSOProviderManagerOpts configures NewSSOProviderManager.
type SSOProviderManagerOpts struct {
	// PublicBaseURL is the externally reachable API base (e.g. https://api.example.com),
	// used to build ACS and metadata URLs. Required.
	PublicBaseURL string
	HTTPClient    *http.Client
	Encryptor     *SSOKeyEncryptor
	// Keys persists newly generated SP keypairs. Optional only when every config
	// already carries SPCertificatePEM / SPPrivateKeyPEM (e.g. unit tests).
	Keys SSOConfigStore
	Now  func() time.Time
}

// NewSSOProviderManager constructs a manager. PublicBaseURL must be an absolute URL.
func NewSSOProviderManager(opts SSOProviderManagerOpts) (*SSOProviderManager, error) {
	raw := strings.TrimRight(strings.TrimSpace(opts.PublicBaseURL), "/")
	if raw == "" {
		return nil, fmt.Errorf("auth: SSO public base URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("auth: invalid SSO public base URL %q", opts.PublicBaseURL)
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	var keys spKeyPersister
	if opts.Keys != nil {
		keys = opts.Keys
	}
	return &SSOProviderManager{
		publicBaseURL: *u,
		httpClient:    client,
		encryptor:     opts.Encryptor,
		keys:          keys,
		now:           now,
		cache:         make(map[string]*cachedServiceProvider),
	}, nil
}

// Invalidate drops a cached ServiceProvider so the next Get rebuilds from config.
func (m *SSOProviderManager) Invalidate(orgID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cache, orgID)
}

// Get returns a ServiceProvider for cfg, building and caching on miss or version change.
// May generate and persist an SP keypair when cfg has none and Keys is configured.
func (m *SSOProviderManager) Get(ctx context.Context, cfg *OrgSSOConfig) (*saml.ServiceProvider, error) {
	if cfg == nil || cfg.OrgID == "" {
		return nil, fmt.Errorf("auth: sso config with org ID is required")
	}

	m.mu.RLock()
	if entry, ok := m.cache[cfg.OrgID]; ok && entry.version == cfg.Version {
		sp := entry.sp
		m.mu.RUnlock()
		return sp, nil
	}
	m.mu.RUnlock()

	sp, version, err := m.build(ctx, cfg)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.cache[cfg.OrgID] = &cachedServiceProvider{sp: sp, version: version}
	m.mu.Unlock()
	return sp, nil
}

func (m *SSOProviderManager) build(ctx context.Context, cfg *OrgSSOConfig) (*saml.ServiceProvider, int64, error) {
	idpMeta, err := m.loadIDPMetadata(ctx, cfg)
	if err != nil {
		return nil, 0, err
	}

	key, cert, version, err := m.ensureSPKeys(ctx, cfg)
	if err != nil {
		return nil, 0, err
	}

	acsURL := m.publicBaseURL.ResolveReference(&url.URL{Path: fmt.Sprintf(ssoACSPathFormat, cfg.OrgID)})
	metadataURL := m.publicBaseURL.ResolveReference(&url.URL{Path: fmt.Sprintf(ssoMetadataPathFormat, cfg.OrgID)})

	entityID := strings.TrimSpace(cfg.SPEntityID)
	if entityID == "" {
		entityID = metadataURL.String()
	}

	sp := &saml.ServiceProvider{
		EntityID:        entityID,
		Key:             key,
		Certificate:     cert,
		MetadataURL:     *metadataURL,
		AcsURL:          *acsURL,
		IDPMetadata:     idpMeta,
		SignatureMethod: dsig.RSASHA256SignatureMethod,
	}
	return sp, version, nil
}

func (m *SSOProviderManager) loadIDPMetadata(ctx context.Context, cfg *OrgSSOConfig) (*saml.EntityDescriptor, error) {
	if cfg.IDPMetadataXML != nil && strings.TrimSpace(*cfg.IDPMetadataXML) != "" {
		return samlsp.ParseMetadata([]byte(*cfg.IDPMetadataXML))
	}
	if cfg.IDPMetadataURL != nil && strings.TrimSpace(*cfg.IDPMetadataURL) != "" {
		u, err := url.Parse(strings.TrimSpace(*cfg.IDPMetadataURL))
		if err != nil {
			return nil, fmt.Errorf("auth: invalid idp metadata url: %w", err)
		}
		return samlsp.FetchMetadata(ctx, m.httpClient, *u)
	}
	return nil, fmt.Errorf("auth: sso config for org %s has neither idp metadata url nor xml", cfg.OrgID)
}

func (m *SSOProviderManager) ensureSPKeys(ctx context.Context, cfg *OrgSSOConfig) (*rsa.PrivateKey, *x509.Certificate, int64, error) {
	if hasSPKeyPair(cfg) {
		return m.parseStoredSPKeys(cfg)
	}

	// Invalidate only drops the in-memory ServiceProvider. Callers may pass a
	// cfg snapshot without SP key fields (e.g. after an unrelated admin edit).
	// Re-fetch before generating so we never mint a new identity when one is
	// already persisted.
	if m.keys != nil {
		fresh, err := m.keys.GetByOrgID(ctx, cfg.OrgID)
		if err != nil {
			return nil, nil, 0, err
		}
		if hasSPKeyPair(fresh) {
			cfg.SPCertificatePEM = fresh.SPCertificatePEM
			cfg.SPPrivateKeyPEM = fresh.SPPrivateKeyPEM
			cfg.Version = fresh.Version
			return m.parseStoredSPKeys(cfg)
		}
	} else {
		return nil, nil, 0, fmt.Errorf("auth: org %s has no SP keypair and key persistence is not configured", cfg.OrgID)
	}

	key, certPEM, keyPEM, err := generateSPKeyPair(m.now(), cfg.OrgID)
	if err != nil {
		return nil, nil, 0, err
	}

	storedKey := keyPEM
	if m.encryptor != nil {
		storedKey, err = m.encryptor.Encrypt(keyPEM)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("auth: encrypt sp private key: %w", err)
		}
	}

	version, ok, err := m.keys.TrySetSPKeys(ctx, cfg.OrgID, certPEM, storedKey)
	if err != nil {
		return nil, nil, 0, err
	}
	if ok {
		// We won the race — only now is it safe to use the locally generated keypair.
		cfg.SPCertificatePEM = &certPEM
		cfg.SPPrivateKeyPEM = storedKey
		cfg.Version = version
		_, cert, err := parseSPKeyPair(keyPEM, []byte(certPEM))
		if err != nil {
			return nil, nil, 0, err
		}
		return key, cert, version, nil
	}

	// Someone else persisted first — discard ours and use the winner's material.
	fresh, err := m.keys.GetByOrgID(ctx, cfg.OrgID)
	if err != nil {
		return nil, nil, 0, err
	}
	if !hasSPKeyPair(fresh) {
		return nil, nil, 0, fmt.Errorf("auth: failed to claim sp keypair for org %s", cfg.OrgID)
	}
	cfg.SPCertificatePEM = fresh.SPCertificatePEM
	cfg.SPPrivateKeyPEM = fresh.SPPrivateKeyPEM
	cfg.Version = fresh.Version
	return m.parseStoredSPKeys(cfg)
}

func hasSPKeyPair(cfg *OrgSSOConfig) bool {
	return cfg != nil && cfg.SPCertificatePEM != nil && *cfg.SPCertificatePEM != "" && len(cfg.SPPrivateKeyPEM) > 0
}

func (m *SSOProviderManager) parseStoredSPKeys(cfg *OrgSSOConfig) (*rsa.PrivateKey, *x509.Certificate, int64, error) {
	key, cert, err := parseSPKeyPair(m.decryptKey(cfg.SPPrivateKeyPEM), []byte(*cfg.SPCertificatePEM))
	if err != nil {
		return nil, nil, 0, err
	}
	return key, cert, cfg.Version, nil
}

func (m *SSOProviderManager) decryptKey(stored []byte) []byte {
	if m.encryptor == nil {
		return stored
	}
	// Encrypted blobs are nonce||ciphertext; PEM starts with '-'. Prefer decrypt,
	// fall back to treating as plaintext for local/dev rows written without a key.
	plain, err := m.encryptor.Decrypt(stored)
	if err != nil {
		return stored
	}
	return plain
}

// ACSURL returns the Assertion Consumer Service URL for an org.
func (m *SSOProviderManager) ACSURL(orgID string) url.URL {
	return *m.publicBaseURL.ResolveReference(&url.URL{Path: fmt.Sprintf(ssoACSPathFormat, orgID)})
}

// MetadataURL returns the SP metadata URL for an org.
func (m *SSOProviderManager) MetadataURL(orgID string) url.URL {
	return *m.publicBaseURL.ResolveReference(&url.URL{Path: fmt.Sprintf(ssoMetadataPathFormat, orgID)})
}

// ssoProviderRuntime is the SSOProviderManager surface used by Service.
type ssoProviderRuntime interface {
	Get(ctx context.Context, cfg *OrgSSOConfig) (*saml.ServiceProvider, error)
	Invalidate(orgID string)
	MetadataURL(orgID string) url.URL
	LoadIDPMetadata(ctx context.Context, cfg *OrgSSOConfig) (*saml.EntityDescriptor, error)
}

// LoadIDPMetadata fetches or parses IdP metadata using the same path Get uses.
func (m *SSOProviderManager) LoadIDPMetadata(ctx context.Context, cfg *OrgSSOConfig) (*saml.EntityDescriptor, error) {
	return m.loadIDPMetadata(ctx, cfg)
}

func generateSPKeyPair(now time.Time, orgID string) (key *rsa.PrivateKey, certPEM string, keyPEM []byte, err error) {
	key, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", nil, fmt.Errorf("auth: generate sp rsa key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, "", nil, fmt.Errorf("auth: generate sp cert serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "bds-sso-sp-" + orgID,
			Organization: []string{"BDS Platform"},
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, "", nil, fmt.Errorf("auth: create sp certificate: %w", err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return key, certPEM, keyPEM, nil
}

func parseSPKeyPair(keyPEM, certPEM []byte) (*rsa.PrivateKey, *x509.Certificate, error) {
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("auth: failed to decode sp private key pem")
	}
	var (
		key *rsa.PrivateKey
		err error
	)
	switch keyBlock.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	case "PRIVATE KEY":
		parsed, parseErr := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("auth: parse sp private key: %w", parseErr)
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, nil, fmt.Errorf("auth: sp private key is not RSA")
		}
	default:
		return nil, nil, fmt.Errorf("auth: unsupported sp private key type %q", keyBlock.Type)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("auth: parse sp private key: %w", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("auth: failed to decode sp certificate pem")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: parse sp certificate: %w", err)
	}
	return key, cert, nil
}
