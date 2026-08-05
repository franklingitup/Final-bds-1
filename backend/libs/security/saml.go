package security

import (
	"compress/flate"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SAMLProvider handles SAML 2.0 authentication.
type SAMLProvider struct {
	config SAMLConfig
	idpMetadata *IDPMetadata
	mu sync.RWMutex
}

// SAMLConfig configures a SAML service provider.
type SAMLConfig struct {
	// Provider identification
	ProviderID   string
	ProviderName string

	// Service Provider (SP) configuration
	EntityID         string // Our entity ID
	AssertionConsumerServiceURL string // Where IdP posts assertions
	SingleLogoutServiceURL string // Optional SLO endpoint

	// Identity Provider (IdP) configuration
	IDPMetadataURL string // URL to fetch IdP metadata
	IDPEntityID    string // IdP's entity ID
	IDPSSOURL      string // IdP's SSO URL (for redirect binding)

	// Certificates
	SPCertificate string // SP's public certificate (PEM)
	SPPrivateKey  string // SP's private key (PEM)
	IDPCertificate string // IdP's public certificate (PEM)

	// Options
	SignRequests        bool
	WantAssertionsSigned bool
	AllowUnencryptedAssertions bool

	// Attribute mapping
	AttributeMap SAMLAttributeMap
}

// SAMLAttributeMap maps SAML attributes to user fields.
type SAMLAttributeMap struct {
	Email     string
	FirstName string
	LastName  string
	Groups    string
	Username  string
}

// DefaultSAMLAttributeMap returns common attribute names.
func DefaultSAMLAttributeMap() SAMLAttributeMap {
	return SAMLAttributeMap{
		Email:     "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
		FirstName: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname",
		LastName:  "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname",
		Groups:    "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
		Username:  "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
	}
}

// IDPMetadata represents IdP SAML metadata.
type IDPMetadata struct {
	EntityID          string
	SSOServiceURL     string
	SSOBinding        string
	SLOServiceURL     string
	SLOBinding        string
	Certificate       *x509.Certificate
	NameIDFormats     []string
}

// SAMLAuthnRequest represents a SAML authentication request.
type SAMLAuthnRequest struct {
	XMLName      xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:protocol AuthnRequest"`
	ID           string   `xml:"ID,attr"`
	Version      string   `xml:"Version,attr"`
	IssueInstant string   `xml:"IssueInstant,attr"`
	Destination  string   `xml:"Destination,attr"`
	AssertionConsumerServiceURL string `xml:"AssertionConsumerServiceURL,attr"`
	ProtocolBinding string `xml:"ProtocolBinding,attr"`
	Issuer       SAMLIssuer `xml:"urn:oasis:names:tc:SAML:2.0:assertion Issuer"`
	NameIDPolicy *SAMLNameIDPolicy `xml:"urn:oasis:names:tc:SAML:2.0:protocol NameIDPolicy,omitempty"`
}

// SAMLIssuer represents the issuer element.
type SAMLIssuer struct {
	XMLName xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion Issuer"`
	Value   string   `xml:",chardata"`
}

// SAMLNameIDPolicy specifies name ID requirements.
type SAMLNameIDPolicy struct {
	XMLName     xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:protocol NameIDPolicy"`
	Format      string   `xml:"Format,attr,omitempty"`
	AllowCreate bool     `xml:"AllowCreate,attr,omitempty"`
}

// SAMLResponse represents a SAML response.
type SAMLResponse struct {
	XMLName      xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:protocol Response"`
	ID           string   `xml:"ID,attr"`
	Version      string   `xml:"Version,attr"`
	IssueInstant string   `xml:"IssueInstant,attr"`
	Destination  string   `xml:"Destination,attr,omitempty"`
	InResponseTo string   `xml:"InResponseTo,attr,omitempty"`
	Status       SAMLStatus `xml:"urn:oasis:names:tc:SAML:2.0:protocol Status"`
	Assertions   []SAMLAssertion `xml:"urn:oasis:names:tc:SAML:2.0:assertion Assertion"`
}

// SAMLStatus represents the status of a SAML response.
type SAMLStatus struct {
	XMLName    xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:protocol Status"`
	StatusCode SAMLStatusCode `xml:"urn:oasis:names:tc:SAML:2.0:protocol StatusCode"`
}

// SAMLStatusCode is a status code value.
type SAMLStatusCode struct {
	XMLName xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:protocol StatusCode"`
	Value   string   `xml:"Value,attr"`
}

// SAMLAssertion represents a SAML assertion.
type SAMLAssertion struct {
	XMLName      xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion Assertion"`
	ID           string   `xml:"ID,attr"`
	Version      string   `xml:"Version,attr"`
	IssueInstant string   `xml:"IssueInstant,attr"`
	Issuer       SAMLIssuer `xml:"urn:oasis:names:tc:SAML:2.0:assertion Issuer"`
	Subject      SAMLSubject `xml:"urn:oasis:names:tc:SAML:2.0:assertion Subject"`
	Conditions   SAMLConditions `xml:"urn:oasis:names:tc:SAML:2.0:assertion Conditions"`
	AttributeStatement SAMLAttributeStatement `xml:"urn:oasis:names:tc:SAML:2.0:assertion AttributeStatement"`
	AuthnStatement SAMLAuthnStatement `xml:"urn:oasis:names:tc:SAML:2.0:assertion AuthnStatement"`
}

// SAMLSubject identifies the principal.
type SAMLSubject struct {
	XMLName xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion Subject"`
	NameID  SAMLNameID `xml:"urn:oasis:names:tc:SAML:2.0:assertion NameID"`
	SubjectConfirmation SAMLSubjectConfirmation `xml:"urn:oasis:names:tc:SAML:2.0:assertion SubjectConfirmation"`
}

// SAMLNameID identifies the subject.
type SAMLNameID struct {
	XMLName xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion NameID"`
	Format  string   `xml:"Format,attr,omitempty"`
	Value   string   `xml:",chardata"`
}

// SAMLSubjectConfirmation confirms the subject.
type SAMLSubjectConfirmation struct {
	XMLName xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion SubjectConfirmation"`
	Method  string   `xml:"Method,attr"`
	SubjectConfirmationData SAMLSubjectConfirmationData `xml:"urn:oasis:names:tc:SAML:2.0:assertion SubjectConfirmationData"`
}

// SAMLSubjectConfirmationData holds confirmation data.
type SAMLSubjectConfirmationData struct {
	XMLName      xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion SubjectConfirmationData"`
	NotOnOrAfter string   `xml:"NotOnOrAfter,attr,omitempty"`
	Recipient    string   `xml:"Recipient,attr,omitempty"`
	InResponseTo string   `xml:"InResponseTo,attr,omitempty"`
}

// SAMLConditions specifies assertion validity.
type SAMLConditions struct {
	XMLName      xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion Conditions"`
	NotBefore    string   `xml:"NotBefore,attr,omitempty"`
	NotOnOrAfter string   `xml:"NotOnOrAfter,attr,omitempty"`
	AudienceRestrictions []SAMLAudienceRestriction `xml:"urn:oasis:names:tc:SAML:2.0:assertion AudienceRestriction"`
}

// SAMLAudienceRestriction limits assertion audience.
type SAMLAudienceRestriction struct {
	XMLName  xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion AudienceRestriction"`
	Audience []string `xml:"urn:oasis:names:tc:SAML:2.0:assertion Audience"`
}

// SAMLAttributeStatement contains attributes.
type SAMLAttributeStatement struct {
	XMLName    xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion AttributeStatement"`
	Attributes []SAMLAttribute `xml:"urn:oasis:names:tc:SAML:2.0:assertion Attribute"`
}

// SAMLAttribute is a SAML attribute.
type SAMLAttribute struct {
	XMLName    xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion Attribute"`
	Name       string   `xml:"Name,attr"`
	NameFormat string   `xml:"NameFormat,attr,omitempty"`
	Values     []SAMLAttributeValue `xml:"urn:oasis:names:tc:SAML:2.0:assertion AttributeValue"`
}

// SAMLAttributeValue is an attribute value.
type SAMLAttributeValue struct {
	XMLName xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion AttributeValue"`
	Type    string   `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr,omitempty"`
	Value   string   `xml:",chardata"`
}

// SAMLAuthnStatement describes authentication.
type SAMLAuthnStatement struct {
	XMLName          xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion AuthnStatement"`
	AuthnInstant     string   `xml:"AuthnInstant,attr"`
	SessionIndex     string   `xml:"SessionIndex,attr,omitempty"`
	SessionNotOnOrAfter string `xml:"SessionNotOnOrAfter,attr,omitempty"`
	AuthnContext     SAMLAuthnContext `xml:"urn:oasis:names:tc:SAML:2.0:assertion AuthnContext"`
}

// SAMLAuthnContext describes authentication context.
type SAMLAuthnContext struct {
	XMLName              xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:assertion AuthnContext"`
	AuthnContextClassRef string   `xml:"urn:oasis:names:tc:SAML:2.0:assertion AuthnContextClassRef"`
}

// SAMLUser represents a user extracted from SAML.
type SAMLUser struct {
	SubjectID   string
	Email       string
	FirstName   string
	LastName    string
	Username    string
	Groups      []string
	Attributes  map[string][]string
	SessionIndex string
}

// NewSAMLProvider creates a new SAML provider.
func NewSAMLProvider(cfg SAMLConfig) *SAMLProvider {
	if cfg.AttributeMap.Email == "" {
		cfg.AttributeMap = DefaultSAMLAttributeMap()
	}
	return &SAMLProvider{
		config: cfg,
	}
}

// GenerateAuthnRequest creates a SAML authentication request.
func (p *SAMLProvider) GenerateAuthnRequest(requestID string) (*SAMLAuthnRequest, error) {
	return &SAMLAuthnRequest{
		ID:           requestID,
		Version:      "2.0",
		IssueInstant: time.Now().UTC().Format(time.RFC3339),
		Destination:  p.config.IDPSSOURL,
		AssertionConsumerServiceURL: p.config.AssertionConsumerServiceURL,
		ProtocolBinding: "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
		Issuer: SAMLIssuer{
			Value: p.config.EntityID,
		},
		NameIDPolicy: &SAMLNameIDPolicy{
			Format:      "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
			AllowCreate: true,
		},
	}, nil
}

// GenerateRedirectURL creates the redirect URL for SP-initiated SSO.
func (p *SAMLProvider) GenerateRedirectURL(relayState string) (string, string, error) {
	// Generate request ID
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return "", "", err
	}
	requestID := "_" + base64.RawURLEncoding.EncodeToString(idBytes)

	// Create AuthnRequest
	authnReq, err := p.GenerateAuthnRequest(requestID)
	if err != nil {
		return "", "", err
	}

	// Marshal to XML
	xmlBytes, err := xml.Marshal(authnReq)
	if err != nil {
		return "", "", err
	}

	// Deflate compress
	var compressed strings.Builder
	w, _ := flate.NewWriter(&compressed, flate.BestCompression)
	w.Write(xmlBytes)
	w.Close()

	// Base64 encode
	encoded := base64.StdEncoding.EncodeToString([]byte(compressed.String()))

	// Build URL
	u, err := url.Parse(p.config.IDPSSOURL)
	if err != nil {
		return "", "", err
	}

	q := u.Query()
	q.Set("SAMLRequest", encoded)
	if relayState != "" {
		q.Set("RelayState", relayState)
	}
	u.RawQuery = q.Encode()

	return u.String(), requestID, nil
}

// ParseResponse parses and validates a SAML response.
func (p *SAMLProvider) ParseResponse(ctx context.Context, samlResponse string) (*SAMLResponse, error) {
	// Base64 decode
	decoded, err := base64.StdEncoding.DecodeString(samlResponse)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Parse XML
	var response SAMLResponse
	if err := xml.Unmarshal(decoded, &response); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Validate status
	if response.Status.StatusCode.Value != "urn:oasis:names:tc:SAML:2.0:status:Success" {
		return nil, fmt.Errorf("SAML authentication failed: %s", response.Status.StatusCode.Value)
	}

	return &response, nil
}

// ExtractUser extracts user information from a SAML response.
func (p *SAMLProvider) ExtractUser(response *SAMLResponse) (*SAMLUser, error) {
	if len(response.Assertions) == 0 {
		return nil, fmt.Errorf("no assertions in response")
	}

	assertion := response.Assertions[0]
	user := &SAMLUser{
		SubjectID:    assertion.Subject.NameID.Value,
		Attributes:   make(map[string][]string),
		SessionIndex: assertion.AuthnStatement.SessionIndex,
	}

	// Extract attributes
	for _, attr := range assertion.AttributeStatement.Attributes {
		values := make([]string, len(attr.Values))
		for i, v := range attr.Values {
			values[i] = v.Value
		}
		user.Attributes[attr.Name] = values

		// Map known attributes
		switch attr.Name {
		case p.config.AttributeMap.Email:
			if len(values) > 0 {
				user.Email = values[0]
			}
		case p.config.AttributeMap.FirstName:
			if len(values) > 0 {
				user.FirstName = values[0]
			}
		case p.config.AttributeMap.LastName:
			if len(values) > 0 {
				user.LastName = values[0]
			}
		case p.config.AttributeMap.Username:
			if len(values) > 0 {
				user.Username = values[0]
			}
		case p.config.AttributeMap.Groups:
			user.Groups = values
		}
	}

	// Fall back to NameID for email if not in attributes
	if user.Email == "" && strings.Contains(assertion.Subject.NameID.Value, "@") {
		user.Email = assertion.Subject.NameID.Value
	}

	return user, nil
}

// FetchMetadata fetches and parses IdP metadata.
func (p *SAMLProvider) FetchMetadata(ctx context.Context) error {
	if p.config.IDPMetadataURL == "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.IDPMetadataURL, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metadata fetch failed: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Parse metadata (simplified - real implementation would use proper XML parsing)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.idpMetadata = &IDPMetadata{}
	_ = body // TODO: Parse actual metadata XML

	return nil
}

// GenerateSPMetadata generates SP metadata XML.
func (p *SAMLProvider) GenerateSPMetadata() (string, error) {
	metadata := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
  <md:SPSSODescriptor AuthnRequestsSigned="%t" WantAssertionsSigned="%t" protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:NameIDFormat>urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress</md:NameIDFormat>
    <md:AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="%s" index="0" isDefault="true"/>
  </md:SPSSODescriptor>
</md:EntityDescriptor>`,
		p.config.EntityID,
		p.config.SignRequests,
		p.config.WantAssertionsSigned,
		p.config.AssertionConsumerServiceURL,
	)

	return metadata, nil
}

// GenerateRequestID generates a unique request ID.
func GenerateRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// ParseCertificate parses a PEM-encoded certificate.
func ParseCertificate(pemData string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

// ParsePrivateKey parses a PEM-encoded RSA private key.
func ParsePrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// ComputeSHA256 computes SHA256 hash.
func ComputeSHA256(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

// SAMLProviderRegistry manages multiple SAML providers.
type SAMLProviderRegistry struct {
	providers map[string]*SAMLProvider
	mu        sync.RWMutex
}

// NewSAMLProviderRegistry creates a new registry.
func NewSAMLProviderRegistry() *SAMLProviderRegistry {
	return &SAMLProviderRegistry{
		providers: make(map[string]*SAMLProvider),
	}
}

// Register adds a provider to the registry.
func (r *SAMLProviderRegistry) Register(provider *SAMLProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[provider.config.ProviderID] = provider
}

// Get retrieves a provider by ID.
func (r *SAMLProviderRegistry) Get(providerID string) (*SAMLProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	return p, ok
}

// List returns all registered provider IDs.
func (r *SAMLProviderRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}
