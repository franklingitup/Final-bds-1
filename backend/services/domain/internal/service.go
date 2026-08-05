package domain

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// DeploymentReader reads deployment information.
type DeploymentReader interface {
	GetDeployment(ctx context.Context, id string) (*DeploymentInfo, error)
}

// DeploymentInfo contains deployment details.
type DeploymentInfo struct {
	ID          string
	OrgID       string
	ClusterID   string
	ServiceName string
	ServicePort int
	Namespace   string
}

// TokenEncryptor encrypts/decrypts certificate data.
type TokenEncryptor interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// Deps holds service dependencies.
type Deps struct {
	Domains       DomainStore
	Certificates  CertificateStore
	Challenges    ACMEChallengeStore
	Ingresses     IngressStore
	Events        DomainEventStore
	Deployments   DeploymentReader
	OrgMembers    authz.OrgMemberStore
	Outbox        events.Outbox
	Tenant        TenantRunner
	Encryptor     TokenEncryptor
	ACMEConfig    *ACMEConfig
	Logger        *slog.Logger
}

// Service implements domain management logic.
type Service struct {
	domains      DomainStore
	certs        CertificateStore
	challenges   ACMEChallengeStore
	ingresses    IngressStore
	events       DomainEventStore
	deployments  DeploymentReader
	orgMembers   authz.OrgMemberStore
	outbox       events.Outbox
	tenant       TenantRunner
	encryptor    TokenEncryptor
	authSvc      *authz.AuthorizationService
	dnsVerifier  *DNSVerifier
	acmeClient   *ACMEClient
	ingressGen   *IngressGenerator
	log          *slog.Logger
}

// NewService creates a new domain service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}

	acmeCfg := DefaultACMEConfig()
	if d.ACMEConfig != nil {
		acmeCfg = *d.ACMEConfig
	}

	return &Service{
		domains:     d.Domains,
		certs:       d.Certificates,
		challenges:  d.Challenges,
		ingresses:   d.Ingresses,
		events:      d.Events,
		deployments: d.Deployments,
		orgMembers:  d.OrgMembers,
		outbox:      d.Outbox,
		tenant:      d.Tenant,
		encryptor:   d.Encryptor,
		authSvc:     authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		dnsVerifier: NewDNSVerifier(),
		acmeClient:  NewACMEClient(acmeCfg),
		ingressGen:  NewIngressGenerator(),
		log:         d.Logger,
	}
}

// ----------------------------------------------------------------------------
// Domain Operations
// ----------------------------------------------------------------------------

// CreateDomain creates a new custom domain.
func (s *Service) CreateDomain(ctx context.Context, orgID, userID string, req CreateDomainRequest) (*Domain, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	// Validate domain format
	if err := ValidateDomainFormat(req.Domain); err != nil {
		return nil, apperrors.Validation(err.Error())
	}

	if req.Subdomain != nil {
		if err := ValidateSubdomain(*req.Subdomain); err != nil {
			return nil, apperrors.Validation(err.Error())
		}
	}

	fullDomain := BuildFullDomain(req.Domain, req.Subdomain)

	// Generate verification token
	token, err := GenerateVerificationToken()
	if err != nil {
		return nil, fmt.Errorf("generate verification token: %w", err)
	}

	dom := &Domain{
		DeploymentID:       req.DeploymentID,
		Domain:             req.Domain,
		Subdomain:          req.Subdomain,
		FullDomain:         fullDomain,
		VerificationStatus: VerificationPending,
		VerificationToken:  token,
		VerificationMethod: VerifyDNSTXT,
		DNSTxtName:         BuildTXTRecordName(fullDomain),
		DNSTxtValue:        token,
		Status:             StatusPending,
	}
	dom.OrgID = orgID
	dom.CreatedBy = &userID

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Verify deployment exists
		_, err := s.deployments.GetDeployment(ctx, req.DeploymentID)
		if err != nil {
			return apperrors.Validation("deployment not found")
		}

		// Check if domain already exists
		existing, _ := s.domains.GetByFullDomain(ctx, fullDomain)
		if existing != nil {
			return apperrors.Conflict("domain already exists")
		}

		if err := s.domains.Create(ctx, dom); err != nil {
			return err
		}

		// Log event
		return s.logEvent(ctx, orgID, dom.ID, "created", "Domain created, awaiting verification", userID,
			map[string]any{"domain": fullDomain, "verificationMethod": VerifyDNSTXT})
	})
	if err != nil {
		return nil, err
	}

	return dom, nil
}

// GetDomain returns a domain by ID.
func (s *Service) GetDomain(ctx context.Context, orgID, userID, domainID string) (*Domain, *Certificate, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, nil, err
	}

	var dom *Domain
	var cert *Certificate
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		dom, err = s.domains.GetByID(ctx, domainID)
		if err != nil {
			return err
		}
		cert, _ = s.certs.GetByDomainID(ctx, domainID) // Certificate may not exist yet
		return nil
	})
	return dom, cert, err
}

// ListDomains returns domains for an organization.
func (s *Service) ListDomains(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[Domain], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Domain]{}, err
	}

	var out database.Page[Domain]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.domains.List(ctx, orgID, page)
		return err
	})
	return out, err
}

// ListDomainsByDeployment returns domains for a deployment.
func (s *Service) ListDomainsByDeployment(ctx context.Context, orgID, userID, deploymentID string, page database.PageRequest) (database.Page[Domain], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Domain]{}, err
	}

	var out database.Page[Domain]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.domains.ListByDeployment(ctx, deploymentID, page)
		return err
	})
	return out, err
}

// UpdateDomain updates a domain.
func (s *Service) UpdateDomain(ctx context.Context, orgID, userID, domainID string, req UpdateDomainRequest) (*Domain, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var dom *Domain
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		dom, err = s.domains.GetByID(ctx, domainID)
		if err != nil {
			return err
		}

		if req.IsPrimary != nil {
			dom.IsPrimary = *req.IsPrimary
		}

		return s.domains.Update(ctx, dom)
	})
	return dom, err
}

// DeleteDomain deletes a domain.
func (s *Service) DeleteDomain(ctx context.Context, orgID, userID, domainID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.domains.Delete(ctx, domainID); err != nil {
			return err
		}
		return s.logEvent(ctx, orgID, domainID, "deleted", "Domain deleted", userID, nil)
	})
}

// ----------------------------------------------------------------------------
// Verification
// ----------------------------------------------------------------------------

// VerifyDomain verifies domain ownership via DNS.
func (s *Service) VerifyDomain(ctx context.Context, orgID, userID, domainID string, force bool) (*Domain, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var dom *Domain
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		dom, err = s.domains.GetByID(ctx, domainID)
		if err != nil {
			return err
		}

		// Skip if already verified (unless force)
		if dom.VerificationStatus == VerificationVerified && !force {
			return nil
		}

		// Mark as verifying
		if err := s.domains.UpdateVerification(ctx, domainID, VerificationVerifying, nil); err != nil {
			return err
		}

		// Perform DNS verification
		verified, verifyErr := s.dnsVerifier.VerifyTXTRecord(ctx, dom.DNSTxtName, dom.DNSTxtValue)

		if verifyErr != nil {
			errMsg := verifyErr.Error()
			_ = s.domains.UpdateVerification(ctx, domainID, VerificationFailed, &errMsg)
			return apperrors.Validation("DNS verification failed: " + errMsg)
		}

		if !verified {
			errMsg := "TXT record not found or value mismatch"
			_ = s.domains.UpdateVerification(ctx, domainID, VerificationFailed, &errMsg)
			return apperrors.Validation(errMsg)
		}

		// Mark as verified
		if err := s.domains.UpdateVerification(ctx, domainID, VerificationVerified, nil); err != nil {
			return err
		}

		// Reload domain
		dom, _ = s.domains.GetByID(ctx, domainID)

		return s.logEvent(ctx, orgID, domainID, "verified", "Domain ownership verified via DNS TXT record", userID, nil)
	})
	if err != nil {
		return nil, err
	}

	return dom, nil
}

// ----------------------------------------------------------------------------
// Certificates
// ----------------------------------------------------------------------------

// IssueCertificate issues a TLS certificate for a verified domain.
func (s *Service) IssueCertificate(ctx context.Context, orgID, userID, domainID string, forceRenewal bool) (*Certificate, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var cert *Certificate
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		dom, err := s.domains.GetByID(ctx, domainID)
		if err != nil {
			return err
		}

		// Domain must be verified
		if dom.VerificationStatus != VerificationVerified {
			return apperrors.Validation("domain must be verified before issuing certificate")
		}

		// Check existing certificate
		existingCert, _ := s.certs.GetByDomainID(ctx, domainID)
		if existingCert != nil && existingCert.Status == CertActive && !forceRenewal {
			if existingCert.ExpiresAt != nil && !ShouldRenew(*existingCert.ExpiresAt) {
				cert = existingCert
				return nil // Certificate is still valid
			}
		}

		// Create or update certificate record
		cert = &Certificate{
			OrgID:      orgID,
			DomainID:   domainID,
			CommonName: dom.FullDomain,
			SANDomains: []byte("[]"),
			Issuer:     "letsencrypt",
			Status:     CertIssuing,
		}

		if err := s.certs.Create(ctx, cert); err != nil {
			return err
		}

		// Request ACME challenge
		challenge, err := s.acmeClient.RequestCertificate(ctx, dom.FullDomain)
		if err != nil {
			errMsg := err.Error()
			_ = s.certs.UpdateStatus(ctx, cert.ID, CertFailed, &errMsg)
			return fmt.Errorf("request certificate: %w", err)
		}

		// Store ACME challenge
		acmeChallenge := &ACMEChallenge{
			OrgID:         orgID,
			DomainID:      domainID,
			ChallengeType: challenge.Type,
			Token:         challenge.Token,
			KeyAuth:       challenge.KeyAuth,
			Status:        "pending",
			ExpiresAt:     time.Now().Add(1 * time.Hour),
		}
		if err := s.challenges.Create(ctx, acmeChallenge); err != nil {
			return err
		}

		// In a real implementation, we would wait for the challenge to be validated
		// For now, we'll complete the certificate request immediately
		result, err := s.acmeClient.CompleteCertificateRequest(ctx, challenge.OrderURL, dom.FullDomain)
		if err != nil {
			errMsg := err.Error()
			_ = s.certs.UpdateStatus(ctx, cert.ID, CertFailed, &errMsg)
			return fmt.Errorf("complete certificate request: %w", err)
		}

		// Encrypt and store certificate data
		var certPEM, keyPEM []byte
		if s.encryptor != nil {
			certPEM, _ = s.encryptor.Encrypt(result.Certificate)
			keyPEM, _ = s.encryptor.Encrypt(result.PrivateKey)
		} else {
			certPEM = result.Certificate
			keyPEM = result.PrivateKey
		}

		cert.CertificatePEM = certPEM
		cert.PrivateKeyPEM = keyPEM
		cert.IssuedAt = &result.IssuedAt
		cert.ExpiresAt = &result.ExpiresAt
		cert.Status = CertActive
		cert.ACMECertURL = &result.CertURL
		now := time.Now()
		cert.LastRenewalAt = &now

		if err := s.certs.Update(ctx, cert); err != nil {
			return err
		}

		return s.logEvent(ctx, orgID, domainID, "cert_issued", "TLS certificate issued", userID,
			map[string]any{"expiresAt": result.ExpiresAt, "issuer": "letsencrypt"})
	})
	if err != nil {
		return nil, err
	}

	return cert, nil
}

// GetCertificate returns a certificate by domain ID.
func (s *Service) GetCertificate(ctx context.Context, orgID, userID, domainID string) (*Certificate, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var cert *Certificate
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		cert, err = s.certs.GetByDomainID(ctx, domainID)
		return err
	})
	return cert, err
}

// ----------------------------------------------------------------------------
// Ingress
// ----------------------------------------------------------------------------

// CreateIngress creates an Ingress for a domain.
func (s *Service) CreateIngress(ctx context.Context, orgID, userID, domainID string) (*IngressRecord, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var ingress *IngressRecord
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		dom, err := s.domains.GetByID(ctx, domainID)
		if err != nil {
			return err
		}

		if dom.VerificationStatus != VerificationVerified {
			return apperrors.Validation("domain must be verified before creating ingress")
		}

		// Get deployment info
		dep, err := s.deployments.GetDeployment(ctx, dom.DeploymentID)
		if err != nil {
			return err
		}

		// Check for certificate
		cert, _ := s.certs.GetByDomainID(ctx, domainID)
		var tlsSecretName *string
		if cert != nil && cert.Status == CertActive {
			name := GenerateTLSSecretName(dom.FullDomain)
			tlsSecretName = &name
		}

		ingress = &IngressRecord{
			OrgID:         orgID,
			DomainID:      domainID,
			ClusterID:     dep.ClusterID,
			IngressName:   GenerateIngressName(dom.FullDomain),
			Namespace:     dep.Namespace,
			IngressClass:  "nginx",
			ServiceName:   dep.ServiceName,
			ServicePort:   dep.ServicePort,
			Path:          "/",
			PathType:      "Prefix",
			TLSSecretName: tlsSecretName,
			Status:        IngressPending,
			Generation:    1,
		}

		if err := s.ingresses.Create(ctx, ingress); err != nil {
			return err
		}

		return s.logEvent(ctx, orgID, domainID, "ingress_created", "Ingress created", userID,
			map[string]any{"ingressName": ingress.IngressName, "clusterId": dep.ClusterID})
	})
	if err != nil {
		return nil, err
	}

	return ingress, nil
}

// GetIngress returns an ingress by domain ID.
func (s *Service) GetIngress(ctx context.Context, orgID, userID, domainID string) (*IngressRecord, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var ingress *IngressRecord
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		ingress, err = s.ingresses.GetByDomainID(ctx, domainID)
		return err
	})
	return ingress, err
}

// ----------------------------------------------------------------------------
// Agent Endpoints
// ----------------------------------------------------------------------------

// GetIngressesForAgent returns pending ingresses for a cluster.
func (s *Service) GetIngressesForAgent(ctx context.Context, orgID, clusterID string) ([]AgentIngressSpec, error) {
	var specs []AgentIngressSpec
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		records, err := s.ingresses.ListPending(ctx, clusterID)
		if err != nil {
			return err
		}

		for _, rec := range records {
			dom, err := s.domains.GetByID(ctx, rec.DomainID)
			if err != nil {
				continue
			}

			// Generate ingress manifest
			cfg := IngressConfig{
				Name:          rec.IngressName,
				Namespace:     rec.Namespace,
				IngressClass:  rec.IngressClass,
				Domain:        dom.FullDomain,
				ServiceName:   rec.ServiceName,
				ServicePort:   rec.ServicePort,
				Path:          rec.Path,
				PathType:      rec.PathType,
				TLSEnabled:    rec.TLSSecretName != nil,
			}
			if rec.TLSSecretName != nil {
				cfg.TLSSecretName = *rec.TLSSecretName
			}

			generated, err := s.ingressGen.Generate(cfg)
			if err != nil {
				continue
			}

			spec := AgentIngressSpec{
				IngressID:    rec.ID,
				DomainID:     rec.DomainID,
				Manifest:     generated.Manifest,
				ManifestHash: generated.ManifestHash,
				Generation:   rec.Generation,
			}

			// Include TLS secret if certificate exists
			if rec.TLSSecretName != nil {
				cert, _ := s.certs.GetByDomainID(ctx, rec.DomainID)
				if cert != nil && cert.Status == CertActive && len(cert.CertificatePEM) > 0 {
					// Decrypt certificate
					var certPEM, keyPEM []byte
					if s.encryptor != nil {
						certPEM, _ = s.encryptor.Decrypt(cert.CertificatePEM)
						keyPEM, _ = s.encryptor.Decrypt(cert.PrivateKeyPEM)
					} else {
						certPEM = cert.CertificatePEM
						keyPEM = cert.PrivateKeyPEM
					}
					tlsSecret, _ := s.ingressGen.GenerateTLSSecret(*rec.TLSSecretName, rec.Namespace, certPEM, keyPEM)
					spec.TLSSecret = tlsSecret
				}
			}

			specs = append(specs, spec)
		}

		return nil
	})
	return specs, err
}

// ReportIngressSync reports ingress sync status from agent.
func (s *Service) ReportIngressSync(ctx context.Context, orgID, ingressID, status string, observedGen int64, errMsg *string) error {
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.ingresses.UpdateSyncStatus(ctx, ingressID, status, observedGen, errMsg)
	})
}

// ----------------------------------------------------------------------------
// ACME Challenges
// ----------------------------------------------------------------------------

// GetACMEChallenge returns an ACME challenge by token (for HTTP-01 validation).
func (s *Service) GetACMEChallenge(ctx context.Context, token string) (*ACMEChallenge, error) {
	// This endpoint doesn't require auth - it's called by the ACME server
	return s.challenges.GetByToken(ctx, token)
}

// ----------------------------------------------------------------------------
// Events
// ----------------------------------------------------------------------------

// ListDomainEvents returns events for a domain.
func (s *Service) ListDomainEvents(ctx context.Context, orgID, userID, domainID string, page database.PageRequest) (database.Page[DomainEvent], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[DomainEvent]{}, err
	}

	var out database.Page[DomainEvent]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.events.List(ctx, domainID, page)
		return err
	})
	return out, err
}

// Helper to log domain events
func (s *Service) logEvent(ctx context.Context, orgID, domainID, eventType, message, userID string, details any) error {
	e := &DomainEvent{
		OrgID:     orgID,
		DomainID:  domainID,
		EventType: eventType,
		Message:   message,
		Details:   marshalDetails(details),
		CreatedBy: &userID,
	}
	return s.events.Create(ctx, e)
}
