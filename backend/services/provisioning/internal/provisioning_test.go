package provisioning

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sts"
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
	configJSON, _ := json.Marshal(ProvisioningConfig{
		Networking: &NetworkingConfig{VPCCidr: "10.50.0.0/16", PrivateCluster: true},
		Addons:     []ClusterAddon{{Name: "vpc-cni", Enabled: true}},
	})
	now := time.Now()

	req := &ProvisioningRequest{
		ID:         "req-123",
		OrgID:      "org-456",
		Name:       "prod-cluster",
		Provider:   ProviderAWS,
		Region:     "us-east-1",
		K8sVersion: "1.28",
		NodePools:  nodePoolsJSON,
		Config:     configJSON,
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
	if view.Networking == nil || view.Networking.VPCCidr != "10.50.0.0/16" || !view.Networking.PrivateCluster {
		t.Errorf("unexpected networking view: %#v", view.Networking)
	}
	if len(view.Addons) != 1 || view.Addons[0].Name != "vpc-cni" || !view.Addons[0].Enabled {
		t.Errorf("unexpected addons view: %#v", view.Addons)
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
			ServiceCidr:      "10.80.0.0/16",
			EnableNATGateway: true,
			PrivateCluster:   true,
		},
		Addons: []ClusterAddon{
			{Name: "vpc-cni", Enabled: true},
			{Name: "kube-proxy", Enabled: false},
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
	if result.TFVars["service_cidr"] != "10.80.0.0/16" {
		t.Errorf("expected custom service CIDR, got %v", result.TFVars["service_cidr"])
	}
	if result.TFVars["private_cluster"] != true {
		t.Errorf("expected private_cluster true, got %v", result.TFVars["private_cluster"])
	}
	addons, ok := result.TFVars["addons"].(map[string]bool)
	if !ok || !addons["vpc-cni"] || addons["kube-proxy"] {
		t.Errorf("unexpected addons vars: %#v", result.TFVars["addons"])
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

func TestGeneratedTerraformParses(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform CLI is not installed")
	}

	tests := []struct {
		name     string
		provider string
		config   TerraformConfig
	}{
		{
			name:     "aws",
			provider: ProviderAWS,
			config: TerraformConfig{
				ClusterName: "parse-aws",
				Region:      "us-east-1",
				NodePools:   []NodePool{{Name: "default", MachineType: "t3.medium", MinNodes: 1, MaxNodes: 2, DesiredNodes: 1}},
				Addons:      []ClusterAddon{{Name: "vpc-cni", Enabled: true}},
				AWS:         &AWSConfig{VPCCidr: "10.0.0.0/16", ServiceCidr: "10.96.0.0/12"},
			},
		},
		{
			name:     "azure",
			provider: ProviderAzure,
			config: TerraformConfig{
				ClusterName: "parse-azure",
				Region:      "eastus",
				NodePools:   []NodePool{{Name: "default", MachineType: "Standard_D2s_v3", MinNodes: 1, MaxNodes: 2, DesiredNodes: 1}},
				Azure:       &AzureConfig{ResourceGroup: "parse-rg", VNetCidr: "10.0.0.0/16", PodCidr: "10.244.0.0/16", ServiceCidr: "10.96.0.0/12", DNSPrefix: "parse"},
			},
		},
		{
			name:     "gcp",
			provider: ProviderGCP,
			config: TerraformConfig{
				ClusterName: "parse-gcp",
				Region:      "us-central1",
				NodePools:   []NodePool{{Name: "default", MachineType: "e2-medium", MinNodes: 1, MaxNodes: 2, DesiredNodes: 1}},
				GCP:         &GCPConfig{ProjectID: "project", SubnetCidr: "10.0.0.0/20", PodCidr: "10.1.0.0/16", ServiceCidr: "10.2.0.0/20", MasterIPv4CIDR: "172.16.0.0/28"},
			},
		},
	}

	generator := NewTerraformGenerator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generated, err := generator.Generate(tt.provider, tt.config)
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command("terraform", "fmt", "-")
			command.Stdin = strings.NewReader(generated.MainTF + "\n" + generated.VariablesTF + "\n" + generated.OutputsTF)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("generated Terraform does not parse: %v\n%s", err, output)
			}
		})
	}
}

func TestGeneratedTerraformValidates(t *testing.T) {
	if os.Getenv("RUN_TERRAFORM_VALIDATE") != "1" {
		t.Skip("set RUN_TERRAFORM_VALIDATE=1 to download providers/modules and run terraform validate")
	}
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform CLI is not installed")
	}

	tests := []struct {
		name     string
		provider string
		config   TerraformConfig
	}{
		{
			name:     "aws",
			provider: ProviderAWS,
			config: TerraformConfig{
				ClusterName: "validate-aws",
				Region:      "us-east-1",
				NodePools:   []NodePool{{Name: "default", MachineType: "t3.medium", MinNodes: 1, MaxNodes: 2, DesiredNodes: 1}},
				Addons:      []ClusterAddon{{Name: "vpc-cni", Enabled: true}},
				AWS:         &AWSConfig{VPCCidr: "10.0.0.0/16", ServiceCidr: "10.96.0.0/12"},
			},
		},
		{
			name:     "azure",
			provider: ProviderAzure,
			config: TerraformConfig{
				ClusterName: "validate-azure",
				Region:      "eastus",
				NodePools:   []NodePool{{Name: "default", MachineType: "Standard_D2s_v3", MinNodes: 1, MaxNodes: 2, DesiredNodes: 1}},
				Addons:      []ClusterAddon{{Name: "azure-policy", Enabled: true}},
				Azure:       &AzureConfig{ResourceGroup: "validate-rg", VNetCidr: "10.0.0.0/16", PodCidr: "10.244.0.0/16", ServiceCidr: "10.96.0.0/12", DNSPrefix: "validate"},
			},
		},
		{
			name:     "gcp",
			provider: ProviderGCP,
			config: TerraformConfig{
				ClusterName: "validate-gcp",
				Region:      "us-central1",
				NodePools:   []NodePool{{Name: "default", MachineType: "e2-medium", MinNodes: 1, MaxNodes: 2, DesiredNodes: 1}},
				Addons:      []ClusterAddon{{Name: "horizontal-pod-autoscaling", Enabled: true}},
				GCP:         &GCPConfig{ProjectID: "project", SubnetCidr: "10.0.0.0/20", PodCidr: "10.1.0.0/16", ServiceCidr: "10.2.0.0/20", MasterIPv4CIDR: "172.16.0.0/28"},
			},
		},
	}

	generator := NewTerraformGenerator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generated, err := generator.Generate(tt.provider, tt.config)
			if err != nil {
				t.Fatal(err)
			}
			dir, err := os.MkdirTemp("", "bds-terraform-validate-*")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				// Provider executables can remain briefly locked on Windows
				// after terraform exits. Cleanup is best-effort; the OS temp
				// directory remains the fallback cleanup boundary.
				_ = os.RemoveAll(dir)
			})
			configuration := generated.MainTF + "\n" + generated.VariablesTF + "\n" + generated.OutputsTF
			if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(configuration), 0o600); err != nil {
				t.Fatal(err)
			}
			vars, err := json.Marshal(generated.TFVars)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars.json"), vars, 0o600); err != nil {
				t.Fatal(err)
			}

			for _, args := range [][]string{
				{"init", "-backend=false", "-input=false", "-no-color"},
				{"validate", "-no-color"},
			} {
				command := exec.Command("terraform", args...)
				command.Dir = dir
				command.Env = append(os.Environ(), "TF_IN_AUTOMATION=1", "CHECKPOINT_DISABLE=1")
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("terraform %s failed: %v\n%s", args[0], err, output)
				}
			}
		})
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

// NOTE: TestBootstrapGenerator_GenerateCommand removed - BootstrapGenerator was removed
// as part of Phase 4 migration to use cluster service's agent manifest.

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

// Note: TestAWSProvider_ValidateCredentials was replaced by
// TestAWSProvider_ValidateCredentials_MissingFields and
// TestAWSProvider_ValidateCredentials_MockedSTS which use mocked clients.

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
		if r.Code == "us-east-1" {
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
		if mt.Name == "t3.medium" {
			found = true
			if mt.VCPUs != 2 {
				t.Errorf("expected t3.medium to have 2 vCPU, got %d", mt.VCPUs)
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

// ----------------------------------------------------------------------------
// Cloud Provider Validation Tests
// ----------------------------------------------------------------------------

func TestAWSProvider_ValidateCredentials_MissingFields(t *testing.T) {
	provider := &AWSProvider{}
	ctx := t.Context()

	tests := []struct {
		name        string
		creds       string
		errContains string
	}{
		{
			name:        "invalid JSON",
			creds:       `{invalid`,
			errContains: "invalid AWS credentials format",
		},
		{
			name:        "missing accessKeyId",
			creds:       `{"secretAccessKey": "secret"}`,
			errContains: "accessKeyId is required",
		},
		{
			name:        "missing secretAccessKey",
			creds:       `{"accessKeyId": "AKIAIOSFODNN7EXAMPLE"}`,
			errContains: "secretAccessKey is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.ValidateCredentials(ctx, []byte(tt.creds))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestAWSProvider_ValidateCredentials_MockedSTS(t *testing.T) {
	ctx := t.Context()

	// Save original factory
	originalFactory := AWSSTSClientFactory
	defer func() { AWSSTSClientFactory = originalFactory }()

	t.Run("valid credentials", func(t *testing.T) {
		AWSSTSClientFactory = func(ctx context.Context, creds AWSCredentials) (STSClient, error) {
			return &mockSTSClient{returnErr: nil}, nil
		}

		provider := &AWSProvider{}
		creds := `{"accessKeyId": "AKIAIOSFODNN7EXAMPLE", "secretAccessKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}`
		err := provider.ValidateCredentials(ctx, []byte(creds))
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("invalid credentials - STS error", func(t *testing.T) {
		AWSSTSClientFactory = func(ctx context.Context, creds AWSCredentials) (STSClient, error) {
			return &mockSTSClient{returnErr: errors.New("InvalidClientTokenId: The security token included in the request is invalid")}, nil
		}

		provider := &AWSProvider{}
		creds := `{"accessKeyId": "AKIAINVALIDKEY", "secretAccessKey": "invalidsecret"}`
		err := provider.ValidateCredentials(ctx, []byte(creds))
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "InvalidClientTokenId") {
			t.Errorf("error should contain AWS error message, got: %v", err)
		}
	})
}

func TestAzureProvider_ValidateCredentials_MissingFields(t *testing.T) {
	provider := &AzureProvider{}
	ctx := t.Context()

	tests := []struct {
		name        string
		creds       string
		errContains string
	}{
		{
			name:        "invalid JSON",
			creds:       `{invalid`,
			errContains: "invalid Azure credentials format",
		},
		{
			name:        "missing subscriptionId",
			creds:       `{"tenantId": "t", "clientId": "c", "clientSecret": "s"}`,
			errContains: "subscriptionId is required",
		},
		{
			name:        "missing tenantId",
			creds:       `{"subscriptionId": "s", "clientId": "c", "clientSecret": "s"}`,
			errContains: "tenantId is required",
		},
		{
			name:        "missing clientId",
			creds:       `{"subscriptionId": "s", "tenantId": "t", "clientSecret": "s"}`,
			errContains: "clientId is required",
		},
		{
			name:        "missing clientSecret",
			creds:       `{"subscriptionId": "s", "tenantId": "t", "clientId": "c"}`,
			errContains: "clientSecret is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.ValidateCredentials(ctx, []byte(tt.creds))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestAzureProvider_ValidateCredentials_MockedHTTP(t *testing.T) {
	ctx := t.Context()

	// Save original factory
	originalFactory := AzureHTTPClientFactory
	defer func() { AzureHTTPClientFactory = originalFactory }()

	creds := `{"subscriptionId": "sub-123", "tenantId": "tenant-456", "clientId": "client-789", "clientSecret": "secret"}`

	t.Run("valid credentials", func(t *testing.T) {
		AzureHTTPClientFactory = func() AzureHTTPClient {
			return &mockHTTPClient{
				statusCode: 200,
				body:       `{"access_token": "token", "token_type": "Bearer", "expires_in": 3600}`,
			}
		}

		provider := &AzureProvider{}
		err := provider.ValidateCredentials(ctx, []byte(creds))
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("invalid credentials - unauthorized", func(t *testing.T) {
		AzureHTTPClientFactory = func() AzureHTTPClient {
			return &mockHTTPClient{
				statusCode: 401,
				body:       `{"error": "invalid_client", "error_description": "AADSTS7000215: Invalid client secret provided."}`,
			}
		}

		provider := &AzureProvider{}
		err := provider.ValidateCredentials(ctx, []byte(creds))
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "invalid_client") {
			t.Errorf("error should contain Azure error code, got: %v", err)
		}
	})
}

func TestGCPProvider_ValidateCredentials_MissingFields(t *testing.T) {
	provider := &GCPProvider{}
	ctx := t.Context()

	tests := []struct {
		name        string
		creds       string
		errContains string
	}{
		{
			name:        "invalid JSON",
			creds:       `{invalid`,
			errContains: "invalid GCP credentials format",
		},
		{
			name:        "missing projectId",
			creds:       `{"serviceAccountJson": "{}"}`,
			errContains: "projectId is required",
		},
		{
			name:        "missing serviceAccountJson",
			creds:       `{"projectId": "my-project"}`,
			errContains: "serviceAccountJson is required",
		},
		{
			name:        "invalid service account type",
			creds:       `{"projectId": "my-project", "serviceAccountJson": "{\"type\": \"user\", \"client_email\": \"e\", \"private_key\": \"k\"}"}`,
			errContains: "type must be 'service_account'",
		},
		{
			name:        "missing client_email",
			creds:       `{"projectId": "my-project", "serviceAccountJson": "{\"type\": \"service_account\", \"private_key\": \"k\"}"}`,
			errContains: "client_email is required",
		},
		{
			name:        "missing private_key",
			creds:       `{"projectId": "my-project", "serviceAccountJson": "{\"type\": \"service_account\", \"client_email\": \"e\"}"}`,
			errContains: "private_key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.ValidateCredentials(ctx, []byte(tt.creds))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestGCPProvider_ValidateCredentials_MockedHTTP(t *testing.T) {
	ctx := t.Context()

	// Save original factory
	originalFactory := GCPHTTPClientFactory
	defer func() { GCPHTTPClientFactory = originalFactory }()

	// Generate a test RSA private key for JWT signing
	testPrivateKey := generateTestRSAPrivateKeyPEM(t)

	// Build valid GCP credentials with the test private key
	serviceAccountJSON := fmt.Sprintf(`{
		"type": "service_account",
		"project_id": "test-project",
		"private_key_id": "key123",
		"private_key": %s,
		"client_email": "test@test-project.iam.gserviceaccount.com",
		"client_id": "123456789",
		"auth_uri": "https://accounts.google.com/o/oauth2/auth",
		"token_uri": "https://oauth2.googleapis.com/token"
	}`, strconv.Quote(testPrivateKey))

	creds := fmt.Sprintf(`{"projectId": "test-project", "serviceAccountJson": %s}`, strconv.Quote(serviceAccountJSON))

	t.Run("valid credentials", func(t *testing.T) {
		GCPHTTPClientFactory = func() GCPHTTPClient {
			return &mockHTTPClient{
				statusCode: 200,
				body:       `{"access_token": "ya29.test-token", "token_type": "Bearer", "expires_in": 3600}`,
			}
		}

		provider := &GCPProvider{}
		err := provider.ValidateCredentials(ctx, []byte(creds))
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("invalid credentials - invalid_grant", func(t *testing.T) {
		GCPHTTPClientFactory = func() GCPHTTPClient {
			return &mockHTTPClient{
				statusCode: 400,
				body:       `{"error": "invalid_grant", "error_description": "Invalid JWT Signature."}`,
			}
		}

		provider := &GCPProvider{}
		err := provider.ValidateCredentials(ctx, []byte(creds))
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "invalid_grant") {
			t.Errorf("error should contain GCP error code 'invalid_grant', got: %v", err)
		}
		if !strings.Contains(err.Error(), "Invalid JWT Signature") {
			t.Errorf("error should contain GCP error description, got: %v", err)
		}
	})

	t.Run("invalid credentials - unauthorized_client", func(t *testing.T) {
		GCPHTTPClientFactory = func() GCPHTTPClient {
			return &mockHTTPClient{
				statusCode: 401,
				body:       `{"error": "unauthorized_client", "error_description": "Client is unauthorized to retrieve access tokens using this method."}`,
			}
		}

		provider := &GCPProvider{}
		err := provider.ValidateCredentials(ctx, []byte(creds))
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "unauthorized_client") {
			t.Errorf("error should contain GCP error code, got: %v", err)
		}
	})
}

// generateTestRSAPrivateKeyPEM generates a test RSA private key in PEM format.
func generateTestRSAPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate test RSA key: %v", err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal test RSA key: %v", err)
	}
	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	}
	return string(pem.EncodeToMemory(pemBlock))
}

// ----------------------------------------------------------------------------
// Mock Helpers
// ----------------------------------------------------------------------------

type mockSTSClient struct {
	returnErr error
}

func (m *mockSTSClient) GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	account := "123456789012"
	arn := "arn:aws:iam::123456789012:user/test"
	userID := "AIDAEXAMPLE"
	return &sts.GetCallerIdentityOutput{
		Account: &account,
		Arn:     &arn,
		UserId:  &userID,
	}, nil
}

type mockHTTPClient struct {
	statusCode int
	body       string
	err        error
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader(m.body)),
		Header:     make(http.Header),
	}, nil
}
