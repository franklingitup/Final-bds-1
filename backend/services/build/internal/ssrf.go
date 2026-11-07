// Package build — SSRF-safe git URL validation.
package build

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// blockedCIDRs is the set of IP ranges that must never be contacted as git URL
// targets. Two-stage defence: raw IPs in the URL are rejected immediately;
// hostname-based URLs are rejected if DNS resolution returns any address in
// this list (including at Kaniko launch time, to defeat DNS rebinding).
//
// Covered ranges:
//   - 127.0.0.0/8      IPv4 loopback
//   - 10.0.0.0/8       RFC 1918 private
//   - 172.16.0.0/12    RFC 1918 private
//   - 192.168.0.0/16   RFC 1918 private
//   - 169.254.0.0/16   link-local / cloud instance metadata
//                       (AWS IMDSv1, GCP, Azure all use 169.254.169.254)
//   - 100.64.0.0/10    carrier-grade NAT (RFC 6598); also used by some cloud
//                       metadata proxy services
//   - 0.0.0.0/8        "this" network
//   - 192.0.0.0/24     IETF protocol assignments
//   - 240.0.0.0/4      reserved
//   - ::1/128           IPv6 loopback
//   - fc00::/7          IPv6 unique-local (includes fd00::/8)
//   - fe80::/10         IPv6 link-local
//
// IPv4-mapped IPv6 addresses (e.g. ::ffff:192.168.1.1) are caught by the IPv4
// CIDRs above because Go's net.IPNet.Contains normalises them via To4() before
// comparing. A separate ::ffff:0:0/96 entry is NOT included because that CIDR
// covers the entire IPv4-mapped space — it would incorrectly block public
// addresses like ::ffff:140.82.121.4.
var blockedCIDRs = func() []*net.IPNet {
	cidrs := []string{
		// IPv4 ranges
		"127.0.0.0/8",    // loopback
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"169.254.0.0/16", // link-local / cloud instance metadata
		"100.64.0.0/10",  // carrier-grade NAT (RFC 6598)
		"0.0.0.0/8",      // "this" network
		"192.0.0.0/24",   // IETF protocol assignments
		"240.0.0.0/4",    // reserved
		// IPv6 ranges
		"::1/128",   // IPv6 loopback
		"fc00::/7",  // IPv6 unique-local (includes fd00::/8)
		"fe80::/10", // IPv6 link-local
		// NOTE: ::ffff:0:0/96 is intentionally omitted. That CIDR would block
		// ALL IPv4-mapped IPv6 addresses (::ffff:a.b.c.d shares the same first
		// 96 bits regardless of a.b.c.d). Go's net.IPNet.Contains already
		// normalises IPv4-mapped addresses to their 4-byte form via To4() before
		// comparing, so private IPv4 addresses in IPv6 form (e.g.
		// ::ffff:192.168.1.1) are caught by the IPv4 CIDRs above without any
		// need for a separate ::ffff:0:0/96 entry.
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, ipNet, err := net.ParseCIDR(c)
		if err != nil {
			panic("ssrf: invalid built-in CIDR " + c + ": " + err.Error())
		}
		nets = append(nets, ipNet)
	}
	return nets
}()

// LookupHostFunc resolves a hostname to its IP addresses. It is a
// package-level variable so tests — including tests in the builder
// sub-package, which imports this package — can replace it without hitting
// real DNS, and without introducing a full interface allocation on the
// hot path.
//
// Production code must never replace this outside of test binaries.
var LookupHostFunc = func(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// isPrivateIP reports whether ip falls within any of the blocked CIDR ranges.
// Go's net.IPNet.Contains normalises IPv4-mapped IPv6 addresses automatically
// (e.g. ::ffff:192.168.1.1 → 192.168.1.1) before the range test, so a single
// pass through blockedCIDRs is sufficient.
func isPrivateIP(ip net.IP) bool {
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateGitURL performs a two-stage SSRF check on rawURL.
//
// Stage 1 — syntax:
//   - Must be http, https, or git@ SSH shorthand.
//   - Any other scheme is rejected.
//
// Stage 2 — DNS resolution:
//   - If the "hostname" component is already a bare IP literal, it is checked
//     directly against blockedCIDRs (no DNS round-trip, so raw IPs like
//     http://169.254.169.254/ are always caught).
//   - Otherwise every address returned by LookupHostFunc must be publicly
//     routable; a single private/reserved address in the response causes
//     rejection.
//
// ValidateGitURL is called at two points in the build pipeline:
//
//  1. At the API boundary (CreateBuild / CreateRepository /
//     TriggerBuildFromWebhook) — prevents submitting an obviously hostile URL.
//
//  2. Immediately before launching a Kaniko job (buildKanikoJobSpec) — defends
//     against DNS rebinding: a hostname that resolved to a public IP at request
//     time may resolve to a private IP minutes later when the worker picks up
//     the build.
//
// ValidateGitURL is exported so the builder sub-package can call it without
// duplicating the CIDR list.
func ValidateGitURL(ctx context.Context, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return apperrors.Validation("git URL is required")
	}

	var hostname string

	switch {
	case strings.HasPrefix(rawURL, "git@"):
		// SSH shorthand: git@HOST:PATH
		rest := strings.TrimPrefix(rawURL, "git@")
		idx := strings.Index(rest, ":")
		if idx <= 0 {
			return apperrors.Validation("invalid git URL format")
		}
		hostname = rest[:idx]

	default:
		u, err := url.Parse(rawURL)
		if err != nil || u.Host == "" {
			return apperrors.Validation("invalid git URL format")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return apperrors.Validation("git URL must use http, https, or git@ SSH")
		}
		// Hostname() strips any port component.
		hostname = u.Hostname()
	}

	if hostname == "" {
		return apperrors.Validation("invalid git URL: empty hostname")
	}

	// Stage 2a: raw IP literal in URL — no DNS lookup needed.
	if ip := net.ParseIP(hostname); ip != nil {
		if isPrivateIP(ip) {
			return apperrors.Validation(fmt.Sprintf(
				"git URL targets a private or reserved address (%s)", ip))
		}
		return nil
	}

	// Stage 2b: resolve the hostname and check every returned address.
	addrs, err := LookupHostFunc(ctx, hostname)
	if err != nil {
		return apperrors.Validation(fmt.Sprintf(
			"git URL hostname %q could not be resolved: %v", hostname, err))
	}
	if len(addrs) == 0 {
		return apperrors.Validation(fmt.Sprintf(
			"git URL hostname %q returned no addresses", hostname))
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue // malformed address from resolver — skip
		}
		if isPrivateIP(ip) {
			return apperrors.Validation(fmt.Sprintf(
				"git URL hostname %q resolves to a private or reserved address (%s)",
				hostname, ip))
		}
	}
	return nil
}
