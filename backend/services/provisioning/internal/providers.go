package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
)

// CloudProvider is the interface for cloud provider operations.
type CloudProvider interface {
	ValidateCredentials(ctx context.Context, creds []byte) error
	ListRegions(ctx context.Context, creds []byte) ([]Region, error)
	ListMachineTypes(ctx context.Context, creds []byte, region string) ([]MachineType, error)
	ListKubernetesVersions(ctx context.Context, creds []byte, region string) ([]string, error)
}

// Region represents a cloud region.
type Region struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// MachineType represents a VM/instance type.
type MachineType struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	VCPU        int     `json:"vcpu"`
	MemoryGB    float64 `json:"memoryGb"`
	Description string  `json:"description,omitempty"`
}

// ProviderRegistry holds cloud provider implementations.
type ProviderRegistry struct {
	providers map[string]CloudProvider
}

// NewProviderRegistry creates a new provider registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: map[string]CloudProvider{
			ProviderAWS:   &AWSProvider{},
			ProviderAzure: &AzureProvider{},
			ProviderGCP:   &GCPProvider{},
		},
	}
}

// Get returns a cloud provider by name.
func (r *ProviderRegistry) Get(name string) (CloudProvider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
	return p, nil
}

// ----------------------------------------------------------------------------
// AWS Provider
// ----------------------------------------------------------------------------

// AWSProvider implements CloudProvider for AWS.
type AWSProvider struct{}

// ValidateCredentials validates AWS credentials.
func (p *AWSProvider) ValidateCredentials(ctx context.Context, creds []byte) error {
	var awsCreds AWSCredentials
	if err := json.Unmarshal(creds, &awsCreds); err != nil {
		return fmt.Errorf("invalid AWS credentials format: %w", err)
	}

	if awsCreds.AccessKeyID == "" {
		return fmt.Errorf("accessKeyId is required")
	}
	if awsCreds.SecretAccessKey == "" {
		return fmt.Errorf("secretAccessKey is required")
	}

	// In production, use AWS SDK to validate:
	// cfg, err := config.LoadDefaultConfig(ctx, ...)
	// stsClient := sts.NewFromConfig(cfg)
	// _, err = stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})

	return nil
}

// ListRegions returns AWS regions.
func (p *AWSProvider) ListRegions(ctx context.Context, creds []byte) ([]Region, error) {
	// In production, use AWS SDK to list regions dynamically
	return []Region{
		{ID: "us-east-1", Name: "us-east-1", DisplayName: "US East (N. Virginia)"},
		{ID: "us-east-2", Name: "us-east-2", DisplayName: "US East (Ohio)"},
		{ID: "us-west-1", Name: "us-west-1", DisplayName: "US West (N. California)"},
		{ID: "us-west-2", Name: "us-west-2", DisplayName: "US West (Oregon)"},
		{ID: "eu-west-1", Name: "eu-west-1", DisplayName: "Europe (Ireland)"},
		{ID: "eu-west-2", Name: "eu-west-2", DisplayName: "Europe (London)"},
		{ID: "eu-central-1", Name: "eu-central-1", DisplayName: "Europe (Frankfurt)"},
		{ID: "ap-southeast-1", Name: "ap-southeast-1", DisplayName: "Asia Pacific (Singapore)"},
		{ID: "ap-southeast-2", Name: "ap-southeast-2", DisplayName: "Asia Pacific (Sydney)"},
		{ID: "ap-northeast-1", Name: "ap-northeast-1", DisplayName: "Asia Pacific (Tokyo)"},
	}, nil
}

// ListMachineTypes returns AWS instance types for EKS.
func (p *AWSProvider) ListMachineTypes(ctx context.Context, creds []byte, region string) ([]MachineType, error) {
	return []MachineType{
		{ID: "t3.medium", Name: "t3.medium", VCPU: 2, MemoryGB: 4, Description: "General purpose"},
		{ID: "t3.large", Name: "t3.large", VCPU: 2, MemoryGB: 8, Description: "General purpose"},
		{ID: "t3.xlarge", Name: "t3.xlarge", VCPU: 4, MemoryGB: 16, Description: "General purpose"},
		{ID: "m5.large", Name: "m5.large", VCPU: 2, MemoryGB: 8, Description: "General purpose"},
		{ID: "m5.xlarge", Name: "m5.xlarge", VCPU: 4, MemoryGB: 16, Description: "General purpose"},
		{ID: "m5.2xlarge", Name: "m5.2xlarge", VCPU: 8, MemoryGB: 32, Description: "General purpose"},
		{ID: "c5.large", Name: "c5.large", VCPU: 2, MemoryGB: 4, Description: "Compute optimized"},
		{ID: "c5.xlarge", Name: "c5.xlarge", VCPU: 4, MemoryGB: 8, Description: "Compute optimized"},
		{ID: "r5.large", Name: "r5.large", VCPU: 2, MemoryGB: 16, Description: "Memory optimized"},
		{ID: "r5.xlarge", Name: "r5.xlarge", VCPU: 4, MemoryGB: 32, Description: "Memory optimized"},
	}, nil
}

// ListKubernetesVersions returns supported EKS versions.
func (p *AWSProvider) ListKubernetesVersions(ctx context.Context, creds []byte, region string) ([]string, error) {
	return []string{"1.30", "1.29", "1.28", "1.27"}, nil
}

// ----------------------------------------------------------------------------
// Azure Provider
// ----------------------------------------------------------------------------

// AzureProvider implements CloudProvider for Azure.
type AzureProvider struct{}

// ValidateCredentials validates Azure credentials.
func (p *AzureProvider) ValidateCredentials(ctx context.Context, creds []byte) error {
	var azureCreds AzureCredentials
	if err := json.Unmarshal(creds, &azureCreds); err != nil {
		return fmt.Errorf("invalid Azure credentials format: %w", err)
	}

	if azureCreds.SubscriptionID == "" {
		return fmt.Errorf("subscriptionId is required")
	}
	if azureCreds.TenantID == "" {
		return fmt.Errorf("tenantId is required")
	}
	if azureCreds.ClientID == "" {
		return fmt.Errorf("clientId is required")
	}
	if azureCreds.ClientSecret == "" {
		return fmt.Errorf("clientSecret is required")
	}

	// In production, use Azure SDK to validate credentials

	return nil
}

// ListRegions returns Azure regions.
func (p *AzureProvider) ListRegions(ctx context.Context, creds []byte) ([]Region, error) {
	return []Region{
		{ID: "eastus", Name: "eastus", DisplayName: "East US"},
		{ID: "eastus2", Name: "eastus2", DisplayName: "East US 2"},
		{ID: "westus", Name: "westus", DisplayName: "West US"},
		{ID: "westus2", Name: "westus2", DisplayName: "West US 2"},
		{ID: "westeurope", Name: "westeurope", DisplayName: "West Europe"},
		{ID: "northeurope", Name: "northeurope", DisplayName: "North Europe"},
		{ID: "uksouth", Name: "uksouth", DisplayName: "UK South"},
		{ID: "southeastasia", Name: "southeastasia", DisplayName: "Southeast Asia"},
		{ID: "australiaeast", Name: "australiaeast", DisplayName: "Australia East"},
		{ID: "japaneast", Name: "japaneast", DisplayName: "Japan East"},
	}, nil
}

// ListMachineTypes returns Azure VM sizes for AKS.
func (p *AzureProvider) ListMachineTypes(ctx context.Context, creds []byte, region string) ([]MachineType, error) {
	return []MachineType{
		{ID: "Standard_D2s_v3", Name: "Standard_D2s_v3", VCPU: 2, MemoryGB: 8, Description: "General purpose"},
		{ID: "Standard_D4s_v3", Name: "Standard_D4s_v3", VCPU: 4, MemoryGB: 16, Description: "General purpose"},
		{ID: "Standard_D8s_v3", Name: "Standard_D8s_v3", VCPU: 8, MemoryGB: 32, Description: "General purpose"},
		{ID: "Standard_D16s_v3", Name: "Standard_D16s_v3", VCPU: 16, MemoryGB: 64, Description: "General purpose"},
		{ID: "Standard_E2s_v3", Name: "Standard_E2s_v3", VCPU: 2, MemoryGB: 16, Description: "Memory optimized"},
		{ID: "Standard_E4s_v3", Name: "Standard_E4s_v3", VCPU: 4, MemoryGB: 32, Description: "Memory optimized"},
		{ID: "Standard_F2s_v2", Name: "Standard_F2s_v2", VCPU: 2, MemoryGB: 4, Description: "Compute optimized"},
		{ID: "Standard_F4s_v2", Name: "Standard_F4s_v2", VCPU: 4, MemoryGB: 8, Description: "Compute optimized"},
	}, nil
}

// ListKubernetesVersions returns supported AKS versions.
func (p *AzureProvider) ListKubernetesVersions(ctx context.Context, creds []byte, region string) ([]string, error) {
	return []string{"1.30", "1.29", "1.28", "1.27"}, nil
}

// ----------------------------------------------------------------------------
// GCP Provider
// ----------------------------------------------------------------------------

// GCPProvider implements CloudProvider for GCP.
type GCPProvider struct{}

// ValidateCredentials validates GCP credentials.
func (p *GCPProvider) ValidateCredentials(ctx context.Context, creds []byte) error {
	var gcpCreds GCPCredentials
	if err := json.Unmarshal(creds, &gcpCreds); err != nil {
		return fmt.Errorf("invalid GCP credentials format: %w", err)
	}

	if gcpCreds.ProjectID == "" {
		return fmt.Errorf("projectId is required")
	}
	if gcpCreds.ServiceAccountJSON == "" {
		return fmt.Errorf("serviceAccountJson is required")
	}

	// Validate JSON structure
	var saKey map[string]interface{}
	if err := json.Unmarshal([]byte(gcpCreds.ServiceAccountJSON), &saKey); err != nil {
		return fmt.Errorf("invalid service account JSON: %w", err)
	}

	// In production, use GCP SDK to validate credentials

	return nil
}

// ListRegions returns GCP regions.
func (p *GCPProvider) ListRegions(ctx context.Context, creds []byte) ([]Region, error) {
	return []Region{
		{ID: "us-central1", Name: "us-central1", DisplayName: "Iowa"},
		{ID: "us-east1", Name: "us-east1", DisplayName: "South Carolina"},
		{ID: "us-east4", Name: "us-east4", DisplayName: "Northern Virginia"},
		{ID: "us-west1", Name: "us-west1", DisplayName: "Oregon"},
		{ID: "us-west2", Name: "us-west2", DisplayName: "Los Angeles"},
		{ID: "europe-west1", Name: "europe-west1", DisplayName: "Belgium"},
		{ID: "europe-west2", Name: "europe-west2", DisplayName: "London"},
		{ID: "europe-west3", Name: "europe-west3", DisplayName: "Frankfurt"},
		{ID: "asia-east1", Name: "asia-east1", DisplayName: "Taiwan"},
		{ID: "asia-southeast1", Name: "asia-southeast1", DisplayName: "Singapore"},
	}, nil
}

// ListMachineTypes returns GCP machine types for GKE.
func (p *GCPProvider) ListMachineTypes(ctx context.Context, creds []byte, region string) ([]MachineType, error) {
	return []MachineType{
		{ID: "e2-medium", Name: "e2-medium", VCPU: 2, MemoryGB: 4, Description: "Cost-optimized"},
		{ID: "e2-standard-2", Name: "e2-standard-2", VCPU: 2, MemoryGB: 8, Description: "General purpose"},
		{ID: "e2-standard-4", Name: "e2-standard-4", VCPU: 4, MemoryGB: 16, Description: "General purpose"},
		{ID: "e2-standard-8", Name: "e2-standard-8", VCPU: 8, MemoryGB: 32, Description: "General purpose"},
		{ID: "n1-standard-2", Name: "n1-standard-2", VCPU: 2, MemoryGB: 7.5, Description: "Balanced"},
		{ID: "n1-standard-4", Name: "n1-standard-4", VCPU: 4, MemoryGB: 15, Description: "Balanced"},
		{ID: "n2-standard-2", Name: "n2-standard-2", VCPU: 2, MemoryGB: 8, Description: "General purpose"},
		{ID: "n2-standard-4", Name: "n2-standard-4", VCPU: 4, MemoryGB: 16, Description: "General purpose"},
		{ID: "c2-standard-4", Name: "c2-standard-4", VCPU: 4, MemoryGB: 16, Description: "Compute optimized"},
		{ID: "n2-highmem-2", Name: "n2-highmem-2", VCPU: 2, MemoryGB: 16, Description: "Memory optimized"},
	}, nil
}

// ListKubernetesVersions returns supported GKE versions.
func (p *GCPProvider) ListKubernetesVersions(ctx context.Context, creds []byte, region string) ([]string, error) {
	return []string{"1.30", "1.29", "1.28", "1.27"}, nil
}
