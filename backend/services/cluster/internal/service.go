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
			return nil, errInvalidToken
		}
		return nil, err
	}

	// Validate token status.
	switch token.Status {
	case TokenStatusUsed:
		return nil, errTokenUsed
	case TokenStatusRevoked:
		return nil, errTokenRevoked
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

	// Return the cluster directly from the registration transaction.
	// NOTE: The previous implementation called GetCluster which requires user
	// authorization. Agents don't have user credentials, so we return the
	// cluster data directly from the transaction instead.
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
