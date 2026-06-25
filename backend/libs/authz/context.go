package authz

import "context"

type ctxKey int

const (
	principalKey ctxKey = iota
	orgKey
)

// WithPrincipal stores the authenticated principal in the context.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// PrincipalFromContext returns the principal and whether one was present.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}

// WithOrg stores the active tenant organization ID in the context. This is the
// value used to scope database access via database.WithTenant.
func WithOrg(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgKey, orgID)
}

// OrgFromContext returns the active organization ID, or "".
func OrgFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(orgKey).(string); ok {
		return v
	}
	return ""
}
