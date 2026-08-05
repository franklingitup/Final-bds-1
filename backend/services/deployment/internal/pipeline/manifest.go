// Package pipeline implements the deployment pipeline orchestration.
package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ManifestGenerator generates Kubernetes manifests from deployment configuration.
type ManifestGenerator struct{}

// NewManifestGenerator creates a new manifest generator.
func NewManifestGenerator() *ManifestGenerator {
	return &ManifestGenerator{}
}

// DeploymentConfig holds configuration for generating manifests.
type DeploymentConfig struct {
	Name          string
	Namespace     string
	Image         string
	Replicas      int
	Port          *int
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
	EnvVars       []EnvVar
	Labels        map[string]string
	Annotations   map[string]string
}

// EnvVar represents an environment variable.
type EnvVar struct {
	Name      string  `json:"name"`
	Value     string  `json:"value,omitempty"`
	SecretRef *string `json:"secretRef,omitempty"`
}

// GeneratedManifests contains all generated Kubernetes manifests.
type GeneratedManifests struct {
	Namespace  string            `json:"namespace"`
	Manifests  []json.RawMessage `json:"manifests"`
	Hash       string            `json:"hash"`
}

// Generate creates Kubernetes manifests from the deployment config.
func (g *ManifestGenerator) Generate(cfg DeploymentConfig) (*GeneratedManifests, error) {
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.Replicas < 1 {
		cfg.Replicas = 1
	}

	// Default labels
	if cfg.Labels == nil {
		cfg.Labels = make(map[string]string)
	}
	cfg.Labels["app.kubernetes.io/name"] = cfg.Name
	cfg.Labels["app.kubernetes.io/managed-by"] = "bdsplatform"

	var manifests []json.RawMessage

	// Generate Deployment
	deployment := g.generateDeployment(cfg)
	deploymentJSON, _ := json.Marshal(deployment)
	manifests = append(manifests, deploymentJSON)

	// Generate Service if port is specified
	if cfg.Port != nil && *cfg.Port > 0 {
		service := g.generateService(cfg)
		serviceJSON, _ := json.Marshal(service)
		manifests = append(manifests, serviceJSON)
	}

	// Compute hash of all manifests
	hash := g.hashManifests(manifests)

	return &GeneratedManifests{
		Namespace: cfg.Namespace,
		Manifests: manifests,
		Hash:      hash,
	}, nil
}

func (g *ManifestGenerator) generateDeployment(cfg DeploymentConfig) map[string]any {
	// Build container spec
	container := map[string]any{
		"name":            cfg.Name,
		"image":           cfg.Image,
		"imagePullPolicy": "IfNotPresent",
	}

	// Add ports if specified
	if cfg.Port != nil && *cfg.Port > 0 {
		container["ports"] = []map[string]any{
			{
				"name":          "http",
				"containerPort": *cfg.Port,
				"protocol":      "TCP",
			},
		}
	}

	// Add resources
	resources := map[string]any{}
	requests := map[string]string{}
	limits := map[string]string{}

	if cfg.CPURequest != "" {
		requests["cpu"] = cfg.CPURequest
	} else {
		requests["cpu"] = "100m"
	}
	if cfg.MemoryRequest != "" {
		requests["memory"] = cfg.MemoryRequest
	} else {
		requests["memory"] = "128Mi"
	}
	if cfg.CPULimit != "" {
		limits["cpu"] = cfg.CPULimit
	} else {
		limits["cpu"] = "500m"
	}
	if cfg.MemoryLimit != "" {
		limits["memory"] = cfg.MemoryLimit
	} else {
		limits["memory"] = "512Mi"
	}

	resources["requests"] = requests
	resources["limits"] = limits
	container["resources"] = resources

	// Add environment variables
	if len(cfg.EnvVars) > 0 {
		envs := make([]map[string]any, 0, len(cfg.EnvVars))
		for _, ev := range cfg.EnvVars {
			env := map[string]any{"name": ev.Name}
			if ev.SecretRef != nil && *ev.SecretRef != "" {
				env["valueFrom"] = map[string]any{
					"secretKeyRef": map[string]any{
						"name": *ev.SecretRef,
						"key":  ev.Name,
					},
				}
			} else {
				env["value"] = ev.Value
			}
			envs = append(envs, env)
		}
		container["env"] = envs
	}

	// Add liveness and readiness probes if port is specified
	if cfg.Port != nil && *cfg.Port > 0 {
		probe := map[string]any{
			"httpGet": map[string]any{
				"path": "/",
				"port": *cfg.Port,
			},
			"initialDelaySeconds": 10,
			"periodSeconds":       10,
			"timeoutSeconds":      5,
			"failureThreshold":    3,
		}
		container["livenessProbe"] = probe
		container["readinessProbe"] = map[string]any{
			"httpGet": map[string]any{
				"path": "/",
				"port": *cfg.Port,
			},
			"initialDelaySeconds": 5,
			"periodSeconds":       5,
			"timeoutSeconds":      3,
			"failureThreshold":    3,
		}
	}

	// Build deployment manifest
	deployment := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      cfg.Name,
			"namespace": cfg.Namespace,
			"labels":    cfg.Labels,
		},
		"spec": map[string]any{
			"replicas": cfg.Replicas,
			"selector": map[string]any{
				"matchLabels": map[string]string{
					"app.kubernetes.io/name": cfg.Name,
				},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": cfg.Labels,
				},
				"spec": map[string]any{
					"containers": []map[string]any{container},
				},
			},
			"strategy": map[string]any{
				"type": "RollingUpdate",
				"rollingUpdate": map[string]any{
					"maxSurge":       "25%",
					"maxUnavailable": "25%",
				},
			},
		},
	}

	// Add annotations if any
	if len(cfg.Annotations) > 0 {
		deployment["metadata"].(map[string]any)["annotations"] = cfg.Annotations
	}

	return deployment
}

func (g *ManifestGenerator) generateService(cfg DeploymentConfig) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      cfg.Name,
			"namespace": cfg.Namespace,
			"labels":    cfg.Labels,
		},
		"spec": map[string]any{
			"type": "ClusterIP",
			"selector": map[string]string{
				"app.kubernetes.io/name": cfg.Name,
			},
			"ports": []map[string]any{
				{
					"name":       "http",
					"port":       80,
					"targetPort": *cfg.Port,
					"protocol":   "TCP",
				},
			},
		},
	}
}

func (g *ManifestGenerator) hashManifests(manifests []json.RawMessage) string {
	h := sha256.New()
	for _, m := range manifests {
		h.Write(m)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ParseEnvVars parses environment variables from JSON.
func ParseEnvVars(data []byte) []EnvVar {
	var envVars []EnvVar
	if len(data) > 0 {
		_ = json.Unmarshal(data, &envVars)
	}
	return envVars
}

// GenerateNamespace creates a namespace name from org and app slugs.
func GenerateNamespace(orgSlug, appSlug string) string {
	// Sanitize and combine
	ns := fmt.Sprintf("%s-%s", sanitizeK8sName(orgSlug), sanitizeK8sName(appSlug))
	// Kubernetes namespace names must be <= 63 chars
	if len(ns) > 63 {
		ns = ns[:63]
	}
	return strings.TrimSuffix(ns, "-")
}

func sanitizeK8sName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	// Remove any invalid characters (keep only a-z, 0-9, -)
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return strings.Trim(result.String(), "-")
}
