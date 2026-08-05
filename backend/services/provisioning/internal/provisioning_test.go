package provisioning

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProviderConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"ProviderAWS", ProviderAWS, "aws"},
		{"ProviderAzure", ProviderAzure, "azure"},
		{"ProviderGCP", ProviderGCP, "gcp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.constant)
			}
		})
	}
}

func TestRequestStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"RequestPending", RequestPending, "pending"},
		{"RequestGenerating", RequestGenerating, "generating"},
		{"RequestReady", RequestReady, "ready"},
		{"RequestProvisioning", RequestProvisioning, "provisioning"},
		{"RequestCompleted", RequestCompleted, "completed"},
		{"RequestFailed", RequestFailed, "failed"},
		{"RequestCancelled", RequestCancelled, "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.constant)
			}
		})
	}
}

func TestSessionStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"SessionActive", SessionActive, "active"},
		{"SessionCompleted", SessionCompleted, "completed"},
		{"SessionFailed", SessionFailed, "failed"},
		{"SessionExpired", SessionExpired, "expired"},
		{"SessionCancelled", SessionCancelled, "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.constant)
			}
		})
	}
}

func TestToCredentialView(t *testing.T) {
	now := time.Now()
	validatedAt := now.Add(-time.Hour)
	region := "us-east-1"
	desc := "Production AWS"

	cred := &CloudCredential{
		ID:          "cred-123",
		OrgID:       "org-456",
		Name:        "aws-prod",
		Provider:    ProviderAWS,
		Validated:   true,
		ValidatedAt: &validatedAt,
		Region:      &region,
		Description: &desc,
		CreatedAt:   now,
	}

	view := ToCredentialView(cred)

	if view.ID != "cred-123" {
		t.Errorf("expected ID cred-123, got %s", view.ID)
	}
	if view.Name != "aws-prod" {
		t.Errorf("expected name aws-prod, got %s", view.Name)
	}
	if view.Provider != ProviderAWS {
		t.Errorf("expected provider aws, got %s", view.Provider)
	}
	if !view.Validated {
		t.Error("expected validated true")
	}
	if view.ValidatedAt == nil {
		t.Error("expected validatedAt to be set")
	}
}

func TestToTemplateView(t *testing.T) {
	nodePools := []NodePool{
		{Name: "default", MachineType: "t3.medium", MinNodes: 1, MaxNodes: 3, DesiredNodes: 2},
	}
	nodePoolsJSON, _ := json.Marshal(nodePools)

	tmpl := &ClusterTemplate{
		ID:         "tmpl-123",
		Name:       "Standard",
		Provider:   ProviderAWS,
		K8sVersion: "1.28",
		NodePools:  nodePoolsJSON,
		IsDefault:  true,
		CreatedAt:  time.Now(),
	}

	view := ToTemplateView(tmpl)

	if view.ID != "tmpl-123" {
		t.Errorf("expected ID tmpl-123, got %s", view.ID)
	}
	if view.Provider != ProviderAWS {
		t.Errorf("expected provider aws, got %s", view.Provider)
	}
	if view.K8sVersion != "1.28" {
		t.Errorf("expected k8sVersion 1.28, got %s", view.K8sVersion)
	}
	if len(view.NodePools) != 1 {
		t.Errorf("expected 1 node pool, got %d", len(view.NodePools))
	}
	if !view.IsDefault {
		t.Error("expected isDefault true")
	}
}

func TestToProvisioningRequestView(t *testing.T) {
	nodePools := []NodePool{
		{Name: "default", MachineType: "t3.medium", MinNodes: 1, MaxNodes: 3},
	}
	nodePoolsJSON, _ := json.Marshal(nodePools)
	now := time.Now()

	req := &ProvisioningRequest{
		ID:         "req-123",
		OrgID:      "org-456",
		Name:       "prod-cluster",
		Provider:   ProviderAWS,
		Region:     "us-east-1",
		K8sVersion: "1.28",
		NodePools:  nodePoolsJSON,
		Status:     RequestReady,
		StartedAt:  &now,
		CreatedAt:  now,
	}

	view := ToProvisioningRequestView(req)

	if view.ID != "req-123" {
		t.Errorf("expected ID req-123, got %s", view.ID)
	}
	if view.Name != "prod-cluster" {
		t.Errorf("expected name prod-cluster, got %s", view.Name)
	}
	if view.Provider != ProviderAWS {
		t.Errorf("expected provider aws, got %s", view.Provider)
	}
	if view.Region != "us-east-1" {
		t.Errorf("expected region us-east-1, got %s", view.Region)
	}
	if view.Status != RequestReady {
		t.Errorf("expected status ready, got %s", view.Status)
	}
}

func TestToInstallSessionView(t *testing.T) {
	steps := []StepInfo{
		{Number: 1, Name: "init", Status: StepCompleted},
		{Number: 2, Name: "plan", Status: StepRunning},
	}
	stepsJSON, _ := json.Marshal(steps)
	cmd := "kubectl apply -f ..."

	session := &InstallSession{
		ID:               "sess-123",
		RequestID:        "req-456",
		SessionToken:     "token-abc",
		CurrentStep:      "plan",
		TotalSteps:       8,
		CompletedSteps:   1,
		Steps:            stepsJSON,
		Status:           SessionActive,
		BootstrapCommand: &cmd,
		AgentConnected:   false,
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		CreatedAt:        time.Now(),
	}

	view := ToInstallSessionView(session)

	if view.ID != "sess-123" {
		t.Errorf("expected ID sess-123, got %s", view.ID)
	}
	if view.CurrentStep != "plan" {
		t.Errorf("expected currentStep plan, got %s", view.CurrentStep)
	}
	if view.TotalSteps != 8 {
		t.Errorf("expected totalSteps 8, got %d", view.TotalSteps)
	}
	if view.CompletedSteps != 1 {
		t.Errorf("expected completedSteps 1, got %d", view.CompletedSteps)
	}
	if len(view.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(view.Steps))
	}
	if view.BootstrapCommand == nil || *view.BootstrapCommand != cmd {
		t.Error("expected bootstrapCommand to match")
	}
}

func TestToStepView(t *testing.T) {
	now := time.Now()
	completed := now.Add(time.Minute)
	durationMs := int64(60000)
	output := "Success"

	step := &InstallSessionStep{
		ID:          "step-123",
		SessionID:   "sess-456",
		StepNumber:  1,
		Name:        "terraform_init",
		Status:      StepCompleted,
		Output:      &output,
		StartedAt:   &now,
		CompletedAt: &completed,
		DurationMs:  &durationMs,
	}

	view := ToStepView(step)

	if view.ID != "step-123" {
		t.Errorf("expected ID step-123, got %s", view.ID)
	}
	if view.StepNumber != 1 {
		t.Errorf("expected stepNumber 1, got %d", view.StepNumber)
	}
	if view.Name != "terraform_init" {
		t.Errorf("expected name terraform_init, got %s", view.Name)
	}
	if view.Status != StepCompleted {
		t.Errorf("expected status completed, got %s", view.Status)
	}
	if view.DurationMs == nil || *view.DurationMs != 60000 {
		t.Error("expected durationMs 60000")
	}
}

func TestToEventView(t *testing.T) {
	actorType := ActorUser
	details := map[string]string{"region": "us-east-1"}
	detailsJSON, _ := json.Marshal(details)

	event := &ProvisioningEvent{
		ID:        "evt-123",
		OrgID:     "org-456",
		EventType: "provisioning.started",
		Severity:  SeverityInfo,
		Message:   "Provisioning started",
		Details:   detailsJSON,
		ActorType: &actorType,
		CreatedAt: time.Now(),
	}

	view := ToEventView(event)

	if view.ID != "evt-123" {
		t.Errorf("expected ID evt-123, got %s", view.ID)
	}
	if view.EventType != "provisioning.started" {
		t.Errorf("expected eventType provisioning.started, got %s", view.EventType)
	}
	if view.Severity != SeverityInfo {
		t.Errorf("expected severity info, got %s", view.Severity)
	}
	if view.ActorType == nil || *view.ActorType != ActorUser {
		t.Error("expected actorType user")
	}
}

func TestTerraformGenerator_Generate(t *testing.T) {
	gen := NewTerraformGenerator()

	cfg := TerraformConfig{
		ClusterName: "test-cluster",
		Region:      "us-east-1",
		K8sVersion:  "1.28",
		NodePools: []NodePool{
			{
				Name:         "default",
				MachineType:  "t3.medium",
				MinNodes:     1,
				MaxNodes:     3,
				DesiredNodes: 2,
				DiskSizeGB:   50,
			},
		},
		AWS: &AWSConfig{
			VPCCidr:          "10.0.0.0/16",
			EnableNATGateway: true,
		},
	}

	result, err := gen.Generate(ProviderAWS, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.MainTF == "" {
		t.Error("expected MainTF to be non-empty")
	}
	if result.VariablesTF == "" {
		t.Error("expected VariablesTF to be non-empty")
	}
	if result.OutputsTF == "" {
		t.Error("expected OutputsTF to be non-empty")
	}
	if result.TFVars == nil {
		t.Error("expected TFVars to be non-nil")
	}

	// Check TFVars
	if result.TFVars["cluster_name"] != "test-cluster" {
		t.Errorf("expected cluster_name test-cluster, got %v", result.TFVars["cluster_name"])
	}
	if result.TFVars["region"] != "us-east-1" {
		t.Errorf("expected region us-east-1, got %v", result.TFVars["region"])
	}
}

func TestTerraformGenerator_Azure(t *testing.T) {
	gen := NewTerraformGenerator()

	cfg := TerraformConfig{
		ClusterName: "azure-cluster",
		Region:      "eastus",
		K8sVersion:  "1.28",
		NodePools: []NodePool{
			{Name: "default", MachineType: "Standard_D2s_v3", MinNodes: 1, MaxNodes: 3, DesiredNodes: 2},
		},
		Azure: &AzureConfig{
			ResourceGroup: "test-rg",
			VNetCidr:      "10.0.0.0/16",
			DNSPrefix:     "test",
		},
	}

	result, err := gen.Generate(ProviderAzure, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.MainTF == "" {
		t.Error("expected MainTF to be non-empty")
	}
}

func TestTerraformGenerator_GCP(t *testing.T) {
	gen := NewTerraformGenerator()

	cfg := TerraformConfig{
		ClusterName: "gcp-cluster",
		Region:      "us-central1",
		K8sVersion:  "1.28",
		NodePools: []NodePool{
			{Name: "default", MachineType: "e2-medium", MinNodes: 1, MaxNodes: 3, DesiredNodes: 2},
		},
		GCP: &GCPConfig{
			ProjectID:      "my-project",
			MasterIPv4CIDR: "172.16.0.0/28",
		},
	}

	result, err := gen.Generate(ProviderGCP, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.MainTF == "" {
		t.Error("expected MainTF to be non-empty")
	}
}

func TestGenerateBootstrapToken(t *testing.T) {
	token, hash, err := GenerateBootstrapToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token == "" {
		t.Error("expected token to be non-empty")
	}
	if hash == "" {
		t.Error("expected hash to be non-empty")
	}

	// Verify hash matches token
	computedHash := HashToken(token)
	if computedHash != hash {
		t.Error("hash mismatch")
	}
}

func TestHashToken(t *testing.T) {
	token := "test-token-123"
	hash1 := HashToken(token)
	hash2 := HashToken(token)

	if hash1 != hash2 {
		t.Error("same token should produce same hash")
	}

	if hash1 == "" {
		t.Error("hash should not be empty")
	}

	// Different tokens should have different hashes
	hash3 := HashToken("different-token")
	if hash1 == hash3 {
		t.Error("different tokens should have different hashes")
	}
}

func TestBootstrapGenerator_GenerateCommand(t *testing.T) {
	gen := NewBootstrapGenerator("https://api.example.com", "ghcr.io/test/agent:latest")

	token := "test-bootstrap-token"
	expiresAt := time.Now().Add(24 * time.Hour)

	cmd, err := gen.GenerateCommand("my-cluster", token, expiresAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.Command == "" {
		t.Error("expected command to be non-empty")
	}
	if cmd.Manifest == "" {
		t.Error("expected manifest to be non-empty")
	}
	if len(cmd.Instructions) == 0 {
		t.Error("expected instructions to be non-empty")
	}
	if cmd.Token != token {
		t.Errorf("expected token %s, got %s", token, cmd.Token)
	}
}

func TestDefaultInstallSteps(t *testing.T) {
	if len(DefaultInstallSteps) == 0 {
		t.Error("expected default install steps to be non-empty")
	}

	// Check that steps are numbered sequentially
	for i, step := range DefaultInstallSteps {
		if step.Number != i+1 {
			t.Errorf("expected step %d to have number %d, got %d", i, i+1, step.Number)
		}
		if step.Name == "" {
			t.Errorf("step %d has empty name", step.Number)
		}
	}
}

func TestAWSProvider_ValidateCredentials(t *testing.T) {
	provider := &AWSProvider{}

	// Valid credentials
	validCreds := AWSCredentials{
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	credsJSON, _ := json.Marshal(validCreds)

	err := provider.ValidateCredentials(nil, credsJSON)
	if err != nil {
		t.Errorf("expected no error for valid credentials, got: %v", err)
	}

	// Missing access key
	invalidCreds := AWSCredentials{
		SecretAccessKey: "secret",
	}
	credsJSON, _ = json.Marshal(invalidCreds)

	err = provider.ValidateCredentials(nil, credsJSON)
	if err == nil {
		t.Error("expected error for missing access key")
	}
}

func TestAWSProvider_ListRegions(t *testing.T) {
	provider := &AWSProvider{}

	regions, err := provider.ListRegions(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(regions) == 0 {
		t.Error("expected regions to be non-empty")
	}

	// Check us-east-1 exists
	found := false
	for _, r := range regions {
		if r.ID == "us-east-1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected us-east-1 in regions")
	}
}

func TestAWSProvider_ListMachineTypes(t *testing.T) {
	provider := &AWSProvider{}

	types, err := provider.ListMachineTypes(nil, nil, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(types) == 0 {
		t.Error("expected machine types to be non-empty")
	}

	// Check t3.medium exists
	found := false
	for _, mt := range types {
		if mt.ID == "t3.medium" {
			found = true
			if mt.VCPU != 2 {
				t.Errorf("expected t3.medium to have 2 vCPU, got %d", mt.VCPU)
			}
			break
		}
	}
	if !found {
		t.Error("expected t3.medium in machine types")
	}
}

func TestProviderRegistry_Get(t *testing.T) {
	registry := NewProviderRegistry()

	// Valid providers
	for _, name := range []string{ProviderAWS, ProviderAzure, ProviderGCP} {
		_, err := registry.Get(name)
		if err != nil {
			t.Errorf("expected no error for %s, got: %v", name, err)
		}
	}

	// Invalid provider
	_, err := registry.Get("invalid")
	if err == nil {
		t.Error("expected error for invalid provider")
	}
}

func TestGetDefaultMachineType(t *testing.T) {
	tests := []struct {
		provider string
		expected string
	}{
		{ProviderAWS, "t3.medium"},
		{ProviderAzure, "Standard_D2s_v3"},
		{ProviderGCP, "e2-medium"},
		{"unknown", "t3.medium"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			result := getDefaultMachineType(tt.provider)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}
