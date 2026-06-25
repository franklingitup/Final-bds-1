package deployment

import (
	"context"

	"github.com/gofiber/fiber/v2"

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
	QueryRow(ctx context.Context, sql string, args ...any) interface{ Scan(dest ...any) error }
}

// clusterValidatorImpl validates clusters by querying the cluster table directly.
type clusterValidatorImpl struct {
	pool PoolQuerier
}

// NewClusterValidator creates a ClusterValidator that queries the database directly.
func NewClusterValidator(pool PoolQuerier) ClusterValidator {
	return &clusterValidatorImpl{pool: pool}
}

// ValidateCluster checks that the cluster is registered and the agent ID matches.
func (v *clusterValidatorImpl) ValidateCluster(ctx context.Context, clusterID, agentID string) (string, error) {
	const sql = `
SELECT org_id, agent_id, status 
FROM clusters 
WHERE id = $1`

	var orgID string
	var storedAgentID *string
	var status string

	if err := v.pool.QueryRow(ctx, sql, clusterID).Scan(&orgID, &storedAgentID, &status); err != nil {
		return "", apperrors.Unauthorized("cluster not found")
	}

	if status != "connected" {
		return "", apperrors.Unauthorized("cluster not connected")
	}

	if storedAgentID == nil || *storedAgentID != agentID {
		return "", apperrors.Unauthorized("invalid agent credentials")
	}

	return orgID, nil
}
