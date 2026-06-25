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

	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/k8s"
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
}

// DeploymentClient fetches desired deployments and reports status.
type DeploymentClient interface {
	GetDesiredState(ctx context.Context, creds controlplane.AgentCredentials) ([]controlplane.DesiredDeployment, error)
	ReportDeploymentStatusWithCreds(ctx context.Context, creds controlplane.AgentCredentials, deploymentID, releaseID string, req controlplane.DeploymentStatusRequest) error
}

// ResourceManager manages Kubernetes resources.
type ResourceManager interface {
	ApplyDeployment(ctx context.Context, spec k8s.DeploymentSpec) (*k8s.ApplyResult, error)
	ApplyService(ctx context.Context, spec k8s.DeploymentSpec) (*k8s.ApplyResult, error)
	GetDeploymentStatus(ctx context.Context, name string) (*k8s.DeploymentStatus, error)
	DeleteDeployment(ctx context.Context, name string) error
	ListManagedDeployments(ctx context.Context) ([]string, error)
}

// ReconcilerState tracks applied revisions.
type ReconcilerState struct {
	// AppliedRevisions maps deployment ID to the last applied revision.
	AppliedRevisions map[string]int `json:"appliedRevisions"`
	// ReportedStatuses maps release ID to the last reported status.
	ReportedStatuses map[string]string `json:"reportedStatuses"`
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
		},
		log: log,
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
	r.log.Debug("starting reconciliation cycle")

	// Fetch desired state from Deployment Service using agent credentials.
	desired, err := r.client.GetDesiredState(ctx, r.cfg.AgentCredentials)
	if err != nil {
		r.log.Error("failed to fetch desired deployments", "error", err)
		return
	}

	r.log.Debug("fetched desired deployments", "count", len(desired))

	// Track which deployments we've seen.
	desiredNames := make(map[string]bool)

	// Reconcile each desired deployment.
	for _, d := range desired {
		if d.Status == "deleted" {
			continue // Skip deleted deployments
		}

		spec := k8s.FromDesiredDeployment(d)
		desiredNames[spec.ResourceName()] = true

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

// State returns a copy of the current reconciler state.
func (r *Reconciler) State() ReconcilerState {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()

	state := ReconcilerState{
		AppliedRevisions: make(map[string]int),
		ReportedStatuses: make(map[string]string),
	}
	for k, v := range r.state.AppliedRevisions {
		state.AppliedRevisions[k] = v
	}
	for k, v := range r.state.ReportedStatuses {
		state.ReportedStatuses[k] = v
	}
	return state
}
