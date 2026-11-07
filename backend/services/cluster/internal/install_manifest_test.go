package cluster

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"gopkg.in/yaml.v3"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/middleware"
)

func TestInstallManifestEndpoint(t *testing.T) {
	env := newTestEnv()
	const token = "valid-registration-token"
	if err := env.tokens.Create(t.Context(), &RegistrationToken{
		ClusterID: "cluster-1",
		TokenHash: hashToken(token),
		Status:    TokenStatusActive,
		ExpiresAt: env.now.Add(DefaultTokenTTL),
	}); err != nil {
		t.Fatalf("create token: %v", err)
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler(),
	})
	RegisterRoutes(app, NewHandler(env.svc, NewTokenVerifier(config.AuthConfig{})), nil)

	endpoint := "https://control.example.com"
	path := "/v1/agent-install/" + token + "/manifest.yaml?controlPlaneEndpoint=" + url.QueryEscape(endpoint)
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
	if err != nil {
		t.Fatalf("valid manifest request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid token status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get(fiber.HeaderContentType); got != "application/yaml" {
		t.Fatalf("content type = %q, want application/yaml", got)
	}

	var documents []map[string]any
	decoder := yaml.NewDecoder(resp.Body)
	for {
		var document map[string]any
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("manifest is not valid YAML: %v", err)
		}
		if len(document) != 0 {
			documents = append(documents, document)
		}
	}
	if len(documents) != 8 {
		t.Fatalf("manifest documents = %d, want 8", len(documents))
	}

	var secret map[string]any
	for _, document := range documents {
		if document["kind"] == "Secret" {
			secret = document
			break
		}
	}
	if secret == nil {
		t.Fatal("manifest does not contain a Secret")
	}
	stringData, ok := secret["stringData"].(map[string]any)
	if !ok || stringData["token"] != token {
		t.Fatalf("Secret token = %#v, want %q", stringData["token"], token)
	}

	for _, tc := range []struct {
		name  string
		token string
	}{
		{name: "invalid", token: "not-a-token"},
		{name: "expired", token: "expired-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "expired" {
				if err := env.tokens.Create(t.Context(), &RegistrationToken{
					ClusterID: "cluster-1",
					TokenHash: hashToken(tc.token),
					Status:    TokenStatusActive,
					ExpiresAt: env.now.Add(-1),
				}); err != nil {
					t.Fatalf("create expired token: %v", err)
				}
			}
			path := "/v1/agent-install/" + tc.token + "/manifest.yaml?controlPlaneEndpoint=" + url.QueryEscape(endpoint)
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
			if err != nil {
				t.Fatalf("manifest request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
		})
	}
}

func TestGenerateRegistrationTokenReturnsBackendManifestURL(t *testing.T) {
	env := newTestEnv()
	cluster := env.createCluster(t, "org-1", "user-1", "Test", "test")

	token, err := env.svc.GenerateRegistrationToken(
		t.Context(),
		"org-1",
		"user-1",
		cluster.ID,
		GenerateTokenRequest{ControlPlaneEndpoint: "https://control.example.com/"},
	)
	if err != nil {
		t.Fatalf("GenerateRegistrationToken: %v", err)
	}
	if !strings.HasPrefix(token.ManifestURL, "https://control.example.com/v1/agent-install/") {
		t.Fatalf("manifestUrl = %q", token.ManifestURL)
	}
	if token.InstallCommand != "kubectl apply -f "+token.ManifestURL {
		t.Fatalf("installCommand = %q", token.InstallCommand)
	}
}

func TestGenerateRegistrationTokenUsesServerDefaultEndpoint(t *testing.T) {
	// Create test env with a server-side default control plane URL.
	env := newTestEnvWithControlPlaneURL("https://api.platform.io")
	cluster := env.createCluster(t, "org-1", "user-1", "Test", "test")

	// Generate token WITHOUT providing a controlPlaneEndpoint override.
	token, err := env.svc.GenerateRegistrationToken(
		t.Context(),
		"org-1",
		"user-1",
		cluster.ID,
		GenerateTokenRequest{}, // No ControlPlaneEndpoint set
	)
	if err != nil {
		t.Fatalf("GenerateRegistrationToken: %v", err)
	}

	// The manifest URL should use the server-side default, not be empty.
	if !strings.HasPrefix(token.ManifestURL, "https://api.platform.io/v1/agent-install/") {
		t.Fatalf("manifestUrl = %q, want prefix https://api.platform.io/v1/agent-install/", token.ManifestURL)
	}
	if token.InstallCommand == "" {
		t.Fatalf("installCommand should not be empty")
	}
	if token.InstallCommand != "kubectl apply -f "+token.ManifestURL {
		t.Fatalf("installCommand = %q", token.InstallCommand)
	}
}

func TestGenerateRegistrationTokenOverridesServerDefault(t *testing.T) {
	// Create test env with a server-side default.
	env := newTestEnvWithControlPlaneURL("https://api.platform.io")
	cluster := env.createCluster(t, "org-1", "user-1", "Test", "test")

	// Generate token WITH an override.
	token, err := env.svc.GenerateRegistrationToken(
		t.Context(),
		"org-1",
		"user-1",
		cluster.ID,
		GenerateTokenRequest{ControlPlaneEndpoint: "https://custom.override.io"},
	)
	if err != nil {
		t.Fatalf("GenerateRegistrationToken: %v", err)
	}

	// The manifest URL should use the explicit override, not the server default.
	if !strings.HasPrefix(token.ManifestURL, "https://custom.override.io/v1/agent-install/") {
		t.Fatalf("manifestUrl = %q, want prefix https://custom.override.io/v1/agent-install/", token.ManifestURL)
	}
}

func TestGenerateRegistrationTokenFailsWithoutEndpoint(t *testing.T) {
	// Create test env WITHOUT a server-side default (explicitly empty).
	env := newTestEnvWithoutControlPlaneURL()
	cluster := env.createCluster(t, "org-1", "user-1", "Test", "test")

	// Generate token WITHOUT providing a controlPlaneEndpoint.
	// This should fail because there's no default and no override.
	_, err := env.svc.GenerateRegistrationToken(
		t.Context(),
		"org-1",
		"user-1",
		cluster.ID,
		GenerateTokenRequest{}, // No ControlPlaneEndpoint
	)
	if err == nil {
		t.Fatalf("expected error when no control plane endpoint is configured")
	}
	if !strings.Contains(err.Error(), "controlPlaneEndpoint") && !strings.Contains(err.Error(), "CONTROL_PLANE_PUBLIC_URL") {
		t.Fatalf("error = %q, should mention controlPlaneEndpoint or CONTROL_PLANE_PUBLIC_URL", err.Error())
	}
}

func TestInstallManifestUsesServerDefaultEndpoint(t *testing.T) {
	// Create test env with a server-side default control plane URL.
	env := newTestEnvWithControlPlaneURL("https://api.platform.io")
	const token = "valid-registration-token"
	if err := env.tokens.Create(t.Context(), &RegistrationToken{
		ClusterID: "cluster-1",
		TokenHash: hashToken(token),
		Status:    TokenStatusActive,
		ExpiresAt: env.now.Add(DefaultTokenTTL),
	}); err != nil {
		t.Fatalf("create token: %v", err)
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler(),
	})
	RegisterRoutes(app, NewHandler(env.svc, NewTokenVerifier(config.AuthConfig{})), nil)

	// Request manifest WITHOUT controlPlaneEndpoint query param.
	path := "/v1/agent-install/" + token + "/manifest.yaml"
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
	if err != nil {
		t.Fatalf("manifest request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, string(body))
	}

	// Parse and verify the manifest contains the server-default endpoint.
	bodyBytes, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(bodyBytes), "https://api.platform.io") {
		t.Fatalf("manifest should contain server-default endpoint https://api.platform.io")
	}
}

func TestInstallManifestIncludesObservabilityEnvironmentDefaults(t *testing.T) {
	manifest, err := NewInstallManifestGenerator(
		"",
		"",
		"https://api.platform.io",
	).Generate("registration-token", "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	type envVar struct {
		Name  string `yaml:"name"`
		Value string `yaml:"value"`
	}
	type document struct {
		Kind string `yaml:"kind"`
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Env []envVar `yaml:"env"`
					} `yaml:"containers"`
				} `yaml:"spec"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}

	var deployment document
	decoder := yaml.NewDecoder(strings.NewReader(manifest))
	for {
		var candidate document
		if err := decoder.Decode(&candidate); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode manifest: %v", err)
		}
		if candidate.Kind == "Deployment" {
			deployment = candidate
			break
		}
	}
	if deployment.Kind == "" {
		t.Fatal("manifest does not contain a Deployment")
	}
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Deployment containers = %d, want 1", len(deployment.Spec.Template.Spec.Containers))
	}

	got := make(map[string]string)
	for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
		got[env.Name] = env.Value
	}
	want := map[string]string{
		"METRICS_COLLECTOR_ENABLED":  "true",
		"METRICS_INTERVAL":           "60s",
		"LOG_SHIPPER_ENABLED":        "true",
		"LOG_SHIPPER_BATCH_INTERVAL": "5s",
		"MAX_TAILED_PODS":            "50",
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s = %q, want %q", name, got[name], value)
		}
	}
}

func TestInstallManifestObservabilityDefaultsMatchHelmChart(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
	valuesPath := filepath.Join(repoRoot, "helm", "agent", "values.yaml")
	valuesBytes, err := os.ReadFile(valuesPath)
	if err != nil {
		t.Fatalf("read Helm values: %v", err)
	}

	var values struct {
		MetricsCollector struct {
			Enabled  bool   `yaml:"enabled"`
			Interval string `yaml:"interval"`
		} `yaml:"metricsCollector"`
		LogShipper struct {
			Enabled       bool   `yaml:"enabled"`
			BatchInterval string `yaml:"batchInterval"`
			MaxTailedPods int    `yaml:"maxTailedPods"`
		} `yaml:"logShipper"`
	}
	if err := yaml.Unmarshal(valuesBytes, &values); err != nil {
		t.Fatalf("parse Helm values: %v", err)
	}

	if got := strconv.FormatBool(values.MetricsCollector.Enabled); got != defaultMetricsCollectorEnabled {
		t.Errorf("Helm metricsCollector.enabled = %q, manifest default = %q", got, defaultMetricsCollectorEnabled)
	}
	if got := values.MetricsCollector.Interval; got != defaultMetricsCollectorInterval {
		t.Errorf("Helm metricsCollector.interval = %q, manifest default = %q", got, defaultMetricsCollectorInterval)
	}
	if got := strconv.FormatBool(values.LogShipper.Enabled); got != defaultLogShipperEnabled {
		t.Errorf("Helm logShipper.enabled = %q, manifest default = %q", got, defaultLogShipperEnabled)
	}
	if got := values.LogShipper.BatchInterval; got != defaultLogShipperBatchInterval {
		t.Errorf("Helm logShipper.batchInterval = %q, manifest default = %q", got, defaultLogShipperBatchInterval)
	}
	if got := strconv.Itoa(values.LogShipper.MaxTailedPods); got != defaultLogShipperMaxTailedPods {
		t.Errorf("Helm logShipper.maxTailedPods = %q, manifest default = %q", got, defaultLogShipperMaxTailedPods)
	}
}

func TestInstallManifestClusterRoleRulesMatchHelmChart(t *testing.T) {
	type policyRule struct {
		APIGroups []string `yaml:"apiGroups"`
		Resources []string `yaml:"resources"`
		Verbs     []string `yaml:"verbs"`
	}
	type clusterRole struct {
		Kind  string       `yaml:"kind"`
		Rules []policyRule `yaml:"rules"`
	}

	manifest, err := NewInstallManifestGenerator(
		"",
		"",
		"https://api.platform.io",
	).Generate("registration-token", "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var generatedRole clusterRole
	decoder := yaml.NewDecoder(strings.NewReader(manifest))
	for {
		var candidate clusterRole
		if err := decoder.Decode(&candidate); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode generated manifest: %v", err)
		}
		if candidate.Kind == "ClusterRole" {
			generatedRole = candidate
			break
		}
	}
	if generatedRole.Kind == "" {
		t.Fatal("generated manifest does not contain a ClusterRole")
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
	rbacPath := filepath.Join(repoRoot, "helm", "agent", "templates", "rbac.yaml")
	rbacBytes, err := os.ReadFile(rbacPath)
	if err != nil {
		t.Fatalf("read Helm RBAC template: %v", err)
	}

	// The ClusterRole rules contain no Helm expressions, so parse that YAML
	// subsection directly without requiring the Helm executable in unit tests.
	rbacSource := strings.ReplaceAll(string(rbacBytes), "\r\n", "\n")
	clusterRoleStart := strings.Index(rbacSource, "kind: ClusterRole\n")
	if clusterRoleStart < 0 {
		t.Fatal("Helm RBAC template does not contain a ClusterRole")
	}
	rulesOffset := strings.Index(rbacSource[clusterRoleStart:], "rules:\n")
	if rulesOffset < 0 {
		t.Fatal("Helm ClusterRole does not contain rules")
	}
	rulesStart := clusterRoleStart + rulesOffset
	rulesEndOffset := strings.Index(rbacSource[rulesStart:], "\n---\n")
	if rulesEndOffset < 0 {
		t.Fatal("cannot locate end of Helm ClusterRole rules")
	}

	var helmRole clusterRole
	if err := yaml.Unmarshal([]byte(rbacSource[rulesStart:rulesStart+rulesEndOffset]), &helmRole); err != nil {
		t.Fatalf("parse Helm ClusterRole rules: %v", err)
	}

	if !reflect.DeepEqual(generatedRole.Rules, helmRole.Rules) {
		t.Fatalf("server-generated ClusterRole rules differ from Helm chart:\ngenerated: %#v\nhelm: %#v",
			generatedRole.Rules, helmRole.Rules)
	}

	hasCorePodRead := false
	hasMetricsPodRead := false
	for _, rule := range generatedRole.Rules {
		if !containsString(rule.Resources, "pods") {
			continue
		}
		hasGetAndList := containsString(rule.Verbs, "get") && containsString(rule.Verbs, "list")
		if containsString(rule.APIGroups, "") && hasGetAndList {
			hasCorePodRead = true
		}
		if containsString(rule.APIGroups, "metrics.k8s.io") && hasGetAndList {
			hasMetricsPodRead = true
		}
	}
	if !hasCorePodRead {
		t.Error("ClusterRole must grant get and list on core pods")
	}
	if !hasMetricsPodRead {
		t.Error("ClusterRole must separately grant get and list on metrics.k8s.io pods")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
