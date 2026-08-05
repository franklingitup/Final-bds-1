package deployment

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// Rollout phases reported by the platform agent's rollout state machine. These
// mirror agents/platform-agent/internal/rollout.Phase and are the single set of
// valid values for rollout_status.phase.
const (
	RolloutPhasePending     = "Pending"
	RolloutPhaseScheduling  = "Scheduling"
	RolloutPhaseReconciling = "Reconciling"
	RolloutPhaseRollingOut  = "RollingOut"
	RolloutPhaseHealthy     = "Healthy"
	RolloutPhaseFailed      = "Failed"
	RolloutPhaseRollback    = "Rollback"
)

// ValidRolloutPhase reports whether s is a known rollout phase.
func ValidRolloutPhase(s string) bool {
	switch s {
	case RolloutPhasePending, RolloutPhaseScheduling, RolloutPhaseReconciling,
		RolloutPhaseRollingOut, RolloutPhaseHealthy, RolloutPhaseFailed, RolloutPhaseRollback:
		return true
	default:
		return false
	}
}

// RolloutStatus is the latest rollout snapshot for a (deployment, release) pair
// reported by the agent. It is an engine read/write model that lives alongside
// (never replaces) the deployments/releases rows.
type RolloutStatus struct {
	DeploymentID        string          `db:"deployment_id"`
	ReleaseID           string          `db:"release_id"`
	OrgID               string          `db:"org_id"`
	Phase               string          `db:"phase"`
	Revision            int             `db:"revision"`
	Image               string          `db:"image"`
	DesiredReplicas     int             `db:"desired_replicas"`
	ReadyReplicas       int             `db:"ready_replicas"`
	UpdatedReplicas     int             `db:"updated_replicas"`
	AvailableReplicas   int             `db:"available_replicas"`
	UnavailableReplicas int             `db:"unavailable_replicas"`
	ObservedGeneration  int64           `db:"observed_generation"`
	RolloutPercentage   int             `db:"rollout_percentage"`
	Conditions          json.RawMessage `db:"conditions"`
	ErrorMessage        *string         `db:"error_message"`
	IsRollback          bool            `db:"is_rollback"`
	StartedAt           time.Time       `db:"started_at"`
	UpdatedAt           time.Time       `db:"updated_at"`
}

// ProgressConditionDTO mirrors a Kubernetes Deployment condition in the agent
// progress request body.
type ProgressConditionDTO struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// UpdateProgressRequest is the body of the agent progress endpoint. It carries
// the continuous rollout snapshot that drives the deployment engine.
type UpdateProgressRequest struct {
	Phase               string                 `json:"phase"`
	Revision            int                    `json:"revision"`
	Image               string                 `json:"image,omitempty"`
	RolloutPercentage   int                    `json:"rolloutPercentage"`
	Timeout             bool                   `json:"timeout,omitempty"`
	DesiredReplicas     int                    `json:"desiredReplicas"`
	ReadyReplicas       int                    `json:"readyReplicas"`
	UpdatedReplicas     int                    `json:"updatedReplicas"`
	AvailableReplicas   int                    `json:"availableReplicas"`
	UnavailableReplicas int                    `json:"unavailableReplicas"`
	ObservedGeneration  int64                  `json:"observedGeneration"`
	Conditions          []ProgressConditionDTO `json:"conditions,omitempty"`
	ErrorMessage        string                 `json:"errorMessage,omitempty"`
}

// RolloutStatusStore persists rollout snapshots.
type RolloutStatusStore interface {
	// Upsert writes the latest snapshot for (deployment_id, release_id). On
	// conflict it updates all reported fields but preserves started_at and
	// never clears an existing is_rollback marker.
	Upsert(ctx context.Context, rs *RolloutStatus) error
	// Get returns the current snapshot, or a not-found error when absent.
	Get(ctx context.Context, deploymentID, releaseID string) (*RolloutStatus, error)
}

type rolloutStatusRepo struct{ db *database.DB }

// NewRolloutStatusStore returns a Postgres-backed RolloutStatusStore.
func NewRolloutStatusStore(db *database.DB) RolloutStatusStore {
	return &rolloutStatusRepo{db: db}
}

func (r *rolloutStatusRepo) Upsert(ctx context.Context, rs *RolloutStatus) error {
	if rs.Phase == "" {
		rs.Phase = RolloutPhasePending
	}
	if len(rs.Conditions) == 0 {
		rs.Conditions = json.RawMessage("[]")
	}

	const sql = `
INSERT INTO rollout_status (
    deployment_id, release_id, org_id, phase, revision, image,
    desired_replicas, ready_replicas, updated_replicas, available_replicas, unavailable_replicas,
    observed_generation, rollout_percentage, conditions, error_message, is_rollback)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (deployment_id, release_id) DO UPDATE SET
    phase = EXCLUDED.phase,
    revision = EXCLUDED.revision,
    image = EXCLUDED.image,
    desired_replicas = EXCLUDED.desired_replicas,
    ready_replicas = EXCLUDED.ready_replicas,
    updated_replicas = EXCLUDED.updated_replicas,
    available_replicas = EXCLUDED.available_replicas,
    unavailable_replicas = EXCLUDED.unavailable_replicas,
    observed_generation = EXCLUDED.observed_generation,
    rollout_percentage = EXCLUDED.rollout_percentage,
    conditions = EXCLUDED.conditions,
    error_message = EXCLUDED.error_message,
    is_rollback = rollout_status.is_rollback OR EXCLUDED.is_rollback,
    updated_at = now()
RETURNING started_at, updated_at, is_rollback`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		rs.DeploymentID, rs.ReleaseID, rs.OrgID, rs.Phase, rs.Revision, rs.Image,
		rs.DesiredReplicas, rs.ReadyReplicas, rs.UpdatedReplicas, rs.AvailableReplicas, rs.UnavailableReplicas,
		rs.ObservedGeneration, rs.RolloutPercentage, rs.Conditions, rs.ErrorMessage, rs.IsRollback)
	return database.MapError(row.Scan(&rs.StartedAt, &rs.UpdatedAt, &rs.IsRollback))
}

func (r *rolloutStatusRepo) Get(ctx context.Context, deploymentID, releaseID string) (*RolloutStatus, error) {
	rs, err := database.QueryOne[RolloutStatus](ctx, r.db.Conn(ctx),
		"SELECT * FROM rollout_status WHERE deployment_id = $1 AND release_id = $2", deploymentID, releaseID)
	if err != nil {
		return nil, err
	}
	return &rs, nil
}
