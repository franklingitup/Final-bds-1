package cluster

import (
	"context"
	"testing"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// TestValidateCluster_ReconnectAcrossStatuses is the regression test for the
// production bug where a cluster marked "disconnected" (missed heartbeats,
// control-plane restart, network partition) could never reconnect: the agent
// auth middleware rejected every non-connected status with 403 BEFORE
// RecordHeartbeat could flip it back to connected. A registered cluster in any
// live status must authenticate so its next heartbeat restores it.
func TestValidateCluster_ReconnectAcrossStatuses(t *testing.T) {
	const (
		clusterID = "cluster-1"
		orgID     = "org-1"
		agentID   = "agent-1"
	)

	agent := agentID
	seed := func(status string, withAgent bool) *fakeClusterStore {
		store := newFakeClusterStore()
		c := &Cluster{Name: "c", Slug: "c", Status: status}
		c.ID = clusterID
		c.OrgID = orgID
		if withAgent {
			c.AgentID = &agent
		}
		store.clusters[clusterID] = c
		return store
	}

	tests := []struct {
		name      string
		status    string
		withAgent bool
		wantOK    bool
		wantCode  string // apperrors code when !wantOK
	}{
		{name: "connected", status: StatusConnected, withAgent: true, wantOK: true},
		{name: "disconnected reconnects", status: StatusDisconnected, withAgent: true, wantOK: true},
		{name: "registering", status: StatusRegistering, withAgent: true, wantOK: true},
		{name: "pending no agent", status: StatusPending, withAgent: false, wantOK: false, wantCode: apperrors.CodeForbidden},
		{name: "deleted rejected", status: StatusDeleted, withAgent: true, wantOK: false, wantCode: apperrors.CodeForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewClusterValidator(seed(tt.status, tt.withAgent))
			gotOrg, err := v.ValidateCluster(context.Background(), clusterID, agentID)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("ValidateCluster(%s) unexpected error: %v", tt.status, err)
				}
				if gotOrg != orgID {
					t.Errorf("orgID = %q, want %q", gotOrg, orgID)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateCluster(%s) expected error, got nil", tt.status)
			}
			if code := apperrors.From(err).Code; code != tt.wantCode {
				t.Errorf("error code = %q, want %q", code, tt.wantCode)
			}
		})
	}
}

// TestValidateCluster_AgentMismatch verifies a wrong agent ID is rejected even
// for a registered, connected cluster (prevents agent impersonation).
func TestValidateCluster_AgentMismatch(t *testing.T) {
	store := newFakeClusterStore()
	other := "agent-real"
	c := &Cluster{Name: "c", Slug: "c", Status: StatusConnected}
	c.ID = "cluster-1"
	c.OrgID = "org-1"
	c.AgentID = &other
	store.clusters["cluster-1"] = c

	v := NewClusterValidator(store)
	if _, err := v.ValidateCluster(context.Background(), "cluster-1", "agent-impostor"); err == nil {
		t.Fatal("expected rejection for mismatched agent ID")
	} else if code := apperrors.From(err).Code; code != apperrors.CodeUnauthenticated {
		t.Errorf("error code = %q, want %q", code, apperrors.CodeUnauthenticated)
	}
}
