package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Deps holds service dependencies.
type Deps struct {
	Credentials     CredentialStore
	Templates       TemplateStore
	Requests        RequestStore
	Sessions        SessionStore
	Steps           StepStore
	BootstrapTokens BootstrapTokenStore
	Events          EventStore
	OrgMembers      authz.OrgMemberStore
	Tenant          TenantRunner
	Encryptor       CredentialEncryptor
	ClusterClient   ClusterCreator // For creating clusters via internal API
	Logger          *slog.Logger
}

// ClusterCreator creates clusters via internal API.
// Matches internalapi.ClusterClient's interface.
type ClusterCreator interface {
	CreateClusterWithToken(ctx context.Context, orgID string, req ClusterCreateRequest) (*ClusterCreateResponse, error)
	GetCluster(ctx context.Context, orgID, clusterID string) (*ClusterStatus, error)
}

// ClusterCreateRequest mirrors internalapi.CreateClusterRequest.
type ClusterCreateRequest struct {
	Name          string            `json:"name"`
	Slug          string            `json:"slug"`
	Description   string            `json:"description,omitempty"`
	CloudProvider string            `json:"cloudProvider,omitempty"`
	Region        string            `json:"region,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	CreatedBy     string            `json:"createdBy"`
}

// ClusterCreateResponse mirrors internalapi.CreateClusterResponse.
type ClusterCreateResponse struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Slug              string `json:"slug"`
	Status            string `json:"status"`
	RegistrationToken string `json:"registrationToken"`
	ManifestURL       string `json:"manifestUrl"`
	InstallCommand    string `json:"installCommand"`
	ExpiresAt         string `json:"expiresAt"`
}

// ClusterStatus is the cluster lifecycle state used to synchronize sessions.
type ClusterStatus struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	AgentID           string `json:"agentId,omitempty"`
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`
}

// CredentialEncryptor encrypts/decrypts credentials.
type CredentialEncryptor interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// Service implements provisioning logic.
type Service struct {
	credentials     CredentialStore
	templates       TemplateStore
	requests        RequestStore
	sessions        SessionStore
	steps           StepStore
	bootstrapTokens BootstrapTokenStore
	events          EventStore
	orgMembers      authz.OrgMemberStore
	tenant          TenantRunner
	authSvc         *authz.AuthorizationService
	encryptor       CredentialEncryptor
	tfGenerator     *TerraformGenerator
	providers       *ProviderRegistry
	clusterClient   ClusterCreator
	log             *slog.Logger
}

// NewService creates a new provisioning service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}

	return &Service{
		credentials:     d.Credentials,
		templates:       d.Templates,
		requests:        d.Requests,
		sessions:        d.Sessions,
		steps:           d.Steps,
		bootstrapTokens: d.BootstrapTokens,
		events:          d.Events,
		orgMembers:      d.OrgMembers,
		tenant:          d.Tenant,
		authSvc:         authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		encryptor:       d.Encryptor,
		tfGenerator:     NewTerraformGenerator(),
		providers:       NewProviderRegistry(),
		clusterClient:   d.ClusterClient,
		log:             d.Logger,
	}
}

// ----------------------------------------------------------------------------
// Credentials
// ----------------------------------------------------------------------------

// CreateCredential creates cloud credentials.
func (s *Service) CreateCredential(ctx context.Context, orgID, userID string, req CreateCredentialRequest) (*CloudCredential, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return nil, err
	}

	if req.Name == "" {
		return nil, apperrors.Validation("name is required")
	}
	if req.Provider == "" {
		return nil, apperrors.Validation("provider is required")
	}
	if req.Provider != ProviderAWS && req.Provider != ProviderAzure && req.Provider != ProviderGCP {
		return nil, apperrors.Validation("provider must be aws, azure, or gcp")
	}

	// Marshal and encrypt credentials
	credsJSON, err := json.Marshal(req.Credentials)
	if err != nil {
		return nil, apperrors.Validation("invalid credentials")
	}

	encryptedCreds := credsJSON
	if s.encryptor != nil {
		encryptedCreds, err = s.encryptor.Encrypt(credsJSON)
		if err != nil {
			return nil, apperrors.Internal("failed to encrypt credentials")
		}
	}

	cred := &CloudCredential{
		OrgID:       orgID,
		Name:        req.Name,
		Provider:    req.Provider,
		Credentials: encryptedCreds,
		Region:      req.Region,
		Description: req.Description,
		CreatedBy:   &userID,
	}

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.credentials.Create(ctx, cred)
	})
	if err != nil {
		return nil, err
	}

	return cred, nil
}

// ValidateCredential validates cloud credentials.
func (s *Service) ValidateCredential(ctx context.Context, orgID, userID, credentialID string) (*CloudCredential, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return nil, err
	}

	var cred *CloudCredential
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		cred, err = s.credentials.GetByID(ctx, credentialID)
		return err
	})
	if err != nil {
		return nil, err
	}

	// Decrypt credentials
	decryptedCreds := cred.Credentials
	if s.encryptor != nil {
		decryptedCreds, _ = s.encryptor.Decrypt(cred.Credentials)
	}

	// Validate with provider
	provider, err := s.providers.Get(cred.Provider)
	if err != nil {
		return nil, err
	}

	validationErr := provider.ValidateCredentials(ctx, decryptedCreds)
	now := time.Now()
	cred.ValidatedAt = &now

	if validationErr != nil {
		errMsg := validationErr.Error()
		cred.Validated = false
		cred.ValidationError = &errMsg
	} else {
		cred.Validated = true
		cred.ValidationError = nil
	}

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.credentials.Update(ctx, cred)
	})
	if err != nil {
		return nil, err
	}

	return cred, validationErr
}

// ListCredentials returns credentials for an organization.
func (s *Service) ListCredentials(ctx context.Context, orgID, userID string) ([]CloudCredential, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var creds []CloudCredential
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		creds, err = s.credentials.List(ctx, orgID)
		return err
	})
	return creds, err
}

// DeleteCredential deletes a credential.
func (s *Service) DeleteCredential(ctx context.Context, orgID, userID, credentialID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.credentials.Delete(ctx, credentialID)
	})
}

// ----------------------------------------------------------------------------
// Templates
// ----------------------------------------------------------------------------

// ListTemplates returns templates for an organization.
func (s *Service) ListTemplates(ctx context.Context, orgID, userID string, provider *string) ([]ClusterTemplate, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var templates []ClusterTemplate
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		if provider != nil && *provider != "" {
			templates, err = s.templates.ListByProvider(ctx, orgID, *provider)
		} else {
			templates, err = s.templates.List(ctx, orgID)
		}
		return err
	})
	return templates, err
}

// ----------------------------------------------------------------------------
// Provisioning Requests
// ----------------------------------------------------------------------------

// CreateProvisioningRequest creates a new provisioning request.
func (s *Service) CreateProvisioningRequest(ctx context.Context, orgID, userID string, req CreateProvisioningRequest) (*ProvisioningRequest, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return nil, err
	}

	if req.Name == "" {
		return nil, apperrors.Validation("name is required")
	}
	if req.Provider == "" {
		return nil, apperrors.Validation("provider is required")
	}
	if req.Region == "" {
		return nil, apperrors.Validation("region is required")
	}
	if req.CredentialID == nil || strings.TrimSpace(*req.CredentialID) == "" {
		return nil, apperrors.Validation("credentialId is required")
	}

	// Set defaults
	if req.K8sVersion == "" {
		req.K8sVersion = "1.28"
	}
	if len(req.NodePools) == 0 {
		req.NodePools = []NodePool{
			{
				Name:         "default",
				MachineType:  getDefaultMachineType(req.Provider),
				MinNodes:     1,
				MaxNodes:     3,
				DesiredNodes: 2,
				DiskSizeGB:   50,
			},
		}
	}

	nodePoolsJSON, _ := json.Marshal(req.NodePools)
	configJSON, _ := json.Marshal(ProvisioningConfig{
		Networking: req.Networking,
		Addons:     req.Addons,
	})

	provReq := &ProvisioningRequest{
		OrgID:        orgID,
		Name:         req.Name,
		Provider:     req.Provider,
		Region:       req.Region,
		CredentialID: req.CredentialID,
		TemplateID:   req.TemplateID,
		Config:       configJSON,
		K8sVersion:   req.K8sVersion,
		NodePools:    nodePoolsJSON,
		Status:       RequestPending,
		CreatedBy:    &userID,
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.requests.Create(ctx, provReq); err != nil {
			return err
		}

		// Log event
		return s.events.Create(ctx, &ProvisioningEvent{
			OrgID:     orgID,
			RequestID: &provReq.ID,
			EventType: "provisioning.request.created",
			Severity:  SeverityInfo,
			Message:   fmt.Sprintf("Provisioning request '%s' created for %s in %s", req.Name, req.Provider, req.Region),
			ActorType: strPtr(ActorUser),
			ActorID:   &userID,
		})
	})
	if err != nil {
		return nil, err
	}

	return provReq, nil
}

// GetProvisioningRequest returns a provisioning request.
func (s *Service) GetProvisioningRequest(ctx context.Context, orgID, userID, requestID string) (*ProvisioningRequest, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var req *ProvisioningRequest
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		req, err = s.requests.GetByID(ctx, requestID)
		return err
	})
	return req, err
}

// ListProvisioningRequests returns provisioning requests.
func (s *Service) ListProvisioningRequests(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[ProvisioningRequest], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[ProvisioningRequest]{}, err
	}

	var result database.Page[ProvisioningRequest]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		result, err = s.requests.List(ctx, orgID, page)
		return err
	})
	return result, err
}

// GenerateTerraform generates Terraform configuration for a request.
func (s *Service) GenerateTerraform(ctx context.Context, orgID, userID, requestID string) (*ProvisioningRequest, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return nil, err
	}

	var req *ProvisioningRequest
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		req, err = s.requests.GetByID(ctx, requestID)
		if err != nil {
			return err
		}

		if req.Status != RequestPending && req.Status != RequestFailed {
			return apperrors.Validation("cannot generate terraform for request in status: " + req.Status)
		}

		// Update status
		req.Status = RequestGenerating
		if err := s.requests.Update(ctx, req); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Generate Terraform outside transaction
	var nodePools []NodePool
	if err := json.Unmarshal(req.NodePools, &nodePools); err != nil {
		return nil, apperrors.Internal("invalid persisted node pool configuration")
	}
	var persistedConfig ProvisioningConfig
	if err := json.Unmarshal(req.Config, &persistedConfig); err != nil {
		return nil, apperrors.Internal("invalid persisted provisioning configuration")
	}
	if req.CredentialID == nil || *req.CredentialID == "" {
		return nil, apperrors.Validation("provisioning request has no cloud credential")
	}
	if s.encryptor == nil {
		return nil, apperrors.Internal("credential encryption is not configured")
	}

	// Resolve and validate the selected credential before rendering Terraform.
	var credential *CloudCredential
	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		credential, err = s.credentials.GetByID(ctx, *req.CredentialID)
		return err
	})
	if err != nil {
		return nil, apperrors.Validation("selected cloud credential was not found")
	}
	if credential.Provider != req.Provider {
		return nil, apperrors.Validation("selected cloud credential does not match provisioning provider")
	}
	credentialJSON, err := s.encryptor.Decrypt(credential.Credentials)
	if err != nil {
		return nil, apperrors.Internal("failed to decrypt selected cloud credential")
	}
	provider, err := s.providers.Get(req.Provider)
	if err != nil {
		return nil, apperrors.Validation(err.Error())
	}
	if err := provider.ValidateCredentials(ctx, credentialJSON); err != nil {
		return nil, apperrors.Validation("selected cloud credential is invalid: " + err.Error())
	}

	tfCfg := TerraformConfig{
		ClusterName: req.Name,
		Region:      req.Region,
		K8sVersion:  req.K8sVersion,
		NodePools:   nodePools,
		Networking:  persistedConfig.Networking,
		Addons:      persistedConfig.Addons,
	}

	// Set provider-specific config
	networking := persistedConfig.Networking
	if networking == nil {
		networking = &NetworkingConfig{
			VPCCidr:        "10.0.0.0/16",
			PodCidr:        "10.244.0.0/16",
			ServiceCidr:    "10.96.0.0/12",
			NATGateway:     true,
			PrivateCluster: false,
		}
	}
	switch req.Provider {
	case ProviderAWS:
		tfCfg.AWS = &AWSConfig{
			VPCCidr:          defaultString(networking.VPCCidr, "10.0.0.0/16"),
			ServiceCidr:      defaultString(networking.ServiceCidr, "10.96.0.0/12"),
			EnableNATGateway: networking.NATGateway,
			PrivateCluster:   networking.PrivateCluster,
		}
	case ProviderAzure:
		tfCfg.Azure = &AzureConfig{
			ResourceGroup:  req.Name + "-rg",
			VNetCidr:       defaultString(networking.VPCCidr, "10.0.0.0/16"),
			PodCidr:        defaultString(networking.PodCidr, "10.244.0.0/16"),
			ServiceCidr:    defaultString(networking.ServiceCidr, "10.96.0.0/12"),
			DNSPrefix:      req.Name,
			PrivateCluster: networking.PrivateCluster,
		}
	case ProviderGCP:
		var gcpCreds GCPCredentials
		if err := json.Unmarshal(credentialJSON, &gcpCreds); err != nil {
			return nil, apperrors.Validation("invalid GCP credential payload")
		}
		tfCfg.GCP = &GCPConfig{
			ProjectID:      gcpCreds.ProjectID,
			SubnetCidr:     defaultString(networking.VPCCidr, "10.0.0.0/20"),
			PodCidr:        defaultString(networking.PodCidr, "10.1.0.0/16"),
			ServiceCidr:    defaultString(networking.ServiceCidr, "10.2.0.0/20"),
			PrivateCluster: networking.PrivateCluster,
			MasterIPv4CIDR: "172.16.0.0/28",
		}
	}

	generated, err := s.tfGenerator.Generate(req.Provider, tfCfg)
	if err != nil {
		// Update with error
		errMsg := err.Error()
		req.Status = RequestFailed
		req.ErrorMessage = &errMsg
		_ = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
			return s.requests.Update(ctx, req)
		})
		return nil, err
	}

	// Update with generated config
	tfVarsJSON, _ := json.Marshal(generated.TFVars)
	fullTF := generated.MainTF + "\n\n" + generated.VariablesTF + "\n\n" + generated.OutputsTF

	req.TerraformConfig = &fullTF
	req.TerraformVars = tfVarsJSON
	req.Status = RequestReady

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.requests.Update(ctx, req); err != nil {
			return err
		}

		return s.events.Create(ctx, &ProvisioningEvent{
			OrgID:     orgID,
			RequestID: &req.ID,
			EventType: "provisioning.terraform.generated",
			Severity:  SeverityInfo,
			Message:   "Terraform configuration generated successfully",
			ActorType: strPtr(ActorSystem),
		})
	})
	if err != nil {
		return nil, err
	}

	return req, nil
}

// StartProvisioning starts the provisioning process.
// It first creates a cluster record + registration token via the cluster service,
// then creates the install session for Terraform provisioning.
func (s *Service) StartProvisioning(ctx context.Context, orgID, userID, requestID string) (*InstallSession, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return nil, err
	}

	// Verify cluster client is configured
	if s.clusterClient == nil {
		return nil, apperrors.Internal("cluster service client not configured")
	}

	var req *ProvisioningRequest
	var session *InstallSession

	// First, get the provisioning request and validate it
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		req, err = s.requests.GetByID(ctx, requestID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if req.Status != RequestReady {
		return nil, apperrors.Validation("request must be in 'ready' status to start provisioning")
	}

	// Create slug from name (lowercase, replace spaces with hyphens)
	slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(req.Name), " ", "-"))
	// Remove any non-alphanumeric characters except hyphens
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, slug)
	// Ensure slug is valid (2-64 chars, starts/ends with alphanumeric)
	if len(slug) < 2 {
		slug = "cluster-" + requestID[:8]
	}
	if len(slug) > 64 {
		slug = slug[:64]
	}

	// Step 1: Create cluster + registration token via cluster service (BEFORE any Terraform work).
	// This validates name/slug availability early, before any cloud costs are incurred.
	clusterReq := ClusterCreateRequest{
		Name:          req.Name,
		Slug:          slug,
		Description:   fmt.Sprintf("Provisioned via Terraform (request: %s)", requestID),
		CloudProvider: req.Provider,
		Region:        req.Region,
		CreatedBy:     userID,
	}

	clusterResp, err := s.clusterClient.CreateClusterWithToken(ctx, orgID, clusterReq)
	if err != nil {
		// If cluster creation fails (e.g., slug already taken), fail immediately
		// before any expensive Terraform work begins
		s.log.ErrorContext(ctx, "failed to create cluster via cluster service",
			"error", err,
			"request_id", requestID,
			"cluster_name", req.Name,
		)
		return nil, apperrors.Wrap(err, apperrors.CodeInternal, "failed to create cluster record")
	}

	s.log.InfoContext(ctx, "created cluster via cluster service",
		"cluster_id", clusterResp.ID,
		"cluster_name", clusterResp.Name,
		"manifest_url", clusterResp.ManifestURL,
	)

	// Step 2: Create install session with the real registration token
	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Generate session token for CLI/script authentication
		sessionToken, _, err := GenerateBootstrapToken()
		if err != nil {
			return err
		}

		// Create steps JSON
		stepsJSON, _ := json.Marshal(DefaultInstallSteps)

		// Create install session with the REAL registration token from cluster service
		now := time.Now()
		expiresAt := time.Now().Add(24 * time.Hour)
		session = &InstallSession{
			OrgID:            orgID,
			RequestID:        requestID,
			SessionToken:     sessionToken,
			CurrentStep:      "initializing",
			TotalSteps:       len(DefaultInstallSteps),
			CompletedSteps:   0,
			Steps:            stepsJSON,
			Status:           SessionActive,
			BootstrapToken:   &clusterResp.RegistrationToken, // Real cluster service token
			BootstrapCommand: &clusterResp.InstallCommand,    // Real kubectl apply command
			ExpiresAt:        expiresAt,
			StartedAt:        &now,
		}

		if err := s.sessions.Create(ctx, session); err != nil {
			return err
		}

		// Create step records
		for _, step := range DefaultInstallSteps {
			stepRecord := &InstallSessionStep{
				SessionID:   session.ID,
				StepNumber:  step.Number,
				Name:        step.Name,
				Description: &step.Description,
				Status:      StepPending,
			}
			if err := s.steps.Create(ctx, stepRecord); err != nil {
				return err
			}
		}

		// Update request status and link to cluster
		req.Status = RequestProvisioning
		req.StartedAt = &now
		req.ClusterID = &clusterResp.ID
		if err := s.requests.Update(ctx, req); err != nil {
			return err
		}

		// Log event
		return s.events.Create(ctx, &ProvisioningEvent{
			OrgID:     orgID,
			RequestID: &requestID,
			SessionID: &session.ID,
			EventType: "provisioning.started",
			Severity:  SeverityInfo,
			Message:   fmt.Sprintf("Provisioning started for cluster '%s' (cluster_id: %s)", req.Name, clusterResp.ID),
			ActorType: strPtr(ActorUser),
			ActorID:   &userID,
		})
	})
	if err != nil {
		return nil, err
	}

	return session, nil
}

// GetInstallSession returns an install session.
func (s *Service) GetInstallSession(ctx context.Context, orgID, userID, sessionID string) (*InstallSession, []InstallSessionStep, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, nil, err
	}

	var session *InstallSession
	var steps []InstallSessionStep

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		session, err = s.sessions.GetByID(ctx, sessionID)
		if err != nil {
			return err
		}
		steps, err = s.steps.ListBySession(ctx, sessionID)
		return err
	})
	if err == nil {
		s.syncAgentConnection(ctx, session)
	}

	return session, steps, err
}

// ValidateSessionForInstall checks if a session exists and is valid for install script serving.
// Returns the session if valid, or an error if not found/expired.
// This is used by the public /install/:sessionId endpoint.
func (s *Service) ValidateSessionForInstall(ctx context.Context, sessionID string) (*InstallSession, error) {
	session, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, apperrors.NotFound("session not found")
	}
	if time.Now().After(session.ExpiresAt) {
		// Return 410 Gone for expired sessions
		return nil, apperrors.NewWithStatus("SESSION_EXPIRED", 410, "session has expired")
	}
	if session.Status == SessionCompleted {
		return nil, apperrors.Validation("session is already completed")
	}
	if session.Status == SessionCancelled {
		return nil, apperrors.Validation("session has been cancelled")
	}
	return session, nil
}

// GetSessionBundle returns the generated installer inputs using the opaque
// session token as authentication. No user JWT is required.
func (s *Service) GetSessionBundle(ctx context.Context, sessionToken string) (*SessionBundle, error) {
	session, err := s.sessions.GetByToken(ctx, sessionToken)
	if err != nil {
		return nil, apperrors.NotFound("invalid or expired session token")
	}
	if time.Now().After(session.ExpiresAt) {
		return nil, apperrors.NotFound("invalid or expired session token")
	}

	var req *ProvisioningRequest
	err = s.tenant.WithTenant(ctx, session.OrgID, func(ctx context.Context) error {
		var getErr error
		req, getErr = s.requests.GetByID(ctx, session.RequestID)
		return getErr
	})
	if err != nil {
		return nil, err
	}
	if req.TerraformConfig == nil || len(req.TerraformVars) == 0 {
		return nil, apperrors.Validation("terraform configuration is not ready")
	}
	s.syncAgentConnection(ctx, session)

	var steps []StepInfo
	if err := json.Unmarshal(session.Steps, &steps); err != nil {
		return nil, apperrors.Internal("invalid install session steps")
	}
	bootstrapToken := ""
	if session.BootstrapToken != nil {
		bootstrapToken = *session.BootstrapToken
	}
	credentialEnv, err := s.sessionCredentialEnvironment(ctx, session.OrgID, req)
	if err != nil {
		return nil, err
	}

	return &SessionBundle{
		SessionID:       session.ID,
		RequestID:       session.RequestID,
		Provider:        req.Provider,
		TerraformConfig: *req.TerraformConfig,
		TerraformVars:   append(json.RawMessage(nil), req.TerraformVars...),
		CredentialEnv:   credentialEnv,
		SessionToken:    session.SessionToken,
		BootstrapToken:  bootstrapToken,
		Steps:           steps,
		Status:          session.Status,
		AgentConnected:  session.AgentConnected,
		AgentVersion:    session.AgentVersion,
		ExpiresAt:       session.ExpiresAt.Format(time.RFC3339),
	}, nil
}

func (s *Service) sessionCredentialEnvironment(ctx context.Context, orgID string, req *ProvisioningRequest) (map[string]string, error) {
	if req == nil || req.CredentialID == nil || *req.CredentialID == "" {
		return nil, nil
	}
	if s.encryptor == nil {
		return nil, apperrors.Internal("credential encryption is not configured")
	}

	var credential *CloudCredential
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		credential, err = s.credentials.GetByID(ctx, *req.CredentialID)
		return err
	})
	if err != nil {
		return nil, err
	}
	plaintext, err := s.encryptor.Decrypt(credential.Credentials)
	if err != nil {
		return nil, apperrors.Internal("failed to decrypt selected cloud credential")
	}

	env := make(map[string]string)
	switch req.Provider {
	case ProviderAWS:
		var creds AWSCredentials
		if err := json.Unmarshal(plaintext, &creds); err != nil {
			return nil, apperrors.Internal("invalid stored AWS credential")
		}
		env["AWS_ACCESS_KEY_ID"] = creds.AccessKeyID
		env["AWS_SECRET_ACCESS_KEY"] = creds.SecretAccessKey
		if creds.SessionToken != "" {
			env["AWS_SESSION_TOKEN"] = creds.SessionToken
		}
	case ProviderAzure:
		var creds AzureCredentials
		if err := json.Unmarshal(plaintext, &creds); err != nil {
			return nil, apperrors.Internal("invalid stored Azure credential")
		}
		env["ARM_SUBSCRIPTION_ID"] = creds.SubscriptionID
		env["ARM_TENANT_ID"] = creds.TenantID
		env["ARM_CLIENT_ID"] = creds.ClientID
		env["ARM_CLIENT_SECRET"] = creds.ClientSecret
	case ProviderGCP:
		var creds GCPCredentials
		if err := json.Unmarshal(plaintext, &creds); err != nil {
			return nil, apperrors.Internal("invalid stored GCP credential")
		}
		env["GOOGLE_PROJECT"] = creds.ProjectID
		env["GOOGLE_CREDENTIALS"] = creds.ServiceAccountJSON
	}
	return env, nil
}

// syncAgentConnection mirrors the real cluster service registration state into
// the install session. The installer polls GetSessionBundle, so this replaces
// the removed provisioning-specific ReportAgentConnection callback.
func (s *Service) syncAgentConnection(ctx context.Context, session *InstallSession) {
	if session == nil || session.AgentConnected || s.clusterClient == nil {
		return
	}

	var req *ProvisioningRequest
	if err := s.tenant.WithTenant(ctx, session.OrgID, func(ctx context.Context) error {
		var err error
		req, err = s.requests.GetByID(ctx, session.RequestID)
		return err
	}); err != nil || req == nil || req.ClusterID == nil || *req.ClusterID == "" {
		return
	}

	cluster, err := s.clusterClient.GetCluster(ctx, session.OrgID, *req.ClusterID)
	if err != nil {
		s.log.WarnContext(ctx, "failed to synchronize agent registration",
			"cluster_id", *req.ClusterID,
			"session_id", session.ID,
			"error", err)
		return
	}
	if cluster.Status != "connected" {
		return
	}

	now := time.Now()
	session.AgentConnected = true
	session.AgentConnectedAt = &now
	if cluster.KubernetesVersion != "" {
		session.AgentVersion = &cluster.KubernetesVersion
	}
	if err := s.tenant.WithTenant(ctx, session.OrgID, func(ctx context.Context) error {
		return s.sessions.Update(ctx, session)
	}); err != nil {
		s.log.WarnContext(ctx, "failed to persist synchronized agent registration",
			"cluster_id", *req.ClusterID,
			"session_id", session.ID,
			"error", err)
	}
}

// UpdateStep updates a step in an install session.
func (s *Service) UpdateStep(ctx context.Context, sessionToken string, stepNumber int, req UpdateStepRequest) (*InstallSessionStep, error) {
	var session *InstallSession
	var step *InstallSessionStep

	// Find session by token (no auth required - token is auth)
	session, err := s.sessions.GetByToken(ctx, sessionToken)
	if err != nil || time.Now().After(session.ExpiresAt) {
		return nil, apperrors.NotFound("session not found")
	}

	if session.Status != SessionActive {
		return nil, apperrors.Validation("session is not active")
	}

	err = s.tenant.WithTenant(ctx, session.OrgID, func(ctx context.Context) error {
		// Get step
		steps, err := s.steps.ListBySession(ctx, session.ID)
		if err != nil {
			return err
		}

		for i := range steps {
			if steps[i].StepNumber == stepNumber {
				step = &steps[i]
				break
			}
		}

		if step == nil {
			return apperrors.NotFound("step not found")
		}

		// Update step
		now := time.Now()
		previousStatus := step.Status
		step.Status = req.Status
		step.Output = req.Output
		step.Error = req.Error

		if req.Status == StepRunning && step.StartedAt == nil {
			step.StartedAt = &now
		}
		if req.Status == StepCompleted || req.Status == StepFailed {
			step.CompletedAt = &now
			if step.StartedAt != nil {
				durationMs := now.Sub(*step.StartedAt).Milliseconds()
				step.DurationMs = &durationMs
			}
		}

		if err := s.steps.Update(ctx, step); err != nil {
			return err
		}

		// Update session progress
		if req.Status == StepCompleted && previousStatus != StepCompleted {
			session.CompletedSteps++
			session.CurrentStep = step.Name
		}

		// Check if all steps completed
		if session.CompletedSteps >= session.TotalSteps {
			session.Status = SessionCompleted
			session.CompletedAt = &now

			// Update request status
			req, _ := s.requests.GetByID(ctx, session.RequestID)
			if req != nil {
				req.Status = RequestCompleted
				req.CompletedAt = &now
				_ = s.requests.Update(ctx, req)
			}
		}

		if req.Status == StepFailed {
			session.Status = SessionFailed
			session.CompletedAt = &now
		}

		return s.sessions.Update(ctx, session)
	})

	return step, err
}

// NOTE: ReportAgentConnection and GetBootstrapManifest methods removed.
// Agents now use the cluster service's /v1/agent/register endpoint directly,
// and the install script fetches manifests from /v1/agent-install/:token/manifest.yaml.

// ListEvents returns events for a request or session.
func (s *Service) ListEvents(ctx context.Context, orgID, userID string, requestID, sessionID *string, page database.PageRequest) (database.Page[ProvisioningEvent], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[ProvisioningEvent]{}, err
	}

	var result database.Page[ProvisioningEvent]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		if sessionID != nil && *sessionID != "" {
			result, err = s.events.ListBySession(ctx, *sessionID, page)
		} else if requestID != nil && *requestID != "" {
			result, err = s.events.ListByRequest(ctx, *requestID, page)
		}
		return err
	})
	return result, err
}

// ----------------------------------------------------------------------------
// Provider Info
// ----------------------------------------------------------------------------

// ListRegions returns regions for a provider.
func (s *Service) ListRegions(ctx context.Context, orgID, userID, provider string, credentialID *string) ([]Region, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	p, err := s.providers.Get(provider)
	if err != nil {
		return nil, apperrors.Validation(err.Error())
	}

	var creds []byte
	if credentialID != nil {
		var cred *CloudCredential
		err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
			var err error
			cred, err = s.credentials.GetByID(ctx, *credentialID)
			return err
		})
		if err == nil && s.encryptor != nil {
			creds, _ = s.encryptor.Decrypt(cred.Credentials)
		}
	}

	return p.ListRegions(ctx, creds)
}

// ListMachineTypes returns machine types for a provider/region.
func (s *Service) ListMachineTypes(ctx context.Context, orgID, userID, provider, region string) ([]MachineType, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	p, err := s.providers.Get(provider)
	if err != nil {
		return nil, apperrors.Validation(err.Error())
	}

	return p.ListMachineTypes(ctx, nil, region)
}

// ListKubernetesVersions returns K8s versions for a provider.
func (s *Service) ListKubernetesVersions(ctx context.Context, orgID, userID, provider string) ([]K8sVersion, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	p, err := s.providers.Get(provider)
	if err != nil {
		return nil, apperrors.Validation(err.Error())
	}

	return p.ListKubernetesVersions(ctx, nil, "")
}

// Helper functions
func strPtr(s string) *string { return &s }

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func getDefaultMachineType(provider string) string {
	switch provider {
	case ProviderAWS:
		return "t3.medium"
	case ProviderAzure:
		return "Standard_D2s_v3"
	case ProviderGCP:
		return "e2-medium"
	default:
		return "t3.medium"
	}
}
