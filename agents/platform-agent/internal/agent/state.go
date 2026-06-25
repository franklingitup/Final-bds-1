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

// SaveState persists state to the state file.
func SaveState(path string, state *State) error {
	// Ensure directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}

	return nil
}
