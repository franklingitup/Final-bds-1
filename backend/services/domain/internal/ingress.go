package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// IngressGenerator generates Kubernetes Ingress manifests.
type IngressGenerator struct{}

// NewIngressGenerator creates a new ingress generator.
func NewIngressGenerator() *IngressGenerator {
	return &IngressGenerator{}
}

// IngressConfig holds configuration for generating an Ingress.
type IngressConfig struct {
	Name           string
	Namespace      string
	IngressClass   string
	Domain         string
	ServiceName    string
	ServicePort    int
	Path           string
	PathType       string
	TLSEnabled     bool
	TLSSecretName  string
	Annotations    map[string]string
	Labels         map[string]string
}

// GeneratedIngress contains the generated Ingress manifest.
type GeneratedIngress struct {
	Manifest     json.RawMessage `json:"manifest"`
	ManifestHash string          `json:"hash"`
}

// Generate creates a Kubernetes Ingress manifest.
func (g *IngressGenerator) Generate(cfg IngressConfig) (*GeneratedIngress, error) {
	if cfg.IngressClass == "" {
		cfg.IngressClass = "nginx"
	}
	if cfg.Path == "" {
		cfg.Path = "/"
	}
	if cfg.PathType == "" {
		cfg.PathType = "Prefix"
	}

	// Default labels
	if cfg.Labels == nil {
		cfg.Labels = make(map[string]string)
	}
	cfg.Labels["app.kubernetes.io/managed-by"] = "bdsplatform"

	// Default annotations for nginx ingress
	if cfg.Annotations == nil {
		cfg.Annotations = make(map[string]string)
	}
	cfg.Annotations["kubernetes.io/ingress.class"] = cfg.IngressClass

	// Build Ingress manifest
	ingress := map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "Ingress",
		"metadata": map[string]any{
			"name":        cfg.Name,
			"namespace":   cfg.Namespace,
			"labels":      cfg.Labels,
			"annotations": cfg.Annotations,
		},
		"spec": map[string]any{
			"ingressClassName": cfg.IngressClass,
			"rules": []map[string]any{
				{
					"host": cfg.Domain,
					"http": map[string]any{
						"paths": []map[string]any{
							{
								"path":     cfg.Path,
								"pathType": cfg.PathType,
								"backend": map[string]any{
									"service": map[string]any{
										"name": cfg.ServiceName,
										"port": map[string]any{
											"number": cfg.ServicePort,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Add TLS configuration if enabled
	if cfg.TLSEnabled {
		tlsConfig := []map[string]any{
			{
				"hosts":      []string{cfg.Domain},
				"secretName": cfg.TLSSecretName,
			},
		}
		ingress["spec"].(map[string]any)["tls"] = tlsConfig
	}

	manifestJSON, err := json.Marshal(ingress)
	if err != nil {
		return nil, fmt.Errorf("marshal ingress: %w", err)
	}

	return &GeneratedIngress{
		Manifest:     manifestJSON,
		ManifestHash: hashManifest(manifestJSON),
	}, nil
}

// GenerateTLSSecret creates a Kubernetes Secret manifest for TLS certificates.
func (g *IngressGenerator) GenerateTLSSecret(name, namespace string, certPEM, keyPEM []byte) (json.RawMessage, error) {
	secret := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "bdsplatform",
			},
		},
		"type": "kubernetes.io/tls",
		"data": map[string]string{
			"tls.crt": string(certPEM),
			"tls.key": string(keyPEM),
		},
	}

	return json.Marshal(secret)
}

// GenerateIngressName creates a valid Kubernetes Ingress name from a domain.
func GenerateIngressName(domain string) string {
	// Replace dots with dashes and ensure it's a valid K8s name
	name := strings.ReplaceAll(domain, ".", "-")
	name = strings.ToLower(name)

	// Remove any invalid characters
	var result strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result.WriteRune(c)
		}
	}
	name = result.String()

	// Ensure it doesn't start or end with a hyphen
	name = strings.Trim(name, "-")

	// Kubernetes names must be <= 63 characters
	if len(name) > 63 {
		name = name[:63]
		name = strings.TrimSuffix(name, "-")
	}

	return name
}

// GenerateTLSSecretName creates a name for the TLS secret.
func GenerateTLSSecretName(domain string) string {
	baseName := GenerateIngressName(domain)
	secretName := fmt.Sprintf("%s-tls", baseName)

	if len(secretName) > 63 {
		secretName = secretName[:63]
		secretName = strings.TrimSuffix(secretName, "-")
	}

	return secretName
}

func hashManifest(manifest []byte) string {
	h := sha256.Sum256(manifest)
	return hex.EncodeToString(h[:8])
}

// AgentIngressSpec is the format sent to agents for applying Ingress.
type AgentIngressSpec struct {
	IngressID     string          `json:"ingressId"`
	DomainID      string          `json:"domainId"`
	Manifest      json.RawMessage `json:"manifest"`
	TLSSecret     json.RawMessage `json:"tlsSecret,omitempty"`
	ManifestHash  string          `json:"manifestHash"`
	Generation    int64           `json:"generation"`
}
