package authz

import (
	"context"
	"testing"

	"github.com/bdsplatform/platform/backend/libs/errors"
)

func TestAuthorize_TenantIsolation(t *testing.T) {
	a := NewPolicyAuthorizer()
	p := Principal{UserID: "u", OrgID: "org-A", OrgRoles: []OrgRole{OrgOwner}}

	err := a.Authorize(context.Background(), p, AccessRequest{Action: ActionReadLogs, OrgID: "org-B"})
	if err == nil {
		t.Fatal("expected cross-tenant denial")
	}
	if errors.From(err).Code != errors.CodeForbidden {
		t.Errorf("expected FORBIDDEN, got %v", err)
	}
}

func TestAuthorize_OrgRoleGrants(t *testing.T) {
	a := NewPolicyAuthorizer()
	owner := Principal{OrgID: "o", OrgRoles: []OrgRole{OrgOwner}}
	if err := a.Authorize(context.Background(), owner, AccessRequest{Action: ActionManageOrg, OrgID: "o"}); err != nil {
		t.Errorf("owner should manage org: %v", err)
	}

	auditor := Principal{OrgID: "o", OrgRoles: []OrgRole{OrgAuditor}}
	if err := a.Authorize(context.Background(), auditor, AccessRequest{Action: ActionReadAudit, OrgID: "o"}); err != nil {
		t.Errorf("auditor should read audit: %v", err)
	}
	if err := a.Authorize(context.Background(), auditor, AccessRequest{Action: ActionManageSecrets, OrgID: "o"}); err == nil {
		t.Error("auditor must not manage secrets")
	}
}

func TestAuthorize_ProjectRoleGrants(t *testing.T) {
	a := NewPolicyAuthorizer()
	dev := Principal{
		OrgID:        "o",
		OrgRoles:     []OrgRole{OrgMember},
		ProjectRoles: map[string]ProjectRole{"p1": ProjectDeveloper},
	}

	if err := a.Authorize(context.Background(), dev, AccessRequest{Action: ActionDeploy, OrgID: "o", ProjectID: "p1"}); err != nil {
		t.Errorf("developer should deploy in p1: %v", err)
	}
	// Developer cannot manage secrets.
	if err := a.Authorize(context.Background(), dev, AccessRequest{Action: ActionManageSecrets, OrgID: "o", ProjectID: "p1"}); err == nil {
		t.Error("developer must not manage secrets")
	}
	// No role in p2.
	if err := a.Authorize(context.Background(), dev, AccessRequest{Action: ActionDeploy, OrgID: "o", ProjectID: "p2"}); err == nil {
		t.Error("developer must not deploy in p2")
	}
}

func TestContextHelpers(t *testing.T) {
	ctx := WithOrg(WithPrincipal(context.Background(), Principal{UserID: "u", OrgID: "o"}), "o")
	if OrgFromContext(ctx) != "o" {
		t.Error("org not stored")
	}
	p, ok := PrincipalFromContext(ctx)
	if !ok || p.UserID != "u" {
		t.Error("principal not stored")
	}
}
