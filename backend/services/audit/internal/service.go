package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// Deps are the audit service dependencies. The store is an interface so the
// service is unit-testable with an in-memory fake.
type Deps struct {
	Store      AuditLogStore
	OrgMembers authz.OrgMemberStore // For org membership authorization
	Tenant     TenantRunner
	Logger     *slog.Logger
	Now        func() time.Time
}

// Service records consumed domain events into the immutable audit log and serves
// tenant-scoped queries over it.
type Service struct {
	store      AuditLogStore
	orgMembers authz.OrgMemberStore
	tenant     TenantRunner
	authSvc    *authz.AuthorizationService
	log        *slog.Logger
	now        func() time.Time
}

// NewService wires an audit Service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{
		store:      d.Store,
		orgMembers: d.OrgMembers,
		tenant:     d.Tenant,
		authSvc:    authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		log:        d.Logger,
		now:        d.Now,
	}
}

// RecordEvent persists a single consumed event. Events outside the supported
// domains are ignored (reported as not recorded). The write is idempotent on the
// event id, so at-least-once redelivery is safe. The boolean reports whether a
// new audit row was written.
func (s *Service) RecordEvent(ctx context.Context, e events.Envelope) (bool, error) {
	if !isSupportedDomain(e.Type) {
		return false, nil
	}
	if e.OrgID == "" {
		// An event without a tenant cannot be RLS-scoped; skip rather than fail
		// the consumer (the framework already validates this on publish).
		s.log.Warn("skipping event without orgId", "type", e.Type, "eventId", e.EventID)
		return false, nil
	}

	rec := recordFromEnvelope(e)
	var inserted bool
	err := s.tenant.WithTenant(ctx, e.OrgID, func(ctx context.Context) error {
		var err error
		inserted, err = s.store.Insert(ctx, rec)
		return err
	})
	if err != nil {
		return false, err
	}
	return inserted, nil
}

// ListLogs returns a filtered, paginated page of audit records for an org.
// SECURITY: Requires org membership with audit read privileges.
func (s *Service) ListLogs(ctx context.Context, orgID, userID string, f AuditFilter, page database.PageRequest) (database.Page[AuditLog], error) {
	// SECURITY: Verify caller has audit read privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionReadAudit); err != nil {
		return database.Page[AuditLog]{}, err
	}

	var out database.Page[AuditLog]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.store.List(ctx, f, page)
		return err
	})
	return out, err
}

// GetLog returns a single audit record by source event id within an org.
// SECURITY: Requires org membership with audit read privileges.
func (s *Service) GetLog(ctx context.Context, orgID, userID, eventID string) (AuditLog, error) {
	// SECURITY: Verify caller has audit read privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionReadAudit); err != nil {
		return AuditLog{}, err
	}

	var out AuditLog
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.store.GetByEventID(ctx, eventID)
		return err
	})
	return out, err
}
