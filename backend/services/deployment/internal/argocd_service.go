package deployment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/bdsplatform/platform/backend/libs/argocd"
	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/libs/telemetry"
)

// argoUnavailable reports an upstream Argo CD failure as a 502 Bad Gateway so
// clients can distinguish a transient dependency outage from a client error.
func argoUnavailable(msg string) error {
	return apperrors.NewWithStatus(apperrors.CodeInternal, http.StatusBadGateway, msg)
}

// ArgoServiceDeps are the dependencies of the GitOps (Argo CD) service.
type ArgoServiceDeps struct {
	Client       argocd.Client
	ArgoApps     ArgoApplicationStore
	Applications ApplicationStore
	Deployments  DeploymentStore
	Releases     ReleaseStore
	Rollouts     RolloutStatusStore
	OrgMembers   authz.OrgMemberStore
	Outbox       events.Outbox
	Tenant       TenantRunner
	Logger       *slog.Logger
	Now          func() time.Time
}

// ArgoService owns the lifecycle of Argo CD Applications that back GitOps
// deployments: it generates and reconciles Application CRs, triggers syncs and
// rollbacks, and mirrors Argo CD status onto the existing rollout model while
// emitting deployment.* engine events. It composes the same stores, outbox,
// tenancy runner and authorization service the rest of the deployment service
// uses; it never bypasses RBAC, RLS or the outbox.
type ArgoService struct {
	client   argocd.Client
	argoApps ArgoApplicationStore
	apps     ApplicationStore
	deps     DeploymentStore
	rels     ReleaseStore
	rollouts RolloutStatusStore
	outbox   events.Outbox
	tenant   TenantRunner
	authSvc  *authz.AuthorizationService
	log      *slog.Logger
	now      func() time.Time
	tracer   trace.Tracer
}

// NewArgoService constructs an ArgoService.
func NewArgoService(d ArgoServiceDeps) *ArgoService {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &ArgoService{
		client:   d.Client,
		argoApps: d.ArgoApps,
		apps:     d.Applications,
		deps:     d.Deployments,
		rels:     d.Releases,
		rollouts: d.Rollouts,
		outbox:   d.Outbox,
		tenant:   d.Tenant,
		authSvc:  authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		log:      d.Logger,
		now:      d.Now,
		tracer:   telemetry.Tracer("deployment-gitops"),
	}
}

// RegisterApplication binds a deployment to an Argo CD Application: it persists
// the GitOps source, creates (or upserts) the Application in Argo CD, and seeds
// the rollout status. It is idempotent — calling it again updates the desired
// source and re-applies the Application.
//
// SECURITY: Requires org membership with deployment write privileges.
func (s *ArgoService) RegisterApplication(ctx context.Context, orgID, userID, deploymentID string, src GitOpsSource) (*ArgoApplication, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}
	if strings.TrimSpace(src.RepoURL) == "" {
		return nil, apperrors.Validation("repoUrl is required")
	}
	if src.SourceType != "" && !ValidSourceType(src.SourceType) {
		return nil, apperrors.Validation("sourceType must be directory, helm, or kustomize")
	}

	ctx, span := s.tracer.Start(ctx, "gitops.RegisterApplication")
	defer span.End()
	span.SetAttributes(attribute.String("deployment.id", deploymentID))

	var record *ArgoApplication

	// Phase 1: persist the desired binding (tenant-scoped, transactional).
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		dep, err := s.deps.GetByID(ctx, deploymentID)
		if err != nil {
			if database.IsNotFound(err) {
				return errDeploymentNotFound
			}
			return err
		}
		app, err := s.apps.GetByID(ctx, dep.ApplicationID)
		if err != nil {
			return err
		}

		record = s.buildRecord(orgID, dep, app, src)
		return s.argoApps.Upsert(ctx, record)
	})
	if err != nil {
		return nil, s.traceErr(span, err)
	}

	span.SetAttributes(attribute.String("argocd.application", record.AppName))

	// Phase 2: reconcile the Application into Argo CD (remote, retried, outside
	// the DB transaction). Idempotent create-or-update via upsert.
	if err := s.ensureRemote(ctx, record); err != nil {
		s.log.ErrorContext(ctx, "argocd application create failed",
			"deployment_id", deploymentID, "application", record.AppName, "error", err)
		return nil, s.traceErr(span, argoUnavailable("argo cd application create failed"))
	}

	// Phase 3: seed the rollout status so the engine reports a Pending GitOps
	// rollout immediately (tenant-scoped).
	_ = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.seedRollout(ctx, orgID, record)
	})

	s.log.InfoContext(ctx, "gitops application registered",
		"deployment_id", deploymentID, "application", record.AppName,
		"repo", record.RepoURL, "revision", record.TargetRevision)
	return record, nil
}

// Sync triggers an Argo CD sync for a deployment's Application. It ensures the
// Application exists first (self-healing) and then requests the sync.
//
// SECURITY: Requires org membership with deployment write privileges.
func (s *ArgoService) Sync(ctx context.Context, orgID, userID, deploymentID string, req ArgoSyncRequest) (*ArgoApplication, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	ctx, span := s.tracer.Start(ctx, "gitops.Sync")
	defer span.End()
	span.SetAttributes(attribute.String("deployment.id", deploymentID))

	var record *ArgoApplication
	if err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		record, err = s.getBinding(ctx, deploymentID)
		return err
	}); err != nil {
		return nil, s.traceErr(span, err)
	}
	span.SetAttributes(attribute.String("argocd.application", record.AppName))

	// Ensure the Application exists, then request the sync (remote, retried).
	if err := s.ensureRemote(ctx, record); err != nil {
		return nil, s.traceErr(span, argoUnavailable("argo cd application unavailable"))
	}

	prune := record.Prune
	if req.Prune != nil {
		prune = *req.Prune
	}
	syncReq := argocd.SyncRequest{Revision: req.Revision, Prune: prune}

	err := s.client.RetryOperation(ctx, argocd.RetryOptions{}, func(ctx context.Context) error {
		_, e := s.client.SyncApplication(ctx, record.AppName, syncReq)
		return e
	})
	if err != nil {
		deploymentSyncFailuresTotal.Inc()
		deploymentSyncTotal.WithLabelValues(syncOutcomeFailed).Inc()
		return nil, s.traceErr(span, argoUnavailable("argo cd sync failed"))
	}

	s.log.InfoContext(ctx, "gitops sync triggered",
		"deployment_id", deploymentID, "application", record.AppName,
		"revision", req.Revision, "prune", prune)
	return record, nil
}

// Rollback reverts a deployment's Argo CD Application to a previous revision by
// resolving the git revision to an Argo CD deploy-history ID (or using an
// explicit history ID) and issuing an Argo CD rollback. It emits
// deployment.rollback.started.
//
// SECURITY: Requires org membership with deployment write privileges.
func (s *ArgoService) Rollback(ctx context.Context, orgID, userID, deploymentID string, req ArgoRollbackRequest) (*ArgoApplication, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}
	if req.Revision == "" && req.HistoryID == nil {
		return nil, apperrors.Validation("revision or historyId is required")
	}

	ctx, span := s.tracer.Start(ctx, "gitops.Rollback")
	defer span.End()
	span.SetAttributes(attribute.String("deployment.id", deploymentID))

	var record *ArgoApplication
	if err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		record, err = s.getBinding(ctx, deploymentID)
		return err
	}); err != nil {
		return nil, s.traceErr(span, err)
	}
	span.SetAttributes(attribute.String("argocd.application", record.AppName))

	// Resolve the target history ID from the requested revision.
	historyID, err := s.resolveHistoryID(ctx, record.AppName, req)
	if err != nil {
		return nil, s.traceErr(span, err)
	}

	err = s.client.RetryOperation(ctx, argocd.RetryOptions{}, func(ctx context.Context) error {
		_, e := s.client.RollbackApplication(ctx, record.AppName, historyID)
		return e
	})
	if err != nil {
		return nil, s.traceErr(span, argoUnavailable("argo cd rollback failed"))
	}
	deploymentRollbacksTotal.Inc()

	// Emit deployment.rollback.started (tenant-scoped, transactional outbox).
	_ = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.emit(ctx, orgID, EventDeploymentRollbackStarted, deploymentID, deploymentRollbackStartedPayload{
			DeploymentID: deploymentID,
			FromRevision: 0,
			TargetRevision: 0,
			Reason:       fmt.Sprintf("argocd rollback to revision %s (history %d)", req.Revision, historyID),
		})
	})

	s.log.InfoContext(ctx, "gitops rollback triggered",
		"deployment_id", deploymentID, "application", record.AppName,
		"revision", req.Revision, "history_id", historyID)
	return record, nil
}

// GetBinding returns the GitOps binding for a deployment (RBAC: org read).
func (s *ArgoService) GetBinding(ctx context.Context, orgID, userID, deploymentID string) (*ArgoApplication, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}
	var record *ArgoApplication
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		record, err = s.getBinding(ctx, deploymentID)
		return err
	})
	return record, err
}

// resolveHistoryID converts a rollback request into an Argo CD history ID.
func (s *ArgoService) resolveHistoryID(ctx context.Context, appName string, req ArgoRollbackRequest) (int64, error) {
	if req.HistoryID != nil {
		return *req.HistoryID, nil
	}
	app, err := s.client.GetApplication(ctx, appName)
	if err != nil {
		if errors.Is(err, argocd.ErrNotFound) {
			return 0, errArgoAppNotFound
		}
		return 0, argoUnavailable("argo cd unavailable")
	}
	id, ok := argocd.FindHistoryID(app, req.Revision)
	if !ok {
		return 0, apperrors.Validation("no deploy history matches the requested revision")
	}
	return id, nil
}

// ensureRemote creates or updates the Application in Argo CD. Create is
// idempotent (the client sets upsert=true), so this converges the remote state
// to the desired record. Retried for transient Argo CD API errors.
func (s *ArgoService) ensureRemote(ctx context.Context, record *ArgoApplication) error {
	desired := buildArgoApplication(record)
	return s.client.RetryOperation(ctx, argocd.RetryOptions{}, func(ctx context.Context) error {
		_, err := s.client.CreateApplication(ctx, desired)
		return err
	})
}

// getBinding loads the binding for a deployment, mapping not-found to a domain
// error. Must run inside a tenant transaction.
func (s *ArgoService) getBinding(ctx context.Context, deploymentID string) (*ArgoApplication, error) {
	record, err := s.argoApps.Get(ctx, deploymentID)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, errArgoAppNotFound
		}
		return nil, err
	}
	return record, nil
}

// buildRecord assembles the persisted ArgoApplication from a deployment,
// its application, and the supplied source, applying naming and defaults.
func (s *ArgoService) buildRecord(orgID string, dep *Deployment, app *Application, src GitOpsSource) *ArgoApplication {
	namespace := strings.TrimSpace(src.DestNamespace)
	if namespace == "" {
		namespace = app.Slug // matches the agent's default namespace convention
	}
	sourceType := src.SourceType
	if sourceType == "" {
		sourceType = SourceTypeDirectory
	}
	project := src.Project
	if project == "" {
		project = "default"
	}

	return &ArgoApplication{
		DeploymentID:   dep.ID,
		OrgID:          orgID,
		AppName:        argoAppName(app.Slug, dep.ID),
		Project:        project,
		RepoURL:        strings.TrimSpace(src.RepoURL),
		Path:           defaultString(src.Path, "."),
		TargetRevision: defaultString(src.TargetRevision, "HEAD"),
		SourceType:     sourceType,
		DestServer:     defaultString(src.DestServer, "https://kubernetes.default.svc"),
		DestNamespace:  namespace,
		AutoSync:       boolOrDefault(src.AutoSync, true),
		SelfHeal:       boolOrDefault(src.SelfHeal, true),
		Prune:          boolOrDefault(src.Prune, true),
	}
}

// seedRollout writes an initial Pending rollout_status row for the GitOps
// deployment so the engine surfaces progress from the first poll.
func (s *ArgoService) seedRollout(ctx context.Context, orgID string, record *ArgoApplication) error {
	if s.rollouts == nil || s.rels == nil {
		return nil
	}
	rel, err := s.rels.GetLatest(ctx, record.DeploymentID)
	if err != nil || rel == nil {
		return nil // no release yet; nothing to seed
	}
	return s.rollouts.Upsert(ctx, &RolloutStatus{
		DeploymentID: record.DeploymentID,
		ReleaseID:    rel.ID,
		OrgID:        orgID,
		Phase:        RolloutPhasePending,
		Revision:     rel.Revision,
		Image:        rel.Image,
	})
}

// emit enqueues an engine event onto the transactional outbox. No-op when no
// outbox is configured. Must run inside a tenant transaction.
func (s *ArgoService) emit(ctx context.Context, orgID, eventType, deploymentID string, payload any) error {
	if s.outbox == nil {
		return nil
	}
	evt, err := events.New(eventType, eventVersion, orgID, payload,
		events.WithActor(events.Actor{Type: "system", ID: "deployment-gitops"}),
		events.WithResource(events.Resource{Type: "deployment", ID: deploymentID}))
	if err != nil {
		return err
	}
	return s.outbox.Enqueue(ctx, evt)
}

func (s *ArgoService) traceErr(span trace.Span, err error) error {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// argoAppName derives a DNS-safe Argo CD Application name from the application
// slug and deployment ID. Argo application names are cluster-unique, so the
// deployment ID suffix guarantees uniqueness while keeping the slug readable.
func argoAppName(slug, deploymentID string) string {
	short := strings.ReplaceAll(deploymentID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	base := strings.ToLower(strings.TrimSpace(slug))
	if base == "" {
		base = "app"
	}
	name := base + "-" + short
	if len(name) > 253 {
		name = name[:253]
	}
	return name
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func boolOrDefault(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
