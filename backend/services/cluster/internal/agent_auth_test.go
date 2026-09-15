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
		wantCode  apperrors.Code // apperrors code when !wantOK
	}{
		{name: "connected", status: StatusConnected, withAgent: true, wantOK: true},
		{name: "disconnected reconnects", status: StatusDisconnected, withAgent: true, wantOK: true},
		{name: "registering", status: StatusRegistering, withAgent: true, wantOK: true},
		{name: "pending no agent", status: StatusPending, withAgent: false, wantOK: false, wantCode: apperrors.CodeForbidden},
		{name: "deleted rejected", status: StatusDeleted, withAgent: true, wantOK: false, wantCode: apperrors.CodeForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewClusterValidator(seed(tt.status, tt.withAgent), nil)
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

	v := NewClusterValidator(store, nil)
	if _, err := v.ValidateCluster(context.Background(), "cluster-1", "agent-impostor"); err == nil {
		t.Fatal("expected rejection for mismatched agent ID")
	} else if code := apperrors.From(err).Code; code != apperrors.CodeUnauthenticated {
		t.Errorf("error code = %q, want %q", code, apperrors.CodeUnauthenticated)
	}
}

// TestValidateCluster_UniformMessages confirms client-facing messages are
// identical within each HTTP status bucket (anti-enumeration) while 401 and
// 403 remain distinguishable by code for agent reconnect semantics.
func TestValidateCluster_UniformMessages(t *testing.T) {
	const (
		clusterID = "cluster-1"
		orgID     = "org-1"
		agentID   = "agent-1"
	)
	agent := agentID

	notFoundStore := newFakeClusterStore()
	vNotFound := NewClusterValidator(notFoundStore, nil)
	_, errNotFound := vNotFound.ValidateCluster(context.Background(), clusterID, agentID)
	if errNotFound == nil {
		t.Fatal("expected error for missing cluster")
	}

	mismatchStore := newFakeClusterStore()
	other := "agent-real"
	cMismatch := &Cluster{Name: "c", Slug: "c", Status: StatusConnected}
	cMismatch.ID = clusterID
	cMismatch.OrgID = orgID
	cMismatch.AgentID = &other
	mismatchStore.clusters[clusterID] = cMismatch
	vMismatch := NewClusterValidator(mismatchStore, nil)
	_, errMismatch := vMismatch.ValidateCluster(context.Background(), clusterID, agentID)
	if errMismatch == nil {
		t.Fatal("expected error for agent mismatch")
	}

	deletedStore := newFakeClusterStore()
	cDeleted := &Cluster{Name: "c", Slug: "c", Status: StatusDeleted}
	cDeleted.ID = clusterID
	cDeleted.OrgID = orgID
	cDeleted.AgentID = &agent
	deletedStore.clusters[clusterID] = cDeleted
	vDeleted := NewClusterValidator(deletedStore, nil)
	_, errDeleted := vDeleted.ValidateCluster(context.Background(), clusterID, agentID)
	if errDeleted == nil {
		t.Fatal("expected error for deleted cluster")
	}

	unregStore := newFakeClusterStore()
	cUnreg := &Cluster{Name: "c", Slug: "c", Status: StatusPending}
	cUnreg.ID = clusterID
	cUnreg.OrgID = orgID
	unregStore.clusters[clusterID] = cUnreg
	vUnreg := NewClusterValidator(unregStore, nil)
	_, errUnreg := vUnreg.ValidateCluster(context.Background(), clusterID, agentID)
	if errUnreg == nil {
		t.Fatal("expected error for unregistered cluster")
	}

	msgNotFound := apperrors.From(errNotFound).Message
	msgMismatch := apperrors.From(errMismatch).Message
	msgDeleted := apperrors.From(errDeleted).Message
	msgUnreg := apperrors.From(errUnreg).Message

	if msgNotFound != msgMismatch {
		t.Errorf("401 messages differ: notFound=%q mismatch=%q", msgNotFound, msgMismatch)
	}
	if msgNotFound != "authentication failed" {
		t.Errorf("401 message = %q, want %q", msgNotFound, "authentication failed")
	}
	if msgDeleted != msgUnreg {
		t.Errorf("403 messages differ: deleted=%q unregistered=%q", msgDeleted, msgUnreg)
	}
	if msgDeleted != "cluster not available" {
		t.Errorf("403 message = %q, want %q", msgDeleted, "cluster not available")
	}
	if msgNotFound == msgDeleted {
		t.Error("401 and 403 buckets must remain distinguishable by message as well as code")
	}
	if apperrors.From(errNotFound).Code != apperrors.CodeUnauthenticated {
		t.Errorf("notFound code = %q, want UNAUTHENTICATED", apperrors.From(errNotFound).Code)
	}
	if apperrors.From(errMismatch).Code != apperrors.CodeUnauthenticated {
		t.Errorf("mismatch code = %q, want UNAUTHENTICATED", apperrors.From(errMismatch).Code)
	}
	if apperrors.From(errDeleted).Code != apperrors.CodeForbidden {
		t.Errorf("deleted code = %q, want FORBIDDEN", apperrors.From(errDeleted).Code)
	}
	if apperrors.From(errUnreg).Code != apperrors.CodeForbidden {
		t.Errorf("unregistered code = %q, want FORBIDDEN", apperrors.From(errUnreg).Code)
	}
}
