package pipeline

import (
	"encoding/json"
	"testing"
)

func TestManifestGenerator_Generate(t *testing.T) {
	gen := NewManifestGenerator()

	port := 8080
	cfg := DeploymentConfig{
		Name:          "nginx-app",
		Namespace:     "test-ns",
		Image:         "nginx:latest",
		Replicas:      3,
		Port:          &port,
		CPURequest:    "100m",
		CPULimit:      "500m",
		MemoryRequest: "128Mi",
		MemoryLimit:   "512Mi",
		EnvVars: []EnvVar{
			{Name: "ENV", Value: "production"},
			{Name: "LOG_LEVEL", Value: "info"},
		},
	}

	result, err := gen.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if result.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", result.Namespace, "test-ns")
	}

	if len(result.Manifests) < 2 {
		t.Errorf("Expected at least 2 manifests (Deployment + Service), got %d", len(result.Manifests))
	}

	if result.Hash == "" {
		t.Error("Expected non-empty hash")
	}

	// Verify first manifest is a Deployment
	var deployment map[string]any
	if err := json.Unmarshal(result.Manifests[0], &deployment); err != nil {
		t.Fatalf("Failed to unmarshal deployment: %v", err)
	}

	if deployment["kind"] != "Deployment" {
		t.Errorf("First manifest kind = %q, want %q", deployment["kind"], "Deployment")
	}

	if deployment["apiVersion"] != "apps/v1" {
		t.Errorf("Deployment apiVersion = %q, want %q", deployment["apiVersion"], "apps/v1")
	}

	// Check spec
	spec, ok := deployment["spec"].(map[string]any)
	if !ok {
		t.Fatal("Failed to get deployment spec")
	}

	if spec["replicas"] != float64(3) {
		t.Errorf("Replicas = %v, want 3", spec["replicas"])
	}
}

func TestManifestGenerator_Generate_NoPort(t *testing.T) {
	gen := NewManifestGenerator()

	cfg := DeploymentConfig{
		Name:      "worker-app",
		Namespace: "default",
		Image:     "worker:v1",
		Replicas:  2,
		Port:      nil, // No port - should skip Service creation
	}

	result, err := gen.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should only have Deployment, no Service
	if len(result.Manifests) != 1 {
		t.Errorf("Expected 1 manifest (Deployment only), got %d", len(result.Manifests))
	}
}

func TestManifestGenerator_Generate_DefaultResources(t *testing.T) {
	gen := NewManifestGenerator()

	port := 80
	cfg := DeploymentConfig{
		Name:      "minimal-app",
		Namespace: "default",
		Image:     "app:latest",
		Replicas:  1,
		Port:      &port,
		// No resources specified - should use defaults
	}

	result, err := gen.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var deployment map[string]any
	if err := json.Unmarshal(result.Manifests[0], &deployment); err != nil {
		t.Fatalf("Failed to unmarshal deployment: %v", err)
	}

	// Navigate to container resources
	spec := deployment["spec"].(map[string]any)
	template := spec["template"].(map[string]any)
	podSpec := template["spec"].(map[string]any)
	containers := podSpec["containers"].([]any)
	container := containers[0].(map[string]any)
	resources := container["resources"].(map[string]any)
	requests := resources["requests"].(map[string]any)
	limits := resources["limits"].(map[string]any)

	// Check defaults
	if requests["cpu"] != "100m" {
		t.Errorf("Default CPU request = %q, want %q", requests["cpu"], "100m")
	}
	if requests["memory"] != "128Mi" {
		t.Errorf("Default memory request = %q, want %q", requests["memory"], "128Mi")
	}
	if limits["cpu"] != "500m" {
		t.Errorf("Default CPU limit = %q, want %q", limits["cpu"], "500m")
	}
	if limits["memory"] != "512Mi" {
		t.Errorf("Default memory limit = %q, want %q", limits["memory"], "512Mi")
	}
}

func TestGenerateNamespace(t *testing.T) {
	tests := []struct {
		orgSlug string
		appSlug string
		want    string
	}{
		{"acme", "myapp", "acme-myapp"},
		{"my_org", "my_app", "my-org-my-app"},
		{"ORG", "APP", "org-app"},
		{"a", "b", "a-b"},
	}

	for _, tc := range tests {
		t.Run(tc.orgSlug+"-"+tc.appSlug, func(t *testing.T) {
			got := GenerateNamespace(tc.orgSlug, tc.appSlug)
			if got != tc.want {
				t.Errorf("GenerateNamespace(%q, %q) = %q, want %q", tc.orgSlug, tc.appSlug, got, tc.want)
			}
		})
	}
}

func TestParseEnvVars(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  int
	}{
		{"empty", nil, 0},
		{"empty array", []byte("[]"), 0},
		{"one var", []byte(`[{"name":"FOO","value":"bar"}]`), 1},
		{"multiple vars", []byte(`[{"name":"A","value":"1"},{"name":"B","value":"2"}]`), 2},
		{"invalid json", []byte("not json"), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseEnvVars(tc.input)
			if len(got) != tc.want {
				t.Errorf("ParseEnvVars returned %d vars, want %d", len(got), tc.want)
			}
		})
	}
}

func TestPipelineConstants(t *testing.T) {
	// Verify constants are properly defined
	if StatusPending != "pending" {
		t.Errorf("StatusPending = %q, want %q", StatusPending, "pending")
	}
	if StatusBuilding != "building" {
		t.Errorf("StatusBuilding = %q, want %q", StatusBuilding, "building")
	}
	if StatusDeploying != "deploying" {
		t.Errorf("StatusDeploying = %q, want %q", StatusDeploying, "deploying")
	}
	if StatusSucceeded != "succeeded" {
		t.Errorf("StatusSucceeded = %q, want %q", StatusSucceeded, "succeeded")
	}
	if StatusFailed != "failed" {
		t.Errorf("StatusFailed = %q, want %q", StatusFailed, "failed")
	}

	if StageInit != "init" {
		t.Errorf("StageInit = %q, want %q", StageInit, "init")
	}
	if StageBuild != "build" {
		t.Errorf("StageBuild = %q, want %q", StageBuild, "build")
	}
	if StageRelease != "release" {
		t.Errorf("StageRelease = %q, want %q", StageRelease, "release")
	}
	if StageDeploy != "deploy" {
		t.Errorf("StageDeploy = %q, want %q", StageDeploy, "deploy")
	}

	if SyncStatusPending != "pending" {
		t.Errorf("SyncStatusPending = %q, want %q", SyncStatusPending, "pending")
	}
	if SyncStatusSynced != "synced" {
		t.Errorf("SyncStatusSynced = %q, want %q", SyncStatusSynced, "synced")
	}
}

func TestToPipelineRunView(t *testing.T) {
	releaseID := "release-123"
	buildID := "build-456"
	pr := &PipelineRun{
		ID:           "pr-789",
		DeploymentID: "dep-abc",
		ReleaseID:    &releaseID,
		SourceType:   SourceTypeImage,
		SourceRef:    "nginx:latest",
		BuildID:      &buildID,
		Status:       StatusDeploying,
		CurrentStage: StageDeploy,
		TriggeredBy:  TriggerUser,
	}

	view := ToPipelineRunView(pr)

	if view.ID != pr.ID {
		t.Errorf("ID = %q, want %q", view.ID, pr.ID)
	}
	if view.DeploymentID != pr.DeploymentID {
		t.Errorf("DeploymentID = %q, want %q", view.DeploymentID, pr.DeploymentID)
	}
	if view.SourceType != pr.SourceType {
		t.Errorf("SourceType = %q, want %q", view.SourceType, pr.SourceType)
	}
	if view.Status != pr.Status {
		t.Errorf("Status = %q, want %q", view.Status, pr.Status)
	}
	if view.CurrentStage != pr.CurrentStage {
		t.Errorf("CurrentStage = %q, want %q", view.CurrentStage, pr.CurrentStage)
	}
}

func TestToDesiredStateView(t *testing.T) {
	ds := &DesiredState{
		ID:           "ds-123",
		DeploymentID: "dep-456",
		ReleaseID:    "rel-789",
		ClusterID:    "cluster-abc",
		Namespace:    "test-ns",
		Manifests:    []byte(`[{"kind":"Deployment"}]`),
		ManifestHash: "abc123",
		SyncStatus:   SyncStatusPending,
		Generation:   5,
		ObservedGeneration: 3,
	}

	view := ToDesiredStateView(ds)

	if view.ID != ds.ID {
		t.Errorf("ID = %q, want %q", view.ID, ds.ID)
	}
	if view.SyncStatus != ds.SyncStatus {
		t.Errorf("SyncStatus = %q, want %q", view.SyncStatus, ds.SyncStatus)
	}
	if view.Generation != ds.Generation {
		t.Errorf("Generation = %d, want %d", view.Generation, ds.Generation)
	}
	if view.ObservedGeneration != ds.ObservedGeneration {
		t.Errorf("ObservedGeneration = %d, want %d", view.ObservedGeneration, ds.ObservedGeneration)
	}
}

func TestToAgentDesiredState(t *testing.T) {
	ds := &DesiredState{
		DeploymentID: "dep-123",
		ReleaseID:    "rel-456",
		Namespace:    "app-ns",
		Manifests:    []byte(`[{"kind":"Deployment"}]`),
		ManifestHash: "xyz789",
		Generation:   10,
	}

	agent := ToAgentDesiredState(ds)

	if agent.DeploymentID != ds.DeploymentID {
		t.Errorf("DeploymentID = %q, want %q", agent.DeploymentID, ds.DeploymentID)
	}
	if agent.Namespace != ds.Namespace {
		t.Errorf("Namespace = %q, want %q", agent.Namespace, ds.Namespace)
	}
	if agent.ManifestHash != ds.ManifestHash {
		t.Errorf("ManifestHash = %q, want %q", agent.ManifestHash, ds.ManifestHash)
	}
	if agent.Generation != ds.Generation {
		t.Errorf("Generation = %d, want %d", agent.Generation, ds.Generation)
	}
}
