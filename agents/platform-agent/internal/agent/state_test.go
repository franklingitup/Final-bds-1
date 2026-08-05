package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// TestLoadState_Corrupt verifies a corrupt state file surfaces an error so the
// caller can fall back to control-plane recovery rather than silently trusting
// garbage.
func TestLoadState_Corrupt(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("expected error loading corrupt state, got nil")
	}
}

// TestSaveState_AtomicOverwrite verifies that repeated saves overwrite cleanly,
// leave the final content intact, and never leave temp files behind (a leaked
// temp file would indicate a non-atomic or aborted write path).
func TestSaveState_AtomicOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	if err := SaveState(path, &State{AgentID: "a1", ClusterID: "c1", Registered: true}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := SaveState(path, &State{AgentID: "a2", ClusterID: "c2", Registered: true}); err != nil {
		t.Fatalf("second save: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.AgentID != "a2" || loaded.ClusterID != "c2" {
		t.Errorf("final state = %+v, want a2/c2", loaded)
	}

	// No temp files must remain in the directory.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), ".state-") {
			t.Errorf("leaked temp file: %s", e.Name())
		}
	}
}

// TestSaveState_Permissions verifies the state file is written with 0600 so the
// persisted (potentially sensitive) cluster/agent identifiers are not
// world-readable. Skipped on Windows, whose permission model differs.
func TestSaveState_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file permissions not applicable on Windows")
	}
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")
	if err := SaveState(path, &State{AgentID: "a1"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("state file perm = %o, want 600", perm)
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
