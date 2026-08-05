package domain

import "errors"

var (
	// ErrDomainNotFound is returned when a domain is not found.
	ErrDomainNotFound = errors.New("domain not found")

	// ErrDomainExists is returned when a domain already exists.
	ErrDomainExists = errors.New("domain already exists")

	// ErrDomainNotVerified is returned when an operation requires a verified domain.
	ErrDomainNotVerified = errors.New("domain is not verified")

	// ErrCertificateNotFound is returned when a certificate is not found.
	ErrCertificateNotFound = errors.New("certificate not found")

	// ErrCertificatePending is returned when certificate issuance is in progress.
	ErrCertificatePending = errors.New("certificate issuance is in progress")

	// ErrCertificateExpired is returned when a certificate has expired.
	ErrCertificateExpired = errors.New("certificate has expired")

	// ErrIngressNotFound is returned when an ingress record is not found.
	ErrIngressNotFound = errors.New("ingress record not found")

	// ErrVerificationFailed is returned when DNS verification fails.
	ErrVerificationFailed = errors.New("DNS verification failed")

	// ErrInvalidDomain is returned when a domain format is invalid.
	ErrInvalidDomain = errors.New("invalid domain format")
)
