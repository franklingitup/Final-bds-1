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

	// PodUID is the Kubernetes Pod UID injected via the downward API
	// (metadata.uid). It is used as a stable AgentID fallback on first boot
	// when no persisted or explicitly configured AgentID exists, so the agent
	// avoids inventing a random identity that would change on every restart.
	PodUID string

	// Token is the registration token from the control plane.
	Token string

	// ControlPlaneURL is the base URL of the control plane API.
	ControlPlaneURL string

	// HeartbeatInterval is how often to send heartbeats.
	HeartbeatInterval time.Duration

	// ReconcileInterval is how often to reconcile deployments.
	ReconcileInterval time.Duration

	// RegistrationRetryInterval is the initial backoff between registration
	// attempts. The agent applies exponential backoff starting from this value.
	RegistrationRetryInterval time.Duration

	// RegistrationMaxRetryInterval caps the exponential registration backoff so
	// a persistently-unavailable control plane never stalls recovery beyond this
	// bound. The agent retries indefinitely (it never crash-loops on
	// registration); only unrecoverable configuration errors terminate it.
	RegistrationMaxRetryInterval time.Duration

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

	// LeaderElectionEnabled controls whether the agent participates in
	// Kubernetes Lease-based leader election. When false (the default) the
	// agent behaves exactly as it did before this feature existed: every
	// replica reconciles and syncs. When true, only the elected leader
	// performs reconciliation, secret sync, orphan cleanup and status
	// updates; followers stay warm (heartbeat + metrics) and take over on
	// failover.
	LeaderElectionEnabled bool

	// LeaseName is the name of the coordination.k8s.io/v1 Lease object used
	// for leader election. All replicas of the same agent must share it.
	LeaseName string

	// LeaseNamespace is the namespace that holds the Lease object. Defaults
	// to the workload Namespace when unset.
	LeaseNamespace string

	// LeaseIdentity uniquely identifies this replica as a candidate for
	// leadership (the Lease holder identity). Defaults to the pod name
	// (POD_NAME / HOSTNAME) and finally the agent ID.
	LeaseIdentity string

	// LeaseDuration is how long a non-leader waits before attempting to
	// acquire leadership. Kubernetes recommended default: 15s.
	LeaseDuration time.Duration

	// RenewDeadline is how long the leader retries refreshing leadership
	// before giving up. Kubernetes recommended default: 10s. Must be less
	// than LeaseDuration.
	RenewDeadline time.Duration

	// RetryPeriod is how often clients retry acquisition/renewal.
	// Kubernetes recommended default: 2s.
	RetryPeriod time.Duration

	// MetricsAddr is the listen address for a DEDICATED Prometheus metrics
	// endpoint (e.g. ":9091"). Empty disables the dedicated metrics server. It
	// is defaulted to ":9091" only when leader election is enabled so that
	// followers can expose metrics out of the box, while a leader-election-
	// disabled agent keeps its previous (no dedicated server) behaviour.
	// NOTE: metrics are ALSO always served on HealthAddr (/metrics), so this is
	// only needed when you want metrics on a separate port.
	MetricsAddr string

	// HealthAddr is the listen address for the always-on health/metrics HTTP
	// server. It serves /healthz (liveness), /readyz (readiness — 200 once the
	// agent is registered) and /metrics. It MUST match the container port the
	// Kubernetes probes target (the Helm chart uses :8080). This server is
	// always started; without it the pod's liveness/readiness probes have
	// nothing to hit and the pod CrashLoopBackOffs.
	HealthAddr string
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval:            30 * time.Second,
		ReconcileInterval:            30 * time.Second,
		RegistrationRetryInterval:    10 * time.Second,
		RegistrationMaxRetryInterval: 5 * time.Minute,
		RequestTimeout:               30 * time.Second,
		StateFile:                    "/var/lib/platform-agent/state.json",
		ReconcilerStateFile:          "/var/lib/platform-agent/reconciler-state.json",
		Namespace:                    "default",
		ReconcilerEnabled:            false,
		SecretsSyncerEnabled:         false,
		SecretsSyncInterval:          60 * time.Second,
		SecretsSyncerStateFile:       "/var/lib/platform-agent/secrets-state.json",
		LeaderElectionEnabled:        false,
		LeaseName:                    "platform-agent-leader",
		LeaseDuration:                15 * time.Second,
		RenewDeadline:                10 * time.Second,
		RetryPeriod:                  2 * time.Second,
		HealthAddr:                   ":8080",
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

	// Pod UID (downward API metadata.uid) is used only as a stable AgentID
	// fallback on first boot; it never overrides a persisted or configured ID.
	cfg.PodUID = os.Getenv("POD_UID")

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

	// Leader election.
	if v := os.Getenv("LEADER_ELECTION_ENABLED"); v == "true" || v == "1" {
		cfg.LeaderElectionEnabled = true
	} else if v == "false" || v == "0" {
		cfg.LeaderElectionEnabled = false
	}

	if v := os.Getenv("LEASE_NAME"); v != "" {
		cfg.LeaseName = v
	}

	if v := os.Getenv("LEASE_NAMESPACE"); v != "" {
		cfg.LeaseNamespace = v
	}
	// Default the lease namespace to the workload namespace when unset.
	if cfg.LeaseNamespace == "" {
		cfg.LeaseNamespace = cfg.Namespace
	}

	// Holder identity: prefer an explicit override, then the pod name (the
	// downward-API convention), then the container hostname, then the agent ID.
	cfg.LeaseIdentity = os.Getenv("LEASE_IDENTITY")
	if cfg.LeaseIdentity == "" {
		cfg.LeaseIdentity = os.Getenv("POD_NAME")
	}
	if cfg.LeaseIdentity == "" {
		cfg.LeaseIdentity = os.Getenv("HOSTNAME")
	}

	if v := os.Getenv("LEASE_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid LEASE_DURATION: %w", err)
		}
		cfg.LeaseDuration = d
	}

	if v := os.Getenv("RENEW_DEADLINE"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid RENEW_DEADLINE: %w", err)
		}
		cfg.RenewDeadline = d
	}

	if v := os.Getenv("RETRY_PERIOD"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid RETRY_PERIOD: %w", err)
		}
		cfg.RetryPeriod = d
	}

	if v := os.Getenv("METRICS_ADDR"); v != "" {
		cfg.MetricsAddr = v
	}

	// HealthAddr backs the always-on liveness/readiness/metrics server. An
	// explicit empty value ("HEALTH_ADDR=") disables it, but that is strongly
	// discouraged in Kubernetes where probes depend on it.
	if v, ok := os.LookupEnv("HEALTH_ADDR"); ok {
		cfg.HealthAddr = v
	}

	// Only validate/observe leader-election timings when the feature is on,
	// so a disabled agent can never fail to start because of them.
	if cfg.LeaderElectionEnabled {
		if err := cfg.validateLeaderElection(); err != nil {
			return cfg, err
		}
		// Expose metrics by default when leader election is enabled so that
		// followers satisfy the "expose metrics" requirement without extra
		// configuration. Operators can still override or disable via METRICS_ADDR.
		if cfg.MetricsAddr == "" {
			cfg.MetricsAddr = ":9091"
		}
	}

	return cfg, nil
}

// validateLeaderElection enforces the client-go leaderelection invariants
// (LeaseDuration > RenewDeadline > RetryPeriod, all positive). It mirrors the
// checks client-go performs so misconfiguration is caught at startup with a
// clear message rather than deep inside the election loop.
func (c Config) validateLeaderElection() error {
	if c.LeaseName == "" {
		return fmt.Errorf("LEASE_NAME must not be empty when leader election is enabled")
	}
	if c.LeaseDuration <= 0 || c.RenewDeadline <= 0 || c.RetryPeriod <= 0 {
		return fmt.Errorf("LEASE_DURATION, RENEW_DEADLINE and RETRY_PERIOD must all be positive")
	}
	if c.LeaseDuration <= c.RenewDeadline {
		return fmt.Errorf("LEASE_DURATION (%s) must be greater than RENEW_DEADLINE (%s)", c.LeaseDuration, c.RenewDeadline)
	}
	if c.RenewDeadline <= c.RetryPeriod {
		return fmt.Errorf("RENEW_DEADLINE (%s) must be greater than RETRY_PERIOD (%s)", c.RenewDeadline, c.RetryPeriod)
	}
	return nil
}
