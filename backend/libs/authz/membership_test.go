package authz

import (
	"context"
	"testing"

	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/errors"
)

// fakeTenantRunner implements TenantRunner for testing.
type fakeTenantRunner struct{}

func (f *fakeTenantRunner) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	return fn(ctx)
}

// fakeOrgMemberStore implements OrgMemberStore for testing.
type fakeOrgMemberStore struct {
	members map[string]*OrgMember // userID -> member
}

func newFakeOrgMemberStore() *fakeOrgMemberStore {
	return &fakeOrgMemberStore{members: make(map[string]*OrgMember)}
}

func (f *fakeOrgMemberStore) GetOrgMember(ctx context.Context, userID string) (*OrgMember, error) {
	m, ok := f.members[userID]
	if !ok {
		return nil, errors.NotFound("member not found")
	}
	return m, nil
}

func (f *fakeOrgMemberStore) addMember(userID, orgID string, role OrgRole, status string) {
	f.members[userID] = &OrgMember{
		OrgID:  orgID,
		UserID: userID,
		Role:   role,
		Status: status,
	}
}

func TestAuthorizeOrgMember_Success(t *testing.T) {
	store := newFakeOrgMemberStore()
	store.addMember("user-1", "org-1", OrgAdmin, "active")

	svc := NewAuthorizationService(&fakeTenantRunner{}, store, nil)

	role, err := svc.AuthorizeOrgMember(context.Background(), "org-1", "user-1", ActionManageClusters)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if role != OrgAdmin {
		t.Errorf("expected OrgAdmin, got %v", role)
	}
}

func TestAuthorizeOrgMember_NotMember(t *testing.T) {
	store := newFakeOrgMemberStore()
	// No member added

	svc := NewAuthorizationService(&fakeTenantRunner{}, store, nil)

	_, err := svc.AuthorizeOrgMember(context.Background(), "org-1", "user-1", ActionManageClusters)
	if err == nil {
		t.Fatal("expected error for non-member")
	}
	if errors.From(err).Code != errors.CodeForbidden {
		t.Errorf("expected forbidden error, got: %v", err)
	}
}

func TestAuthorizeOrgMember_SuspendedMember(t *testing.T) {
	store := newFakeOrgMemberStore()
	store.addMember("user-1", "org-1", OrgAdmin, "suspended")

	svc := NewAuthorizationService(&fakeTenantRunner{}, store, nil)

	_, err := svc.AuthorizeOrgMember(context.Background(), "org-1", "user-1", ActionManageClusters)
	if err == nil {
		t.Fatal("expected error for suspended member")
	}
	if errors.From(err).Code != errors.CodeForbidden {
		t.Errorf("expected forbidden error, got: %v", err)
	}
}

func TestAuthorizeOrgMember_InsufficientRole(t *testing.T) {
	store := newFakeOrgMemberStore()
	store.addMember("user-1", "org-1", RoleMember, "active") // RoleMember cannot ManageClusters

	svc := NewAuthorizationService(&fakeTenantRunner{}, store, nil)

	_, err := svc.AuthorizeOrgMember(context.Background(), "org-1", "user-1", ActionManageClusters)
	if err == nil {
		t.Fatal("expected error for insufficient role")
	}
	if errors.From(err).Code != errors.CodeForbidden {
		t.Errorf("expected forbidden error, got: %v", err)
	}
}

func TestAuthorizeOrgMember_EmptyOrgID(t *testing.T) {
	store := newFakeOrgMemberStore()
	svc := NewAuthorizationService(&fakeTenantRunner{}, store, nil)

	_, err := svc.AuthorizeOrgMember(context.Background(), "", "user-1", ActionManageClusters)
	if err == nil {
		t.Fatal("expected error for empty orgID")
	}
}

func TestAuthorizeOrgMember_EmptyUserID(t *testing.T) {
	store := newFakeOrgMemberStore()
	svc := NewAuthorizationService(&fakeTenantRunner{}, store, nil)

	_, err := svc.AuthorizeOrgMember(context.Background(), "org-1", "", ActionManageClusters)
	if err == nil {
		t.Fatal("expected error for empty userID")
	}
}

func TestAuthorizeOrgRead_Success(t *testing.T) {
	store := newFakeOrgMemberStore()
	store.addMember("user-1", "org-1", OrgAuditor, "active") // Even auditor can read

	svc := NewAuthorizationService(&fakeTenantRunner{}, store, nil)

	role, err := svc.AuthorizeOrgRead(context.Background(), "org-1", "user-1")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if role != OrgAuditor {
		t.Errorf("expected OrgAuditor, got %v", role)
	}
}

func TestAuthorizeOrgRead_NotMember(t *testing.T) {
	store := newFakeOrgMemberStore()
	svc := NewAuthorizationService(&fakeTenantRunner{}, store, nil)

	_, err := svc.AuthorizeOrgRead(context.Background(), "org-1", "user-1")
	if err == nil {
		t.Fatal("expected error for non-member")
	}
}

func TestAuthorizeOrgMember_AllRolesMatrix(t *testing.T) {
	tests := []struct {
		name    string
		role    OrgRole
		action  Action
		wantErr bool
	}{
		// OrgOwner can do everything
		{"owner can manage org", OrgOwner, ActionManageOrg, false},
		{"owner can manage clusters", OrgOwner, ActionManageClusters, false},
		{"owner can read audit", OrgOwner, ActionReadAudit, false},

		// OrgAdmin can manage most things but not org itself
		{"admin cannot manage org", OrgAdmin, ActionManageOrg, true},
		{"admin can manage clusters", OrgAdmin, ActionManageClusters, false},
		{"admin can manage members", OrgAdmin, ActionManageMembers, false},
		{"admin can read audit", OrgAdmin, ActionReadAudit, false},

		// RoleMember has read access only
		{"member cannot manage clusters", RoleMember, ActionManageClusters, true},
		{"member can read clusters", RoleMember, ActionReadClusters, false},
		{"member can read deployments", RoleMember, ActionReadDeployment, false},

		// OrgAuditor has audit read access
		{"auditor can read audit", OrgAuditor, ActionReadAudit, false},
		{"auditor cannot manage clusters", OrgAuditor, ActionManageClusters, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeOrgMemberStore()
			store.addMember("user-1", "org-1", tt.role, "active")

			svc := NewAuthorizationService(&fakeTenantRunner{}, store, nil)

			_, err := svc.AuthorizeOrgMember(context.Background(), "org-1", "user-1", tt.action)
			if (err != nil) != tt.wantErr {
				t.Errorf("AuthorizeOrgMember() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
