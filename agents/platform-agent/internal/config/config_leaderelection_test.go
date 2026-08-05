package config

import (
	"testing"
	"time"
)

// setRequired sets the two mandatory env vars via t.Setenv (auto-restored) and
// clears every leader-election var so subtests are hermetic.
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_TOKEN", "test-token")
	t.Setenv("CONTROL_PLANE_URL", "https://api.example.com")
	for _, k := range []string{
		"LEADER_ELECTION_ENABLED", "LEASE_NAME", "LEASE_NAMESPACE", "LEASE_IDENTITY",
		"LEASE_DURATION", "RENEW_DEADLINE", "RETRY_PERIOD", "METRICS_ADDR",
		"POD_NAME", "HOSTNAME", "NAMESPACE",
	} {
		t.Setenv(k, "")
	}
}

func TestLeaderElectionDefaultsDisabled(t *testing.T) {
	setRequired(t)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.LeaderElectionEnabled {
		t.Error("leader election should be disabled by default")
	}
	if cfg.LeaseName != "platform-agent-leader" {
		t.Errorf("LeaseName = %q, want default", cfg.LeaseName)
	}
	if cfg.LeaseDuration != 15*time.Second {
		t.Errorf("LeaseDuration = %v, want 15s", cfg.LeaseDuration)
	}
	if cfg.RenewDeadline != 10*time.Second {
		t.Errorf("RenewDeadline = %v, want 10s", cfg.RenewDeadline)
	}
	if cfg.RetryPeriod != 2*time.Second {
		t.Errorf("RetryPeriod = %v, want 2s", cfg.RetryPeriod)
	}
	// Backward compatibility: no metrics server when leader election is off.
	if cfg.MetricsAddr != "" {
		t.Errorf("MetricsAddr = %q, want empty when disabled", cfg.MetricsAddr)
	}
}

func TestLeaderElectionEnabledDefaults(t *testing.T) {
	setRequired(t)
	t.Setenv("LEADER_ELECTION_ENABLED", "true")
	t.Setenv("NAMESPACE", "platform-system")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.LeaderElectionEnabled {
		t.Fatal("leader election should be enabled")
	}
	// Lease namespace defaults to the workload namespace.
	if cfg.LeaseNamespace != "platform-system" {
		t.Errorf("LeaseNamespace = %q, want platform-system", cfg.LeaseNamespace)
	}
	// Metrics endpoint is defaulted on so followers expose metrics.
	if cfg.MetricsAddr != ":9091" {
		t.Errorf("MetricsAddr = %q, want :9091", cfg.MetricsAddr)
	}
}

func TestLeaderElectionIdentityResolution(t *testing.T) {
	setRequired(t)
	t.Setenv("LEADER_ELECTION_ENABLED", "true")
	t.Setenv("POD_NAME", "platform-agent-2")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LeaseIdentity != "platform-agent-2" {
		t.Errorf("LeaseIdentity = %q, want platform-agent-2", cfg.LeaseIdentity)
	}

	// Explicit override wins over POD_NAME.
	t.Setenv("LEASE_IDENTITY", "explicit-id")
	cfg, err = LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LeaseIdentity != "explicit-id" {
		t.Errorf("LeaseIdentity = %q, want explicit-id", cfg.LeaseIdentity)
	}
}

func TestLeaderElectionCustomTimings(t *testing.T) {
	setRequired(t)
	t.Setenv("LEADER_ELECTION_ENABLED", "true")
	t.Setenv("LEASE_NAME", "custom-lease")
	t.Setenv("LEASE_NAMESPACE", "kube-system")
	t.Setenv("LEASE_DURATION", "30s")
	t.Setenv("RENEW_DEADLINE", "20s")
	t.Setenv("RETRY_PERIOD", "4s")
	t.Setenv("METRICS_ADDR", ":8080")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LeaseName != "custom-lease" || cfg.LeaseNamespace != "kube-system" {
		t.Errorf("lease name/namespace not parsed: %q/%q", cfg.LeaseName, cfg.LeaseNamespace)
	}
	if cfg.LeaseDuration != 30*time.Second || cfg.RenewDeadline != 20*time.Second || cfg.RetryPeriod != 4*time.Second {
		t.Errorf("timings not parsed: %v/%v/%v", cfg.LeaseDuration, cfg.RenewDeadline, cfg.RetryPeriod)
	}
	// Explicit METRICS_ADDR is preserved (not overwritten by the default).
	if cfg.MetricsAddr != ":8080" {
		t.Errorf("MetricsAddr = %q, want :8080", cfg.MetricsAddr)
	}
}

func TestLeaderElectionValidation(t *testing.T) {
	cases := []struct {
		name     string
		duration string
		renew    string
		retry    string
		wantErr  bool
	}{
		{"valid", "15s", "10s", "2s", false},
		{"duration not greater than renew", "10s", "10s", "2s", true},
		{"renew not greater than retry", "15s", "2s", "2s", true},
		{"invalid duration string", "abc", "10s", "2s", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("LEADER_ELECTION_ENABLED", "true")
			t.Setenv("LEASE_DURATION", tc.duration)
			t.Setenv("RENEW_DEADLINE", tc.renew)
			t.Setenv("RETRY_PERIOD", tc.retry)

			_, err := LoadFromEnv()
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestLeaderElectionDisabledSkipsValidation ensures a disabled agent never
// fails to start due to leader-election timing misconfiguration.
func TestLeaderElectionDisabledSkipsValidation(t *testing.T) {
	setRequired(t)
	t.Setenv("LEADER_ELECTION_ENABLED", "false")
	t.Setenv("LEASE_DURATION", "1s")
	t.Setenv("RENEW_DEADLINE", "10s") // invalid ordering, but must be ignored

	if _, err := LoadFromEnv(); err != nil {
		t.Errorf("disabled agent should ignore invalid LE timings, got: %v", err)
	}
}
