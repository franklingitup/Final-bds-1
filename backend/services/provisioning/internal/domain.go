// Package provisioning generates cloud-specific cluster config and install
// commands, and tracks install sessions.
package provisioning

import (
	"encoding/json"
	"time"
)

// Cloud providers.
const (
	ProviderAWS   = "aws"
	ProviderAzure = "azure"
	ProviderGCP   = "gcp"
)

// Provisioning request status.
const (
	RequestPending      = "pending"
	RequestGenerating   = "generating"
	RequestReady        = "ready"
	RequestProvisioning = "provisioning"
	RequestCompleted    = "completed"
	RequestFailed       = "failed"
	RequestCancelled    = "cancelled"
)

// Install session status.
const (
	SessionActive    = "active"
	SessionCompleted = "completed"
	SessionFailed    = "failed"
	SessionExpired   = "expired"
	SessionCancelled = "cancelled"
)

// Step status.
const (
	StepPending   = "pending"
	StepRunning   = "running"
	StepCompleted = "completed"
	StepFailed    = "failed"
	StepSkipped   = "skipped"
)

// Bootstrap token status.
const (
	TokenActive  = "active"
	TokenUsed    = "used"
	TokenExpired = "expired"
	TokenRevoked = "revoked"
)

// Event severity.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityError    = "error"
	SeverityCritical = "critical"
)

// Actor types.
const (
	ActorUser   = "user"
	ActorSystem = "system"
	ActorAgent  = "agent"
)

// ----------------------------------------------------------------------------
// Cloud Credentials
// ----------------------------------------------------------------------------

// CloudCredential is encrypted cloud provider credentials.
type CloudCredential struct {
	ID              string     `db:"id"`
	OrgID           string     `db:"org_id"`
	Name            string     `db:"name"`
	Provider        string     `db:"provider"`
	Credentials     []byte     `db:"credentials"`
	Validated       bool       `db:"validated"`
	ValidatedAt     *time.Time `db:"validated_at"`
	ValidationError *string    `db:"validation_error"`
	Region          *string    `db:"region"`
	Description     *string    `db:"description"`
	CreatedBy       *string    `db:"created_by"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
}

// AWSCredentials holds AWS credential data.
type AWSCredentials struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken,omitempty"`
	RoleARN         string `json:"roleArn,omitempty"`
	ExternalID      string `json:"externalId,omitempty"`
}

// AzureCredentials holds Azure credential data.
type AzureCredentials struct {
	SubscriptionID string `json:"subscriptionId"`
	TenantID       string `json:"tenantId"`
	ClientID       string `json:"clientId"`
	ClientSecret   string `json:"clientSecret"`
}

// GCPCredentials holds GCP credential data.
type GCPCredentials struct {
	ProjectID          string `json:"projectId"`
	ServiceAccountJSON string `json:"serviceAccountJson"`
}

// ----------------------------------------------------------------------------
// Cluster Template
// ----------------------------------------------------------------------------

// ClusterTemplate is a predefined cluster configuration.
type ClusterTemplate struct {
	ID          string          `db:"id"`
	OrgID       *string         `db:"org_id"`
	Name        string          `db:"name"`
	Provider    string          `db:"provider"`
	Config      json.RawMessage `db:"config"`
	K8sVersion  string          `db:"k8s_version"`
	NodePools   json.RawMessage `db:"node_pools"`
	Networking  json.RawMessage `db:"networking"`
	Addons      json.RawMessage `db:"addons"`
	Description *string         `db:"description"`
	IsDefault   bool            `db:"is_default"`
	CreatedAt   time.Time       `db:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

// NodePool defines a node pool configuration.
type NodePool struct {
	Name         string            `json:"name"`
	MachineType  string            `json:"machineType"`
	MinNodes     int               `json:"minNodes"`
	MaxNodes     int               `json:"maxNodes"`
	DesiredNodes int               `json:"desiredNodes"`
	DiskSizeGB   int               `json:"diskSizeGb"`
	DiskType     string            `json:"diskType,omitempty"`
	Preemptible  bool              `json:"preemptible,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Taints       []NodeTaint       `json:"taints,omitempty"`
}

// NodeTaint defines a Kubernetes taint.
type NodeTaint struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

// NetworkingConfig holds networking configuration.
type NetworkingConfig struct {
	VPCCidr         string   `json:"vpcCidr,omitempty"`
	PodCidr         string   `json:"podCidr,omitempty"`
	ServiceCidr     string   `json:"serviceCidr,omitempty"`
	SubnetCidrs     []string `json:"subnetCidrs,omitempty"`
	PrivateCluster  bool     `json:"privateCluster,omitempty"`
	NATGateway      bool     `json:"natGateway,omitempty"`
}

// ClusterAddon defines an addon to install.
type ClusterAddon struct {
	Name    string                 `json:"name"`
	Enabled bool                   `json:"enabled"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

// ----------------------------------------------------------------------------
// Provisioning Request
// ----------------------------------------------------------------------------

// ProvisioningRequest is a request to provision a cluster.
type ProvisioningRequest struct {
	ID              string          `db:"id"`
	OrgID           string          `db:"org_id"`
	Name            string          `db:"name"`
	Provider        string          `db:"provider"`
	Region          string          `db:"region"`
	CredentialID    *string         `db:"credential_id"`
	TemplateID      *string         `db:"template_id"`
	Config          json.RawMessage `db:"config"`
	K8sVersion      string          `db:"k8s_version"`
	NodePools       json.RawMessage `db:"node_pools"`
	TerraformConfig *string         `db:"terraform_config"`
	TerraformVars   json.RawMessage `db:"terraform_vars"`
	Status          string          `db:"status"`
	ClusterID       *string         `db:"cluster_id"`
	ErrorMessage    *string         `db:"error_message"`
	ErrorDetails    json.RawMessage `db:"error_details"`
	StartedAt       *time.Time      `db:"started_at"`
	CompletedAt     *time.Time      `db:"completed_at"`
	CreatedBy       *string         `db:"created_by"`
	CreatedAt       time.Time       `db:"created_at"`
	UpdatedAt       time.Time       `db:"updated_at"`
}

// ----------------------------------------------------------------------------
// Install Session
// ----------------------------------------------------------------------------

// InstallSession tracks provisioning progress.
type InstallSession struct {
	ID               string     `db:"id"`
	OrgID            string     `db:"org_id"`
	RequestID        string     `db:"request_id"`
	SessionToken     string     `db:"session_token"`
	CurrentStep      string     `db:"current_step"`
	TotalSteps       int        `db:"total_steps"`
	CompletedSteps   int        `db:"completed_steps"`
	Steps            json.RawMessage `db:"steps"`
	Status           string     `db:"status"`
	BootstrapToken   *string    `db:"bootstrap_token"`
	BootstrapCommand *string    `db:"bootstrap_command"`
	AgentConnected   bool       `db:"agent_connected"`
	AgentConnectedAt *time.Time `db:"agent_connected_at"`
	AgentVersion     *string    `db:"agent_version"`
	ExpiresAt        time.Time  `db:"expires_at"`
	StartedAt        *time.Time `db:"started_at"`
	CompletedAt      *time.Time `db:"completed_at"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
}

// InstallSessionStep is a step in an install session.
type InstallSessionStep struct {
	ID          string     `db:"id"`
	SessionID   string     `db:"session_id"`
	StepNumber  int        `db:"step_number"`
	Name        string     `db:"name"`
	Description *string    `db:"description"`
	Status      string     `db:"status"`
	Output      *string    `db:"output"`
	Error       *string    `db:"error"`
	StartedAt   *time.Time `db:"started_at"`
	CompletedAt *time.Time `db:"completed_at"`
	DurationMs  *int64     `db:"duration_ms"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
}

// StepInfo is a summary of a step for the steps JSONB field.
type StepInfo struct {
	Number      int    `json:"number"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
}

// ----------------------------------------------------------------------------
// Bootstrap Token
// ----------------------------------------------------------------------------

// BootstrapToken is a token for cluster registration.
type BootstrapToken struct {
	ID         string     `db:"id"`
	OrgID      string     `db:"org_id"`
	RequestID  *string    `db:"request_id"`
	SessionID  *string    `db:"session_id"`
	TokenHash  string     `db:"token_hash"`
	MaxUses    int        `db:"max_uses"`
	UseCount   int        `db:"use_count"`
	Status     string     `db:"status"`
	ExpiresAt  time.Time  `db:"expires_at"`
	LastUsedAt *time.Time `db:"last_used_at"`
	UsedByIP   *string    `db:"used_by_ip"`
	ClusterID  *string    `db:"cluster_id"`
	CreatedAt  time.Time  `db:"created_at"`
}

// ----------------------------------------------------------------------------
// Provisioning Event
// ----------------------------------------------------------------------------

// ProvisioningEvent is an event in the provisioning lifecycle.
type ProvisioningEvent struct {
	ID         string          `db:"id"`
	OrgID      string          `db:"org_id"`
	RequestID  *string         `db:"request_id"`
	SessionID  *string         `db:"session_id"`
	EventType  string          `db:"event_type"`
	Severity   string          `db:"severity"`
	Message    string          `db:"message"`
	Details    json.RawMessage `db:"details"`
	ActorType  *string         `db:"actor_type"`
	ActorID    *string         `db:"actor_id"`
	CreatedAt  time.Time       `db:"created_at"`
}

// ----------------------------------------------------------------------------
// Request DTOs
// ----------------------------------------------------------------------------

// CreateCredentialRequest is the request to create credentials.
type CreateCredentialRequest struct {
	Name        string      `json:"name"`
	Provider    string      `json:"provider"`
	Credentials interface{} `json:"credentials"`
	Region      *string     `json:"region,omitempty"`
	Description *string     `json:"description,omitempty"`
}

// CreateTemplateRequest is the request to create a template.
type CreateTemplateRequest struct {
	Name        string           `json:"name"`
	Provider    string           `json:"provider"`
	K8sVersion  string           `json:"k8sVersion,omitempty"`
	NodePools   []NodePool       `json:"nodePools,omitempty"`
	Networking  *NetworkingConfig `json:"networking,omitempty"`
	Addons      []ClusterAddon   `json:"addons,omitempty"`
	Description *string          `json:"description,omitempty"`
}

// CreateProvisioningRequest is the request to create a provisioning request.
type CreateProvisioningRequest struct {
	Name         string           `json:"name"`
	Provider     string           `json:"provider"`
	Region       string           `json:"region"`
	CredentialID *string          `json:"credentialId,omitempty"`
	TemplateID   *string          `json:"templateId,omitempty"`
	K8sVersion   string           `json:"k8sVersion,omitempty"`
	NodePools    []NodePool       `json:"nodePools,omitempty"`
	Networking   *NetworkingConfig `json:"networking,omitempty"`
	Addons       []ClusterAddon   `json:"addons,omitempty"`
}

// UpdateStepRequest is the request to update a step.
type UpdateStepRequest struct {
	Status string  `json:"status"`
	Output *string `json:"output,omitempty"`
	Error  *string `json:"error,omitempty"`
}

// ReportAgentRequest is the request from an agent reporting connection.
type ReportAgentRequest struct {
	Version   string `json:"version"`
	ClusterID string `json:"clusterId,omitempty"`
}

// ----------------------------------------------------------------------------
// View Models
// ----------------------------------------------------------------------------

// CredentialView is the API response for a credential.
type CredentialView struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Provider        string  `json:"provider"`
	Validated       bool    `json:"validated"`
	ValidatedAt     *string `json:"validatedAt,omitempty"`
	ValidationError *string `json:"validationError,omitempty"`
	Region          *string `json:"region,omitempty"`
	Description     *string `json:"description,omitempty"`
	CreatedAt       string  `json:"createdAt"`
}

func ToCredentialView(c *CloudCredential) CredentialView {
	view := CredentialView{
		ID:              c.ID,
		Name:            c.Name,
		Provider:        c.Provider,
		Validated:       c.Validated,
		ValidationError: c.ValidationError,
		Region:          c.Region,
		Description:     c.Description,
		CreatedAt:       c.CreatedAt.Format(time.RFC3339),
	}
	if c.ValidatedAt != nil {
		s := c.ValidatedAt.Format(time.RFC3339)
		view.ValidatedAt = &s
	}
	return view
}

// TemplateView is the API response for a template.
type TemplateView struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Provider    string            `json:"provider"`
	K8sVersion  string            `json:"k8sVersion"`
	NodePools   []NodePool        `json:"nodePools"`
	Networking  *NetworkingConfig `json:"networking,omitempty"`
	Addons      []ClusterAddon    `json:"addons,omitempty"`
	Description *string           `json:"description,omitempty"`
	IsDefault   bool              `json:"isDefault"`
	CreatedAt   string            `json:"createdAt"`
}

func ToTemplateView(t *ClusterTemplate) TemplateView {
	var nodePools []NodePool
	var networking *NetworkingConfig
	var addons []ClusterAddon
	_ = json.Unmarshal(t.NodePools, &nodePools)
	_ = json.Unmarshal(t.Networking, &networking)
	_ = json.Unmarshal(t.Addons, &addons)

	return TemplateView{
		ID:          t.ID,
		Name:        t.Name,
		Provider:    t.Provider,
		K8sVersion:  t.K8sVersion,
		NodePools:   nodePools,
		Networking:  networking,
		Addons:      addons,
		Description: t.Description,
		IsDefault:   t.IsDefault,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
	}
}

// ProvisioningRequestView is the API response for a provisioning request.
type ProvisioningRequestView struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Provider        string                 `json:"provider"`
	Region          string                 `json:"region"`
	K8sVersion      string                 `json:"k8sVersion"`
	NodePools       []NodePool             `json:"nodePools"`
	Status          string                 `json:"status"`
	ClusterID       *string                `json:"clusterId,omitempty"`
	ErrorMessage    *string                `json:"errorMessage,omitempty"`
	TerraformConfig *string                `json:"terraformConfig,omitempty"`
	TerraformVars   map[string]interface{} `json:"terraformVars,omitempty"`
	StartedAt       *string                `json:"startedAt,omitempty"`
	CompletedAt     *string                `json:"completedAt,omitempty"`
	CreatedAt       string                 `json:"createdAt"`
}

func ToProvisioningRequestView(r *ProvisioningRequest) ProvisioningRequestView {
	var nodePools []NodePool
	var tfVars map[string]interface{}
	_ = json.Unmarshal(r.NodePools, &nodePools)
	_ = json.Unmarshal(r.TerraformVars, &tfVars)

	view := ProvisioningRequestView{
		ID:              r.ID,
		Name:            r.Name,
		Provider:        r.Provider,
		Region:          r.Region,
		K8sVersion:      r.K8sVersion,
		NodePools:       nodePools,
		Status:          r.Status,
		ClusterID:       r.ClusterID,
		ErrorMessage:    r.ErrorMessage,
		TerraformConfig: r.TerraformConfig,
		TerraformVars:   tfVars,
		CreatedAt:       r.CreatedAt.Format(time.RFC3339),
	}
	if r.StartedAt != nil {
		s := r.StartedAt.Format(time.RFC3339)
		view.StartedAt = &s
	}
	if r.CompletedAt != nil {
		s := r.CompletedAt.Format(time.RFC3339)
		view.CompletedAt = &s
	}
	return view
}

// InstallSessionView is the API response for an install session.
type InstallSessionView struct {
	ID               string     `json:"id"`
	RequestID        string     `json:"requestId"`
	SessionToken     string     `json:"sessionToken"`
	CurrentStep      string     `json:"currentStep"`
	TotalSteps       int        `json:"totalSteps"`
	CompletedSteps   int        `json:"completedSteps"`
	Steps            []StepInfo `json:"steps"`
	Status           string     `json:"status"`
	BootstrapCommand *string    `json:"bootstrapCommand,omitempty"`
	AgentConnected   bool       `json:"agentConnected"`
	AgentVersion     *string    `json:"agentVersion,omitempty"`
	ExpiresAt        string     `json:"expiresAt"`
	CreatedAt        string     `json:"createdAt"`
}

func ToInstallSessionView(s *InstallSession) InstallSessionView {
	var steps []StepInfo
	_ = json.Unmarshal(s.Steps, &steps)

	return InstallSessionView{
		ID:               s.ID,
		RequestID:        s.RequestID,
		SessionToken:     s.SessionToken,
		CurrentStep:      s.CurrentStep,
		TotalSteps:       s.TotalSteps,
		CompletedSteps:   s.CompletedSteps,
		Steps:            steps,
		Status:           s.Status,
		BootstrapCommand: s.BootstrapCommand,
		AgentConnected:   s.AgentConnected,
		AgentVersion:     s.AgentVersion,
		ExpiresAt:        s.ExpiresAt.Format(time.RFC3339),
		CreatedAt:        s.CreatedAt.Format(time.RFC3339),
	}
}

// StepView is the API response for a step.
type StepView struct {
	ID          string  `json:"id"`
	StepNumber  int     `json:"stepNumber"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Status      string  `json:"status"`
	Output      *string `json:"output,omitempty"`
	Error       *string `json:"error,omitempty"`
	StartedAt   *string `json:"startedAt,omitempty"`
	CompletedAt *string `json:"completedAt,omitempty"`
	DurationMs  *int64  `json:"durationMs,omitempty"`
}

func ToStepView(s *InstallSessionStep) StepView {
	view := StepView{
		ID:          s.ID,
		StepNumber:  s.StepNumber,
		Name:        s.Name,
		Description: s.Description,
		Status:      s.Status,
		Output:      s.Output,
		Error:       s.Error,
		DurationMs:  s.DurationMs,
	}
	if s.StartedAt != nil {
		t := s.StartedAt.Format(time.RFC3339)
		view.StartedAt = &t
	}
	if s.CompletedAt != nil {
		t := s.CompletedAt.Format(time.RFC3339)
		view.CompletedAt = &t
	}
	return view
}

// EventView is the API response for an event.
type EventView struct {
	ID        string                 `json:"id"`
	EventType string                 `json:"eventType"`
	Severity  string                 `json:"severity"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	ActorType *string                `json:"actorType,omitempty"`
	CreatedAt string                 `json:"createdAt"`
}

func ToEventView(e *ProvisioningEvent) EventView {
	var details map[string]interface{}
	_ = json.Unmarshal(e.Details, &details)

	return EventView{
		ID:        e.ID,
		EventType: e.EventType,
		Severity:  e.Severity,
		Message:   e.Message,
		Details:   details,
		ActorType: e.ActorType,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
	}
}
