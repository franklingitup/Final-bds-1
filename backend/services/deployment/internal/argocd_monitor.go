package deployment

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/bdsplatform/platform/backend/libs/argocd"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/libs/telemetry"
)

// ArgoMonitorDeps configures the GitOps status monitor.
type ArgoMonitorDeps struct {
	Client   argocd.Client
	ArgoApps ArgoApplicationStore
	Releases ReleaseStore
	Rollouts RolloutStatusStore
	Outbox   events.Outbox
	Tenant   TenantRunner
	Logger   *slog.Logger
	Now      func() time.Time
	// Interval between poll cycles. Defaults to 15s.
	Interval time.Duration
}

// ArgoMonitor continuously mirrors Argo CD application status onto the platform's
// deployment lifecycle. Each cycle it lists the applications the platform manages
// (by the managed-by label selector) directly from Argo CD — the authoritative
// source of truth — and reconciles each one within its own tenant transaction.
//
// Because the tenant identity travels on the Application's labels, the monitor
// enumerates work cross-tenant from Argo CD without ever performing a
// cross-tenant database scan: every database write remains RLS-scoped.
//
// Reconciliation translates Argo CD sync/health/operation status into the
// existing rollout state machine, persists the observed status, and emits
// deployment.sync.* / health.changed / drift.detected engine events on the
// transactional outbox.
type ArgoMonitor struct {
	client   argocd.Client
	argoApps ArgoApplicationStore
	rels     ReleaseStore
	rollouts RolloutStatusStore
	outbox   events.Outbox
	tenant   TenantRunner
	log      *slog.Logger
	now      func() time.Time
	interval time.Duration
	tracer   trace.Tracer
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewArgoMonitor constructs an ArgoMonitor.
func NewArgoMonitor(d ArgoMonitorDeps) *ArgoMonitor {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Interval <= 0 {
		d.Interval = 15 * time.Second
	}
	return &ArgoMonitor{
		client:   d.Client,
		argoApps: d.ArgoApps,
		rels:     d.Releases,
		rollouts: d.Rollouts,
		outbox:   d.Outbox,
		tenant:   d.Tenant,
		log:      d.Logger,
		now:      d.Now,
		interval: d.Interval,
		tracer:   telemetry.Tracer("deployment-gitops-monitor"),
		done:     make(chan struct{}),
	}
}

// Start launches the monitor loop in the background. It returns immediately.
func (m *ArgoMonitor) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	go m.run(runCtx)
}

// Stop halts the monitor loop and waits for the in-flight cycle to finish.
func (m *ArgoMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	<-m.done
}

func (m *ArgoMonitor) run(ctx context.Context) {
	defer close(m.done)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Run one cycle promptly on startup.
	m.cycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.cycle(ctx)
		}
	}
}

// cycle lists managed applications from Argo CD and reconciles each one.
func (m *ArgoMonitor) cycle(ctx context.Context) {
	ctx, span := m.tracer.Start(ctx, "gitops.monitor.cycle")
	defer span.End()

	apps, err := m.client.ListApplications(ctx, argocd.ListOptions{
		Selector: LabelManagedBy + "=" + ManagedByValue,
	})
	if err != nil {
		span.RecordError(err)
		m.log.WarnContext(ctx, "gitops monitor: list applications failed", "error", err)
		return
	}
	span.SetAttributes(attribute.Int("argocd.applications.count", len(apps)))

	for i := range apps {
		app := apps[i]
		orgID := app.Metadata.Labels[LabelOrgID]
		deploymentID := app.Metadata.Labels[LabelDeploymentID]
		if orgID == "" || deploymentID == "" {
			continue // not one of ours, or missing identity labels
		}
		if err := m.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
			return m.reconcile(ctx, orgID, deploymentID, &app)
		}); err != nil {
			m.log.ErrorContext(ctx, "gitops monitor: reconcile failed",
				"deployment_id", deploymentID, "application", app.Metadata.Name, "error", err)
		}
	}
}

// reconcile mirrors one Argo CD application's status into the deployment model
// and emits lifecycle events for status transitions. Runs inside a tenant tx.
func (m *ArgoMonitor) reconcile(ctx context.Context, orgID, deploymentID string, app *argocd.Application) error {
	ctx, span := m.tracer.Start(ctx, "gitops.monitor.reconcile")
	defer span.End()
	span.SetAttributes(
		attribute.String("deployment.id", deploymentID),
		attribute.String("argocd.application", app.Metadata.Name),
		attribute.String("argocd.sync.status", app.Status.Sync.Status),
		attribute.String("argocd.health.status", app.Status.Health.Status),
	)

	record, err := m.argoApps.Get(ctx, deploymentID)
	if err != nil {
		if database.IsNotFound(err) {
			return nil // application exists in Argo CD but not tracked here
		}
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Snapshot the previously observed status for transition detection. When the
	// record has never been observed, use empty "from" values so first-sight
	// gauges only increment.
	firstObservation := record.ObservedAt == nil
	prevSync := record.SyncStatus
	prevHealth := record.HealthStatus
	prevOp := record.OperationPhase
	prevDrift := record.Drift
	if firstObservation {
		prevSync, prevHealth, prevOp = "", "", ""
	}

	record.applyObserved(app)

	if err := m.argoApps.UpdateObserved(ctx, record); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	if err := m.persistRolloutPhase(ctx, orgID, record); err != nil {
		return err
	}

	return m.emitTransitions(ctx, orgID, deploymentID, app, record,
		transitionSnapshot{sync: prevSync, health: prevHealth, op: prevOp, drift: prevDrift})
}

// transitionSnapshot holds the pre-reconcile observed values.
type transitionSnapshot struct {
	sync   string
	health string
	op     string
	drift  bool
}

// emitTransitions publishes engine events for the status changes between the
// previous and current observations.
func (m *ArgoMonitor) emitTransitions(ctx context.Context, orgID, deploymentID string, app *argocd.Application, record *ArgoApplication, prev transitionSnapshot) error {
	// --- Sync operation lifecycle (started / completed / failed) ---
	if record.OperationPhase != prev.op {
		switch {
		case record.OperationPhase == argocd.OperationRunning:
			deploymentSyncTotal.WithLabelValues(syncOutcomeStarted).Inc()
			if err := m.emit(ctx, orgID, EventDeploymentSyncStarted, deploymentID, deploymentSyncStartedPayload{
				DeploymentID: deploymentID,
				Application:  record.AppName,
				Cluster:      record.DestServer,
				Namespace:    record.DestNamespace,
				Revision:     app.Status.Sync.Revision,
			}); err != nil {
				return err
			}
			m.log.InfoContext(ctx, "gitops sync started", "deployment_id", deploymentID, "application", record.AppName)

		case isSyncSuccess(record.OperationPhase):
			deploymentSyncTotal.WithLabelValues(syncOutcomeCompleted).Inc()
			if d := operationDuration(app); d > 0 {
				deploymentSyncDuration.Observe(d)
			}
			if err := m.emit(ctx, orgID, EventDeploymentSyncCompleted, deploymentID, deploymentSyncCompletedPayload{
				DeploymentID: deploymentID,
				Application:  record.AppName,
				Revision:     record.SyncedRevision,
				SyncStatus:   record.SyncStatus,
				HealthStatus: record.HealthStatus,
			}); err != nil {
				return err
			}
			m.log.InfoContext(ctx, "gitops sync completed",
				"deployment_id", deploymentID, "application", record.AppName, "revision", record.SyncedRevision)

		case isSyncFailure(record.OperationPhase):
			deploymentSyncTotal.WithLabelValues(syncOutcomeFailed).Inc()
			deploymentSyncFailuresTotal.Inc()
			msg := ""
			if app.Status.OperationState != nil {
				msg = app.Status.OperationState.Message
			}
			if err := m.emit(ctx, orgID, EventDeploymentSyncFailed, deploymentID, deploymentSyncFailedPayload{
				DeploymentID: deploymentID,
				Application:  record.AppName,
				Revision:     app.Status.Sync.Revision,
				Phase:        record.OperationPhase,
				ErrorMessage: msg,
			}); err != nil {
				return err
			}
			m.log.WarnContext(ctx, "gitops sync failed",
				"deployment_id", deploymentID, "application", record.AppName, "phase", record.OperationPhase, "error", msg)
		}
	}

	// --- Health transition ---
	if record.HealthStatus != prev.health {
		recordHealthGauge(prev.health, record.HealthStatus)
		if err := m.emit(ctx, orgID, EventDeploymentHealthChanged, deploymentID, deploymentHealthChangedPayload{
			DeploymentID: deploymentID,
			Application:  record.AppName,
			From:         prev.health,
			To:           record.HealthStatus,
		}); err != nil {
			return err
		}
		m.log.InfoContext(ctx, "gitops health changed",
			"deployment_id", deploymentID, "application", record.AppName, "from", prev.health, "to", record.HealthStatus)
	}

	// --- Drift newly detected ---
	if record.Drift && !prev.drift {
		deploymentDriftTotal.Inc()
		if err := m.emit(ctx, orgID, EventDeploymentDriftDetected, deploymentID, deploymentDriftDetectedPayload{
			DeploymentID: deploymentID,
			Application:  record.AppName,
			SyncStatus:   record.SyncStatus,
			HealthStatus: record.HealthStatus,
		}); err != nil {
			return err
		}
		m.log.WarnContext(ctx, "gitops drift detected",
			"deployment_id", deploymentID, "application", record.AppName,
			"sync_status", record.SyncStatus, "health_status", record.HealthStatus)
	}

	return nil
}

// persistRolloutPhase maps Argo CD status onto the rollout state machine and
// upserts the rollout_status row for the deployment's latest release.
func (m *ArgoMonitor) persistRolloutPhase(ctx context.Context, orgID string, record *ArgoApplication) error {
	if m.rollouts == nil || m.rels == nil {
		return nil
	}
	rel, err := m.rels.GetLatest(ctx, record.DeploymentID)
	if err != nil || rel == nil {
		return nil
	}
	phase := argoStatusToRolloutPhase(record.SyncStatus, record.HealthStatus, record.OperationPhase)
	return m.rollouts.Upsert(ctx, &RolloutStatus{
		DeploymentID: record.DeploymentID,
		ReleaseID:    rel.ID,
		OrgID:        orgID,
		Phase:        phase,
		Revision:     rel.Revision,
		Image:        rel.Image,
	})
}

// emit enqueues an engine event onto the transactional outbox. Must run inside a
// tenant transaction.
func (m *ArgoMonitor) emit(ctx context.Context, orgID, eventType, deploymentID string, payload any) error {
	if m.outbox == nil {
		return nil
	}
	evt, err := events.New(eventType, eventVersion, orgID, payload,
		events.WithActor(events.Actor{Type: "system", ID: "deployment-gitops-monitor"}),
		events.WithResource(events.Resource{Type: "deployment", ID: deploymentID}))
	if err != nil {
		return err
	}
	return m.outbox.Enqueue(ctx, evt)
}

// operationDuration returns the wall-clock seconds of the last sync operation,
// or 0 when the timestamps are unavailable/unparseable.
func operationDuration(app *argocd.Application) float64 {
	if app.Status.OperationState == nil {
		return 0
	}
	start, err1 := time.Parse(time.RFC3339, app.Status.OperationState.StartedAt)
	finish, err2 := time.Parse(time.RFC3339, app.Status.OperationState.FinishedAt)
	if err1 != nil || err2 != nil || finish.Before(start) {
		return 0
	}
	return finish.Sub(start).Seconds()
}
