package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadFromEnv(t *testing.T) {
	// Save original env and restore after test.
	origToken := os.Getenv("AGENT_TOKEN")
	origURL := os.Getenv("CONTROL_PLANE_URL")
	origID := os.Getenv("AGENT_ID")
	origInterval := os.Getenv("HEARTBEAT_INTERVAL")
	origState := os.Getenv("STATE_FILE")
	defer func() {
		os.Setenv("AGENT_TOKEN", origToken)
		os.Setenv("CONTROL_PLANE_URL", origURL)
		os.Setenv("AGENT_ID", origID)
		os.Setenv("HEARTBEAT_INTERVAL", origInterval)
		os.Setenv("STATE_FILE", origState)
	}()

	tests := []struct {
		name    string
		envs    map[string]string
		check   func(t *testing.T, cfg Config)
		wantErr bool
	}{
		{
			name: "required fields set",
			envs: map[string]string{
				"AGENT_TOKEN":       "test-token",
				"CONTROL_PLANE_URL": "https://api.example.com",
			},
			check: func(t *testing.T, cfg Config) {
				if cfg.Token != "test-token" {
					t.Errorf("Token = %q, want %q", cfg.Token, "test-token")
				}
				if cfg.ControlPlaneURL != "https://api.example.com" {
					t.Errorf("ControlPlaneURL = %q, want %q", cfg.ControlPlaneURL, "https://api.example.com")
				}
				// Check defaults.
				if cfg.HeartbeatInterval != 30*time.Second {
					t.Errorf("HeartbeatInterval = %v, want %v", cfg.HeartbeatInterval, 30*time.Second)
				}
			},
			wantErr: false,
		},
		{
			name: "missing token",
			envs: map[string]string{
				"CONTROL_PLANE_URL": "https://api.example.com",
			},
			wantErr: true,
		},
		{
			name: "missing url",
			envs: map[string]string{
				"AGENT_TOKEN": "test-token",
			},
			wantErr: true,
		},
		{
			name: "all optional fields",
			envs: map[string]string{
				"AGENT_TOKEN":        "test-token",
				"CONTROL_PLANE_URL":  "https://api.example.com",
				"AGENT_ID":           "custom-agent-id",
				"HEARTBEAT_INTERVAL": "1m",
				"STATE_FILE":         "/custom/path/state.json",
			},
			check: func(t *testing.T, cfg Config) {
				if cfg.AgentID != "custom-agent-id" {
					t.Errorf("AgentID = %q, want %q", cfg.AgentID, "custom-agent-id")
				}
				if cfg.HeartbeatInterval != time.Minute {
					t.Errorf("HeartbeatInterval = %v, want %v", cfg.HeartbeatInterval, time.Minute)
				}
				if cfg.StateFile != "/custom/path/state.json" {
					t.Errorf("StateFile = %q, want %q", cfg.StateFile, "/custom/path/state.json")
				}
			},
			wantErr: false,
		},
		{
			name: "invalid heartbeat interval",
			envs: map[string]string{
				"AGENT_TOKEN":        "test-token",
				"CONTROL_PLANE_URL":  "https://api.example.com",
				"HEARTBEAT_INTERVAL": "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env.
			os.Unsetenv("AGENT_TOKEN")
			os.Unsetenv("CONTROL_PLANE_URL")
			os.Unsetenv("AGENT_ID")
			os.Unsetenv("HEARTBEAT_INTERVAL")
			os.Unsetenv("STATE_FILE")

			// Set test env.
			for k, v := range tt.envs {
				os.Setenv(k, v)
			}

			cfg, err := LoadFromEnv()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.HeartbeatInterval != 30*time.Second {
		t.Errorf("HeartbeatInterval = %v, want %v", cfg.HeartbeatInterval, 30*time.Second)
	}
	if cfg.RegistrationRetryInterval != 10*time.Second {
		t.Errorf("RegistrationRetryInterval = %v, want %v", cfg.RegistrationRetryInterval, 10*time.Second)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout = %v, want %v", cfg.RequestTimeout, 30*time.Second)
	}
	if cfg.StateFile != "/var/lib/platform-agent/state.json" {
		t.Errorf("StateFile = %q, want %q", cfg.StateFile, "/var/lib/platform-agent/state.json")
	}
}
