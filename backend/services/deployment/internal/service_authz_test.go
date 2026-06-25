package deployment

import (
	"context"
	"testing"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/errors"
)

// TestMarkDeploymentStarted_AuthorizationRequired verifies that marking a
// deployment as started requires org membership with deployment privileges.
func TestMarkDeploymentStarted_AuthorizationRequired(t *testing.T) {
	ctx := context.Background()

	svc := &Service{
		authSvc: authz.NewAuthorizationService(
			&fakeTenantRunner{},
			&fakeOrgMemberStore{},
			nil,
		),
	}

	err := svc.MarkDeploymentStarted(ctx, "org-123", "non-member", "dep-123", "rel-123")
	if err == nil {
		t.Error("MarkDeploymentStarted() should fail for non-member")
	}

	appErr := errors.From(err)
	if appErr.Code != errors.CodeForbidden {
		t.Errorf("MarkDeploymentStarted() error code = %v, want %v", appErr.Code, errors.CodeForbidden)
	}
}

// TestMarkDeploymentSucceeded_AuthorizationRequired verifies that marking a
// deployment as succeeded requires org membership with deployment privileges.
func TestMarkDeploymentSucceeded_AuthorizationRequired(t *testing.T) {
	ctx := context.Background()

	svc := &Service{
		authSvc: authz.NewAuthorizationService(
			&fakeTenantRunner{},
			&fakeOrgMemberStore{},
			nil,
		),
	}

	err := svc.MarkDeploymentSucceeded(ctx, "org-123", "non-member", "dep-123", "rel-123", 1)
	if err == nil {
		t.Error("MarkDeploymentSucceeded() should fail for non-member")
	}

	appErr := errors.From(err)
	if appErr.Code != errors.CodeForbidden {
		t.Errorf("MarkDeploymentSucceeded() error code = %v, want %v", appErr.Code, errors.CodeForbidden)
	}
}

// TestMarkDeploymentFailed_AuthorizationRequired verifies that marking a
// deployment as failed requires org membership with deployment privileges.
func TestMarkDeploymentFailed_AuthorizationRequired(t *testing.T) {
	ctx := context.Background()

	svc := &Service{
		authSvc: authz.NewAuthorizationService(
			&fakeTenantRunner{},
			&fakeOrgMemberStore{},
			nil,
		),
	}

	err := svc.MarkDeploymentFailed(ctx, "org-123", "non-member", "dep-123", "rel-123", "error")
	if err == nil {
		t.Error("MarkDeploymentFailed() should fail for non-member")
	}

	appErr := errors.From(err)
	if appErr.Code != errors.CodeForbidden {
		t.Errorf("MarkDeploymentFailed() error code = %v, want %v", appErr.Code, errors.CodeForbidden)
	}
}

// TestDeleteDeployment_AuthorizationRequired verifies that deleting a
// deployment requires org membership with deployment privileges.
func TestDeleteDeployment_AuthorizationRequired(t *testing.T) {
	ctx := context.Background()

	svc := &Service{
		authSvc: authz.NewAuthorizationService(
			&fakeTenantRunner{},
			&fakeOrgMemberStore{},
			nil,
		),
	}

	err := svc.DeleteDeployment(ctx, "org-123", "non-member", "dep-123")
	if err == nil {
		t.Error("DeleteDeployment() should fail for non-member")
	}

	appErr := errors.From(err)
	if appErr.Code != errors.CodeForbidden {
		t.Errorf("DeleteDeployment() error code = %v, want %v", appErr.Code, errors.CodeForbidden)
	}
}

// fakeTenantRunner implements TenantRunner for testing.
type fakeTenantRunner struct{}

func (f *fakeTenantRunner) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	return fn(ctx)
}

// fakeOrgMemberStore returns not found for all users (simulates non-membership).
type fakeOrgMemberStore struct{}

func (f *fakeOrgMemberStore) GetOrgMember(ctx context.Context, userID string) (*authz.OrgMember, error) {
	return nil, database.ErrNotFound
}
