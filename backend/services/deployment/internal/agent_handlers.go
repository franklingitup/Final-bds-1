package deployment

import (
	"context"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/bdsplatform/platform/backend/libs/contracts/deploymentstatus"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/libs/telemetry"
)

// AgentHandler handles agent-specific endpoints.
type AgentHandler struct {
	desiredState DesiredStateStore
	tenant       TenantQuerier
	outbox       events.Outbox
	rollouts     RolloutStatusStore
	autoRollback bool
	now          func() time.Time
	tracer       trace.Tracer
	log          *slog.Logger
}

// AgentHandlerDeps holds dependencies for AgentHandler.
type AgentHandlerDeps struct {
	DesiredState DesiredStateStore
	Tenant       TenantQuerier
	// Outbox, when set, receives the same deployment domain events emitted by
	// the service layer for user-driven transitions. It is optional so existing
	// tests that do not assert on events can omit it.
	Outbox events.Outbox
	// Rollouts persists the rollout snapshots reported via the progress
	// endpoint. Optional: when nil the progress endpoint is effectively a no-op
	// persistence-wise (used by tests that do not exercise progress).
	Rollouts RolloutStatusStore
	// AutoRollback enables automatic rollback to the previous successful release
	// when a rollout fails/times out. Defaults to false (backward compatible).
	AutoRollback bool
	// Now overrides the clock (for tests). Defaults to time.Now.
	Now    func() time.Time
	Logger *slog.Logger
}

// NewAgentHandler creates a new agent handler.
func NewAgentHandler(deps AgentHandlerDeps) *AgentHandler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &AgentHandler{
		desiredState: deps.DesiredState,
		tenant:       deps.Tenant,
		outbox:       deps.Outbox,
		rollouts:     deps.Rollouts,
		autoRollback: deps.AutoRollback,
		now:          deps.Now,
		tracer:       telemetry.Tracer("deployment-engine"),
		log:          deps.Logger,
	}
}

// GetDesiredState returns the desired deployment state for the authenticated cluster.
// This endpoint is specifically designed for agents to consume.
//
// Route: GET /v1/agent/clusters/:clusterId/desired-state
// Auth: Cluster credentials (X-Cluster-ID, X-Agent-ID headers)
//
// SECURITY (CRIT-001 fix): This handler now:
// 1. Validates the cluster ID in the path matches the authenticated cluster
// 2. Passes the authenticated organization ID to the query
// 3. Executes the query within a tenant-scoped context
func (h *AgentHandler) GetDesiredState(c *fiber.Ctx) error {
	agent := AgentFromContext(c.UserContext())
	if agent == nil {
		return apperrors.Unauthorized("agent identity required")
	}

	// Verify the path parameter matches the authenticated cluster.
	clusterID := c.Params("clusterId")
	if clusterID != agent.ClusterID {
		h.log.WarnContext(c.UserContext(), "cluster ID mismatch in desired state request",
			slog.String("path_cluster_id", clusterID),
			slog.String("auth_cluster_id", agent.ClusterID),
			slog.String("agent_id", agent.AgentID))
		return apperrors.Forbidden("cluster ID mismatch")
	}

	// SECURITY: Pass the authenticated organization ID to ensure tenant isolation.
	// The GetDesiredState method now executes within a tenant context and
	// explicitly filters by org_id for defense-in-depth.
	deployments, err := h.desiredState.GetDesiredState(c.UserContext(), agent.OrganizationID, clusterID)
	if err != nil {
		return err
	}

	return c.JSON(DesiredStateResponse{
		ClusterID: clusterID,
		Items:     deployments,
	})
}

// UpdateDeploymentStatus allows agents to report deployment status.
// This endpoint updates release status based on agent observations.
//
// Route: POST /v1/agent/deployments/:deploymentId/releases/:releaseId/status
// Auth: Cluster credentials (X-Cluster-ID, X-Agent-ID headers)
//
// SECURITY (CRIT-002 fix): This handler now validates ownership before updates:
// 1. Deployment must belong to the authenticated organization
// 2. Deployment must be assigned to the authenticated cluster
// 3. Release must belong to the specified deployment
func (h *AgentHandler) UpdateDeploymentStatus(c *fiber.Ctx, releases ReleaseStore, deployments DeploymentStore) error {
	agent := AgentFromContext(c.UserContext())
	if agent == nil {
		return apperrors.Unauthorized("agent identity required")
	}

	deploymentID := c.Params("deploymentId")
	releaseID := c.Params("releaseId")

	var req UpdateStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	// Validate the status value using the canonical shared contracts.
	if !deploymentstatus.ValidAgentReleaseStatus(req.Status) {
		return apperrors.Validation("invalid status: must be deploying, succeeded, or failed")
	}

	// SECURITY (CRIT-002 fix): Validate ownership before allowing status updates.
	// Execute all validation and updates within tenant context.
	var updateErr error
	err := h.tenant.WithTenant(c.UserContext(), agent.OrganizationID, func(ctx context.Context) error {
		// Step 1: Fetch and validate deployment ownership.
		deployment, err := deployments.GetByID(ctx, deploymentID)
		if err != nil {
			if database.IsNotFound(err) {
				h.logOwnershipViolation(ctx, "deployment not found", agent, deploymentID, releaseID)
				return apperrors.Forbidden("deployment not found or access denied")
			}
			return err
		}

		// Validate deployment belongs to the authenticated organization.
		// RLS should enforce this, but we check explicitly as defense-in-depth.
		if deployment.OrgID != agent.OrganizationID {
			h.logOwnershipViolation(ctx, "org_id mismatch", agent, deploymentID, releaseID)
			return apperrors.Forbidden("deployment not found or access denied")
		}

		// Validate deployment is assigned to the authenticated cluster.
		if deployment.ClusterID != agent.ClusterID {
			h.logOwnershipViolation(ctx, "cluster_id mismatch", agent, deploymentID, releaseID)
			return apperrors.Forbidden("deployment not assigned to this cluster")
		}

		// Step 2: Fetch and validate release ownership.
		release, err := releases.GetByID(ctx, releaseID)
		if err != nil {
			if database.IsNotFound(err) {
				h.logOwnershipViolation(ctx, "release not found", agent, deploymentID, releaseID)
				return apperrors.Forbidden("release not found or access denied")
			}
			return err
		}

		// Validate release belongs to the specified deployment.
		if release.DeploymentID != deploymentID {
			h.logOwnershipViolation(ctx, "release.deployment_id mismatch", agent, deploymentID, releaseID)
			return apperrors.Forbidden("release does not belong to this deployment")
		}

		// Validate release belongs to the same organization.
		if release.OrgID != agent.OrganizationID {
			h.logOwnershipViolation(ctx, "release.org_id mismatch", agent, deploymentID, releaseID)
			return apperrors.Forbidden("release not found or access denied")
		}

		// Step 3: Ownership validated - proceed with status update.
		errMsg := req.ErrorMessage

		switch req.Status {
		case ReleaseStatusDeploying:
			if err := releases.MarkStarted(ctx, releaseID); err != nil {
				updateErr = err
				return err
			}
		case ReleaseStatusSucceeded, ReleaseStatusFailed:
			if err := releases.MarkFinished(ctx, releaseID, req.Status, errMsg); err != nil {
				updateErr = err
				return err
			}
		}

		// Update the deployment status.
		readyReplicas := req.ReadyReplicas
		var deployStatus string
		switch req.Status {
		case ReleaseStatusDeploying:
			deployStatus = StatusRunning
		case ReleaseStatusSucceeded:
			deployStatus = StatusSucceeded
		case ReleaseStatusFailed:
			deployStatus = StatusFailed
		}

		if err := deployments.UpdateStatus(ctx, deploymentID, deployStatus, readyReplicas, errMsg); err != nil {
			updateErr = err
			return err
		}

		// Emit the SAME domain events as the service-layer status transitions
		// (MarkDeploymentStarted/Succeeded/Failed). The enqueue runs inside this
		// tenant transaction (transactional outbox), so the event is persisted
		// atomically with the status change and later relayed to the broker,
		// reaching the audit service identically to user-driven transitions.
		if h.outbox != nil {
			if err := h.emitStatusEvent(ctx, agent, deploymentID, releaseID, req, release); err != nil {
				updateErr = err
				return err
			}
		}

		return nil
	})

	if err != nil {
		return err
	}
	if updateErr != nil {
		return updateErr
	}

	return c.JSON(fiber.Map{"status": "ok"})
}

// emitStatusEvent builds and enqueues the deployment domain event that
// corresponds to an agent-reported status transition. It mirrors the payloads
// and event types used by the service-layer MarkDeployment* methods so that
// downstream consumers (notably the audit service) observe agent-driven and
// user-driven transitions identically. The actor is recorded as the agent.
func (h *AgentHandler) emitStatusEvent(ctx context.Context, agent *AgentIdentity, deploymentID, releaseID string, req UpdateStatusRequest, release *Release) error {
	actor := events.WithActor(events.Actor{Type: "agent", ID: agent.AgentID})
	resource := events.WithResource(events.Resource{Type: "deployment", ID: deploymentID})

	var (
		evt events.Envelope
		err error
	)
	switch req.Status {
	case ReleaseStatusDeploying:
		evt, err = events.New(EventDeploymentStarted, eventVersion, agent.OrganizationID, deploymentStartedPayload{
			DeploymentID: deploymentID,
			ReleaseID:    releaseID,
			Revision:     release.Revision,
			Image:        release.Image,
		}, actor, resource)
	case ReleaseStatusSucceeded:
		ready := 0
		if req.ReadyReplicas != nil {
			ready = *req.ReadyReplicas
		}
		evt, err = events.New(EventDeploymentSucceeded, eventVersion, agent.OrganizationID, deploymentSucceededPayload{
			DeploymentID:  deploymentID,
			ReleaseID:     releaseID,
			Revision:      release.Revision,
			ReadyReplicas: ready,
		}, actor, resource)
	case ReleaseStatusFailed:
		errorMsg := ""
		if req.ErrorMessage != nil {
			errorMsg = *req.ErrorMessage
		}
		evt, err = events.New(EventDeploymentFailed, eventVersion, agent.OrganizationID, deploymentFailedPayload{
			DeploymentID: deploymentID,
			ReleaseID:    releaseID,
			Revision:     release.Revision,
			ErrorMessage: errorMsg,
		}, actor, resource)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	return h.outbox.Enqueue(ctx, evt)
}

// logOwnershipViolation logs a security event when ownership validation fails.
func (h *AgentHandler) logOwnershipViolation(ctx context.Context, reason string, agent *AgentIdentity, deploymentID, releaseID string) {
	h.log.WarnContext(ctx, "ownership validation failed",
		slog.String("reason", reason),
		slog.String("cluster_id", agent.ClusterID),
		slog.String("agent_id", agent.AgentID),
		slog.String("org_id", agent.OrganizationID),
		slog.String("deployment_id", deploymentID),
		slog.String("release_id", releaseID))
}

// UpdateStatusRequest is defined in domain.go
