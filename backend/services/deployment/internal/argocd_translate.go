package deployment

import (
	"github.com/bdsplatform/platform/backend/libs/argocd"
)

// buildArgoApplication generates the Argo CD Application custom resource for a
// GitOps binding. It is a pure function of the persisted ArgoApplication record
// so it is trivially testable and deterministic.
//
// The generated Application carries platform labels (managed-by + org/deployment
// identity) so the monitor can list and re-associate applications with their
// tenant, and the Argo CD resources finalizer so deleting the Application
// cascades to its managed resources.
func buildArgoApplication(a *ArgoApplication) *argocd.Application {
	source := &argocd.ApplicationSource{
		RepoURL:        a.RepoURL,
		Path:           a.Path,
		TargetRevision: a.TargetRevision,
	}
	switch a.SourceType {
	case SourceTypeHelm:
		source.Helm = &argocd.HelmSource{}
	case SourceTypeKustomize:
		source.Kustomize = &argocd.KustomizeSource{}
	default:
		source.Directory = &argocd.DirectorySource{Recurse: true}
	}

	var syncPolicy *argocd.SyncPolicy
	if a.AutoSync {
		syncPolicy = &argocd.SyncPolicy{
			Automated: &argocd.SyncPolicyAutomated{
				Prune:    a.Prune,
				SelfHeal: a.SelfHeal,
			},
			// CreateNamespace makes the destination namespace self-provisioning;
			// ApplyOutOfSyncOnly bounds each sync to the resources that drifted.
			SyncOptions: []string{"CreateNamespace=true", "ApplyOutOfSyncOnly=true"},
			Retry: &argocd.RetryStrategy{
				Limit: 5,
				Backoff: &argocd.RetryBackoff{
					Duration:    "5s",
					MaxDuration: "3m",
				},
			},
		}
	}

	revisionHistoryLimit := int64(10)

	return &argocd.Application{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Application",
		Metadata: argocd.ObjectMeta{
			Name: a.AppName,
			Labels: map[string]string{
				LabelManagedBy:    ManagedByValue,
				LabelOrgID:        a.OrgID,
				LabelDeploymentID: a.DeploymentID,
			},
			Annotations: map[string]string{
				AnnotationDeployment: a.DeploymentID,
			},
			// Cascade-delete managed resources when the Application is deleted.
			Finalizers: []string{argocd.ResourcesFinalizer},
		},
		Spec: argocd.ApplicationSpec{
			Project:     a.Project,
			Source:      source,
			Destination: argocd.ApplicationDest{Server: a.DestServer, Namespace: a.DestNamespace},
			SyncPolicy:  syncPolicy,
			// Ignore replica drift so an autoscaler adjusting replicas does not
			// register as configuration drift.
			IgnoreDifferences: []argocd.ResourceIgnoreDiff{
				{Group: "apps", Kind: "Deployment", JSONPointers: []string{"/spec/replicas"}},
			},
			RevisionHistoryLimit: &revisionHistoryLimit,
		},
	}
}

// argoStatusToRolloutPhase maps an Argo CD Application's sync + health + operation
// state onto the platform's existing rollout state machine phase. This lets the
// GitOps engine reuse the rollout_status model and downstream consumers unchanged.
//
//	Missing / no operation yet          -> Pending
//	Operation running                   -> RollingOut
//	OutOfSync (auto-sync pending)        -> Reconciling
//	Degraded / operation failed          -> Failed
//	Synced + Healthy                     -> Healthy
//	Progressing                          -> RollingOut
//	otherwise                            -> Reconciling
func argoStatusToRolloutPhase(sync, health, phase string) string {
	switch phase {
	case argocd.OperationRunning, argocd.OperationTerminating:
		return RolloutPhaseRollingOut
	case argocd.OperationFailed, argocd.OperationError:
		return RolloutPhaseFailed
	}

	switch health {
	case argocd.HealthStatusDegraded:
		return RolloutPhaseFailed
	case argocd.HealthStatusMissing, argocd.HealthStatusUnknown:
		if sync == argocd.SyncStatusSynced {
			return RolloutPhaseReconciling
		}
		return RolloutPhasePending
	case argocd.HealthStatusProgressing:
		return RolloutPhaseRollingOut
	case argocd.HealthStatusSuspended:
		return RolloutPhaseReconciling
	case argocd.HealthStatusHealthy:
		if sync == argocd.SyncStatusSynced {
			return RolloutPhaseHealthy
		}
		// Healthy but drifted: live state diverged from git.
		return RolloutPhaseReconciling
	}

	return RolloutPhaseReconciling
}

// isSyncFailure reports whether the operation phase represents a failed sync.
func isSyncFailure(phase string) bool {
	return phase == argocd.OperationFailed || phase == argocd.OperationError
}

// isSyncSuccess reports whether the operation phase represents a succeeded sync.
func isSyncSuccess(phase string) bool { return phase == argocd.OperationSucceeded }
