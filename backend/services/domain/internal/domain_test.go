package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildFullDomain(t *testing.T) {
	tests := []struct {
		name      string
		domain    string
		subdomain *string
		want      string
	}{
		{
			name:   "apex domain",
			domain: "example.com",
			want:   "example.com",
		},
		{
			name:      "with www subdomain",
			domain:    "example.com",
			subdomain: strPtr("www"),
			want:      "www.example.com",
		},
		{
			name:      "with api subdomain",
			domain:    "example.com",
			subdomain: strPtr("api"),
			want:      "api.example.com",
		},
		{
			name:      "empty subdomain",
			domain:    "example.com",
			subdomain: strPtr(""),
			want:      "example.com",
		},
		{
			name:   "mixed case domain",
			domain: "EXAMPLE.COM",
			want:   "example.com",
		},
		{
			name:   "whitespace trimmed",
			domain: "  example.com  ",
			want:   "example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildFullDomain(tt.domain, tt.subdomain)
			if got != tt.want {
				t.Errorf("BuildFullDomain() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildTXTRecordName(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"example.com", "_bdsplatform-verify.example.com"},
		{"api.example.com", "_bdsplatform-verify.api.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := BuildTXTRecordName(tt.domain)
			if got != tt.want {
				t.Errorf("BuildTXTRecordName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateDomainFormat(t *testing.T) {
	tests := []struct {
		domain  string
		wantErr bool
	}{
		{"example.com", false},
		{"sub.example.com", false},
		{"my-domain.co.uk", false},
		{"a123.example.com", false},
		{"", true},                          // empty
		{"example", true},                   // no dot
		{"-example.com", true},              // starts with hyphen
		{"example-.com", true},              // ends with hyphen
		{"exam_ple.com", true},              // underscore
		{"exam ple.com", true},              // space
		{".example.com", true},              // starts with dot
		{"example..com", true},              // double dot
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			err := ValidateDomainFormat(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDomainFormat(%q) error = %v, wantErr %v", tt.domain, err, tt.wantErr)
			}
		})
	}
}

func TestValidateSubdomain(t *testing.T) {
	tests := []struct {
		subdomain string
		wantErr   bool
	}{
		{"", false},     // empty is valid
		{"www", false},
		{"api", false},
		{"my-app", false},
		{"app123", false},
		{"-invalid", true},   // starts with hyphen
		{"invalid-", true},   // ends with hyphen
		{"inva_lid", true},   // underscore
		{"inva lid", true},   // space
	}

	for _, tt := range tests {
		t.Run(tt.subdomain, func(t *testing.T) {
			err := ValidateSubdomain(tt.subdomain)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSubdomain(%q) error = %v, wantErr %v", tt.subdomain, err, tt.wantErr)
			}
		})
	}
}

func TestGenerateVerificationToken(t *testing.T) {
	token1, err := GenerateVerificationToken()
	if err != nil {
		t.Fatalf("GenerateVerificationToken() error = %v", err)
	}

	if len(token1) != 64 { // 32 bytes hex encoded = 64 chars
		t.Errorf("token length = %d, want 64", len(token1))
	}

	token2, _ := GenerateVerificationToken()
	if token1 == token2 {
		t.Error("GenerateVerificationToken() should produce unique tokens")
	}
}

func TestGenerateIngressName(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"example.com", "example-com"},
		{"www.example.com", "www-example-com"},
		{"api.sub.example.com", "api-sub-example-com"},
		{"EXAMPLE.COM", "example-com"},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := GenerateIngressName(tt.domain)
			if got != tt.want {
				t.Errorf("GenerateIngressName(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

func TestGenerateTLSSecretName(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"example.com", "example-com-tls"},
		{"www.example.com", "www-example-com-tls"},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := GenerateTLSSecretName(tt.domain)
			if got != tt.want {
				t.Errorf("GenerateTLSSecretName(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

func TestShouldRenew(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{
			name:      "expires in 60 days",
			expiresAt: time.Now().Add(60 * 24 * time.Hour),
			want:      false,
		},
		{
			name:      "expires in 29 days",
			expiresAt: time.Now().Add(29 * 24 * time.Hour),
			want:      true,
		},
		{
			name:      "expires tomorrow",
			expiresAt: time.Now().Add(24 * time.Hour),
			want:      true,
		},
		{
			name:      "already expired",
			expiresAt: time.Now().Add(-24 * time.Hour),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRenew(tt.expiresAt)
			if got != tt.want {
				t.Errorf("ShouldRenew() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToDomainView(t *testing.T) {
	now := time.Now()
	domain := &Domain{
		DeploymentID:       "dep-123",
		Domain:             "example.com",
		FullDomain:         "example.com",
		VerificationStatus: VerificationVerified,
		VerificationMethod: VerifyDNSTXT,
		DNSTxtName:         "_bdsplatform-verify.example.com",
		DNSTxtValue:        "token123",
		Status:             StatusActive,
		IsPrimary:          true,
	}
	domain.ID = "dom-123"
	domain.OrgID = "org-123"
	domain.CreatedAt = now

	view := ToDomainView(domain, nil)

	if view.ID != "dom-123" {
		t.Errorf("ID = %q, want %q", view.ID, "dom-123")
	}
	if view.OrgID != "org-123" {
		t.Errorf("OrgID = %q, want %q", view.OrgID, "org-123")
	}
	if view.FullDomain != "example.com" {
		t.Errorf("FullDomain = %q, want %q", view.FullDomain, "example.com")
	}
	if view.VerificationStatus != VerificationVerified {
		t.Errorf("VerificationStatus = %q, want %q", view.VerificationStatus, VerificationVerified)
	}
	if view.DNSRecords.TXTName != "_bdsplatform-verify.example.com" {
		t.Errorf("DNSRecords.TXTName = %q, want %q", view.DNSRecords.TXTName, "_bdsplatform-verify.example.com")
	}
	if view.Certificate != nil {
		t.Error("Certificate should be nil")
	}
}

func TestToCertView(t *testing.T) {
	now := time.Now()
	expires := now.Add(90 * 24 * time.Hour)

	cert := &Certificate{
		ID:         "cert-123",
		CommonName: "example.com",
		Issuer:     "letsencrypt",
		Status:     CertActive,
		IssuedAt:   &now,
		ExpiresAt:  &expires,
	}

	view := ToCertView(cert)

	if view.ID != "cert-123" {
		t.Errorf("ID = %q, want %q", view.ID, "cert-123")
	}
	if view.CommonName != "example.com" {
		t.Errorf("CommonName = %q, want %q", view.CommonName, "example.com")
	}
	if view.Status != CertActive {
		t.Errorf("Status = %q, want %q", view.Status, CertActive)
	}
	if view.DaysToExpiry == nil || *view.DaysToExpiry < 89 {
		t.Errorf("DaysToExpiry should be around 90, got %v", view.DaysToExpiry)
	}
}

func TestToCertView_Nil(t *testing.T) {
	view := ToCertView(nil)
	if view != nil {
		t.Error("ToCertView(nil) should return nil")
	}
}

func TestToIngressView(t *testing.T) {
	now := time.Now()
	tlsSecret := "example-com-tls"
	ingress := &IngressRecord{
		ID:            "ing-123",
		IngressName:   "example-com",
		Namespace:     "default",
		IngressClass:  "nginx",
		ServiceName:   "my-app",
		ServicePort:   80,
		Path:          "/",
		TLSSecretName: &tlsSecret,
		Status:        IngressSynced,
		LastSyncedAt:  &now,
		Generation:    5,
	}

	view := ToIngressView(ingress)

	if view.ID != "ing-123" {
		t.Errorf("ID = %q, want %q", view.ID, "ing-123")
	}
	if view.IngressName != "example-com" {
		t.Errorf("IngressName = %q, want %q", view.IngressName, "example-com")
	}
	if view.ServicePort != 80 {
		t.Errorf("ServicePort = %d, want %d", view.ServicePort, 80)
	}
	if view.TLSSecretName == nil || *view.TLSSecretName != "example-com-tls" {
		t.Error("TLSSecretName should be set")
	}
	if view.Generation != 5 {
		t.Errorf("Generation = %d, want %d", view.Generation, 5)
	}
}

func TestToDomainEventView(t *testing.T) {
	now := time.Now()
	details := json.RawMessage(`{"domain":"example.com"}`)
	event := &DomainEvent{
		ID:        "evt-123",
		EventType: "created",
		Message:   "Domain created",
		Details:   details,
		CreatedAt: now,
	}

	view := ToDomainEventView(event)

	if view.ID != "evt-123" {
		t.Errorf("ID = %q, want %q", view.ID, "evt-123")
	}
	if view.EventType != "created" {
		t.Errorf("EventType = %q, want %q", view.EventType, "created")
	}
	if view.Message != "Domain created" {
		t.Errorf("Message = %q, want %q", view.Message, "Domain created")
	}
}

func TestIngressGenerator_Generate(t *testing.T) {
	gen := NewIngressGenerator()

	cfg := IngressConfig{
		Name:         "example-com",
		Namespace:    "default",
		IngressClass: "nginx",
		Domain:       "example.com",
		ServiceName:  "my-app",
		ServicePort:  8080,
		Path:         "/",
		PathType:     "Prefix",
		TLSEnabled:   true,
		TLSSecretName: "example-com-tls",
	}

	result, err := gen.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(result.Manifest) == 0 {
		t.Error("Manifest should not be empty")
	}
	if result.ManifestHash == "" {
		t.Error("ManifestHash should not be empty")
	}

	// Parse manifest and verify structure
	var manifest map[string]any
	if err := json.Unmarshal(result.Manifest, &manifest); err != nil {
		t.Fatalf("Failed to parse manifest: %v", err)
	}

	if manifest["apiVersion"] != "networking.k8s.io/v1" {
		t.Errorf("apiVersion = %v, want networking.k8s.io/v1", manifest["apiVersion"])
	}
	if manifest["kind"] != "Ingress" {
		t.Errorf("kind = %v, want Ingress", manifest["kind"])
	}

	spec := manifest["spec"].(map[string]any)
	if spec["ingressClassName"] != "nginx" {
		t.Errorf("ingressClassName = %v, want nginx", spec["ingressClassName"])
	}

	// Verify TLS is configured
	tls, ok := spec["tls"].([]any)
	if !ok || len(tls) == 0 {
		t.Error("TLS should be configured")
	}
}

func TestIngressGenerator_GenerateNoTLS(t *testing.T) {
	gen := NewIngressGenerator()

	cfg := IngressConfig{
		Name:         "example-com",
		Namespace:    "default",
		Domain:       "example.com",
		ServiceName:  "my-app",
		ServicePort:  8080,
		TLSEnabled:   false,
	}

	result, err := gen.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	var manifest map[string]any
	if err := json.Unmarshal(result.Manifest, &manifest); err != nil {
		t.Fatalf("Failed to parse manifest: %v", err)
	}

	spec := manifest["spec"].(map[string]any)
	if _, hasTLS := spec["tls"]; hasTLS {
		t.Error("TLS should not be configured when TLSEnabled is false")
	}
}

func TestConstants(t *testing.T) {
	// Verification statuses
	if VerificationPending != "pending" {
		t.Errorf("VerificationPending = %q", VerificationPending)
	}
	if VerificationVerified != "verified" {
		t.Errorf("VerificationVerified = %q", VerificationVerified)
	}

	// Certificate statuses
	if CertPending != "pending" {
		t.Errorf("CertPending = %q", CertPending)
	}
	if CertActive != "active" {
		t.Errorf("CertActive = %q", CertActive)
	}

	// Ingress statuses
	if IngressPending != "pending" {
		t.Errorf("IngressPending = %q", IngressPending)
	}
	if IngressSynced != "synced" {
		t.Errorf("IngressSynced = %q", IngressSynced)
	}
}

func strPtr(s string) *string {
	return &s
}
