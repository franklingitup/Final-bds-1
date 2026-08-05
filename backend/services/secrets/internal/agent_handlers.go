package secrets

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
	ValidateCluster(ctx context.Context, clusterID, agentID string) (orgID string, err error)
}

// AgentHandler handles agent-specific endpoints.
type AgentHandler struct {
	svc       *Service
	validator ClusterValidator
	log       *slog.Logger
}

// NewAgentHandler creates a handler for agent endpoints.
func NewAgentHandler(svc *Service, validator ClusterValidator, log *slog.Logger) *AgentHandler {
	if log == nil {
		log = slog.Default()
	}
	return &AgentHandler{
		svc:       svc,
		validator: validator,
		log:       log,
	}
}

// AgentAuthMiddleware creates middleware that authenticates agent requests.
func (h *AgentHandler) AgentAuthMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		clusterID := c.Get("X-Cluster-ID")
		agentID := c.Get("X-Agent-ID")

		if clusterID == "" {
			return apperrors.Unauthorized("missing X-Cluster-ID header")
		}
		if agentID == "" {
			return apperrors.Unauthorized("missing X-Agent-ID header")
		}

		orgID, err := h.validator.ValidateCluster(c.UserContext(), clusterID, agentID)
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

// GetSecrets handles GET /v1/agent/clusters/:clusterId/secrets
// Returns decrypted secrets for deployments on the cluster.
//
// SECURITY:
// - Validates cluster ownership via X-Cluster-ID and X-Agent-ID headers.
// - Validates path parameter matches authenticated cluster ID.
// - Executes query within tenant context for RLS enforcement.
// - Returns only secrets for projects with active deployments on the cluster.
func (h *AgentHandler) GetSecrets(c *fiber.Ctx) error {
	agent := AgentFromContext(c.UserContext())
	if agent == nil {
		return apperrors.Unauthorized("agent authentication required")
	}

	clusterID := c.Params("clusterId")

	// SECURITY: Path parameter must match authenticated cluster
	if clusterID != agent.ClusterID {
		h.log.WarnContext(c.UserContext(), "cluster ID mismatch in agent request",
			slog.String("path_cluster_id", clusterID),
			slog.String("auth_cluster_id", agent.ClusterID),
		)
		return apperrors.Forbidden("cluster ID mismatch")
	}

	secrets, err := h.svc.GetSecretsForCluster(c.UserContext(), agent.OrganizationID, clusterID)
	if err != nil {
		h.log.ErrorContext(c.UserContext(), "failed to get secrets for cluster",
			slog.String("error", err.Error()),
			slog.String("cluster_id", clusterID),
		)
		return err
	}

	return c.JSON(AgentSecretsResponse{Secrets: secrets})
}

// RegisterAgentRoutes mounts agent-specific routes onto the app.
//
// Endpoints:
//   GET /v1/agent/clusters/:clusterId/secrets
//
// Authentication: X-Cluster-ID and X-Agent-ID headers.
func RegisterAgentRoutes(app *fiber.App, h *AgentHandler) {
	agent := app.Group("/v1/agent", h.AgentAuthMiddleware())
	agent.Get("/clusters/:clusterId/secrets", h.GetSecrets)
}

// PoolQuerier is the interface for a connection pool that can query.
type PoolQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
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
