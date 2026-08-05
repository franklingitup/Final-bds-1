package auth

import (
	"context"
	"testing"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/errors"
)

// TestCreateServiceAccount_AuthorizationRequired verifies that service account
// creation requires org membership with admin privileges.
func TestCreateServiceAccount_AuthorizationRequired(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		orgID     string
		userID    string
		wantErr   bool
		errorCode errors.Code
	}{
		{
			name:      "missing org ID",
			orgID:     "",
			userID:    "user-123",
			wantErr:   true,
			errorCode: errors.CodeValidation,
		},
		{
			name:      "missing user ID",
			orgID:     "org-123",
			userID:    "",
			wantErr:   true,
			errorCode: errors.CodeValidation,
		},
		{
			name:      "non-member user",
			orgID:     "org-123",
			userID:    "non-member",
			wantErr:   true,
			errorCode: errors.CodeForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{
				authSvc: authz.NewAuthorizationService(
					&fakeTenantRunner{},
					&fakeOrgMemberStore{},
					nil,
				),
			}

			_, err := svc.CreateServiceAccount(ctx, tt.orgID, tt.userID, CreateServiceAccountRequest{
				Name: "test-sa",
			})

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateServiceAccount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errorCode != "" {
				appErr := errors.From(err)
				if appErr.Code != tt.errorCode {
					t.Errorf("CreateServiceAccount() error code = %v, want %v", appErr.Code, tt.errorCode)
				}
			}
		})
	}
}

// TestDeleteServiceAccount_AuthorizationRequired verifies that deleting a
// service account requires org membership with admin privileges.
func TestDeleteServiceAccount_AuthorizationRequired(t *testing.T) {
	ctx := context.Background()

	svc := &Service{
		authSvc: authz.NewAuthorizationService(
			&fakeTenantRunner{},
			&fakeOrgMemberStore{},
			nil,
		),
	}

	err := svc.DeleteServiceAccount(ctx, "org-123", "non-member", "sa-123")
	if err == nil {
		t.Error("DeleteServiceAccount() should fail for non-member")
	}

	appErr := errors.From(err)
	if appErr.Code != errors.CodeForbidden {
		t.Errorf("DeleteServiceAccount() error code = %v, want %v", appErr.Code, errors.CodeForbidden)
	}
}

// TestListServiceAccounts_AuthorizationRequired verifies that listing service
// accounts requires org membership.
func TestListServiceAccounts_AuthorizationRequired(t *testing.T) {
	ctx := context.Background()

	svc := &Service{
		authSvc: authz.NewAuthorizationService(
			&fakeTenantRunner{},
			&fakeOrgMemberStore{},
			nil,
		),
	}

	_, err := svc.ListServiceAccounts(ctx, "org-123", "non-member", database.PageRequest{})
	if err == nil {
		t.Error("ListServiceAccounts() should fail for non-member")
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
	return nil, errors.NotFound("member not found")
}
