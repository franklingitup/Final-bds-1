// Package argocd is a small, dependency-light client for the Argo CD API
// server. It models the subset of the Application resource the platform needs
// to drive GitOps deployments (create/update/delete, sync, refresh, rollback,
// wait for sync/health) and talks to Argo CD over its documented JSON REST API
// (/api/v1/applications...).
//
// The package intentionally does NOT import the full argo-cd Go module (which
// pulls in the entire Kubernetes controller runtime). Instead it defines the
// wire types it needs with JSON tags matching Argo CD's API, so it stays light
// and trivially unit-testable with httptest.
package argocd

// Sync status codes reported by Argo CD (application.status.sync.status).
const (
	SyncStatusSynced    = "Synced"
	SyncStatusOutOfSync = "OutOfSync"
	SyncStatusUnknown   = "Unknown"
)

// Health status codes reported by Argo CD (application.status.health.status).
const (
	HealthStatusHealthy     = "Healthy"
	HealthStatusProgressing = "Progressing"
	HealthStatusDegraded    = "Degraded"
	HealthStatusSuspended   = "Suspended"
	HealthStatusMissing     = "Missing"
	HealthStatusUnknown     = "Unknown"
)

// Operation phases reported by Argo CD (application.status.operationState.phase).
const (
	OperationRunning     = "Running"
	OperationSucceeded   = "Succeeded"
	OperationFailed      = "Failed"
	OperationError       = "Error"
	OperationTerminating = "Terminating"
)

// ResourcesFinalizer is Argo CD's finalizer that triggers cascade deletion of
// the application's managed resources when the Application is deleted.
const ResourcesFinalizer = "resources-finalizer.argocd.argoproj.io"

// Application is the Argo CD Application custom resource (subset). It is both
// the create/update request body and the get/list response element.
type Application struct {
	APIVersion string          `json:"apiVersion,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Metadata   ObjectMeta      `json:"metadata"`
	Spec       ApplicationSpec `json:"spec"`
	// Status is populated by Argo CD on read; it is ignored on create/update.
	Status ApplicationStatus `json:"status,omitempty"`
}

// ObjectMeta is the subset of Kubernetes object metadata Argo CD honors on an
// Application.
type ObjectMeta struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
	Finalizers      []string          `json:"finalizers,omitempty"`
	OwnerReferences []OwnerReference  `json:"ownerReferences,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
}

// OwnerReference links the Application back to an owning object.
type OwnerReference struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Controller *bool  `json:"controller,omitempty"`
}

// ApplicationSpec is the desired state of an Argo CD Application.
type ApplicationSpec struct {
	Project           string              `json:"project"`
	Source            *ApplicationSource  `json:"source,omitempty"`
	Destination       ApplicationDest     `json:"destination"`
	SyncPolicy        *SyncPolicy         `json:"syncPolicy,omitempty"`
	IgnoreDifferences []ResourceIgnoreDiff `json:"ignoreDifferences,omitempty"`
	RevisionHistoryLimit *int64           `json:"revisionHistoryLimit,omitempty"`
}

// ApplicationSource describes where the manifests come from.
type ApplicationSource struct {
	RepoURL        string             `json:"repoURL"`
	Path           string             `json:"path,omitempty"`
	TargetRevision string             `json:"targetRevision,omitempty"`
	Helm           *HelmSource        `json:"helm,omitempty"`
	Kustomize      *KustomizeSource   `json:"kustomize,omitempty"`
	Directory      *DirectorySource   `json:"directory,omitempty"`
}

// HelmSource configures a Helm-based source.
type HelmSource struct {
	ValueFiles  []string        `json:"valueFiles,omitempty"`
	Values      string          `json:"values,omitempty"`
	ReleaseName string          `json:"releaseName,omitempty"`
	Parameters  []HelmParameter `json:"parameters,omitempty"`
}

// HelmParameter is a single Helm --set parameter.
type HelmParameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// KustomizeSource configures a Kustomize-based source.
type KustomizeSource struct {
	NamePrefix string            `json:"namePrefix,omitempty"`
	NameSuffix string            `json:"nameSuffix,omitempty"`
	Images     []string          `json:"images,omitempty"`
	CommonLabels map[string]string `json:"commonLabels,omitempty"`
}

// DirectorySource configures a plain-directory source.
type DirectorySource struct {
	Recurse bool `json:"recurse,omitempty"`
}

// ApplicationDest is the target cluster/namespace.
type ApplicationDest struct {
	Server    string `json:"server,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace"`
}

// SyncPolicy controls automated syncing.
type SyncPolicy struct {
	Automated   *SyncPolicyAutomated `json:"automated,omitempty"`
	SyncOptions []string             `json:"syncOptions,omitempty"`
	Retry       *RetryStrategy       `json:"retry,omitempty"`
}

// SyncPolicyAutomated enables automated sync with optional self-heal/prune.
type SyncPolicyAutomated struct {
	Prune    bool `json:"prune"`
	SelfHeal bool `json:"selfHeal"`
}

// RetryStrategy controls automated sync retries.
type RetryStrategy struct {
	Limit   int64        `json:"limit,omitempty"`
	Backoff *RetryBackoff `json:"backoff,omitempty"`
}

// RetryBackoff configures exponential backoff for sync retries.
type RetryBackoff struct {
	Duration    string `json:"duration,omitempty"`
	Factor      *int64 `json:"factor,omitempty"`
	MaxDuration string `json:"maxDuration,omitempty"`
}

// ResourceIgnoreDiff declares fields Argo CD should ignore when computing drift.
type ResourceIgnoreDiff struct {
	Group             string   `json:"group,omitempty"`
	Kind              string   `json:"kind"`
	Name              string   `json:"name,omitempty"`
	Namespace         string   `json:"namespace,omitempty"`
	JSONPointers      []string `json:"jsonPointers,omitempty"`
	JQPathExpressions []string `json:"jqPathExpressions,omitempty"`
}

// ApplicationStatus is the observed state Argo CD reports.
type ApplicationStatus struct {
	Sync           SyncStatus       `json:"sync"`
	Health         HealthStatus     `json:"health"`
	OperationState *OperationState  `json:"operationState,omitempty"`
	History        []RevisionHistory `json:"history,omitempty"`
	Conditions     []AppCondition   `json:"conditions,omitempty"`
	Resources      []ResourceStatus `json:"resources,omitempty"`
	ReconciledAt   string           `json:"reconciledAt,omitempty"`
}

// SyncStatus is the sync sub-status.
type SyncStatus struct {
	Status   string `json:"status"`
	Revision string `json:"revision,omitempty"`
}

// HealthStatus is the health sub-status.
type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// OperationState reports the in-flight or last sync operation.
type OperationState struct {
	Phase      string `json:"phase"`
	Message    string `json:"message,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
	SyncResult *SyncOperationResult `json:"syncResult,omitempty"`
}

// SyncOperationResult carries the revision a completed operation synced to.
type SyncOperationResult struct {
	Revision string `json:"revision,omitempty"`
}

// RevisionHistory is one entry in an application's deploy history. The ID is
// what a rollback targets.
type RevisionHistory struct {
	ID         int64  `json:"id"`
	Revision   string `json:"revision"`
	DeployedAt string `json:"deployedAt,omitempty"`
}

// AppCondition is a status condition (e.g. ComparisonError, SyncError).
type AppCondition struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ResourceStatus is the per-resource status inside the application resource tree.
type ResourceStatus struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Status    string `json:"status,omitempty"`
	Health    *HealthStatus `json:"health,omitempty"`
}

// SyncRequest is the body of POST /applications/{name}/sync.
type SyncRequest struct {
	Revision  string          `json:"revision,omitempty"`
	Prune     bool            `json:"prune,omitempty"`
	DryRun    bool            `json:"dryRun,omitempty"`
	Strategy  *SyncStrategy   `json:"strategy,omitempty"`
	Resources []SyncOperationResource `json:"resources,omitempty"`
}

// SyncStrategy selects the apply/hook strategy for a sync.
type SyncStrategy struct {
	Apply *SyncStrategyApply `json:"apply,omitempty"`
	Hook  *SyncStrategyHook  `json:"hook,omitempty"`
}

// SyncStrategyApply configures the apply strategy.
type SyncStrategyApply struct {
	Force bool `json:"force,omitempty"`
}

// SyncStrategyHook configures the hook strategy.
type SyncStrategyHook struct {
	SyncStrategyApply `json:",inline"`
}

// SyncOperationResource targets specific resources in a sync.
type SyncOperationResource struct {
	Group     string `json:"group,omitempty"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// RollbackRequest is the body of POST /applications/{name}/rollback.
type RollbackRequest struct {
	ID     int64 `json:"id"`
	Prune  bool  `json:"prune,omitempty"`
	DryRun bool  `json:"dryRun,omitempty"`
}

// applicationList is the response wrapper for GET /applications.
type applicationList struct {
	Items []Application `json:"items"`
}

// IsSynced reports whether the application's sync status is Synced.
func (a *Application) IsSynced() bool { return a.Status.Sync.Status == SyncStatusSynced }

// IsHealthy reports whether the application's health status is Healthy.
func (a *Application) IsHealthy() bool { return a.Status.Health.Status == HealthStatusHealthy }

// OperationPhase returns the current operation phase, or "" if none.
func (a *Application) OperationPhase() string {
	if a.Status.OperationState == nil {
		return ""
	}
	return a.Status.OperationState.Phase
}

// SyncedRevision returns the revision the application is currently synced to.
func (a *Application) SyncedRevision() string { return a.Status.Sync.Revision }
