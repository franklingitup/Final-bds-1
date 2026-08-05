// Package rollout implements the deployment rollout state machine.
//
// It is a pure, side-effect-free derivation of a rollout Phase (and rollout
// percentage) from an observed Snapshot of a Kubernetes Deployment plus
// pod-level health. It contains no Kubernetes client code and no I/O, which
// makes the state machine exhaustively unit-testable with fabricated inputs
// (progressing, healthy, timeout, crash-loop, image-pull, unschedulable, ...).
//
// The reconciler feeds it a Snapshot built from k8s.DeploymentStatus +
// k8s.PodHealth every cycle and reports the resulting Phase/percentage to the
// Deployment Service. The state machine itself holds no state.
package rollout

// Phase is a coarse-grained rollout lifecycle state. The canonical progression
// for a healthy rollout is:
//
//	Pending -> Scheduling -> Reconciling -> RollingOut -> Healthy
//
// A rollout that cannot make progress transitions to Failed (which includes the
// deadline-exceeded "timeout" case, distinguished by IsTimeout). Rollback is a
// control-plane-driven state used when a failed rollout is being reverted.
type Phase string

const (
	// PhasePending means the desired Deployment has not yet been applied to the
	// cluster by the agent.
	PhasePending Phase = "Pending"
	// PhaseScheduling means the Deployment has been applied but the Kubernetes
	// controller has not yet observed the latest spec generation.
	PhaseScheduling Phase = "Scheduling"
	// PhaseReconciling means the latest spec is observed but no updated replicas
	// have become available yet (pods are being created/scheduled/pulled).
	PhaseReconciling Phase = "Reconciling"
	// PhaseRollingOut means the rollout is partially complete: some but not all
	// replicas are updated and available.
	PhaseRollingOut Phase = "RollingOut"
	// PhaseHealthy means all desired replicas are updated, available and ready.
	PhaseHealthy Phase = "Healthy"
	// PhaseFailed means the rollout cannot make progress: the progress deadline
	// was exceeded, a replica failure was reported, or a fatal pod issue
	// (ImagePullBackOff, CrashLoopBackOff, Unschedulable) was observed.
	PhaseFailed Phase = "Failed"
	// PhaseRollback means a failed rollout is being reverted to a previous
	// successful revision. It is set by the control plane, not derived here.
	PhaseRollback Phase = "Rollback"
)

// String returns the phase as a string.
func (p Phase) String() string { return string(p) }

// IsTerminal reports whether the phase is a settled end state that will not
// change without a new desired-state revision.
func (p Phase) IsTerminal() bool {
	return p == PhaseHealthy || p == PhaseFailed
}

// Snapshot is the observable input to the state machine. Every field is derived
// deterministically from the Kubernetes Deployment status and pod health; the
// struct carries no history so DerivePhase is a pure function of its argument.
type Snapshot struct {
	// Applied is true once the agent has created/updated the Deployment object.
	Applied bool
	// DesiredReplicas is the Deployment's spec.replicas.
	DesiredReplicas int32
	// ReadyReplicas is status.readyReplicas.
	ReadyReplicas int32
	// UpdatedReplicas is status.updatedReplicas (replicas on the latest template).
	UpdatedReplicas int32
	// AvailableReplicas is status.availableReplicas.
	AvailableReplicas int32
	// UnavailableReplicas is status.unavailableReplicas.
	UnavailableReplicas int32
	// Generation is metadata.generation (the spec version).
	Generation int64
	// ObservedGeneration is status.observedGeneration (spec version the
	// controller has acted on).
	ObservedGeneration int64
	// ProgressDeadlineExceeded is true when the Progressing condition is False
	// with reason ProgressDeadlineExceeded (the native rollout timeout signal).
	ProgressDeadlineExceeded bool
	// ReplicaFailure is true when the ReplicaFailure condition is True.
	ReplicaFailure bool
	// PodIssue is a non-empty fatal pod reason when a pod is stuck
	// (ImagePullBackOff, ErrImagePull, CrashLoopBackOff, Unschedulable). Empty
	// means no fatal pod issue was observed.
	PodIssue string
}

// DerivePhase computes the rollout Phase from an observed Snapshot. It is pure:
// the same Snapshot always yields the same Phase.
func DerivePhase(s Snapshot) Phase {
	// Not applied yet.
	if !s.Applied {
		return PhasePending
	}

	// Fatal conditions take precedence over progress: a stuck rollout must be
	// reported Failed even if some replicas are still reported available.
	if s.ProgressDeadlineExceeded || s.ReplicaFailure || s.PodIssue != "" {
		return PhaseFailed
	}

	// The Kubernetes controller has not yet observed the latest spec.
	if s.Generation > 0 && s.ObservedGeneration < s.Generation {
		return PhaseScheduling
	}

	// Scale-to-zero: no replicas desired and the spec is observed => healthy.
	if s.DesiredReplicas <= 0 {
		return PhaseHealthy
	}

	// Fully rolled out: every desired replica is updated, available and ready
	// and none are unavailable.
	if s.UpdatedReplicas >= s.DesiredReplicas &&
		s.AvailableReplicas >= s.DesiredReplicas &&
		s.ReadyReplicas >= s.DesiredReplicas &&
		s.UnavailableReplicas == 0 {
		return PhaseHealthy
	}

	// Nothing is up yet: pods are being created/scheduled/pulled.
	if s.UpdatedReplicas == 0 && s.AvailableReplicas == 0 && s.ReadyReplicas == 0 {
		return PhaseReconciling
	}

	// Partial progress.
	return PhaseRollingOut
}

// IsTimeout reports whether a (Failed) rollout failed specifically because the
// progress deadline was exceeded, as opposed to a replica/pod failure.
func IsTimeout(s Snapshot) bool {
	return s.ProgressDeadlineExceeded
}

// Percentage returns the rollout completion percentage in the range [0,100],
// based on how many of the desired replicas are available.
func Percentage(s Snapshot) int {
	if s.DesiredReplicas <= 0 {
		// Scale-to-zero is complete once the controller has observed the spec.
		if s.Applied && (s.Generation == 0 || s.ObservedGeneration >= s.Generation) {
			return 100
		}
		return 0
	}

	avail := s.AvailableReplicas
	if avail < 0 {
		avail = 0
	}
	if avail > s.DesiredReplicas {
		avail = s.DesiredReplicas
	}
	return int(float64(avail) / float64(s.DesiredReplicas) * 100.0)
}
