package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/saml"
	"github.com/gofiber/fiber/v2"
	dsig "github.com/russellhaering/goxmldsig"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/middleware"
)

const (
	acsTestOrgID     = "org-1"
	acsTestOrgSlug   = "acme"
	acsTestPublicAPI = "https://api.example.com"
	acsTestIDPEntity = "https://idp.example.com/metadata"
	acsTestEmail     = "sso-acs@example.com"
)

// acsHarness drives the real Fiber ACS handler against crewjam-built IdP responses.
type acsHarness struct {
	svc      *Service
	app      *fiber.App
	users    *fakeUserStore
	members  *fakeSSOMemberStore
	handoffs *fakeSSOHandoffStore
	idp      *saml.IdentityProvider
	acsURL   string
	spEntity string
}

func newACSHarness(t *testing.T) *acsHarness {
	t.Helper()
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)

	idpKey, idpCert := testIDPKeyPair(t)
	idp := &saml.IdentityProvider{
		Key:             idpKey,
		Certificate:     idpCert,
		MetadataURL:     mustParseURL(t, acsTestIDPEntity),
		SSOURL:          mustParseURL(t, "https://idp.example.com/sso"),
		SignatureMethod: dsig.RSASHA256SignatureMethod,
	}
	metaXML, err := xml.Marshal(idp.Metadata())
	if err != nil {
		t.Fatalf("marshal IdP metadata: %v", err)
	}
	metaStr := string(metaXML)

	spCert, spKey := testSPKeyPair(t)
	spEntity := fmt.Sprintf("%s/v1/auth/sso/%s/metadata", acsTestPublicAPI, acsTestOrgID)
	cfg := &OrgSSOConfig{
		TenantModel: database.TenantModel{
			Model: database.Model{ID: "cfg-" + acsTestOrgID, Version: 1},
			OrgID: acsTestOrgID,
		},
		IDPMetadataXML:   &metaStr,
		IDPEntityID:      acsTestIDPEntity,
		SPEntityID:       spEntity,
		AttributeEmail:   "email",
		DefaultRole:      "member",
		Enabled:          true,
		SPCertificatePEM: &spCert,
		SPPrivateKeyPEM:  spKey,
	}
	configs := newMemorySSOConfigStore(cfg)
	manager, err := NewSSOProviderManager(SSOProviderManagerOpts{
		PublicBaseURL: acsTestPublicAPI,
		Keys:          configs,
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSSOProviderManager: %v", err)
	}

	users := newFakeUserStore()
	handoffs := newFakeSSOHandoffStore()
	members := &fakeSSOMemberStore{}
	svc := NewService(Deps{
		Users:            users,
		Sessions:         newFakeSessionStore(),
		OneTimeTokens:    newFakeOTTStore(),
		SSOConfigs:       configs,
		SSOProviders:     manager,
		SSOOrganizations: &fakeSSOOrganizationStore{bySlug: map[string]string{acsTestOrgSlug: acsTestOrgID}},
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

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler(),
	})
	RegisterRoutes(app, NewHandler(svc))

	return &acsHarness{
		svc:      svc,
		app:      app,
		users:    users,
		members:  members,
		handoffs: handoffs,
		idp:      idp,
		acsURL:   fmt.Sprintf("%s/v1/auth/sso/%s/acs", acsTestPublicAPI, acsTestOrgID),
		spEntity: spEntity,
	}
}

func (h *acsHarness) startLogin(t *testing.T) (relayState, requestID string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/sso/"+acsTestOrgSlug+"/login", nil)
	resp, err := h.app.Test(req, 5000)
	if err != nil {
		t.Fatalf("GET login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET login status = %d, body = %s", resp.StatusCode, body)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse IdP redirect: %v", err)
	}
	relayState = loc.Query().Get("RelayState")
	if relayState == "" {
		t.Fatal("login redirect missing RelayState")
	}
	state := h.handoffs.states[hashToken(relayState)]
	if state == nil || state.RequestID == "" {
		t.Fatal("login state was not persisted")
	}
	return relayState, state.RequestID
}

func (h *acsHarness) postACS(t *testing.T, samlResponse, relayState string) *http.Response {
	t.Helper()
	form := url.Values{
		"SAMLResponse": {samlResponse},
		"RelayState":   {relayState},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/sso/"+acsTestOrgID+"/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.app.Test(req, 10000)
	if err != nil {
		t.Fatalf("POST ACS: %v", err)
	}
	return resp
}

type acsAssertionOpts struct {
	unsigned bool
	signer   *saml.IdentityProvider
	mutate   func(*saml.Assertion)
}

func (h *acsHarness) samlResponse(t *testing.T, requestID string, opts acsAssertionOpts) string {
	t.Helper()
	idp := h.idp
	if opts.signer != nil {
		idp = opts.signer
	}

	httpReq := httptest.NewRequest(http.MethodPost, h.acsURL, nil)
	req := &saml.IdpAuthnRequest{
		IDP:         idp,
		HTTPRequest: httpReq,
		Now:         time.Now().UTC(),
		Request:     saml.AuthnRequest{ID: requestID, IssueInstant: time.Now().UTC()},
		ServiceProviderMetadata: &saml.EntityDescriptor{
			EntityID: h.spEntity,
		},
		SPSSODescriptor: &saml.SPSSODescriptor{},
		ACSEndpoint: &saml.IndexedEndpoint{
			Binding:  saml.HTTPPostBinding,
			Location: h.acsURL,
		},
	}

	session := &saml.Session{
		ID:            "sess-acs",
		CreateTime:    req.Now,
		ExpireTime:    req.Now.Add(time.Hour),
		Index:         "idx-acs",
		NameID:        acsTestEmail,
		UserEmail:     acsTestEmail,
		UserGivenName: "ACS",
		UserSurname:   "Tester",
		CustomAttributes: []saml.Attribute{{
			Name: "email",
			Values: []saml.AttributeValue{{
				Type:  "xs:string",
				Value: acsTestEmail,
			}},
		}},
	}
	if err := (saml.DefaultAssertionMaker{}).MakeAssertion(req, session); err != nil {
		t.Fatalf("MakeAssertion: %v", err)
	}
	if opts.mutate != nil {
		opts.mutate(req.Assertion)
	}

	if opts.unsigned {
		if err := writeUnsignedResponse(req); err != nil {
			t.Fatalf("unsigned response: %v", err)
		}
	} else {
		if err := req.MakeResponse(); err != nil {
			t.Fatalf("MakeResponse: %v", err)
		}
	}

	form, err := req.PostBinding()
	if err != nil {
		t.Fatalf("PostBinding: %v", err)
	}
	return form.SAMLResponse
}

func writeUnsignedResponse(req *saml.IdpAuthnRequest) error {
	req.AssertionEl = req.Assertion.Element()
	response := &saml.Response{
		Destination:  req.ACSEndpoint.Location,
		ID:           fmt.Sprintf("id-unsigned-%x", time.Now().UnixNano()),
		InResponseTo: req.Request.ID,
		IssueInstant: req.Now,
		Version:      "2.0",
		Issuer: &saml.Issuer{
			Format: "urn:oasis:names:tc:SAML:2.0:nameid-format:entity",
			Value:  req.IDP.MetadataURL.String(),
		},
		Status: saml.Status{
			StatusCode: saml.StatusCode{Value: saml.StatusSuccess},
		},
	}
	responseEl := response.Element()
	responseEl.AddChild(req.AssertionEl)
	req.ResponseEl = responseEl
	return nil
}

func (h *acsHarness) assertNoProvisioning(t *testing.T) {
	t.Helper()
	if len(h.users.byID) != 0 || len(h.users.byEmail) != 0 {
		t.Fatalf("expected no User rows, got %d", len(h.users.byID))
	}
	if h.members.calls != 0 {
		t.Fatalf("expected no OrgMember Ensure calls, got %d", h.members.calls)
	}
	if len(h.handoffs.exchanges) != 0 {
		t.Fatalf("expected no SSOExchangeCode rows, got %d", len(h.handoffs.exchanges))
	}
}

func assertACSRejected(t *testing.T, resp *http.Response) {
	t.Helper()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
		t.Fatalf("expected rejection, got redirect to %s", resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, body)
	}
	loc := resp.Header.Get("Location")
	if strings.Contains(loc, "code=") {
		t.Fatalf("rejection included exchange code redirect %s", loc)
	}
	if !bytes.Contains(body, []byte(apperrors.CodeUnauthenticated)) && !bytes.Contains(body, []byte("authentication failed")) {
		t.Fatalf("expected unauthenticated error envelope, body=%s", body)
	}
}

func TestSSOACS_ValidAssertionRedirectsWithExchangeCode(t *testing.T) {
	h := newACSHarness(t)
	relayState, requestID := h.startLogin(t)
	samlResp := h.samlResponse(t, requestID, acsAssertionOpts{})

	resp := h.postACS(t, samlResp, relayState)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("valid ACS status = %d, body = %s", resp.StatusCode, body)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse callback: %v", err)
	}
	if loc.Query().Get("code") == "" || loc.Query().Get("orgId") != acsTestOrgID {
		t.Fatalf("callback missing code/orgId: %s", loc)
	}
	if loc.Query().Get("accessToken") != "" || loc.Query().Get("refreshToken") != "" {
		t.Fatal("tokens must not appear on the ACS redirect")
	}
	if _, ok := h.users.byEmail[acsTestEmail]; !ok {
		t.Fatal("expected JIT user to be created")
	}
	if h.members.calls != 1 {
		t.Fatalf("OrgMember Ensure calls = %d, want 1", h.members.calls)
	}
	if len(h.handoffs.exchanges) != 1 {
		t.Fatalf("SSOExchangeCode rows = %d, want 1", len(h.handoffs.exchanges))
	}
}

func TestSSOACS_RejectsUnsignedAssertion(t *testing.T) {
	h := newACSHarness(t)
	relayState, requestID := h.startLogin(t)
	samlResp := h.samlResponse(t, requestID, acsAssertionOpts{unsigned: true})
	raw, err := base64.StdEncoding.DecodeString(samlResp)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("Signature")) {
		t.Fatal("unsigned fixture unexpectedly contains a Signature element")
	}

	resp := h.postACS(t, samlResp, relayState)
	assertACSRejected(t, resp)
	h.assertNoProvisioning(t)
}

func TestSSOACS_RejectsWrongSignature(t *testing.T) {
	h := newACSHarness(t)
	wrongKey, wrongCert := testIDPKeyPair(t)
	wrongIDP := &saml.IdentityProvider{
		Key:             wrongKey,
		Certificate:     wrongCert,
		MetadataURL:     h.idp.MetadataURL,
		SSOURL:          h.idp.SSOURL,
		SignatureMethod: dsig.RSASHA256SignatureMethod,
	}

	relayState, requestID := h.startLogin(t)
	samlResp := h.samlResponse(t, requestID, acsAssertionOpts{signer: wrongIDP})
	raw, _ := base64.StdEncoding.DecodeString(samlResp)
	if !bytes.Contains(raw, []byte("Signature")) {
		t.Fatal("wrong-signer fixture is missing a Signature element")
	}

	resp := h.postACS(t, samlResp, relayState)
	assertACSRejected(t, resp)
	h.assertNoProvisioning(t)
}

func TestSSOACS_RejectsWrongAudience(t *testing.T) {
	h := newACSHarness(t)
	relayState, requestID := h.startLogin(t)
	samlResp := h.samlResponse(t, requestID, acsAssertionOpts{
		mutate: func(a *saml.Assertion) {
			a.Conditions.AudienceRestrictions = []saml.AudienceRestriction{{
				Audience: saml.Audience{Value: "https://evil.example.com/sp"},
			}}
		},
	})

	resp := h.postACS(t, samlResp, relayState)
	assertACSRejected(t, resp)
	h.assertNoProvisioning(t)
}

func TestSSOACS_RejectsExpiredAssertion(t *testing.T) {
	h := newACSHarness(t)
	relayState, requestID := h.startLogin(t)
	expired := time.Now().UTC().Add(-2 * time.Hour)
	samlResp := h.samlResponse(t, requestID, acsAssertionOpts{
		mutate: func(a *saml.Assertion) {
			a.Conditions.NotOnOrAfter = expired
			a.Conditions.NotBefore = expired.Add(-time.Hour)
			for i := range a.Subject.SubjectConfirmations {
				a.Subject.SubjectConfirmations[i].SubjectConfirmationData.NotOnOrAfter = expired
			}
		},
	})

	resp := h.postACS(t, samlResp, relayState)
	assertACSRejected(t, resp)
	h.assertNoProvisioning(t)
}

func TestSSOACS_RejectsNotYetValidAssertion(t *testing.T) {
	h := newACSHarness(t)
	relayState, requestID := h.startLogin(t)
	future := time.Now().UTC().Add(2 * time.Hour)
	samlResp := h.samlResponse(t, requestID, acsAssertionOpts{
		mutate: func(a *saml.Assertion) {
			a.Conditions.NotBefore = future
			a.Conditions.NotOnOrAfter = future.Add(time.Hour)
		},
	})

	resp := h.postACS(t, samlResp, relayState)
	assertACSRejected(t, resp)
	h.assertNoProvisioning(t)
}

func testIDPKeyPair(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate IdP key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-idp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create IdP cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse IdP cert: %v", err)
	}
	return key, cert
}

func mustParseURL(t *testing.T, raw string) url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return *u
}
