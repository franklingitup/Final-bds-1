package tenant

import (
	"testing"

	"github.com/bdsplatform/platform/backend/libs/authz"
)

func TestRole_Valid(t *testing.T) {
	for _, r := range []Role{RoleOwner, RoleAdmin, RoleDeveloper, RoleViewer} {
		if !r.Valid() {
			t.Errorf("%s should be valid", r)
		}
	}
	if Role("superadmin").Valid() {
		t.Error("unknown role should be invalid")
	}
}

func TestRole_AssignableByInvite(t *testing.T) {
	if RoleOwner.AssignableByInvite() {
		t.Error("owner must not be assignable by invite")
	}
	for _, r := range []Role{RoleAdmin, RoleDeveloper, RoleViewer} {
		if !r.AssignableByInvite() {
			t.Errorf("%s should be assignable by invite", r)
		}
	}
}

func TestRole_toAuthzRole(t *testing.T) {
	cases := map[Role]authz.OrgRole{
		RoleOwner:     authz.OrgOwner,
		RoleAdmin:     authz.OrgAdmin,
		RoleDeveloper: authz.OrgMember,
		RoleViewer:    authz.OrgAuditor,
	}
	for in, want := range cases {
		if got := in.toAuthzRole(); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}
