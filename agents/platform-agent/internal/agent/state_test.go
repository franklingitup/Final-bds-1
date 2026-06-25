package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadState_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nonexistent.json")

	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if state.Registered {
		t.Error("expected new state to be unregistered")
	}
	if state.AgentID != "" {
		t.Errorf("expected empty AgentID, got %q", state.AgentID)
	}
}

func TestLoadState_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	// Write test state.
	data := []byte(`{"agentId":"agent-123","clusterId":"cluster-456","organizationId":"org-789","registered":true}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if state.AgentID != "agent-123" {
		t.Errorf("AgentID = %q, want %q", state.AgentID, "agent-123")
	}
	if state.ClusterID != "cluster-456" {
		t.Errorf("ClusterID = %q, want %q", state.ClusterID, "cluster-456")
	}
	if state.OrganizationID != "org-789" {
		t.Errorf("OrganizationID = %q, want %q", state.OrganizationID, "org-789")
	}
	if !state.Registered {
		t.Error("Registered = false, want true")
	}
}

func TestSaveState(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "subdir", "state.json")

	state := &State{
		AgentID:        "agent-123",
		ClusterID:      "cluster-456",
		OrganizationID: "org-789",
		Registered:     true,
	}

	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	// Verify file was created.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	// Verify content.
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if loaded.AgentID != state.AgentID {
		t.Errorf("AgentID = %q, want %q", loaded.AgentID, state.AgentID)
	}
	if loaded.ClusterID != state.ClusterID {
		t.Errorf("ClusterID = %q, want %q", loaded.ClusterID, state.ClusterID)
	}
	if loaded.Registered != state.Registered {
		t.Errorf("Registered = %v, want %v", loaded.Registered, state.Registered)
	}
}

func TestSaveState_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deep", "nested", "dir", "state.json")

	state := &State{
		AgentID:    "agent-123",
		Registered: false,
	}

	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	// Verify file was created.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not created: %v", err)
	}
}
