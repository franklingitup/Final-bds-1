package tenant

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// ----------------------------------------------------------------------------
// Fakes
// ----------------------------------------------------------------------------

type orgCtxKey struct{}

func withOrg(ctx context.Context, org string) context.Context {
	return context.WithValue(ctx, orgCtxKey{}, org)
}

func orgOf(ctx context.Context) string {
	v, _ := ctx.Value(orgCtxKey{}).(string)
	return v
}

// fakeRunner mimics WithTenant by stashing the org in context so the fake
// stores can scope themselves, just as RLS would.
type fakeRunner struct{}

func (fakeRunner) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	return fn(withOrg(ctx, orgID))
}

type fakeOutbox struct{ records []events.Envelope }

func (o *fakeOutbox) Enqueue(_ context.Context, e events.Envelope) error {
	o.records = append(o.records, e)
	return nil
}
func (o *fakeOutbox) FetchUnpublished(context.Context, int) ([]events.OutboxRecord, error) {
	return nil, nil
}
func (o *fakeOutbox) MarkPublished(context.Context, []string) error { return nil }

func (o *fakeOutbox) find(eventType string) (events.Envelope, bool) {
	for _, e := range o.records {
		if e.Type == eventType {
			return e, true
		}
	}
	return events.Envelope{}, false
}

// fakeNotifier captures invitation tokens delivered out-of-band, standing in
// for the Notification Service.
type fakeNotifier struct{ invites []InviteDelivery }

func (n *fakeNotifier) DeliverInvite(_ context.Context, d InviteDelivery) {
	n.invites = append(n.invites, d)
}

func (n *fakeNotifier) lastToken() string {
	if len(n.invites) == 0 {
		return ""
	}
	return n.invites[len(n.invites)-1].Token
}

type fakeOrgStore struct {
	byID  map[string]*Organization
	slugs map[string]bool
}

func newFakeOrgStore() *fakeOrgStore {
	return &fakeOrgStore{byID: map[string]*Organization{}, slugs: map[string]bool{}}
}

func (s *fakeOrgStore) Create(_ context.Context, o *Organization) error {
	if s.slugs[o.Slug] {
		return errors.Conflict("slug taken")
	}
	o.CreatedAt = time.Now()
	o.UpdatedAt = o.CreatedAt
	o.Version = 1
	s.byID[o.ID] = o
	s.slugs[o.Slug] = true
	return nil
}

func (s *fakeOrgStore) GetByID(_ context.Context, id string) (*Organization, error) {
	if o, ok := s.byID[id]; ok {
		return o, nil
	}
	return nil, errors.NotFound("org")
}

func (s *fakeOrgStore) Update(_ context.Context, o *Organization) error {
	if _, ok := s.byID[o.ID]; !ok {
		return errors.NotFound("org")
	}
	o.Version++
	s.byID[o.ID] = o
	return nil
}

func (s *fakeOrgStore) Delete(_ context.Context, id string) error {
	if _, ok := s.byID[id]; !ok {
		return errors.NotFound("org")
	}
	delete(s.byID, id)
	return nil
}

type fakeMemberStore struct {
	byKey map[string]*Member // orgID + "/" + userID
}

func newFakeMemberStore() *fakeMemberStore {
	return &fakeMemberStore{byKey: map[string]*Member{}}
}

func mkey(org, user string) string { return org + "/" + user }

func (s *fakeMemberStore) Create(_ context.Context, m *Member) error {
	k := mkey(m.OrgID, m.UserID)
	if _, ok := s.byKey[k]; ok {
		return errors.Conflict("member exists")
	}
	m.ID = uuid.NewString()
	m.CreatedAt = time.Now()
	m.UpdatedAt = m.CreatedAt
	m.Version = 1
	s.byKey[k] = m
	return nil
}

func (s *fakeMemberStore) GetByUser(ctx context.Context, userID string) (*Member, error) {
	if m, ok := s.byKey[mkey(orgOf(ctx), userID)]; ok {
		return m, nil
	}
	return nil, errors.NotFound("member")
}

func (s *fakeMemberStore) List(ctx context.Context, req database.PageRequest) (database.Page[Member], error) {
	req = req.Normalize()
	var items []Member
	for _, m := range s.byKey {
		if m.OrgID == orgOf(ctx) {
			items = append(items, *m)
		}
	}
	return database.BuildPage(items, req.Limit, func(m Member) database.Cursor { return m.Cursor() }), nil
}

func (s *fakeMemberStore) CountByRole(ctx context.Context, role Role) (int, error) {
	n := 0
	for _, m := range s.byKey {
		if m.OrgID == orgOf(ctx) && m.Role == role {
			n++
		}
	}
	return n, nil
}

func (s *fakeMemberStore) UpdateRole(ctx context.Context, userID string, role Role) error {
	m, ok := s.byKey[mkey(orgOf(ctx), userID)]
	if !ok {
		return errors.NotFound("member")
	}
	m.Role = role
	return nil
}

func (s *fakeMemberStore) Delete(ctx context.Context, userID string) error {
	k := mkey(orgOf(ctx), userID)
	if _, ok := s.byKey[k]; !ok {
		return errors.NotFound("member")
	}
	delete(s.byKey, k)
	return nil
}

type fakeInviteStore struct {
	byID   map[string]*Invitation
	byHash map[string]*Invitation
}

func newFakeInviteStore() *fakeInviteStore {
	return &fakeInviteStore{byID: map[string]*Invitation{}, byHash: map[string]*Invitation{}}
}

func (s *fakeInviteStore) Create(_ context.Context, i *Invitation) error {
	for _, ex := range s.byID {
		if ex.OrgID == i.OrgID && ex.Email == i.Email && ex.Status == InviteStatusPending {
			return errors.Conflict("pending invite exists")
		}
	}
	i.ID = uuid.NewString()
	i.CreatedAt = time.Now()
	i.UpdatedAt = i.CreatedAt
	i.Version = 1
	s.byID[i.ID] = i
	s.byHash[i.TokenHash] = i
	return nil
}

func (s *fakeInviteStore) GetByTokenHash(_ context.Context, hash string) (*Invitation, error) {
	if i, ok := s.byHash[hash]; ok {
		return i, nil
	}
	return nil, errors.NotFound("invite")
}

func (s *fakeInviteStore) GetByID(_ context.Context, id string) (*Invitation, error) {
	if i, ok := s.byID[id]; ok {
		return i, nil
	}
	return nil, errors.NotFound("invite")
}

func (s *fakeInviteStore) List(ctx context.Context, req database.PageRequest) (database.Page[Invitation], error) {
	req = req.Normalize()
	var items []Invitation
	for _, i := range s.byID {
		if i.OrgID == orgOf(ctx) {
			items = append(items, *i)
		}
	}
	return database.BuildPage(items, req.Limit, func(i Invitation) database.Cursor { return i.Cursor() }), nil
}

func (s *fakeInviteStore) UpdateStatus(_ context.Context, id, status string, acceptedBy *string) error {
	i, ok := s.byID[id]
	if !ok {
		return errors.NotFound("invite")
	}
	i.Status = status
	i.AcceptedBy = acceptedBy
	return nil
}

// ----------------------------------------------------------------------------
// Harness
// ----------------------------------------------------------------------------

type testEnv struct {
	svc      *Service
	orgs     *fakeOrgStore
	members  *fakeMemberStore
	invites  *fakeInviteStore
	outbox   *fakeOutbox
	notifier *fakeNotifier
	now      time.Time
}

func newTestEnv() *testEnv {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	orgs := newFakeOrgStore()
	members := newFakeMemberStore()
	invites := newFakeInviteStore()
	outbox := &fakeOutbox{}
	notifier := &fakeNotifier{}
	svc := NewService(Deps{
		Orgs:        orgs,
		Members:     members,
		Invitations: invites,
		Outbox:      outbox,
		Tenant:      fakeRunner{},
		Notifier:    notifier,
		Now:         func() time.Time { return now },
	})
	return &testEnv{svc: svc, orgs: orgs, members: members, invites: invites, outbox: outbox, notifier: notifier, now: now}
}

// addMember directly seeds a membership (bypassing invite flow) for setup.
func (e *testEnv) addMember(orgID, userID string, role Role) {
	m := &Member{UserID: userID, Role: role, Status: MemberStatusActive}
	m.OrgID = orgID
	m.ID = uuid.NewString()
	m.CreatedAt = e.now
	e.members.byKey[mkey(orgID, userID)] = m
}

func mustCreateOrg(t *testing.T, e *testEnv, owner string) *Organization {
	t.Helper()
	org, err := e.svc.CreateOrganization(context.Background(), owner, CreateOrganizationRequest{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	return org
}

func isForbidden(err error) bool { return errors.From(err).Code == errors.CodeForbidden }

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestCreateOrganization(t *testing.T) {
	e := newTestEnv()
	org := mustCreateOrg(t, e, "owner-1")

	if org.ID == "" || org.Slug != "acme" {
		t.Fatalf("unexpected org %+v", org)
	}
	m, err := e.members.GetByUser(withOrg(context.Background(), org.ID), "owner-1")
	if err != nil || m.Role != RoleOwner {
		t.Fatalf("expected owner membership, got %+v err=%v", m, err)
	}
	if _, ok := e.outbox.find(EventOrganizationCreated); !ok {
		t.Error("expected organization.created event in outbox")
	}
}

func TestCreateOrganization_DuplicateSlug(t *testing.T) {
	e := newTestEnv()
	mustCreateOrg(t, e, "owner-1")
	_, err := e.svc.CreateOrganization(context.Background(), "owner-2", CreateOrganizationRequest{Name: "Acme2", Slug: "acme"})
	if err != errSlugTaken {
		t.Errorf("expected errSlugTaken, got %v", err)
	}
}

func TestCreateOrganization_InvalidSlug(t *testing.T) {
	e := newTestEnv()
	_, err := e.svc.CreateOrganization(context.Background(), "owner-1", CreateOrganizationRequest{Name: "Acme", Slug: "Bad Slug!"})
	if err == nil {
		t.Error("expected validation error for bad slug")
	}
}

func TestGetOrganization_NonMember(t *testing.T) {
	e := newTestEnv()
	org := mustCreateOrg(t, e, "owner-1")
	if _, err := e.svc.GetOrganization(context.Background(), "stranger", org.ID); err != errNotMember {
		t.Errorf("expected errNotMember, got %v", err)
	}
}

func TestUpdateOrganization_OwnerOnly(t *testing.T) {
	e := newTestEnv()
	org := mustCreateOrg(t, e, "owner-1")
	e.addMember(org.ID, "admin-1", RoleAdmin)

	// Admin cannot manage the org entity (org:manage is owner-only).
	if _, err := e.svc.UpdateOrganization(context.Background(), "admin-1", org.ID, UpdateOrganizationRequest{}); !isForbidden(err) {
		t.Errorf("expected forbidden for admin update, got %v", err)
	}

	newName := "Acme Inc"
	updated, err := e.svc.UpdateOrganization(context.Background(), "owner-1", org.ID, UpdateOrganizationRequest{Name: &newName})
	if err != nil {
		t.Fatalf("owner update: %v", err)
	}
	if updated.Name != "Acme Inc" {
		t.Errorf("name = %s", updated.Name)
	}
	if _, ok := e.outbox.find(EventOrganizationUpdated); !ok {
		t.Error("expected organization.updated event")
	}
}

func TestInviteMember(t *testing.T) {
	e := newTestEnv()
	org := mustCreateOrg(t, e, "owner-1")
	e.addMember(org.ID, "viewer-1", RoleViewer)

	// Viewer cannot manage members.
	if _, err := e.svc.InviteMember(context.Background(), "viewer-1", org.ID, InviteMemberRequest{Email: "x@y.com", Role: RoleDeveloper}); !isForbidden(err) {
		t.Errorf("expected forbidden for viewer invite, got %v", err)
	}

	// Owner cannot invite as owner.
	if _, err := e.svc.InviteMember(context.Background(), "owner-1", org.ID, InviteMemberRequest{Email: "x@y.com", Role: RoleOwner}); err == nil {
		t.Error("expected validation error inviting as owner")
	}

	inv, err := e.svc.InviteMember(context.Background(), "owner-1", org.ID, InviteMemberRequest{Email: "new@y.com", Role: RoleDeveloper})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if inv.Status != InviteStatusPending {
		t.Errorf("status = %s", inv.Status)
	}
	if _, ok := e.outbox.find(EventMemberInvited); !ok {
		t.Error("expected member.invited event")
	}
}

func TestAcceptInvite(t *testing.T) {
	e := newTestEnv()
	org := mustCreateOrg(t, e, "owner-1")
	if _, err := e.svc.InviteMember(context.Background(), "owner-1", org.ID, InviteMemberRequest{Email: "new@y.com", Role: RoleDeveloper}); err != nil {
		t.Fatalf("invite: %v", err)
	}
	// The event carries a delivery reference, never the token itself.
	env, ok := e.outbox.find(EventMemberInvited)
	if !ok {
		t.Fatal("expected member.invited event")
	}
	payload, err := events.DecodePayload[memberInvitedPayload](env)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.DeliveryRef == "" || payload.InvitationID == "" {
		t.Errorf("expected invitationId and deliveryRef in payload, got %+v", payload)
	}
	// The plaintext token is delivered only over the secure channel.
	token := e.notifier.lastToken()
	if token == "" {
		t.Fatal("expected token delivered out-of-band")
	}

	// Email mismatch is rejected.
	if _, err := e.svc.AcceptInvite(context.Background(), Identity{UserID: "u-new", Email: "other@y.com"}, token); err != errInviteEmail {
		t.Errorf("expected errInviteEmail, got %v", err)
	}

	member, err := e.svc.AcceptInvite(context.Background(), Identity{UserID: "u-new", Email: "new@y.com"}, token)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if member.Role != RoleDeveloper || member.OrgID != org.ID {
		t.Errorf("unexpected member %+v", member)
	}
	accepted, ok := e.outbox.find(EventInvitationAccepted)
	if !ok {
		t.Fatal("expected invitation.accepted event")
	}
	ap, err := events.DecodePayload[invitationAcceptedPayload](accepted)
	if err != nil {
		t.Fatalf("decode accepted payload: %v", err)
	}
	if ap.UserID != "u-new" || ap.Role != RoleDeveloper || ap.InvitationID == "" {
		t.Errorf("unexpected accepted payload %+v", ap)
	}

	// Re-accepting a now-accepted invite fails.
	if _, err := e.svc.AcceptInvite(context.Background(), Identity{UserID: "u-new", Email: "new@y.com"}, token); err != errInviteNotUsable {
		t.Errorf("expected errInviteNotUsable, got %v", err)
	}
}

func TestRevokeInvitation(t *testing.T) {
	e := newTestEnv()
	org := mustCreateOrg(t, e, "owner-1")
	inv, err := e.svc.InviteMember(context.Background(), "owner-1", org.ID, InviteMemberRequest{Email: "new@y.com", Role: RoleDeveloper})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	if err := e.svc.RevokeInvitation(context.Background(), "owner-1", org.ID, inv.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	revoked, ok := e.outbox.find(EventInvitationRevoked)
	if !ok {
		t.Fatal("expected invitation.revoked event")
	}
	rp, err := events.DecodePayload[invitationRevokedPayload](revoked)
	if err != nil {
		t.Fatalf("decode revoked payload: %v", err)
	}
	if rp.InvitationID != inv.ID || rp.RevokedBy != "owner-1" {
		t.Errorf("unexpected revoked payload %+v", rp)
	}

	// Revoking again is idempotent and emits no second event.
	before := len(e.outbox.records)
	if err := e.svc.RevokeInvitation(context.Background(), "owner-1", org.ID, inv.ID); err != nil {
		t.Fatalf("idempotent revoke: %v", err)
	}
	if len(e.outbox.records) != before {
		t.Errorf("expected no new event on idempotent revoke, got %d new", len(e.outbox.records)-before)
	}
}

func TestDeleteOrganization(t *testing.T) {
	e := newTestEnv()
	org := mustCreateOrg(t, e, "owner-1")
	e.addMember(org.ID, "admin-1", RoleAdmin)

	// Admin cannot delete the org (owner-only).
	if err := e.svc.DeleteOrganization(context.Background(), "admin-1", org.ID); !isForbidden(err) {
		t.Errorf("expected forbidden for admin delete, got %v", err)
	}

	if err := e.svc.DeleteOrganization(context.Background(), "owner-1", org.ID); err != nil {
		t.Fatalf("delete org: %v", err)
	}
	if _, ok := e.orgs.byID[org.ID]; ok {
		t.Error("expected org to be deleted")
	}
	deleted, ok := e.outbox.find(EventOrganizationDeleted)
	if !ok {
		t.Fatal("expected organization.deleted event")
	}
	dp, err := events.DecodePayload[orgDeletedPayload](deleted)
	if err != nil {
		t.Fatalf("decode deleted payload: %v", err)
	}
	if dp.DeletedBy != "owner-1" {
		t.Errorf("unexpected deleted payload %+v", dp)
	}
}

func TestChangeRole_OwnerRules(t *testing.T) {
	e := newTestEnv()
	org := mustCreateOrg(t, e, "owner-1")
	e.addMember(org.ID, "admin-1", RoleAdmin)
	e.addMember(org.ID, "dev-1", RoleDeveloper)

	// Admin promoting someone to owner is owner-only.
	if _, err := e.svc.ChangeRole(context.Background(), "admin-1", org.ID, "dev-1", RoleOwner); err != errOwnerOnly {
		t.Errorf("expected errOwnerOnly, got %v", err)
	}

	// Admin may change among non-owner roles.
	if _, err := e.svc.ChangeRole(context.Background(), "admin-1", org.ID, "dev-1", RoleViewer); err != nil {
		t.Errorf("admin change role: %v", err)
	}

	// Demoting the last owner is blocked.
	if _, err := e.svc.ChangeRole(context.Background(), "owner-1", org.ID, "owner-1", RoleAdmin); err != errLastOwner {
		t.Errorf("expected errLastOwner, got %v", err)
	}
	if _, ok := e.outbox.find(EventRoleChanged); !ok {
		t.Error("expected role.changed event")
	}
}

func TestRemoveMember_Rules(t *testing.T) {
	e := newTestEnv()
	org := mustCreateOrg(t, e, "owner-1")
	e.addMember(org.ID, "admin-1", RoleAdmin)
	e.addMember(org.ID, "dev-1", RoleDeveloper)

	// Admin cannot remove an owner.
	if err := e.svc.RemoveMember(context.Background(), "admin-1", org.ID, "owner-1"); err != errOwnerOnly {
		t.Errorf("expected errOwnerOnly, got %v", err)
	}

	// Admin removes a developer.
	if err := e.svc.RemoveMember(context.Background(), "admin-1", org.ID, "dev-1"); err != nil {
		t.Errorf("remove member: %v", err)
	}
	if _, ok := e.outbox.find(EventMemberRemoved); !ok {
		t.Error("expected member.removed event")
	}

	// The last owner cannot be removed.
	if err := e.svc.RemoveMember(context.Background(), "owner-1", org.ID, "owner-1"); err != errLastOwner {
		t.Errorf("expected errLastOwner, got %v", err)
	}
}
