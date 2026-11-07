package build

import (
	"context"
	"fmt"
	"testing"
)

// stubLookup replaces LookupHostFunc for the duration of t, restoring it on
// cleanup. addrs maps hostname → []IP-string. Any hostname not in the map
// returns an error (simulating NXDOMAIN), making tests explicit about which
// hosts they allow.
func stubLookup(t *testing.T, addrs map[string][]string) {
	t.Helper()
	orig := LookupHostFunc
	t.Cleanup(func() { LookupHostFunc = orig })
	LookupHostFunc = func(_ context.Context, host string) ([]string, error) {
		if ips, ok := addrs[host]; ok {
			return ips, nil
		}
		return nil, fmt.Errorf("stubLookup: no record for %q", host)
	}
}

// publicIP is a real, routable GitHub IP used wherever we need a "safe" stub.
const publicIP = "140.82.121.4"

// ----------------------------------------------------------------------------
// Stage 1 — raw IP literals in the URL (no DNS needed)
// ----------------------------------------------------------------------------

func TestValidateGitURL_BlocksIPv4Loopback(t *testing.T) {
	if err := ValidateGitURL(context.Background(), "http://127.0.0.1/repo.git"); err == nil {
		t.Error("loopback 127.0.0.1 must be rejected")
	}
}

func TestValidateGitURL_BlocksLoopbackSubnet(t *testing.T) {
	// Any 127.x.x.x address, not just .1
	if err := ValidateGitURL(context.Background(), "http://127.0.0.2/repo.git"); err == nil {
		t.Error("loopback 127.0.0.2 must be rejected")
	}
}

func TestValidateGitURL_BlocksCloudMetadataEndpoint(t *testing.T) {
	// AWS IMDSv1 / GCP / Azure all use 169.254.169.254.
	// This is the highest-priority SSRF target in a cloud environment.
	if err := ValidateGitURL(context.Background(), "http://169.254.169.254/latest/meta-data/"); err == nil {
		t.Error("link-local cloud metadata endpoint must be rejected")
	}
	// Also via HTTPS
	if err := ValidateGitURL(context.Background(), "https://169.254.169.254/token"); err == nil {
		t.Error("link-local cloud metadata endpoint (https) must be rejected")
	}
}

func TestValidateGitURL_BlocksRFC1918_10(t *testing.T) {
	if err := ValidateGitURL(context.Background(), "http://10.0.0.1/repo.git"); err == nil {
		t.Error("RFC1918 10.x.x.x must be rejected")
	}
}

func TestValidateGitURL_BlocksRFC1918_172(t *testing.T) {
	if err := ValidateGitURL(context.Background(), "http://172.16.0.1/repo.git"); err == nil {
		t.Error("RFC1918 172.16.x.x must be rejected")
	}
	if err := ValidateGitURL(context.Background(), "http://172.31.255.255/repo.git"); err == nil {
		t.Error("RFC1918 172.31.x.x must be rejected")
	}
}

func TestValidateGitURL_BlocksRFC1918_192(t *testing.T) {
	if err := ValidateGitURL(context.Background(), "http://192.168.1.1/repo.git"); err == nil {
		t.Error("RFC1918 192.168.x.x must be rejected")
	}
}

func TestValidateGitURL_BlocksCGNAT(t *testing.T) {
	// 100.64.0.0/10 — carrier-grade NAT (RFC 6598)
	if err := ValidateGitURL(context.Background(), "http://100.64.0.1/repo.git"); err == nil {
		t.Error("CGNAT range 100.64.0.0/10 must be rejected")
	}
}

func TestValidateGitURL_BlocksIPv6Loopback(t *testing.T) {
	if err := ValidateGitURL(context.Background(), "http://[::1]/repo.git"); err == nil {
		t.Error("IPv6 loopback ::1 must be rejected")
	}
}

func TestValidateGitURL_BlocksIPv6LinkLocal(t *testing.T) {
	if err := ValidateGitURL(context.Background(), "http://[fe80::1]/repo.git"); err == nil {
		t.Error("IPv6 link-local fe80::1 must be rejected")
	}
}

func TestValidateGitURL_BlocksIPv6UniqueLocal(t *testing.T) {
	if err := ValidateGitURL(context.Background(), "http://[fc00::1]/repo.git"); err == nil {
		t.Error("IPv6 unique-local fc00::1 must be rejected")
	}
}

// ----------------------------------------------------------------------------
// Stage 1 — syntax failures (no DNS needed)
// ----------------------------------------------------------------------------

func TestValidateGitURL_RejectsEmptyURL(t *testing.T) {
	if err := ValidateGitURL(context.Background(), ""); err == nil {
		t.Error("empty URL must be rejected")
	}
}

func TestValidateGitURL_RejectsInvalidScheme(t *testing.T) {
	cases := []string{
		"ftp://github.com/repo.git",
		"file:///etc/passwd",
		"gopher://10.0.0.1/",
		"not-a-url",
	}
	for _, u := range cases {
		if err := ValidateGitURL(context.Background(), u); err == nil {
			t.Errorf("URL %q with unsupported scheme must be rejected", u)
		}
	}
}

func TestValidateGitURL_RejectsMalformedSSH(t *testing.T) {
	// git@ URLs must contain a colon separating host from path.
	if err := ValidateGitURL(context.Background(), "git@github.com/repo.git"); err == nil {
		t.Error("git@ URL without colon must be rejected")
	}
}

// ----------------------------------------------------------------------------
// Stage 2 — DNS resolution checks
// ----------------------------------------------------------------------------

func TestValidateGitURL_BlocksHostnameResolvingToPrivate(t *testing.T) {
	stubLookup(t, map[string][]string{
		"evil.internal.example.com": {"192.168.100.1"},
	})
	err := ValidateGitURL(context.Background(), "https://evil.internal.example.com/repo.git")
	if err == nil {
		t.Error("hostname resolving to private IP must be rejected")
	}
}

func TestValidateGitURL_BlocksHostnameResolvingToMetadata(t *testing.T) {
	// Attacker registers a public hostname that points to 169.254.169.254.
	stubLookup(t, map[string][]string{
		"metadata-spoof.example.com": {"169.254.169.254"},
	})
	err := ValidateGitURL(context.Background(), "https://metadata-spoof.example.com/repo.git")
	if err == nil {
		t.Error("hostname resolving to link-local metadata IP must be rejected")
	}
}

func TestValidateGitURL_BlocksIfAnyResolvedIPIsPrivate(t *testing.T) {
	// Even if most IPs are public, a single private address must cause rejection.
	stubLookup(t, map[string][]string{
		"mixed-host.example.com": {publicIP, "10.0.0.1"},
	})
	err := ValidateGitURL(context.Background(), "https://mixed-host.example.com/repo.git")
	if err == nil {
		t.Error("hostname with one private IP in multi-address response must be rejected")
	}
}

func TestValidateGitURL_BlocksUnresolvableHostname(t *testing.T) {
	stubLookup(t, map[string][]string{}) // no entry → returns error
	err := ValidateGitURL(context.Background(), "https://does-not-exist.example.com/repo.git")
	if err == nil {
		t.Error("unresolvable hostname must be rejected")
	}
}

// ----------------------------------------------------------------------------
// Acceptance cases — these must PASS
// ----------------------------------------------------------------------------

func TestValidateGitURL_AcceptsPublicHTTPS(t *testing.T) {
	stubLookup(t, map[string][]string{
		"github.com": {publicIP},
	})
	if err := ValidateGitURL(context.Background(), "https://github.com/org/repo.git"); err != nil {
		t.Errorf("public HTTPS git URL must be accepted: %v", err)
	}
}

func TestValidateGitURL_AcceptsPublicHTTP(t *testing.T) {
	stubLookup(t, map[string][]string{
		"github.com": {publicIP},
	})
	if err := ValidateGitURL(context.Background(), "http://github.com/org/repo.git"); err != nil {
		t.Errorf("public HTTP git URL must be accepted: %v", err)
	}
}

func TestValidateGitURL_AcceptsSSHShorthand(t *testing.T) {
	// SSH shorthand resolved via DNS stub (the host is "github.com").
	stubLookup(t, map[string][]string{
		"github.com": {publicIP},
	})
	if err := ValidateGitURL(context.Background(), "git@github.com:org/repo.git"); err != nil {
		t.Errorf("valid git@ SSH URL must be accepted: %v", err)
	}
}

func TestValidateGitURL_AcceptsPublicRawIP(t *testing.T) {
	// A non-private raw IP address should be accepted directly (no DNS).
	// 8.8.8.8 is Google's DNS, squarely in public space.
	if err := ValidateGitURL(context.Background(), "http://8.8.8.8/repo.git"); err != nil {
		t.Errorf("public raw IP must be accepted: %v", err)
	}
}

// ----------------------------------------------------------------------------
// DNS rebinding scenario
// ----------------------------------------------------------------------------

// TestValidateGitURL_DNSRebindingDefense demonstrates the defence-in-depth
// property of calling ValidateGitURL at both the API boundary and immediately
// before launching the Kaniko job.
//
// Scenario:
//  1. Attacker registers a hostname whose TTL=0 DNS record initially points to
//     a public IP (passes the first check at CreateBuild time).
//  2. By the time the build worker picks up the job and calls ValidateGitURL
//     again (the second check in buildKanikoJobSpec), the attacker has flipped
//     the DNS record to 169.254.169.254 (the cloud metadata endpoint).
//
// Without the second check the Kaniko executor would clone from the metadata
// endpoint inside the cluster network. With it, the build is refused.
func TestValidateGitURL_DNSRebindingDefense(t *testing.T) {
	const rebindHost = "rebind.attacker.example.com"
	callCount := 0

	orig := LookupHostFunc
	t.Cleanup(func() { LookupHostFunc = orig })

	LookupHostFunc = func(_ context.Context, host string) ([]string, error) {
		if host != rebindHost {
			return nil, fmt.Errorf("stubLookup: no record for %q", host)
		}
		callCount++
		if callCount == 1 {
			// First call (API boundary): resolve to a legitimate public IP.
			return []string{publicIP}, nil
		}
		// Second call (pre-launch check): the attacker has flipped DNS to the
		// cloud instance metadata endpoint.
		return []string{"169.254.169.254"}, nil
	}

	url := "https://" + rebindHost + "/repo.git"

	// API-boundary check — must pass.
	if err := ValidateGitURL(context.Background(), url); err != nil {
		t.Fatalf("first check (API boundary) should pass for a public-resolving hostname: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 DNS call after first ValidateGitURL, got %d", callCount)
	}

	// Pre-launch check — must catch the rebinding and fail.
	if err := ValidateGitURL(context.Background(), url); err == nil {
		t.Error("second check (pre-launch) must reject the hostname after DNS rebinding to 169.254.169.254")
	}
	if callCount != 2 {
		t.Fatalf("expected 2 DNS calls total after second ValidateGitURL, got %d", callCount)
	}
}
