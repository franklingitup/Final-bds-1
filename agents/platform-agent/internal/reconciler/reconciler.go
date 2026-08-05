// Package reconciler implements the deployment reconciliation engine.
package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/k8s"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/metrics"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/rollout"
)

// Config holds reconciler configuration.
type Config struct {
	// Interval between reconciliation cycles.
	Interval time.Duration
	// StateFile path for persisting applied revisions.
	StateFile string
	// Namespace to deploy workloads to.
	Namespace string
	// AgentCredentials for authenticating with the Deployment Service.
	AgentCredentials controlplane.AgentCredentials
	// IsLeader gates reconciliation on leadership. When nil (the default, and
	// the behaviour when leader election is disabled) every cycle proceeds. When
	// set, a cycle is skipped whenever it returns false, so followers never
	// fetch desired state, apply resources, or clean up orphans.
	IsLeader func() bool
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		Interval:  30 * time.Second,
		StateFile: "/var/lib/platform-agent/reconciler-state.json",
		Namespace: "default",
	}
}

// Reconciler continuously reconciles desired deployment state into Kubernetes.
type Reconciler struct {
	client  DeploymentClient
	manager ResourceManager
	cfg     Config
	state   *ReconcilerState
	stateMu sync.RWMutex
	log     *slog.Logger
	tracer  trace.Tracer
}

// DeploymentClient fetches desired deployments and reports status.
type DeploymentClient interface {
	GetDesiredState(ctx context.Context, creds controlplane.AgentCredentials) ([]controlplane.DesiredDeployment, error)
	ReportDeploymentStatusWithCreds(ctx context.Context, creds controlplane.AgentCredentials, deploymentID, releaseID string, req controlplane.DeploymentStatusRequest) error
	// ReportDeploymentProgressWithCreds publishes a rich rollout progress
	// snapshot. It is additive: failures (e.g. an older control plane without
	// the endpoint) are logged and never abort reconciliation.
	ReportDeploymentProgressWithCreds(ctx context.Context, creds controlplane.AgentCredentials, deploymentID, releaseID string, req controlplane.DeploymentProgressRequest) error
}

// ResourceManager manages Kubernetes resources.
type ResourceManager interface {
	ApplyDeployment(ctx context.Context, spec k8s.DeploymentSpec) (*k8s.ApplyResult, error)
	ApplyService(ctx context.Context, spec k8s.DeploymentSpec) (*k8s.ApplyResult, error)
	GetDeploymentStatus(ctx context.Context, name string) (*k8s.DeploymentStatus, error)
	GetPodHealth(ctx context.Context, appSlug string) (*k8s.PodHealth, error)
	DeleteDeployment(ctx context.Context, name string) error
	ListManagedDeployments(ctx context.Context) ([]string, error)

	// ConfigMap controller surface (mirrors the Deployment methods).
	ApplyConfigMap(ctx context.Context, spec k8s.ConfigMapSpec) (*k8s.ApplyResult, error)
	DeleteConfigMap(ctx context.Context, name string) error
	ListManagedConfigMaps(ctx context.Context) ([]string, error)

	// PersistentVolumeClaim controller surface (mirrors the ConfigMap methods).
	ApplyPVC(ctx context.Context, spec k8s.PVCSpec) (*k8s.ApplyResult, error)
	DeletePVC(ctx context.Context, name string) error
	ListManagedPVCs(ctx context.Context) ([]string, error)

	// Ingress controller surface (mirrors the ConfigMap methods).
	ApplyIngress(ctx context.Context, spec k8s.IngressSpec) (*k8s.ApplyResult, error)
	DeleteIngress(ctx context.Context, name string) error
	ListManagedIngresses(ctx context.Context) ([]string, error)
}

// ReconcilerState tracks applied revisions.
type ReconcilerState struct {
	// AppliedRevisions maps deployment ID to the last applied revision.
	AppliedRevisions map[string]int `json:"appliedRevisions"`
	// ReportedStatuses maps release ID to the last reported status.
	ReportedStatuses map[string]string `json:"reportedStatuses"`
	// ReportedProgress maps release ID to the last reported rollout progress
	// key ("phase|percentage"). It throttles progress reporting so the agent
	// only POSTs when the phase or percentage actually changed.
	ReportedProgress map[string]string `json:"reportedProgress"`
}

// New creates a new Reconciler.
func New(client DeploymentClient, manager ResourceManager, cfg Config, log *slog.Logger) *Reconciler {
	return &Reconciler{
		client:  client,
		manager: manager,
		cfg:     cfg,
		state: &ReconcilerState{
			AppliedRevisions: make(map[string]int),
			ReportedStatuses: make(map[string]string),
			ReportedProgress: make(map[string]string),
		},
		log:    log,
		tracer: otel.Tracer("platform-agent/reconciler"),
	}
}

// Run starts the reconciliation loop and blocks until context is cancelled.
func (r *Reconciler) Run(ctx context.Context) error {
	// Load persisted state.
	if err := r.loadState(); err != nil {
		r.log.Warn("failed to load reconciler state, starting fresh", "error", err)
	}

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	// Run initial reconciliation.
	r.reconcile(ctx)

	for {
		select {
		case <-ctx.Done():
			r.log.Info("reconciler stopped")
			return ctx.Err()
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}
}

// reconcile performs a single reconciliation cycle.
func (r *Reconciler) reconcile(ctx context.Context) {
	// Leadership gate: when leader election is enabled and this replica is a
	// follower, skip the entire cycle so only one replica reconciles at a time.
	if r.cfg.IsLeader != nil && !r.cfg.IsLeader() {
		metrics.ReconcileSkipped.Inc()
		r.log.Debug("skipping reconciliation cycle: not leader")
		return
	}

	r.log.Debug("starting reconciliation cycle")

	// Fetch desired state from Deployment Service using agent credentials.
	desired, err := r.client.GetDesiredState(ctx, r.cfg.AgentCredentials)
	if err != nil {
		r.log.Error("failed to fetch desired deployments", "error", err)
		return
	}

	r.log.Debug("fetched desired deployments", "count", len(desired))

	// Track which deployments, configmaps, pvcs and ingresses we've seen.
	desiredNames := make(map[string]bool)
	desiredConfigMaps := make(map[string]bool)
	desiredPVCs := make(map[string]bool)
	desiredIngresses := make(map[string]bool)

	// Reconcile each desired deployment.
	for _, d := range desired {
		if d.Status == "deleted" {
			continue // Skip deleted deployments
		}

		spec := k8s.FromDesiredDeployment(d)
		desiredNames[spec.ResourceName()] = true
		for _, cm := range d.ConfigMaps {
			desiredConfigMaps[cm.Name] = true
		}
		for _, pvc := range d.PersistentVolumeClaims {
			desiredPVCs[pvc.Name] = true
		}
		for _, ing := range d.Ingresses {
			desiredIngresses[ing.Name] = true
		}

		if err := r.reconcileDeployment(ctx, d, spec); err != nil {
			r.log.Error("failed to reconcile deployment",
				"deployment_id", d.DeploymentID,
				"application", d.ApplicationName,
				"error", err)
		}
	}

	// Clean up deployments that are no longer desired.
	if err := r.cleanupOrphanedDeployments(ctx, desiredNames); err != nil {
		r.log.Error("failed to cleanup orphaned deployments", "error", err)
	}

	// Clean up configmaps that are no longer desired.
	if err := r.cleanupOrphanedConfigMaps(ctx, desiredConfigMaps); err != nil {
		r.log.Error("failed to cleanup orphaned configmaps", "error", err)
	}

	// Clean up pvcs that are no longer desired.
	if err := r.cleanupOrphanedPVCs(ctx, desiredPVCs); err != nil {
		r.log.Error("failed to cleanup orphaned pvcs", "error", err)
	}

	// Clean up ingresses that are no longer desired.
	if err := r.cleanupOrphanedIngresses(ctx, desiredIngresses); err != nil {
		r.log.Error("failed to cleanup orphaned ingresses", "error", err)
	}

	// Save state after reconciliation.
	if err := r.saveState(); err != nil {
		r.log.Warn("failed to save reconciler state", "error", err)
	}
}

// reconcileDeployment reconciles a single deployment.
func (r *Reconciler) reconcileDeployment(ctx context.Context, desired controlplane.DesiredDeployment, spec k8s.DeploymentSpec) error {
	log := r.log.With(
		"deployment_id", desired.DeploymentID,
		"release_id", desired.ReleaseID,
		"application", desired.ApplicationName,
		"revision", desired.Revision,
	)

	// Check if we need to apply this revision.
	r.stateMu.RLock()
	appliedRev := r.state.AppliedRevisions[desired.DeploymentID]
	reportedStatus := r.state.ReportedStatuses[desired.ReleaseID]
	r.stateMu.RUnlock()

	// Reconcile ConfigMaps and PVCs before the workload so mounted/referenced
	// config and storage exist first. Failures are non-fatal (like Service),
	// matching existing behaviour: they are logged and reconciliation continues.
	r.reconcileConfigMaps(ctx, desired)
	r.reconcilePVCs(ctx, desired)

	// Apply Kubernetes Deployment.
	depResult, err := r.manager.ApplyDeployment(ctx, spec)
	if err != nil {
		// Report failure.
		if reportedStatus != controlplane.StatusFailed {
			r.reportStatus(ctx, desired, controlplane.StatusFailed, 0, err.Error())
		}
		return fmt.Errorf("apply deployment: %w", err)
	}

	// Apply Kubernetes Service.
	_, err = r.manager.ApplyService(ctx, spec)
	if err != nil {
		log.Warn("failed to apply service", "error", err)
		// Continue - deployment can work without service
	}

	// Reconcile Ingresses after the Service, since they route to it. Failures
	// are non-fatal (like Service): logged, reconciliation continues.
	r.reconcileIngresses(ctx, desired)

	// Report deploying if this is a new revision.
	if depResult.Created || depResult.Updated {
		log.Info("applied deployment", "created", depResult.Created, "updated", depResult.Updated)
		if reportedStatus != controlplane.StatusDeploying && reportedStatus != controlplane.StatusSucceeded && reportedStatus != controlplane.StatusFailed {
			r.reportStatus(ctx, desired, controlplane.StatusDeploying, 0, "")
		}
	}

	// Check current status.
	status, err := r.manager.GetDeploymentStatus(ctx, spec.ResourceName())
	if err != nil {
		log.Warn("failed to get deployment status", "error", err)
		return nil
	}

	if status == nil {
		return nil
	}

	// Update applied revision.
	if status.Revision != "" && appliedRev < desired.Revision {
		r.stateMu.Lock()
		r.state.AppliedRevisions[desired.DeploymentID] = desired.Revision
		r.stateMu.Unlock()
	}

	// Publish a rich rollout progress snapshot (state machine phase, replica
	// counts, conditions, percentage). This is additive to the terminal status
	// reporting below and drives the deployment engine's monitoring.
	r.reportProgress(ctx, desired, spec, status)

	// Report status based on Kubernetes state.
	if status.Ready && reportedStatus != controlplane.StatusSucceeded {
		log.Info("deployment succeeded", "ready_replicas", status.ReadyReplicas)
		r.reportStatus(ctx, desired, controlplane.StatusSucceeded, status.ReadyReplicas, "")
	} else if status.Failed && reportedStatus != controlplane.StatusFailed {
		log.Warn("deployment failed", "reason", status.FailureReason, "message", status.FailureMessage)
		r.reportStatus(ctx, desired, controlplane.StatusFailed, 0, status.FailureMessage)
	}

	return nil
}

// reportStatus reports deployment status to the Deployment Service.
func (r *Reconciler) reportStatus(ctx context.Context, d controlplane.DesiredDeployment, status string, readyReplicas int, errorMsg string) {
	req := controlplane.DeploymentStatusRequest{
		Status: status,
	}
	if readyReplicas > 0 {
		req.ReadyReplicas = &readyReplicas
	}
	if errorMsg != "" {
		req.ErrorMessage = &errorMsg
	}

	err := r.client.ReportDeploymentStatusWithCreds(ctx, r.cfg.AgentCredentials, d.DeploymentID, d.ReleaseID, req)
	if err != nil {
		r.log.Warn("failed to report deployment status",
			"deployment_id", d.DeploymentID,
			"status", status,
			"error", err)
		return
	}

	// Update reported status in state.
	r.stateMu.Lock()
	r.state.ReportedStatuses[d.ReleaseID] = status
	r.stateMu.Unlock()

	r.log.Info("reported deployment status",
		"deployment_id", d.DeploymentID,
		"release_id", d.ReleaseID,
		"status", status)
}

// reportProgress derives the rollout phase from the observed Kubernetes state
// (deployment status + pod health), computes a rollout percentage, and reports
// a progress snapshot to the Deployment Service. Reporting is throttled: the
// agent only POSTs when the phase or percentage changed since the last report,
// so a steady rollout does not spam the control plane. Failures are logged and
// never abort reconciliation (backward compatible with older control planes).
func (r *Reconciler) reportProgress(ctx context.Context, d controlplane.DesiredDeployment, spec k8s.DeploymentSpec, status *k8s.DeploymentStatus) {
	// Inspect pod health to fail fast on stuck pods (ImagePullBackOff,
	// CrashLoopBackOff, Unschedulable) rather than waiting for the deadline.
	// Pod inspection is best-effort: an error leaves PodIssue empty.
	var podIssue, podMessage string
	if ph, err := r.manager.GetPodHealth(ctx, spec.ApplicationSlug); err != nil {
		r.log.Debug("pod health inspection failed", "application", d.ApplicationName, "error", err)
	} else if ph != nil {
		podIssue = ph.Issue
		podMessage = ph.Message
	}

	snap := rollout.Snapshot{
		Applied:                  true,
		DesiredReplicas:          int32(status.DesiredReplicas),
		ReadyReplicas:            int32(status.ReadyReplicas),
		UpdatedReplicas:          int32(status.UpdatedReplicas),
		AvailableReplicas:        int32(status.AvailableReplicas),
		UnavailableReplicas:      int32(status.UnavailableReplicas),
		Generation:               status.Generation,
		ObservedGeneration:       status.ObservedGeneration,
		ProgressDeadlineExceeded: status.ProgressDeadlineExceeded,
		ReplicaFailure:           status.ReplicaFailure,
		PodIssue:                 podIssue,
	}

	phase := rollout.DerivePhase(snap)
	pct := rollout.Percentage(snap)

	// Throttle: skip if neither phase nor percentage changed since last report.
	key := fmt.Sprintf("%s|%d", phase, pct)
	r.stateMu.RLock()
	last := r.state.ReportedProgress[d.ReleaseID]
	r.stateMu.RUnlock()
	if last == key {
		return
	}

	// Build the error message from the most specific signal available.
	errMsg := ""
	if phase == rollout.PhaseFailed {
		switch {
		case podMessage != "":
			errMsg = podMessage
		case status.FailureMessage != "":
			errMsg = status.FailureMessage
		case podIssue != "":
			errMsg = podIssue
		}
	}

	conditions := make([]controlplane.DeploymentConditionDTO, 0, len(status.Conditions))
	for _, c := range status.Conditions {
		conditions = append(conditions, controlplane.DeploymentConditionDTO{
			Type:    c.Type,
			Status:  c.Status,
			Reason:  c.Reason,
			Message: c.Message,
		})
	}

	req := controlplane.DeploymentProgressRequest{
		Phase:               phase.String(),
		Revision:            d.Revision,
		Image:               status.Image,
		RolloutPercentage:   pct,
		Timeout:             rollout.IsTimeout(snap),
		DesiredReplicas:     status.DesiredReplicas,
		ReadyReplicas:       status.ReadyReplicas,
		UpdatedReplicas:     status.UpdatedReplicas,
		AvailableReplicas:   status.AvailableReplicas,
		UnavailableReplicas: status.UnavailableReplicas,
		ObservedGeneration:  status.ObservedGeneration,
		Conditions:          conditions,
		ErrorMessage:        errMsg,
	}

	if err := r.client.ReportDeploymentProgressWithCreds(ctx, r.cfg.AgentCredentials, d.DeploymentID, d.ReleaseID, req); err != nil {
		r.log.Warn("failed to report deployment progress",
			"deployment_id", d.DeploymentID,
			"release_id", d.ReleaseID,
			"phase", phase,
			"error", err)
		return
	}

	r.stateMu.Lock()
	r.state.ReportedProgress[d.ReleaseID] = key
	r.stateMu.Unlock()

	r.log.Info("reported deployment progress",
		"deployment_id", d.DeploymentID,
		"release_id", d.ReleaseID,
		"phase", phase,
		"rollout_percentage", pct,
		"ready_replicas", status.ReadyReplicas,
		"desired_replicas", status.DesiredReplicas)
}

// cleanupOrphanedDeployments removes deployments that are no longer desired.
func (r *Reconciler) cleanupOrphanedDeployments(ctx context.Context, desiredNames map[string]bool) error {
	managed, err := r.manager.ListManagedDeployments(ctx)
	if err != nil {
		return fmt.Errorf("list managed deployments: %w", err)
	}

	for _, name := range managed {
		if !desiredNames[name] {
			r.log.Info("deleting orphaned deployment", "name", name)
			if err := r.manager.DeleteDeployment(ctx, name); err != nil {
				r.log.Warn("failed to delete orphaned deployment", "name", name, "error", err)
			}
		}
	}

	return nil
}

// reconcileConfigMaps reconciles all ConfigMaps declared by a desired
// deployment. Each ConfigMap gets its own trace span. Failures are logged and
// do not abort the deployment reconciliation.
func (r *Reconciler) reconcileConfigMaps(ctx context.Context, desired controlplane.DesiredDeployment) {
	for _, cm := range desired.ConfigMaps {
		r.reconcileConfigMap(ctx, desired, cm)
	}
}

// reconcileConfigMap reconciles a single ConfigMap: create, drift-correcting
// update, or no-op. It is fully instrumented (span, metrics, structured logs).
func (r *Reconciler) reconcileConfigMap(ctx context.Context, desired controlplane.DesiredDeployment, cm controlplane.DesiredConfigMap) {
	ctx, span := r.tracer.Start(ctx, "reconcile.configmap",
		trace.WithAttributes(
			attribute.String("configmap.name", cm.Name),
			attribute.String("deployment.id", desired.DeploymentID),
			attribute.String("application", desired.ApplicationName),
		),
	)
	defer span.End()

	log := r.log.With(
		"configmap", cm.Name,
		"deployment_id", desired.DeploymentID,
		"application", desired.ApplicationName,
	)

	if cm.Name == "" {
		log.Warn("skipping configmap with empty name: reconcile skipped")
		span.AddEvent("reconcile_skipped")
		return
	}

	spec := k8s.FromDesiredConfigMap(desired, cm)

	start := time.Now()
	result, err := r.manager.ApplyConfigMap(ctx, spec)
	metrics.ConfigMapApplyDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		log.Error("configmap apply failed", "error", err)
		span.RecordError(err)
		return
	}

	switch {
	case result.Created:
		metrics.ConfigMapApply.Inc()
		log.Info("created configmap")
		span.AddEvent("created")
	case result.Updated:
		metrics.ConfigMapApply.Inc()
		metrics.ConfigMapDrift.Inc()
		log.Info("updated configmap: drift detected")
		span.AddEvent("drift_detected")
	default:
		log.Debug("configmap up to date: reconcile skipped")
		span.AddEvent("noop")
	}
}

// cleanupOrphanedConfigMaps removes platform-owned ConfigMaps that are no longer
// desired. It reuses the same orphan-cleanup pattern as deployments and, via the
// manager's ownership guard, never deletes user-owned ConfigMaps.
func (r *Reconciler) cleanupOrphanedConfigMaps(ctx context.Context, desiredConfigMaps map[string]bool) error {
	managed, err := r.manager.ListManagedConfigMaps(ctx)
	if err != nil {
		return fmt.Errorf("list managed configmaps: %w", err)
	}

	for _, name := range managed {
		if !desiredConfigMaps[name] {
			r.log.Info("deleting orphaned configmap", "name", name)
			if err := r.manager.DeleteConfigMap(ctx, name); err != nil {
				r.log.Warn("failed to delete orphaned configmap", "name", name, "error", err)
				continue
			}
			metrics.ConfigMapDelete.Inc()
		}
	}

	return nil
}

// reconcilePVCs reconciles all PersistentVolumeClaims declared by a desired
// deployment. Each PVC gets its own trace span. Failures are logged and do not
// abort the deployment reconciliation.
func (r *Reconciler) reconcilePVCs(ctx context.Context, desired controlplane.DesiredDeployment) {
	for _, pvc := range desired.PersistentVolumeClaims {
		r.reconcilePVC(ctx, desired, pvc)
	}
}

// reconcilePVC reconciles a single PVC: create, legal drift-correcting update
// (metadata / storage expansion), no-op, or immutable-change skip. Fully
// instrumented (span, metrics, structured logs).
func (r *Reconciler) reconcilePVC(ctx context.Context, desired controlplane.DesiredDeployment, pvc controlplane.DesiredPVC) {
	ctx, span := r.tracer.Start(ctx, "reconcile.pvc",
		trace.WithAttributes(
			attribute.String("pvc.name", pvc.Name),
			attribute.String("deployment.id", desired.DeploymentID),
			attribute.String("application", desired.ApplicationName),
		),
	)
	defer span.End()

	log := r.log.With(
		"pvc", pvc.Name,
		"deployment_id", desired.DeploymentID,
		"application", desired.ApplicationName,
	)

	if pvc.Name == "" {
		log.Warn("PVC skipped: empty name")
		span.AddEvent("reconcile_skipped")
		return
	}

	spec, err := k8s.FromDesiredPVC(desired, pvc)
	if err != nil {
		log.Error("PVC apply failed: invalid spec", "error", err)
		span.RecordError(err)
		return
	}

	start := time.Now()
	result, err := r.manager.ApplyPVC(ctx, spec)
	metrics.PVCApplyDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		log.Error("PVC apply failed", "error", err)
		span.RecordError(err)
		return
	}

	switch {
	case result.Created:
		metrics.PVCApply.Inc()
		log.Info("PVC created")
		span.AddEvent("created")
	case result.Updated:
		metrics.PVCApply.Inc()
		metrics.PVCDrift.Inc()
		log.Info("PVC updated: drift detected")
		span.AddEvent("drift_detected")
	case result.ImmutableSkipped:
		metrics.PVCImmutableChange.Inc()
		log.Warn("PVC skipped: immutable field changed")
		span.AddEvent("immutable_change_skipped")
	default:
		log.Debug("PVC up to date: reconcile skipped")
		span.AddEvent("noop")
	}
}

// cleanupOrphanedPVCs removes platform-owned PVCs that are no longer desired. It
// reuses the same orphan-cleanup pattern and, via the manager's ownership guard,
// never deletes user-owned PVCs.
func (r *Reconciler) cleanupOrphanedPVCs(ctx context.Context, desiredPVCs map[string]bool) error {
	managed, err := r.manager.ListManagedPVCs(ctx)
	if err != nil {
		return fmt.Errorf("list managed pvcs: %w", err)
	}

	for _, name := range managed {
		if !desiredPVCs[name] {
			r.log.Info("PVC deleted: orphaned", "name", name)
			if err := r.manager.DeletePVC(ctx, name); err != nil {
				r.log.Warn("failed to delete orphaned pvc", "name", name, "error", err)
				continue
			}
			metrics.PVCDelete.Inc()
		}
	}

	return nil
}

// reconcileIngresses reconciles all Ingresses declared by a desired deployment.
// Each Ingress gets its own trace span. Failures are logged and do not abort the
// deployment reconciliation.
func (r *Reconciler) reconcileIngresses(ctx context.Context, desired controlplane.DesiredDeployment) {
	for _, ing := range desired.Ingresses {
		r.reconcileIngress(ctx, desired, ing)
	}
}

// reconcileIngress reconciles a single Ingress: create, drift-correcting update,
// or no-op. Fully instrumented (span, metrics, structured logs).
func (r *Reconciler) reconcileIngress(ctx context.Context, desired controlplane.DesiredDeployment, ing controlplane.DesiredIngress) {
	ctx, span := r.tracer.Start(ctx, "reconcile.ingress",
		trace.WithAttributes(
			attribute.String("ingress.name", ing.Name),
			attribute.String("deployment.id", desired.DeploymentID),
			attribute.String("application", desired.ApplicationName),
		),
	)
	defer span.End()

	log := r.log.With(
		"ingress", ing.Name,
		"deployment_id", desired.DeploymentID,
		"application", desired.ApplicationName,
	)

	if ing.Name == "" {
		log.Warn("Ingress skipped: empty name")
		span.AddEvent("reconcile_skipped")
		return
	}

	spec := k8s.FromDesiredIngress(desired, ing)

	start := time.Now()
	result, err := r.manager.ApplyIngress(ctx, spec)
	metrics.IngressApplyDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		log.Error("Ingress apply failed", "error", err)
		span.RecordError(err)
		return
	}

	switch {
	case result.Created:
		metrics.IngressApply.Inc()
		log.Info("Ingress created")
		span.AddEvent("created")
	case result.Updated:
		metrics.IngressApply.Inc()
		metrics.IngressDrift.Inc()
		log.Info("Ingress updated: drift detected")
		span.AddEvent("drift_detected")
	default:
		log.Debug("Ingress up to date: reconcile skipped")
		span.AddEvent("noop")
	}
}

// cleanupOrphanedIngresses removes platform-owned Ingresses that are no longer
// desired. It reuses the same orphan-cleanup pattern and, via the manager's
// ownership guard, never deletes user-owned Ingresses.
func (r *Reconciler) cleanupOrphanedIngresses(ctx context.Context, desiredIngresses map[string]bool) error {
	managed, err := r.manager.ListManagedIngresses(ctx)
	if err != nil {
		return fmt.Errorf("list managed ingresses: %w", err)
	}

	for _, name := range managed {
		if !desiredIngresses[name] {
			r.log.Info("Ingress deleted: orphaned", "name", name)
			if err := r.manager.DeleteIngress(ctx, name); err != nil {
				r.log.Warn("failed to delete orphaned ingress", "name", name, "error", err)
				continue
			}
			metrics.IngressDelete.Inc()
		}
	}

	return nil
}

// loadState loads the reconciler state from disk.
func (r *Reconciler) loadState() error {
	data, err := os.ReadFile(r.cfg.StateFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	r.stateMu.Lock()
	defer r.stateMu.Unlock()

	if err := json.Unmarshal(data, r.state); err != nil {
		return err
	}

	if r.state.AppliedRevisions == nil {
		r.state.AppliedRevisions = make(map[string]int)
	}
	if r.state.ReportedStatuses == nil {
		r.state.ReportedStatuses = make(map[string]string)
	}
	if r.state.ReportedProgress == nil {
		r.state.ReportedProgress = make(map[string]string)
	}

	return nil
}

// saveState saves the reconciler state to disk.
func (r *Reconciler) saveState() error {
	r.stateMu.RLock()
	data, err := json.MarshalIndent(r.state, "", "  ")
	r.stateMu.RUnlock()

	if err != nil {
		return err
	}

	return os.WriteFile(r.cfg.StateFile, data, 0600)
}

// Flush persists the current reconciler state to disk. It is called during
// graceful shutdown so that the last applied/reported status survives a
// leadership handover or process exit.
func (r *Reconciler) Flush() error {
	return r.saveState()
}

// State returns a copy of the current reconciler state.
func (r *Reconciler) State() ReconcilerState {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()

	state := ReconcilerState{
		AppliedRevisions: make(map[string]int),
		ReportedStatuses: make(map[string]string),
		ReportedProgress: make(map[string]string),
	}
	for k, v := range r.state.AppliedRevisions {
		state.AppliedRevisions[k] = v
	}
	for k, v := range r.state.ReportedStatuses {
		state.ReportedStatuses[k] = v
	}
	for k, v := range r.state.ReportedProgress {
		state.ReportedProgress[k] = v
	}
	return state
}
