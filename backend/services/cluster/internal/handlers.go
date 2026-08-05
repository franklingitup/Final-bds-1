package cluster

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler adapts the cluster Service to Fiber HTTP handlers.
type Handler struct {
	svc      *Service
	verifier *TokenVerifier
}

// NewHandler constructs an HTTP handler.
func NewHandler(svc *Service, verifier *TokenVerifier) *Handler {
	return &Handler{svc: svc, verifier: verifier}
}

func parseBody[T any](c *fiber.Ctx) (T, error) {
	var body T
	if err := c.BodyParser(&body); err != nil {
		return body, apperrors.Validation("invalid request body")
	}
	return body, nil
}

func pageRequest(c *fiber.Ctx) database.PageRequest {
	return database.PageRequest{Limit: c.QueryInt("limit", 0), Cursor: c.Query("cursor")}
}

func orgID(c *fiber.Ctx) string {
	return c.Params("orgId")
}

func clusterID(c *fiber.Ctx) string {
	return c.Params("clusterId")
}

// ----------------------------------------------------------------------------
// Clusters
// ----------------------------------------------------------------------------

func (h *Handler) CreateCluster(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[CreateClusterRequest](c)
	if err != nil {
		return err
	}
	cluster, err := h.svc.CreateCluster(c.UserContext(), org, callerIdentity(c).UserID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toClusterView(cluster))
}

func (h *Handler) GetCluster(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	cluster, err := h.svc.GetCluster(c.UserContext(), org, callerIdentity(c).UserID, clusterID(c))
	if err != nil {
		return err
	}
	return c.JSON(toClusterView(cluster))
}

func (h *Handler) ListClusters(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	status := c.Query("status")
	page, err := h.svc.ListClusters(c.UserContext(), org, callerIdentity(c).UserID, pageRequest(c), status)
	if err != nil {
		return err
	}
	views := make([]ClusterView, 0, len(page.Items))
	for _, cluster := range page.Items {
		views = append(views, toClusterView(&cluster))
	}
	return c.JSON(fiber.Map{"items": views, "nextCursor": page.NextCursor})
}

func (h *Handler) UpdateCluster(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[UpdateClusterRequest](c)
	if err != nil {
		return err
	}
	cluster, err := h.svc.UpdateCluster(c.UserContext(), org, callerIdentity(c).UserID, clusterID(c), req)
	if err != nil {
		return err
	}
	return c.JSON(toClusterView(cluster))
}

func (h *Handler) DeleteCluster(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	if err := h.svc.DeleteCluster(c.UserContext(), org, callerIdentity(c).UserID, clusterID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Registration Tokens
// ----------------------------------------------------------------------------

func (h *Handler) GenerateRegistrationToken(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[GenerateTokenRequest](c)
	if err != nil {
		return err
	}
	token, err := h.svc.GenerateRegistrationToken(c.UserContext(), org, callerIdentity(c).UserID, clusterID(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(token)
}

func (h *Handler) RevokeRegistrationToken(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	tokenID := c.Params("tokenId")
	if err := h.svc.RevokeRegistrationToken(c.UserContext(), org, callerIdentity(c).UserID, clusterID(c), tokenID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Agent Endpoints (capability-based, no user auth required)
// ----------------------------------------------------------------------------

func (h *Handler) RegisterAgent(c *fiber.Ctx) error {
	req, err := parseBody[AgentRegisterRequest](c)
	if err != nil {
		return err
	}
	cluster, err := h.svc.RegisterAgent(c.UserContext(), req)
	if err != nil {
		return err
	}
	return c.JSON(toClusterView(cluster))
}

// RecoverAgent handles GET /v1/agent/recover. It is capability-based: the
// installation token is presented via the X-Registration-Token header (or an
// Authorization: Bearer header) and maps to exactly one cluster. Tokens are
// never accepted from the query string to keep them out of access logs.
func (h *Handler) RecoverAgent(c *fiber.Ctx) error {
	token := c.Get("X-Registration-Token")
	if token == "" {
		if auth := c.Get("Authorization"); len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
			token = strings.TrimSpace(auth[7:])
		}
	}
	agentID := c.Get("X-Agent-ID")

	cluster, err := h.svc.RecoverCluster(c.UserContext(), token, agentID)
	if err != nil {
		return err
	}
	return c.JSON(toClusterView(cluster))
}

func (h *Handler) AgentHeartbeat(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[AgentHeartbeatRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.RecordHeartbeat(c.UserContext(), org, clusterID(c), req); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"status": "ok"})
}

// ----------------------------------------------------------------------------
// Heartbeat History
// ----------------------------------------------------------------------------

func (h *Handler) GetHeartbeats(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	limit := c.QueryInt("limit", 50)
	heartbeats, err := h.svc.GetHeartbeats(c.UserContext(), org, callerIdentity(c).UserID, clusterID(c), limit)
	if err != nil {
		return err
	}
	views := make([]HeartbeatView, 0, len(heartbeats))
	for _, hb := range heartbeats {
		views = append(views, toHeartbeatView(&hb))
	}
	return c.JSON(fiber.Map{"items": views})
}
