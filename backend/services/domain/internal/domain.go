// Package domain manages custom domains, DNS verification, ingress bindings, and
// TLS certificate lifecycle.
package domain

import (
	"encoding/json"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// Verification status constants.
const (
	VerificationPending   = "pending"
	VerificationVerifying = "verifying"
	VerificationVerified  = "verified"
	VerificationFailed    = "failed"
)

// Verification method constants.
const (
	VerifyDNSTXT   = "dns_txt"
	VerifyDNSCNAME = "dns_cname"
	VerifyHTTP     = "http"
)

// Domain status constants.
const (
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusDeleted   = "deleted"
)

// Certificate status constants.
const (
	CertPending  = "pending"
	CertIssuing  = "issuing"
	CertActive   = "active"
	CertExpired  = "expired"
	CertRevoked  = "revoked"
	CertFailed   = "failed"
)

// Challenge type constants.
const (
	ChallengeHTTP01 = "http-01"
	ChallengeDNS01  = "dns-01"
)

// Ingress status constants.
const (
	IngressPending = "pending"
	IngressSynced  = "synced"
	IngressFailed  = "failed"
)

// Domain represents a custom domain attached to a deployment.
type Domain struct {
	database.TenantModel
	DeploymentID       string     `db:"deployment_id"`
	Domain             string     `db:"domain"`
	Subdomain          *string    `db:"subdomain"`
	FullDomain         string     `db:"full_domain"`
	VerificationStatus string     `db:"verification_status"`
	VerificationToken  string     `db:"verification_token"`
	VerificationMethod string     `db:"verification_method"`
	VerifiedAt         *time.Time `db:"verified_at"`
	LastCheckAt        *time.Time `db:"last_check_at"`
	VerificationError  *string    `db:"verification_error"`
	DNSTxtName         string     `db:"dns_txt_name"`
	DNSTxtValue        string     `db:"dns_txt_value"`
	DNSCnameTarget     *string    `db:"dns_cname_target"`
	Status             string     `db:"status"`
	IsPrimary          bool       `db:"is_primary"`
	CreatedBy          *string    `db:"created_by"`
}

// Certificate represents a TLS certificate for a domain.
type Certificate struct {
	ID             string          `db:"id"`
	OrgID          string          `db:"org_id"`
	DomainID       string          `db:"domain_id"`
	CommonName     string          `db:"common_name"`
	SANDomains     json.RawMessage `db:"san_domains"`
	Issuer         string          `db:"issuer"`
	CertificatePEM []byte          `db:"certificate_pem"`
	PrivateKeyPEM  []byte          `db:"private_key_pem"`
	IssuedAt       *time.Time      `db:"issued_at"`
	ExpiresAt      *time.Time      `db:"expires_at"`
	Status         string          `db:"status"`
	LastRenewalAt  *time.Time      `db:"last_renewal_at"`
	RenewalError   *string         `db:"renewal_error"`
	ACMEOrderURL   *string         `db:"acme_order_url"`
	ACMECertURL    *string         `db:"acme_cert_url"`
	CreatedAt      time.Time       `db:"created_at"`
	UpdatedAt      time.Time       `db:"updated_at"`
}

// ACMEChallenge represents an ACME challenge for domain verification.
type ACMEChallenge struct {
	ID            string     `db:"id"`
	OrgID         string     `db:"org_id"`
	DomainID      string     `db:"domain_id"`
	ChallengeType string     `db:"challenge_type"`
	Token         string     `db:"token"`
	KeyAuth       string     `db:"key_auth"`
	Status        string     `db:"status"`
	ValidatedAt   *time.Time `db:"validated_at"`
	ExpiresAt     time.Time  `db:"expires_at"`
	CreatedAt     time.Time  `db:"created_at"`
}

// IngressRecord represents a Kubernetes Ingress for a domain.
type IngressRecord struct {
	ID                 string     `db:"id"`
	OrgID              string     `db:"org_id"`
	DomainID           string     `db:"domain_id"`
	ClusterID          string     `db:"cluster_id"`
	IngressName        string     `db:"ingress_name"`
	Namespace          string     `db:"namespace"`
	IngressClass       string     `db:"ingress_class"`
	ServiceName        string     `db:"service_name"`
	ServicePort        int        `db:"service_port"`
	Path               string     `db:"path"`
	PathType           string     `db:"path_type"`
	TLSSecretName      *string    `db:"tls_secret_name"`
	Status             string     `db:"status"`
	LastSyncedAt       *time.Time `db:"last_synced_at"`
	SyncError          *string    `db:"sync_error"`
	ManifestHash       *string    `db:"manifest_hash"`
	Generation         int64      `db:"generation"`
	ObservedGeneration int64      `db:"observed_generation"`
	CreatedAt          time.Time  `db:"created_at"`
	UpdatedAt          time.Time  `db:"updated_at"`
}

// DomainEvent represents an event in the domain lifecycle.
type DomainEvent struct {
	ID        string          `db:"id"`
	OrgID     string          `db:"org_id"`
	DomainID  string          `db:"domain_id"`
	EventType string          `db:"event_type"`
	Message   string          `db:"message"`
	Details   json.RawMessage `db:"details"`
	CreatedBy *string         `db:"created_by"`
	CreatedAt time.Time       `db:"created_at"`
}

// ----------------------------------------------------------------------------
// Request DTOs
// ----------------------------------------------------------------------------

// CreateDomainRequest is the request to create a custom domain.
type CreateDomainRequest struct {
	DeploymentID string  `json:"deploymentId"`
	Domain       string  `json:"domain"`
	Subdomain    *string `json:"subdomain,omitempty"`
}

// VerifyDomainRequest is the request to verify domain ownership.
type VerifyDomainRequest struct {
	Force bool `json:"force,omitempty"` // Force re-verification
}

// IssueCertificateRequest is the request to issue a TLS certificate.
type IssueCertificateRequest struct {
	ForceRenewal bool `json:"forceRenewal,omitempty"`
}

// UpdateDomainRequest is the request to update a domain.
type UpdateDomainRequest struct {
	IsPrimary *bool `json:"isPrimary,omitempty"`
}

// ----------------------------------------------------------------------------
// View Models
// ----------------------------------------------------------------------------

// DomainView is the API response for a domain.
type DomainView struct {
	ID                 string     `json:"id"`
	OrgID              string     `json:"organizationId"`
	DeploymentID       string     `json:"deploymentId"`
	Domain             string     `json:"domain"`
	Subdomain          *string    `json:"subdomain,omitempty"`
	FullDomain         string     `json:"fullDomain"`
	VerificationStatus string     `json:"verificationStatus"`
	VerificationMethod string     `json:"verificationMethod"`
	VerifiedAt         *time.Time `json:"verifiedAt,omitempty"`
	DNSRecords         DNSRecords `json:"dnsRecords"`
	Status             string     `json:"status"`
	IsPrimary          bool       `json:"isPrimary"`
	Certificate        *CertView  `json:"certificate,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
}

// DNSRecords contains the DNS records needed for verification.
type DNSRecords struct {
	TXTName   string  `json:"txtName"`
	TXTValue  string  `json:"txtValue"`
	CNAMEHost *string `json:"cnameHost,omitempty"`
}

func ToDomainView(d *Domain, cert *Certificate) DomainView {
	view := DomainView{
		ID:                 d.ID,
		OrgID:              d.OrgID,
		DeploymentID:       d.DeploymentID,
		Domain:             d.Domain,
		Subdomain:          d.Subdomain,
		FullDomain:         d.FullDomain,
		VerificationStatus: d.VerificationStatus,
		VerificationMethod: d.VerificationMethod,
		VerifiedAt:         d.VerifiedAt,
		DNSRecords: DNSRecords{
			TXTName:   d.DNSTxtName,
			TXTValue:  d.DNSTxtValue,
			CNAMEHost: d.DNSCnameTarget,
		},
		Status:    d.Status,
		IsPrimary: d.IsPrimary,
		CreatedAt: d.CreatedAt,
	}
	if cert != nil {
		view.Certificate = ToCertView(cert)
	}
	return view
}

// CertView is the API response for a certificate.
type CertView struct {
	ID           string     `json:"id"`
	CommonName   string     `json:"commonName"`
	Issuer       string     `json:"issuer"`
	Status       string     `json:"status"`
	IssuedAt     *time.Time `json:"issuedAt,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	DaysToExpiry *int       `json:"daysToExpiry,omitempty"`
}

func ToCertView(c *Certificate) *CertView {
	if c == nil {
		return nil
	}
	view := &CertView{
		ID:         c.ID,
		CommonName: c.CommonName,
		Issuer:     c.Issuer,
		Status:     c.Status,
		IssuedAt:   c.IssuedAt,
		ExpiresAt:  c.ExpiresAt,
	}
	if c.ExpiresAt != nil {
		days := int(time.Until(*c.ExpiresAt).Hours() / 24)
		view.DaysToExpiry = &days
	}
	return view
}

// IngressView is the API response for an ingress record.
type IngressView struct {
	ID            string     `json:"id"`
	IngressName   string     `json:"ingressName"`
	Namespace     string     `json:"namespace"`
	IngressClass  string     `json:"ingressClass"`
	ServiceName   string     `json:"serviceName"`
	ServicePort   int        `json:"servicePort"`
	Path          string     `json:"path"`
	TLSSecretName *string    `json:"tlsSecretName,omitempty"`
	Status        string     `json:"status"`
	LastSyncedAt  *time.Time `json:"lastSyncedAt,omitempty"`
	Generation    int64      `json:"generation"`
}

func ToIngressView(i *IngressRecord) IngressView {
	return IngressView{
		ID:            i.ID,
		IngressName:   i.IngressName,
		Namespace:     i.Namespace,
		IngressClass:  i.IngressClass,
		ServiceName:   i.ServiceName,
		ServicePort:   i.ServicePort,
		Path:          i.Path,
		TLSSecretName: i.TLSSecretName,
		Status:        i.Status,
		LastSyncedAt:  i.LastSyncedAt,
		Generation:    i.Generation,
	}
}

// DomainEventView is the API response for a domain event.
type DomainEventView struct {
	ID        string          `json:"id"`
	EventType string          `json:"eventType"`
	Message   string          `json:"message"`
	Details   json.RawMessage `json:"details,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

func ToDomainEventView(e *DomainEvent) DomainEventView {
	return DomainEventView{
		ID:        e.ID,
		EventType: e.EventType,
		Message:   e.Message,
		Details:   e.Details,
		CreatedAt: e.CreatedAt,
	}
}

// ACMEChallengeResponse is returned for HTTP-01 challenges.
type ACMEChallengeResponse struct {
	Token   string `json:"token"`
	KeyAuth string `json:"keyAuth"`
}
