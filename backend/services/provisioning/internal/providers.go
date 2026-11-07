package provisioning

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// CloudProvider is the interface for cloud provider operations.
type CloudProvider interface {
	ValidateCredentials(ctx context.Context, creds []byte) error
	ListRegions(ctx context.Context, creds []byte) ([]Region, error)
	ListMachineTypes(ctx context.Context, creds []byte, region string) ([]MachineType, error)
	ListKubernetesVersions(ctx context.Context, creds []byte, region string) ([]K8sVersion, error)
}

// Region represents a cloud region.
type Region struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

// MachineType represents a VM/instance type.
type MachineType struct {
	Name        string  `json:"name"`
	VCPUs       int     `json:"vcpus"`
	MemoryGB    float64 `json:"memoryGb"`
	Description string  `json:"description,omitempty"`
}

// K8sVersion represents a Kubernetes version with metadata.
type K8sVersion struct {
	Version    string `json:"version"`
	IsDefault  bool   `json:"isDefault"`
	Deprecated bool   `json:"deprecated"`
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

// STSClient interface for testing.
type STSClient interface {
	GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// AWSSTSClientFactory creates STS clients. Allows injection for testing.
var AWSSTSClientFactory = func(ctx context.Context, creds AWSCredentials) (STSClient, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			creds.AccessKeyID,
			creds.SecretAccessKey,
			creds.SessionToken,
		)),
		config.WithRegion("us-east-1"), // STS is global, region doesn't matter
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return sts.NewFromConfig(cfg), nil
}

// ValidateCredentials validates AWS credentials by calling sts:GetCallerIdentity.
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

	// Create STS client with the provided credentials
	stsClient, err := AWSSTSClientFactory(ctx, awsCreds)
	if err != nil {
		return err
	}

	// Call GetCallerIdentity - this is the standard "do these credentials work" check.
	// It requires no permissions beyond basic authentication.
	_, err = stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		// Return the actual AWS error message (e.g., "InvalidClientTokenId")
		return fmt.Errorf("AWS credential validation failed: %w", err)
	}

	return nil
}

// ListRegions returns AWS regions.
func (p *AWSProvider) ListRegions(ctx context.Context, creds []byte) ([]Region, error) {
	// In production, use AWS SDK to list regions dynamically
	return []Region{
		{Code: "us-east-1", Name: "US East (N. Virginia)", Available: true},
		{Code: "us-east-2", Name: "US East (Ohio)", Available: true},
		{Code: "us-west-1", Name: "US West (N. California)", Available: true},
		{Code: "us-west-2", Name: "US West (Oregon)", Available: true},
		{Code: "eu-west-1", Name: "Europe (Ireland)", Available: true},
		{Code: "eu-west-2", Name: "Europe (London)", Available: true},
		{Code: "eu-central-1", Name: "Europe (Frankfurt)", Available: true},
		{Code: "ap-southeast-1", Name: "Asia Pacific (Singapore)", Available: true},
		{Code: "ap-southeast-2", Name: "Asia Pacific (Sydney)", Available: true},
		{Code: "ap-northeast-1", Name: "Asia Pacific (Tokyo)", Available: true},
	}, nil
}

// ListMachineTypes returns AWS instance types for EKS.
func (p *AWSProvider) ListMachineTypes(ctx context.Context, creds []byte, region string) ([]MachineType, error) {
	return []MachineType{
		{Name: "t3.medium", VCPUs: 2, MemoryGB: 4, Description: "General purpose"},
		{Name: "t3.large", VCPUs: 2, MemoryGB: 8, Description: "General purpose"},
		{Name: "t3.xlarge", VCPUs: 4, MemoryGB: 16, Description: "General purpose"},
		{Name: "m5.large", VCPUs: 2, MemoryGB: 8, Description: "General purpose"},
		{Name: "m5.xlarge", VCPUs: 4, MemoryGB: 16, Description: "General purpose"},
		{Name: "m5.2xlarge", VCPUs: 8, MemoryGB: 32, Description: "General purpose"},
		{Name: "c5.large", VCPUs: 2, MemoryGB: 4, Description: "Compute optimized"},
		{Name: "c5.xlarge", VCPUs: 4, MemoryGB: 8, Description: "Compute optimized"},
		{Name: "r5.large", VCPUs: 2, MemoryGB: 16, Description: "Memory optimized"},
		{Name: "r5.xlarge", VCPUs: 4, MemoryGB: 32, Description: "Memory optimized"},
	}, nil
}

// ListKubernetesVersions returns supported EKS versions.
func (p *AWSProvider) ListKubernetesVersions(ctx context.Context, creds []byte, region string) ([]K8sVersion, error) {
	return []K8sVersion{
		{Version: "1.30", IsDefault: true, Deprecated: false},
		{Version: "1.29", IsDefault: false, Deprecated: false},
		{Version: "1.28", IsDefault: false, Deprecated: false},
		{Version: "1.27", IsDefault: false, Deprecated: true},
	}, nil
}

// ----------------------------------------------------------------------------
// Azure Provider
// ----------------------------------------------------------------------------

// AzureProvider implements CloudProvider for Azure.
type AzureProvider struct{}

// AzureHTTPClient interface for testing.
type AzureHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// AzureHTTPClientFactory creates HTTP clients. Allows injection for testing.
var AzureHTTPClientFactory = func() AzureHTTPClient {
	return &http.Client{Timeout: 30 * time.Second}
}

// ValidateCredentials validates Azure credentials by obtaining an OAuth2 token.
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

	// Validate by obtaining an OAuth2 token from Azure AD.
	// This is equivalent to Azure CLI's "az login --service-principal" check.
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", azureCreds.TenantID)
	data := url.Values{}
	data.Set("client_id", azureCreds.ClientID)
	data.Set("client_secret", azureCreds.ClientSecret)
	data.Set("scope", "https://management.azure.com/.default")
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := AzureHTTPClientFactory()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Azure credential validation failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("Azure credential validation failed: %s - %s", errResp.Error, errResp.ErrorDescription)
		}
		return fmt.Errorf("Azure credential validation failed: HTTP %d", resp.StatusCode)
	}

	return nil
}

// ListRegions returns Azure regions.
func (p *AzureProvider) ListRegions(ctx context.Context, creds []byte) ([]Region, error) {
	return []Region{
		{Code: "eastus", Name: "East US", Available: true},
		{Code: "eastus2", Name: "East US 2", Available: true},
		{Code: "westus", Name: "West US", Available: true},
		{Code: "westus2", Name: "West US 2", Available: true},
		{Code: "westeurope", Name: "West Europe", Available: true},
		{Code: "northeurope", Name: "North Europe", Available: true},
		{Code: "uksouth", Name: "UK South", Available: true},
		{Code: "southeastasia", Name: "Southeast Asia", Available: true},
		{Code: "australiaeast", Name: "Australia East", Available: true},
		{Code: "japaneast", Name: "Japan East", Available: true},
	}, nil
}

// ListMachineTypes returns Azure VM sizes for AKS.
func (p *AzureProvider) ListMachineTypes(ctx context.Context, creds []byte, region string) ([]MachineType, error) {
	return []MachineType{
		{Name: "Standard_D2s_v3", VCPUs: 2, MemoryGB: 8, Description: "General purpose"},
		{Name: "Standard_D4s_v3", VCPUs: 4, MemoryGB: 16, Description: "General purpose"},
		{Name: "Standard_D8s_v3", VCPUs: 8, MemoryGB: 32, Description: "General purpose"},
		{Name: "Standard_D16s_v3", VCPUs: 16, MemoryGB: 64, Description: "General purpose"},
		{Name: "Standard_E2s_v3", VCPUs: 2, MemoryGB: 16, Description: "Memory optimized"},
		{Name: "Standard_E4s_v3", VCPUs: 4, MemoryGB: 32, Description: "Memory optimized"},
		{Name: "Standard_F2s_v2", VCPUs: 2, MemoryGB: 4, Description: "Compute optimized"},
		{Name: "Standard_F4s_v2", VCPUs: 4, MemoryGB: 8, Description: "Compute optimized"},
	}, nil
}

// ListKubernetesVersions returns supported AKS versions.
func (p *AzureProvider) ListKubernetesVersions(ctx context.Context, creds []byte, region string) ([]K8sVersion, error) {
	return []K8sVersion{
		{Version: "1.30", IsDefault: true, Deprecated: false},
		{Version: "1.29", IsDefault: false, Deprecated: false},
		{Version: "1.28", IsDefault: false, Deprecated: false},
		{Version: "1.27", IsDefault: false, Deprecated: true},
	}, nil
}

// ----------------------------------------------------------------------------
// GCP Provider
// ----------------------------------------------------------------------------

// GCPProvider implements CloudProvider for GCP.
type GCPProvider struct{}

// GCPHTTPClient interface for testing.
type GCPHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// GCPHTTPClientFactory creates HTTP clients. Allows injection for testing.
var GCPHTTPClientFactory = func() GCPHTTPClient {
	return &http.Client{Timeout: 30 * time.Second}
}

// ValidateCredentials validates GCP credentials by obtaining an OAuth2 access token.
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

	// Parse service account JSON
	var saKey struct {
		Type                    string `json:"type"`
		ProjectID               string `json:"project_id"`
		PrivateKeyID            string `json:"private_key_id"`
		PrivateKey              string `json:"private_key"`
		ClientEmail             string `json:"client_email"`
		ClientID                string `json:"client_id"`
		AuthURI                 string `json:"auth_uri"`
		TokenURI                string `json:"token_uri"`
		AuthProviderX509CertURL string `json:"auth_provider_x509_cert_url"`
		ClientX509CertURL       string `json:"client_x509_cert_url"`
	}
	if err := json.Unmarshal([]byte(gcpCreds.ServiceAccountJSON), &saKey); err != nil {
		return fmt.Errorf("invalid service account JSON: %w", err)
	}

	if saKey.Type != "service_account" {
		return fmt.Errorf("invalid service account JSON: type must be 'service_account', got '%s'", saKey.Type)
	}
	if saKey.ClientEmail == "" {
		return fmt.Errorf("invalid service account JSON: client_email is required")
	}
	if saKey.PrivateKey == "" {
		return fmt.Errorf("invalid service account JSON: private_key is required")
	}
	if saKey.TokenURI == "" {
		saKey.TokenURI = "https://oauth2.googleapis.com/token"
	}

	// Create a JWT assertion and exchange it for an access token.
	// This validates that the service account credentials are authentic.
	jwt, err := createGCPJWT(saKey.ClientEmail, saKey.PrivateKey, saKey.TokenURI)
	if err != nil {
		return fmt.Errorf("failed to create JWT: %w", err)
	}

	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	data.Set("assertion", jwt)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, saKey.TokenURI, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := GCPHTTPClientFactory()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GCP credential validation failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("GCP credential validation failed: %s - %s", errResp.Error, errResp.ErrorDescription)
		}
		return fmt.Errorf("GCP credential validation failed: HTTP %d", resp.StatusCode)
	}

	return nil
}

// createGCPJWT creates a signed JWT for GCP OAuth2 token exchange.
func createGCPJWT(clientEmail, privateKeyPEM, tokenURI string) (string, error) {
	// Parse the private key
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to parse PEM block containing the private key")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("private key is not RSA")
	}

	// Create JWT header and claims
	now := time.Now()
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	claims := map[string]interface{}{
		"iss":   clientEmail,
		"sub":   clientEmail,
		"aud":   tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"scope": "https://www.googleapis.com/auth/cloud-platform",
	}

	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := headerB64 + "." + claimsB64

	// Sign with RS256
	hash := crypto.SHA256.New()
	hash.Write([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(nil, rsaKey, crypto.SHA256, hash.Sum(nil))
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	signatureB64 := base64.RawURLEncoding.EncodeToString(signature)

	return signingInput + "." + signatureB64, nil
}

// ListRegions returns GCP regions.
func (p *GCPProvider) ListRegions(ctx context.Context, creds []byte) ([]Region, error) {
	return []Region{
		{Code: "us-central1", Name: "Iowa", Available: true},
		{Code: "us-east1", Name: "South Carolina", Available: true},
		{Code: "us-east4", Name: "Northern Virginia", Available: true},
		{Code: "us-west1", Name: "Oregon", Available: true},
		{Code: "us-west2", Name: "Los Angeles", Available: true},
		{Code: "europe-west1", Name: "Belgium", Available: true},
		{Code: "europe-west2", Name: "London", Available: true},
		{Code: "europe-west3", Name: "Frankfurt", Available: true},
		{Code: "asia-east1", Name: "Taiwan", Available: true},
		{Code: "asia-southeast1", Name: "Singapore", Available: true},
	}, nil
}

// ListMachineTypes returns GCP machine types for GKE.
func (p *GCPProvider) ListMachineTypes(ctx context.Context, creds []byte, region string) ([]MachineType, error) {
	return []MachineType{
		{Name: "e2-medium", VCPUs: 2, MemoryGB: 4, Description: "Cost-optimized"},
		{Name: "e2-standard-2", VCPUs: 2, MemoryGB: 8, Description: "General purpose"},
		{Name: "e2-standard-4", VCPUs: 4, MemoryGB: 16, Description: "General purpose"},
		{Name: "e2-standard-8", VCPUs: 8, MemoryGB: 32, Description: "General purpose"},
		{Name: "n1-standard-2", VCPUs: 2, MemoryGB: 7.5, Description: "Balanced"},
		{Name: "n1-standard-4", VCPUs: 4, MemoryGB: 15, Description: "Balanced"},
		{Name: "n2-standard-2", VCPUs: 2, MemoryGB: 8, Description: "General purpose"},
		{Name: "n2-standard-4", VCPUs: 4, MemoryGB: 16, Description: "General purpose"},
		{Name: "c2-standard-4", VCPUs: 4, MemoryGB: 16, Description: "Compute optimized"},
		{Name: "n2-highmem-2", VCPUs: 2, MemoryGB: 16, Description: "Memory optimized"},
	}, nil
}

// ListKubernetesVersions returns supported GKE versions.
func (p *GCPProvider) ListKubernetesVersions(ctx context.Context, creds []byte, region string) ([]K8sVersion, error) {
	return []K8sVersion{
		{Version: "1.30", IsDefault: true, Deprecated: false},
		{Version: "1.29", IsDefault: false, Deprecated: false},
		{Version: "1.28", IsDefault: false, Deprecated: false},
		{Version: "1.27", IsDefault: false, Deprecated: true},
	}, nil
}
