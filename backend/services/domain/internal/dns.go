package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"
)

// DNSVerifier performs DNS-based domain verification.
type DNSVerifier struct {
	resolver *net.Resolver
	timeout  time.Duration
}

// NewDNSVerifier creates a new DNS verifier.
func NewDNSVerifier() *DNSVerifier {
	return &DNSVerifier{
		resolver: net.DefaultResolver,
		timeout:  10 * time.Second,
	}
}

// NewDNSVerifierWithResolver creates a DNS verifier with a custom resolver.
func NewDNSVerifierWithResolver(resolver *net.Resolver) *DNSVerifier {
	return &DNSVerifier{
		resolver: resolver,
		timeout:  10 * time.Second,
	}
}

// GenerateVerificationToken generates a random verification token.
func GenerateVerificationToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// BuildTXTRecordName constructs the TXT record name for verification.
func BuildTXTRecordName(domain string) string {
	return fmt.Sprintf("_bdsplatform-verify.%s", domain)
}

// BuildFullDomain constructs the full domain from domain and optional subdomain.
func BuildFullDomain(domain string, subdomain *string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if subdomain != nil && *subdomain != "" {
		sub := strings.ToLower(strings.TrimSpace(*subdomain))
		return fmt.Sprintf("%s.%s", sub, domain)
	}
	return domain
}

// VerifyTXTRecord verifies that the TXT record exists with the expected value.
func (v *DNSVerifier) VerifyTXTRecord(ctx context.Context, recordName, expectedValue string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	records, err := v.resolver.LookupTXT(ctx, recordName)
	if err != nil {
		// DNS error could mean record doesn't exist
		if dnsErr, ok := err.(*net.DNSError); ok {
			if dnsErr.IsNotFound || dnsErr.IsTemporary {
				return false, nil
			}
		}
		return false, fmt.Errorf("DNS lookup failed: %w", err)
	}

	for _, record := range records {
		if strings.TrimSpace(record) == expectedValue {
			return true, nil
		}
	}

	return false, nil
}

// VerifyCNAMERecord verifies that the CNAME record points to the expected target.
func (v *DNSVerifier) VerifyCNAMERecord(ctx context.Context, domain, expectedTarget string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	cname, err := v.resolver.LookupCNAME(ctx, domain)
	if err != nil {
		if dnsErr, ok := err.(*net.DNSError); ok {
			if dnsErr.IsNotFound {
				return false, nil
			}
		}
		return false, fmt.Errorf("CNAME lookup failed: %w", err)
	}

	// Normalize both values
	cname = strings.TrimSuffix(strings.ToLower(cname), ".")
	expectedTarget = strings.TrimSuffix(strings.ToLower(expectedTarget), ".")

	return cname == expectedTarget, nil
}

// LookupIP resolves a domain to its IP addresses.
func (v *DNSVerifier) LookupIP(ctx context.Context, domain string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	ips, err := v.resolver.LookupHost(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("IP lookup failed: %w", err)
	}

	return ips, nil
}

// VerifyDomainReachable checks if the domain resolves to valid IP addresses.
func (v *DNSVerifier) VerifyDomainReachable(ctx context.Context, domain string) (bool, error) {
	ips, err := v.LookupIP(ctx, domain)
	if err != nil {
		return false, err
	}
	return len(ips) > 0, nil
}

// ValidateDomainFormat validates that a domain has a valid format.
func ValidateDomainFormat(domain string) error {
	domain = strings.TrimSpace(domain)

	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}

	if len(domain) > 253 {
		return fmt.Errorf("domain name too long (max 253 characters)")
	}

	// Must contain at least one dot
	if !strings.Contains(domain, ".") {
		return fmt.Errorf("invalid domain format: must contain at least one dot")
	}

	parts := strings.Split(domain, ".")
	for _, part := range parts {
		if len(part) == 0 {
			return fmt.Errorf("invalid domain format: empty label")
		}
		if len(part) > 63 {
			return fmt.Errorf("invalid domain format: label too long (max 63 characters)")
		}
		// Check for valid characters (simplified)
		for _, c := range part {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return fmt.Errorf("invalid domain format: invalid character '%c'", c)
			}
		}
		// Cannot start or end with hyphen
		if part[0] == '-' || part[len(part)-1] == '-' {
			return fmt.Errorf("invalid domain format: label cannot start or end with hyphen")
		}
	}

	return nil
}

// ValidateSubdomain validates that a subdomain has a valid format.
func ValidateSubdomain(subdomain string) error {
	if subdomain == "" {
		return nil // Empty subdomain is valid (apex domain)
	}

	subdomain = strings.TrimSpace(subdomain)

	if len(subdomain) > 63 {
		return fmt.Errorf("subdomain too long (max 63 characters)")
	}

	// Check for valid characters
	for _, c := range subdomain {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
			return fmt.Errorf("invalid subdomain: invalid character '%c'", c)
		}
	}

	// Cannot start or end with hyphen
	if subdomain[0] == '-' || subdomain[len(subdomain)-1] == '-' {
		return fmt.Errorf("invalid subdomain: cannot start or end with hyphen")
	}

	return nil
}
