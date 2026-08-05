package rollout

import "testing"

func TestDerivePhase(t *testing.T) {
	tests := []struct {
		name string
		snap Snapshot
		want Phase
	}{
		{
			name: "not applied is pending",
			snap: Snapshot{Applied: false, DesiredReplicas: 3},
			want: PhasePending,
		},
		{
			name: "spec not observed is scheduling",
			snap: Snapshot{Applied: true, DesiredReplicas: 3, Generation: 5, ObservedGeneration: 4},
			want: PhaseScheduling,
		},
		{
			name: "observed but no replicas up is reconciling",
			snap: Snapshot{Applied: true, DesiredReplicas: 3, Generation: 5, ObservedGeneration: 5},
			want: PhaseReconciling,
		},
		{
			name: "partial progress is rolling out",
			snap: Snapshot{Applied: true, DesiredReplicas: 3, Generation: 5, ObservedGeneration: 5,
				UpdatedReplicas: 3, AvailableReplicas: 1, ReadyReplicas: 1, UnavailableReplicas: 2},
			want: PhaseRollingOut,
		},
		{
			name: "all replicas available is healthy",
			snap: Snapshot{Applied: true, DesiredReplicas: 3, Generation: 5, ObservedGeneration: 5,
				UpdatedReplicas: 3, AvailableReplicas: 3, ReadyReplicas: 3},
			want: PhaseHealthy,
		},
		{
			name: "scale to zero is healthy",
			snap: Snapshot{Applied: true, DesiredReplicas: 0, Generation: 2, ObservedGeneration: 2},
			want: PhaseHealthy,
		},
		{
			name: "progress deadline exceeded is failed",
			snap: Snapshot{Applied: true, DesiredReplicas: 3, Generation: 5, ObservedGeneration: 5,
				UpdatedReplicas: 3, AvailableReplicas: 1, ProgressDeadlineExceeded: true},
			want: PhaseFailed,
		},
		{
			name: "replica failure is failed",
			snap: Snapshot{Applied: true, DesiredReplicas: 3, Generation: 5, ObservedGeneration: 5, ReplicaFailure: true},
			want: PhaseFailed,
		},
		{
			name: "image pull backoff pod issue is failed",
			snap: Snapshot{Applied: true, DesiredReplicas: 3, Generation: 5, ObservedGeneration: 5, PodIssue: "ImagePullBackOff"},
			want: PhaseFailed,
		},
		{
			name: "crash loop pod issue is failed even with a ready replica",
			snap: Snapshot{Applied: true, DesiredReplicas: 3, Generation: 5, ObservedGeneration: 5,
				UpdatedReplicas: 3, AvailableReplicas: 1, ReadyReplicas: 1, PodIssue: "CrashLoopBackOff"},
			want: PhaseFailed,
		},
		{
			name: "unschedulable pod issue is failed",
			snap: Snapshot{Applied: true, DesiredReplicas: 3, Generation: 1, ObservedGeneration: 1, PodIssue: "Unschedulable"},
			want: PhaseFailed,
		},
		{
			name: "fatal condition takes precedence over full availability",
			snap: Snapshot{Applied: true, DesiredReplicas: 3, Generation: 5, ObservedGeneration: 5,
				UpdatedReplicas: 3, AvailableReplicas: 3, ReadyReplicas: 3, ProgressDeadlineExceeded: true},
			want: PhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DerivePhase(tt.snap); got != tt.want {
				t.Errorf("DerivePhase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsTimeout(t *testing.T) {
	if !IsTimeout(Snapshot{ProgressDeadlineExceeded: true}) {
		t.Error("expected timeout for ProgressDeadlineExceeded")
	}
	if IsTimeout(Snapshot{ReplicaFailure: true}) {
		t.Error("replica failure is not a timeout")
	}
}

func TestPercentage(t *testing.T) {
	tests := []struct {
		name string
		snap Snapshot
		want int
	}{
		{"zero desired not applied", Snapshot{DesiredReplicas: 0}, 0},
		{"zero desired applied observed", Snapshot{Applied: true, DesiredReplicas: 0, Generation: 1, ObservedGeneration: 1}, 100},
		{"none available", Snapshot{Applied: true, DesiredReplicas: 4, AvailableReplicas: 0}, 0},
		{"half available", Snapshot{Applied: true, DesiredReplicas: 4, AvailableReplicas: 2}, 50},
		{"all available", Snapshot{Applied: true, DesiredReplicas: 4, AvailableReplicas: 4}, 100},
		{"over available capped", Snapshot{Applied: true, DesiredReplicas: 4, AvailableReplicas: 6}, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Percentage(tt.snap); got != tt.want {
				t.Errorf("Percentage() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPhaseIsTerminal(t *testing.T) {
	terminal := []Phase{PhaseHealthy, PhaseFailed}
	for _, p := range terminal {
		if !p.IsTerminal() {
			t.Errorf("%q should be terminal", p)
		}
	}
	nonTerminal := []Phase{PhasePending, PhaseScheduling, PhaseReconciling, PhaseRollingOut, PhaseRollback}
	for _, p := range nonTerminal {
		if p.IsTerminal() {
			t.Errorf("%q should not be terminal", p)
		}
	}
}
