package cluster

import (
	"encoding/json"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// Cluster statuses track the lifecycle from creation through agent registration.
const (
	StatusPending      = "pending"      // Created, awaiting agent registration.
	StatusRegistering  = "registering"  // Agent initiated registration.
	StatusConnected    = "connected"    // Agent registered and sending heartbeats.
	StatusDisconnected = "disconnected" // Agent stopped sending heartbeats.
	StatusDeleted      = "deleted"      // Soft-deleted.
)

// Cloud providers for inventory tracking.
const (
	ProviderAWS    = "aws"
	ProviderGCP    = "gcp"
	ProviderAzure  = "azure"
	ProviderOnPrem = "on-prem"
	ProviderOther  = "other"
)

// Registration token statuses.
const (
	TokenStatusActive  = "active"
	TokenStatusUsed    = "used"
	TokenStatusRevoked = "revoked"
	TokenStatusExpired = "expired"
)

// Cluster represents a registered Kubernetes cluster.
type Cluster struct {
	database.TenantModel
	Name        string  `db:"name"`
	Slug        string  `db:"slug"`
	Description *string `db:"description"`
	Status      string  `db:"status"`

	// Inventory (populated after agent registration).
	KubernetesVersion *string `db:"kubernetes_version"`
	NodeCount         *int    `db:"node_count"`
	CloudProvider     *string `db:"cloud_provider"`
	Region            *string `db:"region"`

	// Agent connection.
	AgentID         *string    `db:"agent_id"`
	RegisteredAt    *time.Time `db:"registered_at"`
	LastHeartbeatAt *time.Time `db:"last_heartbeat_at"`

	// Metadata.
	Labels    json.RawMessage `db:"labels"`
	CreatedBy *string         `db:"created_by"`
}

// RegistrationToken is a one-time token for agent registration.
type RegistrationToken struct {
	database.TenantModel
	ClusterID   string     `db:"cluster_id"`
	TokenHash   string     `db:"token_hash"`
	Status      string     `db:"status"`
	ExpiresAt   time.Time  `db:"expires_at"`
	UsedAt      *time.Time `db:"used_at"`
	UsedByAgent *string    `db:"used_by_agent"`
	CreatedBy   *string    `db:"created_by"`
}

// Heartbeat records a single heartbeat from an agent.
type Heartbeat struct {
	ID                 string    `db:"id"`
	OrgID              string    `db:"org_id"`
	ClusterID          string    `db:"cluster_id"`
	AgentID            string    `db:"agent_id"`
	KubernetesVersion  *string   `db:"kubernetes_version"`
	NodeCount          *int      `db:"node_count"`
	APIServerHealthy   bool      `db:"api_server_healthy"`
	ReceivedAt         time.Time `db:"received_at"`
}

// ----------------------------------------------------------------------------
// Request / Response DTOs
// ----------------------------------------------------------------------------

type CreateClusterRequest struct {
	Name          string            `json:"name"`
	Slug          string            `json:"slug"`
	Description   *string           `json:"description,omitempty"`
	CloudProvider *string           `json:"cloudProvider,omitempty"`
	Region        *string           `json:"region,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type UpdateClusterRequest struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type GenerateTokenRequest struct {
	ExpiresIn string `json:"expiresIn,omitempty"` // Duration like "24h", "7d".
}

// AgentRegisterRequest is sent by the cluster agent during registration.
type AgentRegisterRequest struct {
	Token             string `json:"token"`
	AgentID           string `json:"agentId"`
	KubernetesVersion string `json:"kubernetesVersion"`
	NodeCount         int    `json:"nodeCount"`
	CloudProvider     string `json:"cloudProvider,omitempty"`
	Region            string `json:"region,omitempty"`
}

// AgentHeartbeatRequest is sent periodically by the cluster agent.
type AgentHeartbeatRequest struct {
	AgentID            string `json:"agentId"`
	KubernetesVersion  string `json:"kubernetesVersion"`
	NodeCount          int    `json:"nodeCount"`
	APIServerHealthy   bool   `json:"apiServerHealthy"`
}

// ----------------------------------------------------------------------------
// View Models
// ----------------------------------------------------------------------------

// ClusterView is the public projection of a cluster.
type ClusterView struct {
	ID                string            `json:"id"`
	OrgID             string            `json:"organizationId"`
	Name              string            `json:"name"`
	Slug              string            `json:"slug"`
	Description       string            `json:"description,omitempty"`
	Status            string            `json:"status"`
	KubernetesVersion string            `json:"kubernetesVersion,omitempty"`
	NodeCount         *int              `json:"nodeCount,omitempty"`
	CloudProvider     string            `json:"cloudProvider,omitempty"`
	Region            string            `json:"region,omitempty"`
	AgentID           string            `json:"agentId,omitempty"`
	RegisteredAt      *time.Time        `json:"registeredAt,omitempty"`
	LastHeartbeatAt   *time.Time        `json:"lastHeartbeatAt,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toClusterView(c *Cluster) ClusterView {
	var labels map[string]string
	if len(c.Labels) > 0 {
		_ = json.Unmarshal(c.Labels, &labels)
	}

	return ClusterView{
		ID:                c.ID,
		OrgID:             c.OrgID,
		Name:              c.Name,
		Slug:              c.Slug,
		Description:       deref(c.Description),
		Status:            c.Status,
		KubernetesVersion: deref(c.KubernetesVersion),
		NodeCount:         c.NodeCount,
		CloudProvider:     deref(c.CloudProvider),
		Region:            deref(c.Region),
		AgentID:           deref(c.AgentID),
		RegisteredAt:      c.RegisteredAt,
		LastHeartbeatAt:   c.LastHeartbeatAt,
		Labels:            labels,
		CreatedAt:         c.CreatedAt,
	}
}

// RegistrationTokenView is the public projection of a registration token.
type RegistrationTokenView struct {
	ID        string    `json:"id"`
	ClusterID string    `json:"clusterId"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

func toTokenView(t *RegistrationToken) RegistrationTokenView {
	return RegistrationTokenView{
		ID:        t.ID,
		ClusterID: t.ClusterID,
		Status:    t.Status,
		ExpiresAt: t.ExpiresAt,
		CreatedAt: t.CreatedAt,
	}
}

// TokenWithSecret is returned only at token creation.
type TokenWithSecret struct {
	RegistrationTokenView
	Token string `json:"token"` // Plaintext token, shown only once.
}

// HeartbeatView is the public projection of a heartbeat.
type HeartbeatView struct {
	ClusterID          string    `json:"clusterId"`
	AgentID            string    `json:"agentId"`
	KubernetesVersion  string    `json:"kubernetesVersion,omitempty"`
	NodeCount          *int      `json:"nodeCount,omitempty"`
	APIServerHealthy   bool      `json:"apiServerHealthy"`
	ReceivedAt         time.Time `json:"receivedAt"`
}

func toHeartbeatView(h *Heartbeat) HeartbeatView {
	return HeartbeatView{
		ClusterID:          h.ClusterID,
		AgentID:            h.AgentID,
		KubernetesVersion:  deref(h.KubernetesVersion),
		NodeCount:          h.NodeCount,
		APIServerHealthy:   h.APIServerHealthy,
		ReceivedAt:         h.ReceivedAt,
	}
}
