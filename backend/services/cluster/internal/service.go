package cluster

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// DefaultTokenTTL is the default registration token validity period.
const DefaultTokenTTL = 24 * time.Hour

// HeartbeatTimeout is how long without a heartbeat before marking disconnected.
const HeartbeatTimeout = 5 * time.Minute

// Deps are the cluster service dependencies.
type Deps struct {
	Clusters   ClusterStore
	Tokens     TokenStore
	Heartbeats HeartbeatStore
	OrgMembers authz.OrgMemberStore // For org membership authorization
	Outbox     events.Outbox
	Tenant     TenantRunner
	Notifier   Notifier
	Logger     *slog.Logger
	Now        func() time.Time
}

// Service implements the cluster domain logic.
type Service struct {
	clusters   ClusterStore
	tokens     TokenStore
	heartbeats HeartbeatStore
	orgMembers authz.OrgMemberStore
	outbox     events.Outbox
	tenant     TenantRunner
	authSvc    *authz.AuthorizationService
	notifier   Notifier
	log        *slog.Logger
	now        func() time.Time
}

// NewService wires a cluster Service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Notifier == nil {
		d.Notifier = noopNotifier{}
	}
	return &Service{
		clusters:   d.Clusters,
		tokens:     d.Tokens,
		heartbeats: d.Heartbeats,
		orgMembers: d.OrgMembers,
		outbox:     d.Outbox,
		tenant:     d.Tenant,
		authSvc:    authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		notifier:   d.Notifier,
		log:        d.Logger,
		now:        d.Now,
	}
}

// ----------------------------------------------------------------------------
// Cluster CRUD
// ----------------------------------------------------------------------------

// CreateCluster creates a new cluster in pending status.
// SECURITY: Requires org admin role to create clusters.
func (s *Service) CreateCluster(ctx context.Context, orgID, userID string, req CreateClusterRequest) (*Cluster, error) {
	// SECURITY: Verify caller is org member with cluster management privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageClusters); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	if err := validateName(name); err != nil {
		return nil, err
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if !slugPattern.MatchString(slug) {
		return nil, apperrors.Validation("slug must be 2-64 chars of lowercase letters, digits, or hyphens")
	}

	c := &Cluster{
		Name:   name,
		Slug:   slug,
		Status: StatusPending,
	}
	c.OrgID = orgID
	c.CreatedBy = &userID

	if req.Description != nil {
		d := strings.TrimSpace(*req.Description)
		c.Description = &d
	}
	if req.CloudProvider != nil {
		c.CloudProvider = req.CloudProvider
	}
	if req.Region != nil {
		c.Region = req.Region
	}
	c.Labels = marshalLabels(req.Labels)

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.clusters.Create(ctx, c); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errSlugTaken
			}
			return err
		}
		return s.enqueue(ctx, EventClusterCreated, orgID, clusterCreatedPayload{
			ClusterID:     c.ID,
			Name:          c.Name,
			Slug:          c.Slug,
			CloudProvider: deref(c.CloudProvider),
			Region:        deref(c.Region),
			CreatedBy:     userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "cluster", ID: c.ID}))
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetCluster returns a cluster by ID.
// SECURITY: Requires org membership to read clusters.
func (s *Service) GetCluster(ctx context.Context, orgID, userID, clusterID string) (*Cluster, error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var c *Cluster
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		c, err = s.clusters.GetByID(ctx, clusterID)
		return err
	})
	return c, err
}

// ListClusters returns a paginated list of clusters.
// SECURITY: Requires org membership to list clusters.
func (s *Service) ListClusters(ctx context.Context, orgID, userID string, page database.PageRequest, status string) (database.Page[Cluster], error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[Cluster]{}, err
	}

	var out database.Page[Cluster]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.clusters.List(ctx, page, status)
		return err
	})
	return out, err
}

// UpdateCluster updates mutable cluster fields.
// SECURITY: Requires org admin role to update clusters.
func (s *Service) UpdateCluster(ctx context.Context, orgID, userID, clusterID string, req UpdateClusterRequest) (*Cluster, error) {
	// SECURITY: Verify caller has cluster management privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageClusters); err != nil {
		return nil, err
	}

	var c *Cluster
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		c, err = s.clusters.GetByID(ctx, clusterID)
		if err != nil {
			return err
		}
		if c.Status == StatusDeleted {
			return errClusterNotFound
		}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if err := validateName(name); err != nil {
				return err
			}
			c.Name = name
		}
		if req.Description != nil {
			d := strings.TrimSpace(*req.Description)
			c.Description = &d
		}
		if req.Labels != nil {
			c.Labels = marshalLabels(req.Labels)
		}
		return s.clusters.Update(ctx, c)
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// DeleteCluster soft-deletes a cluster.
// SECURITY: Requires org admin role to delete clusters.
func (s *Service) DeleteCluster(ctx context.Context, orgID, userID, clusterID string) error {
	// SECURITY: Verify caller has cluster management privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageClusters); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		c, err := s.clusters.GetByID(ctx, clusterID)
		if err != nil {
			return err
		}
		if c.Status == StatusDeleted {
			return nil // Idempotent.
		}
		if err := s.clusters.Delete(ctx, clusterID); err != nil {
			return err
		}
		return s.enqueue(ctx, EventClusterDeleted, orgID, clusterDeletedPayload{
			ClusterID: clusterID,
			DeletedBy: userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "cluster", ID: clusterID}))
	})
}

// ----------------------------------------------------------------------------
// Registration Token Management
// ----------------------------------------------------------------------------

// GenerateRegistrationToken creates a new registration token for a cluster.
// SECURITY: Requires org admin role to generate registration tokens.
func (s *Service) GenerateRegistrationToken(ctx context.Context, orgID, userID, clusterID string, req GenerateTokenRequest) (*TokenWithSecret, error) {
	// SECURITY: Verify caller has cluster management privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageClusters); err != nil {
		return nil, err
	}

	ttl := DefaultTokenTTL
	if req.ExpiresIn != "" {
		d, err := time.ParseDuration(req.ExpiresIn)
		if err != nil || d <= 0 {
			return nil, apperrors.Validation("invalid expiresIn duration")
		}
		ttl = d
	}

	plainToken, err := generateRegistrationToken()
	if err != nil {
		return nil, err
	}
	tokenHash := hashToken(plainToken)
	deliveryRef := uuid.NewString()

	t := &RegistrationToken{
		ClusterID: clusterID,
		TokenHash: tokenHash,
		Status:    TokenStatusActive,
		ExpiresAt: s.now().Add(ttl),
	}
	t.OrgID = orgID
	t.CreatedBy = &userID

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Verify cluster exists and is in pending status.
		c, err := s.clusters.GetByID(ctx, clusterID)
		if err != nil {
			return err
		}
		if c.Status != StatusPending && c.Status != StatusDisconnected {
			return errClusterNotPending
		}

		// Revoke any existing active token.
		existing, err := s.tokens.GetActiveByCluster(ctx, clusterID)
		if err == nil && existing != nil {
			_ = s.tokens.Revoke(ctx, existing.ID)
		}

		if err := s.tokens.Create(ctx, t); err != nil {
			return err
		}

		// Deliver token out-of-band (notifier captures for tests).
		s.notifier.DeliverToken(ctx, TokenDelivery{
			ClusterID:   clusterID,
			Token:       plainToken,
			DeliveryRef: deliveryRef,
		})

		return s.enqueue(ctx, EventRegistrationTokenCreated, orgID, tokenCreatedPayload{
			ClusterID:   clusterID,
			TokenID:     t.ID,
			ExpiresAt:   t.ExpiresAt,
			DeliveryRef: deliveryRef,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "cluster_registration_token", ID: t.ID}))
	})
	if err != nil {
		return nil, err
	}

	return &TokenWithSecret{
		RegistrationTokenView: toTokenView(t),
		Token:                 plainToken,
	}, nil
}

// RevokeRegistrationToken revokes an active registration token.
// SECURITY: Requires org admin role to revoke registration tokens.
func (s *Service) RevokeRegistrationToken(ctx context.Context, orgID, userID, clusterID, tokenID string) error {
	// SECURITY: Verify caller has cluster management privileges
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageClusters); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.tokens.Revoke(ctx, tokenID)
	})
}

// ----------------------------------------------------------------------------
// Agent Registration
// ----------------------------------------------------------------------------

// RegisterAgent is called by the cluster agent to complete registration.
// This is a capability-based endpoint: the token itself authorizes the operation.
//
// Registration is idempotent (see docs/adr/0007-agent-registration-recovery.md):
//   - Unknown token                     -> 401 (errInvalidToken)
//   - Revoked token                     -> 401 (errTokenRevoked)
//   - Active, expired token             -> 401 (errTokenExpired)
//   - Active, unexpired token           -> register the cluster (fresh path)
//   - Already-used token + live cluster -> return existing metadata (200)
//
// The already-used arm is what lets a restarted agent that lost its local state
// recover without a new installation token, and never fails on a duplicate
// registration. A used token stays valid for recovery until it is explicitly
// revoked, decoupling recovery from the short bootstrap TTL.
func (s *Service) RegisterAgent(ctx context.Context, req AgentRegisterRequest) (*Cluster, error) {
	if req.Token == "" || req.AgentID == "" {
		return nil, apperrors.Validation("token and agentId are required")
	}

	tokenHash := hashToken(req.Token)
	now := s.now()

	// Look up token (cross-tenant).
	token, err := s.tokens.GetByHash(ctx, tokenHash)
	if err != nil {
		if database.IsNotFound(err) {
			s.log.WarnContext(ctx, "agent registration rejected: unknown token",
				"agent_id", req.AgentID)
			return nil, errInvalidToken
		}
		return nil, err
	}

	// Validate token status. Revocation is the hard kill-switch and always wins.
	switch token.Status {
	case TokenStatusRevoked:
		return nil, errTokenRevoked
	case TokenStatusUsed:
		// Idempotent recovery arm: the token was already consumed. Return the
		// existing cluster (HTTP 200) so the agent can rebuild local state.
		// Intentionally not gated on ExpiresAt: a used token remains a valid
		// recovery credential for its cluster until revoked.
		return s.recoverRegisteredCluster(ctx, token, req.AgentID, "register")
	case TokenStatusExpired:
		return nil, errTokenExpired
	}
	if now.After(token.ExpiresAt) {
		return nil, errTokenExpired
	}

	// Complete registration within tenant scope.
	var c *Cluster
	err = s.tenant.WithTenant(ctx, token.OrgID, func(ctx context.Context) error {
		var err error
		c, err = s.clusters.GetByID(ctx, token.ClusterID)
		if err != nil {
			return err
		}
		if c.Status != StatusPending && c.Status != StatusDisconnected {
			return errClusterNotPending
		}

		// Mark token as used.
		if err := s.tokens.MarkUsed(ctx, token.ID, req.AgentID); err != nil {
			return err
		}

		// Update cluster with agent info.
		var cloudProvider, region *string
		if req.CloudProvider != "" {
			cloudProvider = &req.CloudProvider
		}
		if req.Region != "" {
			region = &req.Region
		}
		if err := s.clusters.RegisterAgent(ctx, c.ID, req.AgentID, req.KubernetesVersion, req.NodeCount, cloudProvider, region, now); err != nil {
			return err
		}

		// Update the cluster object in place with registration data.
		// This avoids a secondary fetch that would require user authorization.
		c.AgentID = &req.AgentID
		c.Status = StatusConnected
		c.KubernetesVersion = &req.KubernetesVersion
		c.NodeCount = &req.NodeCount
		c.RegisteredAt = &now
		c.LastHeartbeatAt = &now
		if cloudProvider != nil {
			c.CloudProvider = cloudProvider
		}
		if region != nil {
			c.Region = region
		}

		return s.enqueue(ctx, EventClusterRegistered, token.OrgID, clusterRegisteredPayload{
			ClusterID:         c.ID,
			AgentID:           req.AgentID,
			KubernetesVersion: req.KubernetesVersion,
			NodeCount:         req.NodeCount,
			CloudProvider:     req.CloudProvider,
			Region:            req.Region,
		}, events.WithActor(events.Actor{Type: "agent", ID: req.AgentID}),
			events.WithResource(events.Resource{Type: "cluster", ID: c.ID}))
	})
	if err != nil {
		return nil, err
	}

	s.log.InfoContext(ctx, "agent registered",
		"cluster_id", c.ID, "org_id", c.OrgID, "agent_id", req.AgentID)

	// Return the cluster directly from the registration transaction.
	// NOTE: The previous implementation called GetCluster which requires user
	// authorization. Agents don't have user credentials, so we return the
	// cluster data directly from the transaction instead.
	return c, nil
}

// RecoverCluster returns the cluster bound to an installation token so an agent
// that lost its local state can rebuild it without a fresh token. It is served
// by GET /v1/agent/recover and authenticated solely by possession of the
// installation token (which maps to exactly one cluster).
//
// Active and already-used tokens are both accepted (both bind to a cluster);
// revoked and expired-active tokens are rejected. The requesting AgentID is
// advisory (used for audit only) — the authoritative, stable AgentID is always
// taken from the cluster record so identity never changes on recovery.
func (s *Service) RecoverCluster(ctx context.Context, plainToken, requestAgentID string) (*Cluster, error) {
	if plainToken == "" {
		return nil, errInvalidToken
	}

	token, err := s.tokens.GetByHash(ctx, hashToken(plainToken))
	if err != nil {
		if database.IsNotFound(err) {
			return nil, errInvalidToken
		}
		return nil, err
	}

	switch token.Status {
	case TokenStatusRevoked:
		return nil, errTokenRevoked
	case TokenStatusUsed:
		// A consumed token stays valid for recovery until revoked.
		return s.recoverRegisteredCluster(ctx, token, requestAgentID, "recover")
	case TokenStatusExpired:
		return nil, errTokenExpired
	}
	if s.now().After(token.ExpiresAt) {
		return nil, errTokenExpired
	}
	// Active, unexpired token: only meaningful to recover once the cluster has
	// actually registered; recoverRegisteredCluster returns errClusterNotFound
	// otherwise so the agent falls back to a fresh registration.
	return s.recoverRegisteredCluster(ctx, token, requestAgentID, "recover")
}

// recoverRegisteredCluster loads the cluster a token is bound to and returns its
// current metadata without mutating the stored agent identity. This is the
// shared, idempotent core of both RegisterAgent (used-token arm) and
// RecoverCluster. It never overwrites agent_id, keeping AgentID/ClusterID stable
// across agent restarts and local-state loss.
func (s *Service) recoverRegisteredCluster(ctx context.Context, token *RegistrationToken, requestAgentID, source string) (*Cluster, error) {
	// Cross-tenant read (bypasses RLS): we authorize by token possession, not by
	// an org-scoped user identity.
	c, err := s.clusters.GetByIDWithoutTenant(ctx, token.ClusterID)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, errClusterNotFound
		}
		return nil, err
	}
	if c.Status == StatusDeleted {
		return nil, errClusterNotFound
	}

	// The authoritative, stable identity is what the control plane recorded.
	stableAgentID := deref(c.AgentID)
	if stableAgentID == "" {
		stableAgentID = deref(token.UsedByAgent)
	}
	if stableAgentID == "" {
		// Token bound to a cluster that has not completed registration yet;
		// there is nothing to recover. The caller should register fresh.
		return nil, errClusterNotFound
	}

	// Best-effort audit of the recovery. It must never cause recovery to fail —
	// the registration/recovery contract is "never fail when the cluster
	// already exists".
	auditErr := s.tenant.WithTenant(ctx, c.OrgID, func(ctx context.Context) error {
		return s.enqueue(ctx, EventClusterRecovered, c.OrgID, clusterRecoveredPayload{
			ClusterID:        c.ID,
			AgentID:          stableAgentID,
			RequestedAgentID: requestAgentID,
			Source:           source,
		}, events.WithActor(events.Actor{Type: "agent", ID: stableAgentID}),
			events.WithResource(events.Resource{Type: "cluster", ID: c.ID}))
	})
	if auditErr != nil {
		s.log.WarnContext(ctx, "failed to audit cluster recovery",
			"cluster_id", c.ID, "error", auditErr)
	}

	s.log.InfoContext(ctx, "agent registration recovered from existing cluster",
		"cluster_id", c.ID, "org_id", c.OrgID, "agent_id", stableAgentID,
		"requested_agent_id", requestAgentID, "source", source)

	return c, nil
}

// ----------------------------------------------------------------------------
// Agent Heartbeat
// ----------------------------------------------------------------------------

// RecordHeartbeat is called periodically by the cluster agent.
func (s *Service) RecordHeartbeat(ctx context.Context, orgID, clusterID string, req AgentHeartbeatRequest) error {
	if req.AgentID == "" {
		return apperrors.Validation("agentId is required")
	}

	now := s.now()

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		c, err := s.clusters.GetByID(ctx, clusterID)
		if err != nil {
			return err
		}

		// Validate agent ID matches.
		if c.AgentID == nil || *c.AgentID != req.AgentID {
			return errAgentMismatch
		}

		// Record heartbeat history.
		h := &Heartbeat{
			OrgID:              orgID,
			ClusterID:          clusterID,
			AgentID:            req.AgentID,
			KubernetesVersion:  &req.KubernetesVersion,
			NodeCount:          &req.NodeCount,
			APIServerHealthy:   req.APIServerHealthy,
			ReceivedAt:         now,
		}
		if err := s.heartbeats.Create(ctx, h); err != nil {
			s.log.WarnContext(ctx, "failed to record heartbeat history", "error", err)
		}

		// Update cluster's last heartbeat.
		if err := s.clusters.UpdateHeartbeat(ctx, clusterID, now, req.KubernetesVersion, req.NodeCount); err != nil {
			return err
		}

		return s.enqueue(ctx, EventClusterHeartbeatReceived, orgID, heartbeatReceivedPayload{
			ClusterID:          clusterID,
			AgentID:            req.AgentID,
			KubernetesVersion:  req.KubernetesVersion,
			NodeCount:          req.NodeCount,
			APIServerHealthy:   req.APIServerHealthy,
		}, events.WithActor(events.Actor{Type: "agent", ID: req.AgentID}),
			events.WithResource(events.Resource{Type: "cluster", ID: clusterID}))
	})
}

// GetHeartbeats returns recent heartbeats for a cluster.
// GetHeartbeats returns the recent heartbeats for a cluster.
// SECURITY: Requires org membership to read heartbeats.
func (s *Service) GetHeartbeats(ctx context.Context, orgID, userID, clusterID string, limit int) ([]Heartbeat, error) {
	// SECURITY: Verify caller is org member
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var out []Heartbeat
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.heartbeats.ListByCluster(ctx, clusterID, limit)
		return err
	})
	return out, err
}

// ----------------------------------------------------------------------------
// Disconnection Detection (called by background job)
// ----------------------------------------------------------------------------

// MarkDisconnected marks clusters that haven't sent heartbeats as disconnected.
// This would be called by a periodic background job.
func (s *Service) MarkDisconnected(ctx context.Context, orgID string, timeout time.Duration) (int, error) {
	if timeout <= 0 {
		timeout = HeartbeatTimeout
	}
	cutoff := s.now().Add(-timeout)
	count := 0

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// List connected clusters with stale heartbeats.
		page, err := s.clusters.List(ctx, database.PageRequest{Limit: 100}, StatusConnected)
		if err != nil {
			return err
		}

		for _, c := range page.Items {
			if c.LastHeartbeatAt != nil && c.LastHeartbeatAt.Before(cutoff) {
				if err := s.clusters.UpdateStatus(ctx, c.ID, StatusDisconnected); err != nil {
					s.log.WarnContext(ctx, "failed to mark cluster disconnected",
						"cluster_id", c.ID, "error", err)
					continue
				}

				_ = s.enqueue(ctx, EventClusterDisconnected, orgID, clusterDisconnectedPayload{
					ClusterID:         c.ID,
					LastHeartbeatAt:   *c.LastHeartbeatAt,
					DisconnectedAfter: timeout.String(),
				}, events.WithResource(events.Resource{Type: "cluster", ID: c.ID}))

				count++
			}
		}
		return nil
	})

	return count, err
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func validateName(name string) error {
	if l := len(name); l < 2 || l > 64 {
		return apperrors.Validation("name must be between 2 and 64 characters")
	}
	return nil
}
