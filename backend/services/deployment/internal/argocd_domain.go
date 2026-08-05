package deployment

import (
	"time"

	"github.com/bdsplatform/platform/backend/libs/argocd"
)

// GitOps source types supported by the deployment engine. They map 1:1 to Argo
// CD source tools.
const (
	SourceTypeDirectory = "directory"
	SourceTypeHelm      = "helm"
	SourceTypeKustomize = "kustomize"
)

// ValidSourceType reports whether s is a supported GitOps source type.
func ValidSourceType(s string) bool {
	switch s {
	case SourceTypeDirectory, SourceTypeHelm, SourceTypeKustomize:
		return true
	default:
		return false
	}
}

// Managed-by label value + key used on every Argo CD Application the platform
// creates. The monitor lists applications by this selector, and the labels
// carry the org/deployment identity back so status can be persisted
// tenant-scoped without a cross-tenant database scan.
const (
	LabelManagedBy    = "app.kubernetes.io/managed-by"
	ManagedByValue    = "bdsplatform"
	LabelOrgID        = "bdsplatform.io/org-id"
	LabelDeploymentID = "bdsplatform.io/deployment-id"
	AnnotationDeployment = "bdsplatform.io/deployment"
)

// ArgoApplication is the GitOps binding for a deployment: the desired Argo CD
// source/destination/policy plus the last observed Argo CD status. It lives
// alongside (never replaces) the deployments/releases rows.
type ArgoApplication struct {
	DeploymentID string `db:"deployment_id"`
	OrgID        string `db:"org_id"`

	AppName string `db:"app_name"`
	Project string `db:"project"`

	RepoURL        string `db:"repo_url"`
	Path           string `db:"path"`
	TargetRevision string `db:"target_revision"`
	SourceType     string `db:"source_type"`

	DestServer    string `db:"dest_server"`
	DestNamespace string `db:"dest_namespace"`

	AutoSync bool `db:"auto_sync"`
	SelfHeal bool `db:"self_heal"`
	Prune    bool `db:"prune"`

	SyncStatus     string     `db:"sync_status"`
	HealthStatus   string     `db:"health_status"`
	OperationPhase string     `db:"operation_phase"`
	SyncedRevision string     `db:"synced_revision"`
	Drift          bool       `db:"drift"`
	ObservedAt     *time.Time `db:"observed_at"`

	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// GitOpsSource is the additive, optional GitOps configuration a caller supplies
// when registering an Argo CD Application for a deployment. It is deliberately
// separate from CreateDeploymentRequest so the image-based deployment API is
// unchanged.
type GitOpsSource struct {
	RepoURL        string `json:"repoUrl"`
	Path           string `json:"path,omitempty"`
	TargetRevision string `json:"targetRevision,omitempty"`
	SourceType     string `json:"sourceType,omitempty"`
	Project        string `json:"project,omitempty"`
	DestServer     string `json:"destServer,omitempty"`
	DestNamespace  string `json:"destNamespace,omitempty"`
	// AutoSync/SelfHeal/Prune default to enabled; pointers distinguish
	// "unset" (use default) from an explicit false.
	AutoSync *bool `json:"autoSync,omitempty"`
	SelfHeal *bool `json:"selfHeal,omitempty"`
	Prune    *bool `json:"prune,omitempty"`
}

// ArgoSyncRequest is the body of the user-facing sync endpoint.
type ArgoSyncRequest struct {
	// Revision optionally overrides the target revision for this sync.
	Revision string `json:"revision,omitempty"`
	// Prune optionally overrides the app's prune policy for this sync.
	Prune *bool `json:"prune,omitempty"`
}

// ArgoRollbackRequest is the body of the user-facing Argo rollback endpoint.
type ArgoRollbackRequest struct {
	// Revision is the git revision (SHA/ref) to roll back to. Required unless
	// HistoryID is set.
	Revision string `json:"revision,omitempty"`
	// HistoryID targets a specific Argo CD deploy-history entry directly.
	HistoryID *int64 `json:"historyId,omitempty"`
}

// ArgoApplicationView is the API representation of an Argo application binding.
type ArgoApplicationView struct {
	DeploymentID   string     `json:"deploymentId"`
	Application    string     `json:"application"`
	Project        string     `json:"project"`
	RepoURL        string     `json:"repoUrl"`
	Path           string     `json:"path"`
	TargetRevision string     `json:"targetRevision"`
	SourceType     string     `json:"sourceType"`
	DestServer     string     `json:"destServer"`
	DestNamespace  string     `json:"destNamespace"`
	AutoSync       bool       `json:"autoSync"`
	SelfHeal       bool       `json:"selfHeal"`
	Prune          bool       `json:"prune"`
	SyncStatus     string     `json:"syncStatus"`
	HealthStatus   string     `json:"healthStatus"`
	OperationPhase string     `json:"operationPhase,omitempty"`
	SyncedRevision string     `json:"syncedRevision,omitempty"`
	Drift          bool       `json:"drift"`
	ObservedAt     *time.Time `json:"observedAt,omitempty"`
}

func toArgoApplicationView(a *ArgoApplication) ArgoApplicationView {
	return ArgoApplicationView{
		DeploymentID:   a.DeploymentID,
		Application:    a.AppName,
		Project:        a.Project,
		RepoURL:        a.RepoURL,
		Path:           a.Path,
		TargetRevision: a.TargetRevision,
		SourceType:     a.SourceType,
		DestServer:     a.DestServer,
		DestNamespace:  a.DestNamespace,
		AutoSync:       a.AutoSync,
		SelfHeal:       a.SelfHeal,
		Prune:          a.Prune,
		SyncStatus:     a.SyncStatus,
		HealthStatus:   a.HealthStatus,
		OperationPhase: a.OperationPhase,
		SyncedRevision: a.SyncedRevision,
		Drift:          a.Drift,
		ObservedAt:     a.ObservedAt,
	}
}

// argoStatusFrom copies the observed fields of an Argo CD Application into the
// persisted record. It returns whether any observed field changed.
func (a *ArgoApplication) applyObserved(app *argocd.Application) (changed bool) {
	drift := app.Status.Sync.Status == argocd.SyncStatusOutOfSync ||
		app.Status.Health.Status == argocd.HealthStatusDegraded ||
		app.Status.Health.Status == argocd.HealthStatusMissing

	next := struct {
		sync, health, phase, rev string
		drift                    bool
	}{
		sync:   app.Status.Sync.Status,
		health: app.Status.Health.Status,
		phase:  app.OperationPhase(),
		rev:    app.SyncedRevision(),
		drift:  drift,
	}

	if a.SyncStatus != next.sync || a.HealthStatus != next.health ||
		a.OperationPhase != next.phase || a.SyncedRevision != next.rev || a.Drift != next.drift {
		changed = true
	}

	a.SyncStatus = next.sync
	a.HealthStatus = next.health
	a.OperationPhase = next.phase
	a.SyncedRevision = next.rev
	a.Drift = next.drift
	return changed
}
