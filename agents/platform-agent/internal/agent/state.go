package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State holds the persisted agent state.
type State struct {
	AgentID        string `json:"agentId"`
	ClusterID      string `json:"clusterId"`
	OrganizationID string `json:"organizationId"`
	Registered     bool   `json:"registered"`
}

// LoadState loads state from the state file.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &State{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	return &state, nil
}

// SaveState persists state to the state file atomically and durably.
//
// A naive os.WriteFile can leave a truncated/corrupt file if the process is
// killed (OOM, SIGKILL, node loss) or the volume fills mid-write — exactly the
// crash scenarios this agent must survive. Instead we write to a temp file in
// the same directory, fsync it, atomically rename it over the target, and fsync
// the directory so the rename is durable. A reader therefore always observes
// either the complete old file or the complete new one, never a partial write.
func SaveState(path string, state *State) error {
	// Ensure directory exists (the mounted writable volume, e.g.
	// /var/lib/platform-agent). MkdirAll is a no-op when it already exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// Temp file in the SAME directory so os.Rename is a same-filesystem atomic
	// operation (cross-device renames are not atomic and fail on some volumes).
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail out before the rename succeeds.
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp state file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp state file: %w", err)
	}
	// Flush file contents to stable storage before the rename.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename state file: %w", err)
	}

	// Fsync the directory so the rename itself survives a crash. Failure here is
	// non-fatal: the data is already written and renamed; on platforms where a
	// directory handle cannot be synced we simply skip it.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	return nil
}
