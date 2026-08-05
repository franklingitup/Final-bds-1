package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	Logger          *slog.Logger
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
	bootstrapGen    *BootstrapGenerator
	providers       *ProviderRegistry
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
		bootstrapGen:    NewBootstrapGenerator("", ""),
		providers:       NewProviderRegistry(),
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

	provReq := &ProvisioningRequest{
		OrgID:        orgID,
		Name:         req.Name,
		Provider:     req.Provider,
		Region:       req.Region,
		CredentialID: req.CredentialID,
		TemplateID:   req.TemplateID,
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
	_ = json.Unmarshal(req.NodePools, &nodePools)

	tfCfg := TerraformConfig{
		ClusterName: req.Name,
		Region:      req.Region,
		K8sVersion:  req.K8sVersion,
		NodePools:   nodePools,
	}

	// Set provider-specific config
	switch req.Provider {
	case ProviderAWS:
		tfCfg.AWS = &AWSConfig{
			VPCCidr:          "10.0.0.0/16",
			EnableNATGateway: true,
		}
	case ProviderAzure:
		tfCfg.Azure = &AzureConfig{
			ResourceGroup: req.Name + "-rg",
			VNetCidr:      "10.0.0.0/16",
			DNSPrefix:     req.Name,
		}
	case ProviderGCP:
		tfCfg.GCP = &GCPConfig{
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
func (s *Service) StartProvisioning(ctx context.Context, orgID, userID, requestID string) (*InstallSession, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg); err != nil {
		return nil, err
	}

	var req *ProvisioningRequest
	var session *InstallSession

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		req, err = s.requests.GetByID(ctx, requestID)
		if err != nil {
			return err
		}

		if req.Status != RequestReady {
			return apperrors.Validation("request must be in 'ready' status to start provisioning")
		}

		// Generate bootstrap token
		token, tokenHash, err := GenerateBootstrapToken()
		if err != nil {
			return err
		}

		// Generate session token
		sessionToken, _, err := GenerateBootstrapToken()
		if err != nil {
			return err
		}

		// Generate bootstrap command
		expiresAt := time.Now().Add(24 * time.Hour)
		bootstrapCmd, err := s.bootstrapGen.GenerateCommand(req.Name, token, expiresAt)
		if err != nil {
			return err
		}

		// Create steps JSON
		stepsJSON, _ := json.Marshal(DefaultInstallSteps)

		// Create install session
		now := time.Now()
		session = &InstallSession{
			OrgID:            orgID,
			RequestID:        requestID,
			SessionToken:     sessionToken,
			CurrentStep:      "initializing",
			TotalSteps:       len(DefaultInstallSteps),
			CompletedSteps:   0,
			Steps:            stepsJSON,
			Status:           SessionActive,
			BootstrapToken:   &token,
			BootstrapCommand: &bootstrapCmd.Command,
			ExpiresAt:        expiresAt,
			StartedAt:        &now,
		}

		if err := s.sessions.Create(ctx, session); err != nil {
			return err
		}

		// Create bootstrap token record
		bootstrapToken := &BootstrapToken{
			OrgID:     orgID,
			RequestID: &requestID,
			SessionID: &session.ID,
			TokenHash: tokenHash,
			MaxUses:   1,
			Status:    TokenActive,
			ExpiresAt: expiresAt,
		}

		if err := s.bootstrapTokens.Create(ctx, bootstrapToken); err != nil {
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

		// Update request status
		req.Status = RequestProvisioning
		req.StartedAt = &now
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
			Message:   fmt.Sprintf("Provisioning started for cluster '%s'", req.Name),
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

	return session, steps, err
}

// UpdateStep updates a step in an install session.
func (s *Service) UpdateStep(ctx context.Context, sessionToken string, stepNumber int, req UpdateStepRequest) (*InstallSessionStep, error) {
	var session *InstallSession
	var step *InstallSessionStep

	// Find session by token (no auth required - token is auth)
	sessions, err := database.QueryAll[InstallSession](ctx, nil,
		"SELECT * FROM install_sessions WHERE session_token = $1", sessionToken)
	if err != nil || len(sessions) == 0 {
		return nil, apperrors.NotFound("session not found")
	}
	session = &sessions[0]

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
		if req.Status == StepCompleted {
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

// ReportAgentConnection reports agent connection from bootstrap.
func (s *Service) ReportAgentConnection(ctx context.Context, bootstrapToken string, req ReportAgentRequest, ip string) error {
	hash := HashToken(bootstrapToken)

	var token *BootstrapToken

	// Find token (no tenant context needed for bootstrap)
	token, err := s.bootstrapTokens.GetByHash(ctx, hash)
	if err != nil {
		return apperrors.Unauthorized("invalid or expired bootstrap token")
	}

	return s.tenant.WithTenant(ctx, token.OrgID, func(ctx context.Context) error {
		// Use the token
		if err := s.bootstrapTokens.Use(ctx, hash, ip, nil); err != nil {
			return err
		}

		// Update session
		if token.SessionID != nil {
			session, err := s.sessions.GetByID(ctx, *token.SessionID)
			if err == nil {
				now := time.Now()
				session.AgentConnected = true
				session.AgentConnectedAt = &now
				session.AgentVersion = &req.Version
				_ = s.sessions.Update(ctx, session)
			}
		}

		// Log event
		return s.events.Create(ctx, &ProvisioningEvent{
			OrgID:     token.OrgID,
			RequestID: token.RequestID,
			SessionID: token.SessionID,
			EventType: "provisioning.agent.connected",
			Severity:  SeverityInfo,
			Message:   fmt.Sprintf("Platform agent connected (version: %s)", req.Version),
			Details:   marshalJSON(map[string]string{"version": req.Version, "ip": ip}),
			ActorType: strPtr(ActorAgent),
		})
	})
}

// GetBootstrapManifest returns the bootstrap manifest for a token.
func (s *Service) GetBootstrapManifest(ctx context.Context, bootstrapToken string) (string, error) {
	hash := HashToken(bootstrapToken)

	token, err := s.bootstrapTokens.GetByHash(ctx, hash)
	if err != nil {
		return "", apperrors.NotFound("invalid or expired bootstrap token")
	}

	var req *ProvisioningRequest
	err = s.tenant.WithTenant(ctx, token.OrgID, func(ctx context.Context) error {
		var err error
		if token.RequestID != nil {
			req, err = s.requests.GetByID(ctx, *token.RequestID)
		}
		return err
	})
	if err != nil || req == nil {
		return "", apperrors.NotFound("provisioning request not found")
	}

	// Generate manifest
	cmd, err := s.bootstrapGen.GenerateCommand(req.Name, bootstrapToken, token.ExpiresAt)
	if err != nil {
		return "", err
	}

	return cmd.Manifest, nil
}

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
func (s *Service) ListKubernetesVersions(ctx context.Context, orgID, userID, provider string) ([]string, error) {
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
