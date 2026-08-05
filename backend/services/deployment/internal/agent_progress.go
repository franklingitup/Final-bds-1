package deployment

import (
	"context"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// UpdateDeploymentProgress ingests a rollout progress snapshot reported by the
// platform agent. It is the control-plane half of the deployment engine:
//
//   - persists the latest rollout snapshot (rollout_status table),
//   - drives the rollout state machine and emits engine events
//     (deployment.progress / healthy / timeout / rollback.*),
//   - records rollout metrics,
//   - and, when enabled, triggers an automatic rollback on failure.
//
// It is additive to UpdateDeploymentStatus: the terminal release-status flow
// (deploying/succeeded/failed) is unchanged. Ownership is validated identically
// to the status endpoint.
//
// Route: POST /v1/agent/deployments/:deploymentId/releases/:releaseId/progress
// Auth: Cluster credentials (X-Cluster-ID, X-Agent-ID headers)
func (h *AgentHandler) UpdateDeploymentProgress(c *fiber.Ctx, releases ReleaseStore, deployments DeploymentStore) error {
	agent := AgentFromContext(c.UserContext())
	if agent == nil {
		return apperrors.Unauthorized("agent identity required")
	}

	deploymentID := c.Params("deploymentId")
	releaseID := c.Params("releaseId")

	var req UpdateProgressRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}
	if req.Phase == "" || !ValidRolloutPhase(req.Phase) {
		return apperrors.Validation("invalid phase")
	}

	ctx, span := h.tracer.Start(c.UserContext(), "deployment.progress")
	defer span.End()
	span.SetAttributes(
		attribute.String("deployment.id", deploymentID),
		attribute.String("release.id", releaseID),
		attribute.String("rollout.phase", req.Phase),
		attribute.Int("rollout.percentage", req.RolloutPercentage),
	)

	err := h.tenant.WithTenant(ctx, agent.OrganizationID, func(ctx context.Context) error {
		deployment, release, err := h.validateAgentOwnership(ctx, agent, deploymentID, releaseID, deployments, releases)
		if err != nil {
			return err
		}

		// Read the previous snapshot so we can detect phase transitions and the
		// rollback marker. Absence is normal on the first report.
		var prevPhase string
		var wasRollback bool
		if prev, gerr := h.rolloutGet(ctx, deploymentID, releaseID); gerr == nil && prev != nil {
			prevPhase = prev.Phase
			wasRollback = prev.IsRollback
		}

		if err := h.persistRollout(ctx, agent.OrganizationID, deploymentID, releaseID, req); err != nil {
			return err
		}

		rolloutProgressPercentage.Set(float64(req.RolloutPercentage))

		// Every accepted report is a meaningful change (the agent throttles to
		// phase/percentage changes), so publish a progress event each time.
		if err := h.emitEvent(ctx, agent, EventDeploymentProgress, deploymentID, deploymentProgressPayload{
			DeploymentID:      deploymentID,
			ReleaseID:         releaseID,
			Revision:          release.Revision,
			Phase:             req.Phase,
			RolloutPercentage: req.RolloutPercentage,
			ReadyReplicas:     req.ReadyReplicas,
			DesiredReplicas:   req.DesiredReplicas,
			Image:             req.Image,
		}); err != nil {
			return err
		}

		return h.applyTransition(ctx, agent, deployment, release, req, prevPhase, wasRollback, releases, deployments)
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	return c.JSON(fiber.Map{"status": "ok"})
}

// applyTransition emits terminal engine events and drives rollback based on the
// phase transition (prevPhase -> req.Phase). Terminal events fire once per
// transition (guarded by prevPhase) so repeated healthy/failed reports are
// idempotent.
func (h *AgentHandler) applyTransition(ctx context.Context, agent *AgentIdentity, deployment *Deployment, release *Release, req UpdateProgressRequest, prevPhase string, wasRollback bool, releases ReleaseStore, deployments DeploymentStore) error {
	switch req.Phase {
	case RolloutPhaseHealthy:
		if prevPhase == RolloutPhaseHealthy {
			return nil
		}
		rolloutSuccessTotal.Inc()
		duration := 0.0
		if release.StartedAt != nil {
			duration = h.now().Sub(*release.StartedAt).Seconds()
			if duration > 0 {
				rolloutDuration.Observe(duration)
			}
		}
		if err := h.emitEvent(ctx, agent, EventDeploymentHealthy, deployment.ID, deploymentHealthyPayload{
			DeploymentID:    deployment.ID,
			ReleaseID:       release.ID,
			Revision:        release.Revision,
			ReadyReplicas:   req.ReadyReplicas,
			DurationSeconds: duration,
		}); err != nil {
			return err
		}
		// A healthy rollback release completes the rollback lifecycle.
		if wasRollback {
			if err := h.emitEvent(ctx, agent, EventDeploymentRollbackCompleted, deployment.ID, deploymentRollbackCompletedPayload{
				DeploymentID:  deployment.ID,
				ReleaseID:     release.ID,
				Revision:      release.Revision,
				ReadyReplicas: req.ReadyReplicas,
			}); err != nil {
				return err
			}
			h.log.InfoContext(ctx, "rollback completed",
				"deployment_id", deployment.ID, "release_id", release.ID, "revision", release.Revision)
		}
		return nil

	case RolloutPhaseFailed:
		if prevPhase == RolloutPhaseFailed {
			return nil
		}
		rolloutFailureTotal.Inc()
		if req.Timeout {
			rolloutTimeoutTotal.Inc()
			if err := h.emitEvent(ctx, agent, EventDeploymentTimeout, deployment.ID, deploymentTimeoutPayload{
				DeploymentID: deployment.ID,
				ReleaseID:    release.ID,
				Revision:     release.Revision,
				ErrorMessage: req.ErrorMessage,
			}); err != nil {
				return err
			}
		}
		// Automatic rollback: only for genuine forward rollouts, never for a
		// rollback release that itself failed (avoids rollback loops).
		if h.autoRollback && !wasRollback {
			if err := h.triggerAutoRollback(ctx, agent, deployment, release, req, releases, deployments); err != nil {
				// Rollback is best-effort: a failure here must not fail the
				// progress report (the failure is already recorded).
				h.log.ErrorContext(ctx, "auto rollback failed",
					"deployment_id", deployment.ID, "release_id", release.ID, "error", err)
			}
		}
		return nil

	default:
		return nil
	}
}

// triggerAutoRollback reverts a failed rollout to the previous successful
// release by creating a new release from it, repointing the deployment, and
// pre-seeding a rollback-marked rollout_status row so the eventual healthy
// report emits deployment.rollback.completed. It emits deployment.rollback.started.
func (h *AgentHandler) triggerAutoRollback(ctx context.Context, agent *AgentIdentity, deployment *Deployment, failed *Release, req UpdateProgressRequest, releases ReleaseStore, deployments DeploymentStore) error {
	target, err := releases.GetPreviousSuccessful(ctx, deployment.ID, failed.Revision)
	if err != nil {
		if database.IsNotFound(err) {
			h.log.InfoContext(ctx, "auto rollback skipped: no previous successful release",
				"deployment_id", deployment.ID, "release_id", failed.ID)
			return nil
		}
		return err
	}

	newRel := &Release{
		OrgID:        agent.OrganizationID,
		DeploymentID: deployment.ID,
		Revision:     failed.Revision + 1,
		Image:        target.Image,
		Replicas:     target.Replicas,
		ConfigHash:   target.ConfigHash,
		Config:       target.Config,
		Status:       ReleaseStatusPending,
	}
	if err := releases.Create(ctx, newRel); err != nil {
		return err
	}

	deployment.Image = target.Image
	deployment.Replicas = target.Replicas
	deployment.Status = StatusPending
	if err := deployments.Update(ctx, deployment); err != nil {
		return err
	}

	// Pre-seed the new release's rollout snapshot with the rollback marker so
	// the progress handler recognizes it on the next report.
	if h.rollouts != nil {
		if err := h.rollouts.Upsert(ctx, &RolloutStatus{
			DeploymentID:    deployment.ID,
			ReleaseID:       newRel.ID,
			OrgID:           agent.OrganizationID,
			Phase:           RolloutPhasePending,
			Revision:        newRel.Revision,
			Image:           target.Image,
			DesiredReplicas: target.Replicas,
			IsRollback:      true,
		}); err != nil {
			return err
		}
	}

	if err := h.emitEvent(ctx, agent, EventDeploymentRollbackStarted, deployment.ID, deploymentRollbackStartedPayload{
		DeploymentID:    deployment.ID,
		FromReleaseID:   failed.ID,
		FromRevision:    failed.Revision,
		TargetReleaseID: newRel.ID,
		TargetRevision:  target.Revision,
		Reason:          req.ErrorMessage,
	}); err != nil {
		return err
	}

	h.log.InfoContext(ctx, "auto rollback started",
		"deployment_id", deployment.ID,
		"from_release_id", failed.ID,
		"from_revision", failed.Revision,
		"target_revision", target.Revision,
		"new_release_id", newRel.ID)
	return nil
}

// persistRollout upserts the reported snapshot into rollout_status. When no
// store is configured it is a no-op (backward compatible).
func (h *AgentHandler) persistRollout(ctx context.Context, orgID, deploymentID, releaseID string, req UpdateProgressRequest) error {
	if h.rollouts == nil {
		return nil
	}

	conditions := json.RawMessage("[]")
	if len(req.Conditions) > 0 {
		if b, err := json.Marshal(req.Conditions); err == nil {
			conditions = b
		}
	}

	var errMsg *string
	if req.ErrorMessage != "" {
		msg := req.ErrorMessage
		errMsg = &msg
	}

	return h.rollouts.Upsert(ctx, &RolloutStatus{
		DeploymentID:        deploymentID,
		ReleaseID:           releaseID,
		OrgID:               orgID,
		Phase:               req.Phase,
		Revision:            req.Revision,
		Image:               req.Image,
		DesiredReplicas:     req.DesiredReplicas,
		ReadyReplicas:       req.ReadyReplicas,
		UpdatedReplicas:     req.UpdatedReplicas,
		AvailableReplicas:   req.AvailableReplicas,
		UnavailableReplicas: req.UnavailableReplicas,
		ObservedGeneration:  req.ObservedGeneration,
		RolloutPercentage:   req.RolloutPercentage,
		Conditions:          conditions,
		ErrorMessage:        errMsg,
	})
}

// rolloutGet fetches the current snapshot, tolerating a nil store.
func (h *AgentHandler) rolloutGet(ctx context.Context, deploymentID, releaseID string) (*RolloutStatus, error) {
	if h.rollouts == nil {
		return nil, nil
	}
	return h.rollouts.Get(ctx, deploymentID, releaseID)
}

// emitEvent enqueues an engine event onto the transactional outbox with the
// agent recorded as the actor. It is a no-op when no outbox is configured.
func (h *AgentHandler) emitEvent(ctx context.Context, agent *AgentIdentity, eventType, deploymentID string, payload any) error {
	if h.outbox == nil {
		return nil
	}
	evt, err := events.New(eventType, eventVersion, agent.OrganizationID, payload,
		events.WithActor(events.Actor{Type: "agent", ID: agent.AgentID}),
		events.WithResource(events.Resource{Type: "deployment", ID: deploymentID}))
	if err != nil {
		return err
	}
	return h.outbox.Enqueue(ctx, evt)
}

// validateAgentOwnership performs the same tenant/cluster/release ownership
// checks the status endpoint uses, returning the validated deployment and
// release. It must run inside a tenant transaction.
func (h *AgentHandler) validateAgentOwnership(ctx context.Context, agent *AgentIdentity, deploymentID, releaseID string, deployments DeploymentStore, releases ReleaseStore) (*Deployment, *Release, error) {
	deployment, err := deployments.GetByID(ctx, deploymentID)
	if err != nil {
		if database.IsNotFound(err) {
			h.logOwnershipViolation(ctx, "deployment not found", agent, deploymentID, releaseID)
			return nil, nil, apperrors.Forbidden("deployment not found or access denied")
		}
		return nil, nil, err
	}
	if deployment.OrgID != agent.OrganizationID {
		h.logOwnershipViolation(ctx, "org_id mismatch", agent, deploymentID, releaseID)
		return nil, nil, apperrors.Forbidden("deployment not found or access denied")
	}
	if deployment.ClusterID != agent.ClusterID {
		h.logOwnershipViolation(ctx, "cluster_id mismatch", agent, deploymentID, releaseID)
		return nil, nil, apperrors.Forbidden("deployment not assigned to this cluster")
	}

	release, err := releases.GetByID(ctx, releaseID)
	if err != nil {
		if database.IsNotFound(err) {
			h.logOwnershipViolation(ctx, "release not found", agent, deploymentID, releaseID)
			return nil, nil, apperrors.Forbidden("release not found or access denied")
		}
		return nil, nil, err
	}
	if release.DeploymentID != deploymentID {
		h.logOwnershipViolation(ctx, "release.deployment_id mismatch", agent, deploymentID, releaseID)
		return nil, nil, apperrors.Forbidden("release does not belong to this deployment")
	}
	if release.OrgID != agent.OrganizationID {
		h.logOwnershipViolation(ctx, "release.org_id mismatch", agent, deploymentID, releaseID)
		return nil, nil, apperrors.Forbidden("release not found or access denied")
	}

	return deployment, release, nil
}
