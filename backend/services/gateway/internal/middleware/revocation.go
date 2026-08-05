package middleware

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/bdsplatform/platform/backend/services/gateway/internal/auth"
)

// Revocation metrics. These are the enforcement-point counters an operator
// alerts on: a rising store-error rate means the gateway is failing open (see
// RevocationChecker.Revoked) and revoked tokens may be getting through.
var (
	jwtRevocationChecks = promauto.NewCounter(prometheus.CounterOpts{
		Name: "jwt_revocation_checks_total",
		Help: "Total access tokens checked against the revocation store after signature validation.",
	})
	jwtRevocationHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "jwt_revocation_hits_total",
		Help: "Total checks that found the token or its session revoked (request rejected).",
	})
	jwtRevocationStoreErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "jwt_revocation_store_errors_total",
		Help: "Total revocation-store errors; the gateway fails open on these, allowing the request.",
	})
)

// revocationStore is the behavior the checker needs from the revocation list.
// *security.TokenRevocationList satisfies it. The seam keeps the middleware
// unit-testable without a live Redis (mirroring redisAllower for rate limiting).
type revocationStore interface {
	AnyRevoked(ctx context.Context, ids ...string) (bool, error)
}

// RevocationChecker enforces token revocation at the gateway. After the JWT
// signature is validated, it asks the shared store whether the token's JTI or
// owning session (SID) has been revoked and, if so, rejects the request.
//
// Fail-open policy: if the store errors (e.g. Redis outage) the checker records
// jwt_revocation_store_errors_total, logs a warning, and treats the token as
// NOT revoked. This matches the gateway's existing rate-limiter decision to
// prioritize availability over enforcement during a Redis outage; the error
// metric is the alerting signal. Revocations still hold whenever Redis is
// reachable, which is the steady state.
type RevocationChecker struct {
	store revocationStore
	log   *slog.Logger
}

// NewRevocationChecker builds a checker over the given store. It panics on a nil
// store so misconfiguration fails at startup rather than silently disabling
// enforcement; callers that want revocation disabled pass a nil *RevocationChecker
// to the middleware instead.
func NewRevocationChecker(store revocationStore, log *slog.Logger) *RevocationChecker {
	if store == nil {
		panic("middleware: NewRevocationChecker requires a non-nil store")
	}
	if log == nil {
		log = slog.Default()
	}
	return &RevocationChecker{store: store, log: log}
}

// Revoked reports whether the identity's access token has been revoked. It
// checks the token's JTI and, for user tokens, the owning session ID in a single
// store round-trip. Service-account tokens carry no session, so only their JTI
// is checked - they stay valid unless explicitly revoked. requestID is used only
// for structured logging.
func (rc *RevocationChecker) Revoked(ctx context.Context, requestID string, id auth.Identity) bool {
	jwtRevocationChecks.Inc()

	revoked, err := rc.store.AnyRevoked(ctx, id.JTI, id.SessionID)
	if err != nil {
		jwtRevocationStoreErrors.Inc()
		rc.log.WarnContext(ctx, "revocation store error; allowing request (fail-open)",
			slog.String("request_id", requestID),
			slog.String("user_id", id.UserID),
			slog.String("jti", id.JTI),
			slog.String("reason", "store_error"),
			slog.String("error", err.Error()),
		)
		return false
	}
	if !revoked {
		return false
	}

	jwtRevocationHits.Inc()
	rc.log.InfoContext(ctx, "rejected revoked token",
		slog.String("request_id", requestID),
		slog.String("user_id", id.UserID),
		slog.String("jti", id.JTI),
		slog.String("reason", "revoked"),
	)
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.AddEvent("jwt.revoked", trace.WithAttributes(
			attribute.String("auth.jti", id.JTI),
			attribute.String("auth.session_id", id.SessionID),
			attribute.String("auth.user_id", id.UserID),
		))
	}
	return true
}
