package cluster

import (
	"context"
	"time"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// Cluster domain event types (canonical names, version 1). The Cluster Service
// is the single owner of the "cluster.*" namespace.
const (
	EventClusterCreated               = "cluster.created"
	EventRegistrationTokenCreated     = "cluster.registration.token.created"
	EventClusterRegistered            = "cluster.registered"
	EventClusterHeartbeatReceived     = "cluster.heartbeat.received"
	EventClusterDisconnected          = "cluster.disconnected"
	EventClusterDeleted               = "cluster.deleted"

	eventVersion = 1
)

// Event payloads carry domain facts only. Envelope metadata (eventId,
// occurredAt, correlationId, traceparent, actor, orgId) is never duplicated.

type clusterCreatedPayload struct {
	ClusterID     string `json:"clusterId"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	CloudProvider string `json:"cloudProvider,omitempty"`
	Region        string `json:"region,omitempty"`
	CreatedBy     string `json:"createdBy,omitempty"`
}

type tokenCreatedPayload struct {
	ClusterID   string    `json:"clusterId"`
	TokenID     string    `json:"tokenId"`
	ExpiresAt   time.Time `json:"expiresAt"`
	DeliveryRef string    `json:"deliveryRef"`
}

type clusterRegisteredPayload struct {
	ClusterID         string `json:"clusterId"`
	AgentID           string `json:"agentId"`
	KubernetesVersion string `json:"kubernetesVersion"`
	NodeCount         int    `json:"nodeCount"`
	CloudProvider     string `json:"cloudProvider,omitempty"`
	Region            string `json:"region,omitempty"`
}

type heartbeatReceivedPayload struct {
	ClusterID          string `json:"clusterId"`
	AgentID            string `json:"agentId"`
	KubernetesVersion  string `json:"kubernetesVersion,omitempty"`
	NodeCount          int    `json:"nodeCount,omitempty"`
	APIServerHealthy   bool   `json:"apiServerHealthy"`
}

type clusterDisconnectedPayload struct {
	ClusterID          string    `json:"clusterId"`
	LastHeartbeatAt    time.Time `json:"lastHeartbeatAt"`
	DisconnectedAfter  string    `json:"disconnectedAfter"` // e.g., "5m"
}

type clusterDeletedPayload struct {
	ClusterID string `json:"clusterId"`
	DeletedBy string `json:"deletedBy"`
}

// enqueue builds an envelope and writes it to the transactional outbox within
// the caller's transaction.
func (s *Service) enqueue(ctx context.Context, eventType, orgID string, payload any, opts ...events.Option) error {
	e, err := events.New(eventType, eventVersion, orgID, payload, opts...)
	if err != nil {
		return err
	}
	return s.outbox.Enqueue(ctx, e)
}
