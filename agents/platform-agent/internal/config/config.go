// Package config provides configuration for the platform agent.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds the agent configuration.
type Config struct {
	// AgentID is a unique identifier for this agent instance.
	// Generated at first run and persisted.
	AgentID string

	// Token is the registration token from the control plane.
	Token string

	// ControlPlaneURL is the base URL of the control plane API.
	ControlPlaneURL string

	// HeartbeatInterval is how often to send heartbeats.
	HeartbeatInterval time.Duration

	// ReconcileInterval is how often to reconcile deployments.
	ReconcileInterval time.Duration

	// RegistrationRetryInterval is how long to wait between registration attempts.
	RegistrationRetryInterval time.Duration

	// RequestTimeout is the HTTP request timeout.
	RequestTimeout time.Duration

	// StateFile is the path to persist agent state (agent ID, cluster ID).
	StateFile string

	// ReconcilerStateFile is the path to persist reconciler state.
	ReconcilerStateFile string

	// Namespace is the Kubernetes namespace to deploy workloads into.
	Namespace string

	// ReconcilerEnabled controls whether deployment reconciliation is enabled.
	// When enabled, the agent will use its registered cluster credentials to
	// fetch desired state and report status via the dedicated agent API.
	ReconcilerEnabled bool

	// SecretsSyncerEnabled controls whether secrets synchronization is enabled.
	// When enabled, the agent will sync secrets from the control plane to Kubernetes.
	SecretsSyncerEnabled bool

	// SecretsSyncInterval is how often to sync secrets.
	SecretsSyncInterval time.Duration

	// SecretsSyncerStateFile is the path to persist secrets syncer state.
	SecretsSyncerStateFile string
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval:         30 * time.Second,
		ReconcileInterval:         30 * time.Second,
		RegistrationRetryInterval: 10 * time.Second,
		RequestTimeout:            30 * time.Second,
		StateFile:                 "/var/lib/platform-agent/state.json",
		ReconcilerStateFile:       "/var/lib/platform-agent/reconciler-state.json",
		Namespace:                 "default",
		ReconcilerEnabled:         false,
		SecretsSyncerEnabled:      false,
		SecretsSyncInterval:       60 * time.Second,
		SecretsSyncerStateFile:    "/var/lib/platform-agent/secrets-state.json",
	}
}

// LoadFromEnv loads configuration from environment variables.
func LoadFromEnv() (Config, error) {
	cfg := DefaultConfig()

	cfg.Token = os.Getenv("AGENT_TOKEN")
	if cfg.Token == "" {
		return cfg, fmt.Errorf("AGENT_TOKEN is required")
	}

	cfg.ControlPlaneURL = os.Getenv("CONTROL_PLANE_URL")
	if cfg.ControlPlaneURL == "" {
		return cfg, fmt.Errorf("CONTROL_PLANE_URL is required")
	}

	// Optional overrides.
	if v := os.Getenv("AGENT_ID"); v != "" {
		cfg.AgentID = v
	}

	if v := os.Getenv("HEARTBEAT_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid HEARTBEAT_INTERVAL: %w", err)
		}
		cfg.HeartbeatInterval = d
	}

	if v := os.Getenv("RECONCILE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid RECONCILE_INTERVAL: %w", err)
		}
		cfg.ReconcileInterval = d
	}

	if v := os.Getenv("STATE_FILE"); v != "" {
		cfg.StateFile = v
	}

	if v := os.Getenv("RECONCILER_STATE_FILE"); v != "" {
		cfg.ReconcilerStateFile = v
	}

	if v := os.Getenv("NAMESPACE"); v != "" {
		cfg.Namespace = v
	}

	// Enable/disable deployment reconciliation.
	// When enabled, the agent uses its registered cluster credentials (from state file)
	// to communicate with the dedicated agent API endpoints.
	if v := os.Getenv("RECONCILER_ENABLED"); v == "true" || v == "1" {
		cfg.ReconcilerEnabled = true
	} else if v == "false" || v == "0" {
		cfg.ReconcilerEnabled = false
	}

	// Enable/disable secrets synchronization.
	if v := os.Getenv("SECRETS_SYNCER_ENABLED"); v == "true" || v == "1" {
		cfg.SecretsSyncerEnabled = true
	} else if v == "false" || v == "0" {
		cfg.SecretsSyncerEnabled = false
	}

	if v := os.Getenv("SECRETS_SYNC_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid SECRETS_SYNC_INTERVAL: %w", err)
		}
		cfg.SecretsSyncInterval = d
	}

	if v := os.Getenv("SECRETS_SYNCER_STATE_FILE"); v != "" {
		cfg.SecretsSyncerStateFile = v
	}

	return cfg, nil
}
