package cluster

import (
	"context"
	"log/slog"

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
	ValidateCluster(ctx context.Context, clusterID, agentID string) (orgID string, err error)
}

// AgentHandler handles agent-specific endpoints.
type AgentHandler struct {
	svc       *Service
	validator ClusterValidator
	log       *slog.Logger
}

// NewAgentHandler creates a new agent handler.
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
// This validates X-Cluster-ID and X-Agent-ID headers against the clusters table.
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

// AgentHeartbeat handles POST /v1/agent/clusters/:clusterId/heartbeat
// This endpoint allows agents to send heartbeats using credential-based auth.
//
// Authentication: X-Cluster-ID and X-Agent-ID headers
// Validation:
//   - Cluster must exist and be registered (status = connected)
//   - Agent ID must match the registered agent
//   - Cluster must not be deleted
func (h *AgentHandler) AgentHeartbeat(c *fiber.Ctx) error {
	agent := AgentFromContext(c.UserContext())
	if agent == nil {
		return apperrors.Unauthorized("agent authentication required")
	}

	pathClusterID := c.Params("clusterId")

	// Verify the path parameter matches the authenticated cluster.
	if pathClusterID != agent.ClusterID {
		h.log.WarnContext(c.UserContext(), "cluster ID mismatch in heartbeat request",
			slog.String("path_cluster_id", pathClusterID),
			slog.String("auth_cluster_id", agent.ClusterID),
			slog.String("agent_id", agent.AgentID))
		return apperrors.Forbidden("cluster ID mismatch")
	}

	var req AgentHeartbeatRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	// Override the agent ID from credentials to prevent spoofing.
	req.AgentID = agent.AgentID

	if err := h.svc.RecordHeartbeat(c.UserContext(), agent.OrganizationID, agent.ClusterID, req); err != nil {
		return err
	}

	return c.JSON(fiber.Map{"status": "ok"})
}

// RegisterAgentRoutes mounts agent-specific routes onto the app.
// These routes use credential-based authentication (X-Cluster-ID, X-Agent-ID).
//
// Routes:
//   - POST /v1/agent/clusters/:clusterId/heartbeat - Agent heartbeat
//
// The /v1/agent/register endpoint remains capability-based (registration token).
func RegisterAgentRoutes(app *fiber.App, h *AgentHandler) {
	agent := app.Group("/v1/agent", h.AgentAuthMiddleware())
	agent.Post("/clusters/:clusterId/heartbeat", h.AgentHeartbeat)
}

// clusterValidatorImpl implements ClusterValidator using direct database queries.
type clusterValidatorImpl struct {
	clusters ClusterStore
}

// NewClusterValidator creates a new cluster validator.
func NewClusterValidator(clusters ClusterStore) ClusterValidator {
	return &clusterValidatorImpl{clusters: clusters}
}

// ValidateCluster checks that the cluster is registered and the agent ID matches.
// Returns the organization ID on success.
func (v *clusterValidatorImpl) ValidateCluster(ctx context.Context, clusterID, agentID string) (string, error) {
	// Fetch the cluster without tenant context (cross-tenant lookup for agent auth).
	cluster, err := v.clusters.GetByIDWithoutTenant(ctx, clusterID)
	if err != nil {
		return "", apperrors.Unauthorized("invalid cluster credentials")
	}

	// A deleted cluster is gone for good; the agent must not silently recover it.
	if cluster.Status == StatusDeleted {
		return "", apperrors.Forbidden("cluster deleted")
	}

	// Accept any *registered* cluster, i.e. one that has completed registration
	// and therefore has an agent_id. This deliberately includes StatusDisconnected
	// (and StatusRegistering): a cluster whose heartbeats lapsed — because of a
	// control-plane restart, network partition, or the disconnection sweep — must
	// be able to reconnect simply by heartbeating again. RecordHeartbeat ->
	// UpdateHeartbeat flips the status back to "connected". Rejecting non-connected
	// clusters here (as the previous code did) meant a disconnected cluster could
	// NEVER reconnect: the middleware returned 403 before RecordHeartbeat ran, and
	// the agent treats 403 as a terminal mismatch rather than a recoverable state.
	// Only genuinely-unregistered clusters (pending, no agent) are rejected.
	if cluster.AgentID == nil {
		return "", apperrors.Forbidden("cluster not registered")
	}

	// Validate agent ID matches.
	if *cluster.AgentID != agentID {
		return "", apperrors.Unauthorized("invalid agent credentials")
	}

	return cluster.OrgID, nil
}
