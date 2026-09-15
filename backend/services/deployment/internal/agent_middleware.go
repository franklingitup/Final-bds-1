package deployment

import (
	"context"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// agentContextKey is used to store agent identity in request context.
type agentContextKey struct{}

// AgentIdentity represents an authenticated agent.
type AgentIdentity struct {
	ClusterID      string
	OrganizationID string
	AgentID        string
}

// AgentFromContext extracts the agent identity from the context.
func AgentFromContext(ctx context.Context) *AgentIdentity {
	v := ctx.Value(agentContextKey{})
	if v == nil {
		return nil
	}
	agent, _ := v.(*AgentIdentity)
	return agent
}

// ClusterValidator validates cluster credentials.
type ClusterValidator interface {
	// ValidateCluster checks that the cluster exists, is connected, and has the given agent ID.
	ValidateCluster(ctx context.Context, clusterID, agentID string) (orgID string, err error)
}

// AgentAuthMiddleware creates middleware that authenticates agent requests.
// Agents must provide X-Cluster-ID and X-Agent-ID headers.
// The middleware validates these against the cluster service and injects AgentIdentity into context.
func AgentAuthMiddleware(validator ClusterValidator) fiber.Handler {
	return func(c *fiber.Ctx) error {
		clusterID := c.Get("X-Cluster-ID")
		agentID := c.Get("X-Agent-ID")

		if clusterID == "" {
			return apperrors.Unauthorized("missing X-Cluster-ID header")
		}
		if agentID == "" {
			return apperrors.Unauthorized("missing X-Agent-ID header")
		}

		orgID, err := validator.ValidateCluster(c.UserContext(), clusterID, agentID)
		if err != nil {
			return err
		}

		identity := &AgentIdentity{
			ClusterID:      clusterID,
			OrganizationID: orgID,
			AgentID:        agentID,
		}

		ctx := context.WithValue(c.UserContext(), agentContextKey{}, identity)
		c.SetUserContext(ctx)

		return c.Next()
	}
}

// PoolQuerier is the interface for a connection pool that can query.
type PoolQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// clusterValidatorImpl validates clusters by querying the cluster table directly.
type clusterValidatorImpl struct {
	pool PoolQuerier
	log  *slog.Logger
}

// NewClusterValidator creates a ClusterValidator that queries the database directly.
// The logger is used for debug-level logging of failure reasons; client-facing
// errors are always generic to prevent information leakage.
func NewClusterValidator(pool PoolQuerier, log *slog.Logger) ClusterValidator {
	if log == nil {
		log = slog.Default()
	}
	return &clusterValidatorImpl{pool: pool, log: log}
}

// ValidateCluster checks that the cluster is registered and the agent ID matches.
// All failure cases return the same generic error message to prevent enumeration
// of cluster IDs or status probing by attackers.
func (v *clusterValidatorImpl) ValidateCluster(ctx context.Context, clusterID, agentID string) (string, error) {
	const sql = `
SELECT org_id, agent_id, status 
FROM clusters 
WHERE id = $1`

	var orgID string
	var storedAgentID *string
	var status string

	// Generic error message for all failure cases to prevent information leakage.
	const genericErr = "authentication failed"

	if err := v.pool.QueryRow(ctx, sql, clusterID).Scan(&orgID, &storedAgentID, &status); err != nil {
		v.log.DebugContext(ctx, "agent auth failed: cluster not found",
			slog.String("cluster_id", clusterID))
		return "", apperrors.Unauthorized(genericErr)
	}

	if status != "connected" {
		v.log.DebugContext(ctx, "agent auth failed: cluster not connected",
			slog.String("cluster_id", clusterID),
			slog.String("status", status))
		return "", apperrors.Unauthorized(genericErr)
	}

	if storedAgentID == nil || *storedAgentID != agentID {
		v.log.DebugContext(ctx, "agent auth failed: agent ID mismatch",
			slog.String("cluster_id", clusterID))
		return "", apperrors.Unauthorized(genericErr)
	}

	return orgID, nil
}
