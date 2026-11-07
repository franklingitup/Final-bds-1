package deployment

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// ----------------------------------------------------------------------------
// Fake stores for environments and promotions
// ----------------------------------------------------------------------------

type fakeEnvironmentStore struct {
	envs   map[string]*Environment
	byName map[string]*Environment // key: "appID/name"
}

func newFakeEnvironmentStore() *fakeEnvironmentStore {
	return &fakeEnvironmentStore{
		envs:   make(map[string]*Environment),
		byName: make(map[string]*Environment),
	}
}

func (f *fakeEnvironmentStore) Create(ctx context.Context, e *Environment) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	key := e.ApplicationID + "/" + e.Name
	if _, exists := f.byName[key]; exists {
		return apperrors.Conflict("environment name taken")
	}
	e.CreatedAt = time.Now()
	e.UpdatedAt = time.Now()
	f.envs[e.ID] = e
	f.byName[key] = e
	return nil
}

func (f *fakeEnvironmentStore) GetByID(ctx context.Context, id string) (*Environment, error) {
	e, ok := f.envs[id]
	if !ok {
		return nil, apperrors.NotFound("environment not found")
	}
	return e, nil
}

func (f *fakeEnvironmentStore) List(ctx context.Context, appID string) ([]Environment, error) {
	var items []Environment
	for _, e := range f.envs {
		if e.ApplicationID == appID {
			items = append(items, *e)
		}
	}
	return items, nil
}

func (f *fakeEnvironmentStore) Update(ctx context.Context, e *Environment) error {
	if _, ok := f.envs[e.ID]; !ok {
		return apperrors.NotFound("environment not found")
	}
	f.envs[e.ID] = e
	return nil
}

func (f *fakeEnvironmentStore) Delete(ctx context.Context, id string) error {
	e, ok := f.envs[id]
	if !ok {
		return apperrors.NotFound("environment not found")
	}
	delete(f.byName, e.ApplicationID+"/"+e.Name)
	delete(f.envs, id)
	return nil
}

// ----------------------------------------------------------------------------

type fakePromotionStore struct {
	mu     sync.Mutex
	promos map[string]*PromotionRequest
	byApp  map[string][]*PromotionRequest
}

func newFakePromotionStore() *fakePromotionStore {
	return &fakePromotionStore{
		promos: make(map[string]*PromotionRequest),
		byApp:  make(map[string][]*PromotionRequest),
	}
}

func (f *fakePromotionStore) Create(ctx context.Context, p *PromotionRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	cp := *p
	f.promos[p.ID] = &cp
	f.byApp[p.ApplicationID] = append(f.byApp[p.ApplicationID], &cp)
	return nil
}

func (f *fakePromotionStore) GetByID(ctx context.Context, id string) (*PromotionRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.promos[id]
	if !ok {
		return nil, apperrors.NotFound("promotion not found")
	}
	cp := *p
	return &cp, nil
}

func (f *fakePromotionStore) ListByApplication(ctx context.Context, appID string) ([]PromotionRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []PromotionRequest
	for _, p := range f.byApp[appID] {
		items = append(items, *p)
	}
	return items, nil
}

func (f *fakePromotionStore) Update(ctx context.Context, p *PromotionRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.promos[p.ID]; !ok {
		return apperrors.NotFound("promotion not found")
	}
	cp := *p
	f.promos[p.ID] = &cp
	return nil
}

// UpdateIfPending atomically transitions the promotion from 'pending' to the new
// status. Returns (true, nil) on success, (false, nil) on CAS miss (status was
// already changed by a concurrent writer). The mutex makes this operation atomic
// within the in-memory store, mirroring the "AND status='pending'" WHERE clause
// in the postgres implementation.
func (f *fakePromotionStore) UpdateIfPending(ctx context.Context, p *PromotionRequest) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.promos[p.ID]
	if !ok {
		return false, apperrors.NotFound("promotion not found")
	}
	if existing.Status != PromotionStatusPending {
		return false, nil // CAS miss
	}
	cp := *p
	f.promos[p.ID] = &cp
	// Update byApp slice entry.
	for i, pr := range f.byApp[p.ApplicationID] {
		if pr.ID == p.ID {
			f.byApp[p.ApplicationID][i] = &cp
			break
		}
	}
	return true, nil
}

// ----------------------------------------------------------------------------
// snapshotableTenant — simulates DB transaction rollback for single-goroutine tests
// ----------------------------------------------------------------------------

// snapshotableTenant simulates Postgres transaction semantics for in-memory fake
// stores. Before running each WithTenant closure it deep-copies the mutable store
// maps. If the closure returns a non-nil error, it restores those copies — exactly
// what a Postgres ROLLBACK does.
//
// Use only in single-goroutine test scenarios. For concurrent tests use the
// standard fakeTenant (stores must be individually thread-safe) because holding
// a global mutex across fn would serialize all goroutines, preventing the race
// from actually occurring.
type snapshotableTenant struct {
	deps   *fakeDeploymentStore
	rels   *fakeReleaseStore
	promos *fakePromotionStore
}

func (t *snapshotableTenant) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	// Deep-copy only the maps that are mutated in the apply block.
	depSnap := make(map[string]*Deployment, len(t.deps.deps))
	for k, v := range t.deps.deps {
		cp := *v
		depSnap[k] = &cp
	}
	relSnap := make(map[string]*Release, len(t.rels.releases))
	for k, v := range t.rels.releases {
		cp := *v
		relSnap[k] = &cp
	}
	relByDepSnap := make(map[string][]*Release, len(t.rels.byDep))
	for k, vs := range t.rels.byDep {
		sls := make([]*Release, len(vs))
		for i, v := range vs {
			cp := *v
			sls[i] = &cp
		}
		relByDepSnap[k] = sls
	}
	promoSnap := make(map[string]*PromotionRequest, len(t.promos.promos))
	for k, v := range t.promos.promos {
		cp := *v
		promoSnap[k] = &cp
	}
	byAppSnap := make(map[string][]*PromotionRequest, len(t.promos.byApp))
	for k, vs := range t.promos.byApp {
		sls := make([]*PromotionRequest, len(vs))
		for i, v := range vs {
			cp := *v
			sls[i] = &cp
		}
		byAppSnap[k] = sls
	}

	err := fn(ctx)
	if err != nil {
		// Restore — this is the "ROLLBACK" that a real DB transaction provides.
		t.deps.deps = depSnap
		t.rels.releases = relSnap
		t.rels.byDep = relByDepSnap
		t.promos.promos = promoSnap
		t.promos.byApp = byAppSnap
	}
	return err
}

// ----------------------------------------------------------------------------
// alwaysMissPromotionStore — injects a deterministic CAS miss for rollback tests
// ----------------------------------------------------------------------------

// alwaysMissPromotionStore wraps fakePromotionStore but makes UpdateIfPending
// always return (false, nil) — the "losing side" of the concurrent-approval race.
// Used in single-goroutine tests with snapshotableTenant to verify that a CAS
// miss causes the entire apply block to be rolled back.
type alwaysMissPromotionStore struct {
	*fakePromotionStore
}

func (s *alwaysMissPromotionStore) UpdateIfPending(_ context.Context, _ *PromotionRequest) (bool, error) {
	return false, nil
}

// ----------------------------------------------------------------------------
// barrierPromotionStore — synchronises concurrent goroutines at the CAS point
// ----------------------------------------------------------------------------

// barrierPromotionStore wraps fakePromotionStore and adds a rendezvous barrier
// inside UpdateIfPending. All n goroutines block at the barrier until every one
// of them has completed its dep/rel creates and is ready to write. This guarantees
// that the concurrent-approval race scenario actually occurs in the test, rather
// than being accidentally serialised by the goroutine scheduler.
type barrierPromotionStore struct {
	*fakePromotionStore
	barrier chan struct{}
	total   int32
	arrived int32
}

func newBarrierPromotionStore(inner *fakePromotionStore, n int) *barrierPromotionStore {
	return &barrierPromotionStore{
		fakePromotionStore: inner,
		barrier:            make(chan struct{}),
		total:              int32(n),
	}
}

func (s *barrierPromotionStore) UpdateIfPending(ctx context.Context, p *PromotionRequest) (bool, error) {
	// Block until all n goroutines have arrived, then release simultaneously.
	if atomic.AddInt32(&s.arrived, 1) >= s.total {
		close(s.barrier) // last arrival releases everyone
	} else {
		<-s.barrier
	}
	// Delegate to the inner store whose UpdateIfPending is mutex-protected and
	// acts as the true CAS arbiter.
	return s.fakePromotionStore.UpdateIfPending(ctx, p)
}

// ----------------------------------------------------------------------------
// Test helper
// ----------------------------------------------------------------------------

func newTestServiceFull() (*Service, *fakeApplicationStore, *fakeDeploymentStore, *fakeReleaseStore, *fakeEnvironmentStore, *fakePromotionStore, *fakeOutbox) {
	apps := newFakeApplicationStore()
	deps := newFakeDeploymentStore()
	rels := newFakeReleaseStore()
	envs := newFakeEnvironmentStore()
	promos := newFakePromotionStore()
	outbox := &fakeOutbox{}

	svc := NewService(Deps{
		Applications: apps,
		Deployments:  deps,
		Releases:     rels,
		Environments: envs,
		Promotions:   promos,
		OrgMembers:   fakeOrgMemberStoreAllow{},
		Outbox:       outbox,
		Tenant:       &fakeTenant{},
	})

	return svc, apps, deps, rels, envs, promos, outbox
}

// ----------------------------------------------------------------------------
// Environment CRUD tests
// ----------------------------------------------------------------------------

func TestCreateEnvironment(t *testing.T) {
	svc, apps, _, _, envs, _, _ := newTestServiceFull()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	userID := uuid.NewString()

	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	env, err := svc.CreateEnvironment(ctx, orgID, userID, app.ID, CreateEnvironmentRequest{
		Name:       "staging",
		Namespace:  "staging-myapp",
		ClusterID:  uuid.NewString(),
		OrderIndex: 0,
	})

	require.NoError(t, err)
	assert.Equal(t, "staging", env.Name)
	assert.Equal(t, "staging-myapp", env.Namespace)
	assert.Equal(t, app.ID, env.ApplicationID)
	assert.Len(t, envs.envs, 1)
}

func TestCreateEnvironment_DuplicateName(t *testing.T) {
	svc, apps, _, _, _, _, _ := newTestServiceFull()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	userID := uuid.NewString()
	clusterID := uuid.NewString()

	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	_, err := svc.CreateEnvironment(ctx, orgID, userID, app.ID, CreateEnvironmentRequest{
		Name: "production", Namespace: "prod-a", ClusterID: clusterID,
	})
	require.NoError(t, err)

	_, err = svc.CreateEnvironment(ctx, orgID, userID, app.ID, CreateEnvironmentRequest{
		Name: "production", Namespace: "prod-b", ClusterID: clusterID,
	})
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeConflict, apperrors.From(err).Code)
}

func TestCreateEnvironment_ValidationErrors(t *testing.T) {
	svc, apps, _, _, _, _, _ := newTestServiceFull()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	userID := uuid.NewString()
	clusterID := uuid.NewString()

	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	tests := []struct {
		name string
		req  CreateEnvironmentRequest
	}{
		{"empty name", CreateEnvironmentRequest{Name: "", Namespace: "valid", ClusterID: clusterID}},
		{"invalid namespace uppercase", CreateEnvironmentRequest{Name: "staging", Namespace: "PROD", ClusterID: clusterID}},
		{"invalid namespace trailing dash", CreateEnvironmentRequest{Name: "staging", Namespace: "prod-", ClusterID: clusterID}},
		{"empty namespace", CreateEnvironmentRequest{Name: "staging", Namespace: "", ClusterID: clusterID}},
		{"missing clusterID", CreateEnvironmentRequest{Name: "staging", Namespace: "prod"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateEnvironment(ctx, orgID, userID, app.ID, tc.req)
			require.Error(t, err)
			assert.Equal(t, apperrors.CodeValidation, apperrors.From(err).Code, "expected validation error for: %s", tc.name)
		})
	}
}

func TestListEnvironments(t *testing.T) {
	svc, apps, _, _, _, _, _ := newTestServiceFull()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	userID := uuid.NewString()
	clusterID := uuid.NewString()

	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	for _, name := range []string{"staging", "production"} {
		_, err := svc.CreateEnvironment(ctx, orgID, userID, app.ID, CreateEnvironmentRequest{
			Name: name, Namespace: name + "-ns", ClusterID: clusterID,
		})
		require.NoError(t, err)
	}

	items, err := svc.ListEnvironments(ctx, orgID, userID, app.ID)
	require.NoError(t, err)
	assert.Len(t, items, 2)
}

func TestUpdateEnvironment(t *testing.T) {
	svc, apps, _, _, envs, _, _ := newTestServiceFull()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	userID := uuid.NewString()

	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	env, err := svc.CreateEnvironment(ctx, orgID, userID, app.ID, CreateEnvironmentRequest{
		Name: "staging", Namespace: "staging-ns", ClusterID: uuid.NewString(),
	})
	require.NoError(t, err)

	newNS := "staging-v2"
	updated, err := svc.UpdateEnvironment(ctx, orgID, userID, env.ID, UpdateEnvironmentRequest{
		Namespace: &newNS,
	})
	require.NoError(t, err)
	assert.Equal(t, "staging-v2", updated.Namespace)
	assert.Equal(t, "staging-v2", envs.envs[env.ID].Namespace)
}

func TestDeleteEnvironment(t *testing.T) {
	svc, apps, _, _, envs, _, _ := newTestServiceFull()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	userID := uuid.NewString()

	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	env, err := svc.CreateEnvironment(ctx, orgID, userID, app.ID, CreateEnvironmentRequest{
		Name: "staging", Namespace: "staging-ns", ClusterID: uuid.NewString(),
	})
	require.NoError(t, err)
	assert.Len(t, envs.envs, 1)

	require.NoError(t, svc.DeleteEnvironment(ctx, orgID, userID, env.ID))
	assert.Empty(t, envs.envs)
}

// ----------------------------------------------------------------------------
// Promotion tests
// ----------------------------------------------------------------------------

// setupTwoEnvironments creates an app + staging + production environments and a
// deployment+release in staging, returning everything needed for promotion tests.
func setupTwoEnvironments(t *testing.T) (
	svc *Service,
	deps *fakeDeploymentStore,
	rels *fakeReleaseStore,
	promos *fakePromotionStore,
	orgID, userID string,
	stagingEnv, prodEnv *Environment,
	stagingDep *Deployment,
	stagingRel *Release,
) {
	t.Helper()
	var apps *fakeApplicationStore
	var envs *fakeEnvironmentStore
	svc, apps, deps, rels, envs, promos, _ = newTestServiceFull()
	ctx := context.Background()

	orgID = uuid.NewString()
	projID := uuid.NewString()
	userID = uuid.NewString()
	clusterID := uuid.NewString()

	app := &Application{ProjectID: projID, Name: "My App", Slug: "my-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	var err error
	stagingEnv, err = svc.CreateEnvironment(ctx, orgID, userID, app.ID, CreateEnvironmentRequest{
		Name: "staging", Namespace: "staging-ns", ClusterID: clusterID, OrderIndex: 0,
	})
	require.NoError(t, err)
	_ = envs

	prodEnv, err = svc.CreateEnvironment(ctx, orgID, userID, app.ID, CreateEnvironmentRequest{
		Name: "production", Namespace: "prod-ns", ClusterID: clusterID, OrderIndex: 1,
	})
	require.NoError(t, err)

	// Create a deployment in staging (bound to stagingEnv).
	stagingDep = &Deployment{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "registry/myapp@sha256:abc123",
		Replicas:      2,
		EnvVars:       []byte("[]"),
		Status:        StatusSucceeded,
		EnvironmentID: &stagingEnv.ID,
	}
	stagingDep.OrgID = orgID
	require.NoError(t, deps.Create(ctx, stagingDep))

	// Create a release for that deployment.
	stagingRel = &Release{
		OrgID:        orgID,
		DeploymentID: stagingDep.ID,
		Revision:     1,
		Image:        stagingDep.Image,
		Replicas:     stagingDep.Replicas,
		ConfigHash:   "abc",
		Config:       []byte(`{"image":"registry/myapp@sha256:abc123"}`),
		Status:       ReleaseStatusSucceeded,
	}
	require.NoError(t, rels.Create(ctx, stagingRel))

	return
}

func TestRequestPromotion(t *testing.T) {
	svc, _, _, promos, orgID, userID, stagingEnv, prodEnv, stagingDep, stagingRel := setupTwoEnvironments(t)
	ctx := context.Background()
	_ = stagingDep

	promo, err := svc.RequestPromotion(ctx, orgID, userID, RequestPromotionRequest{
		ReleaseID:         stagingRel.ID,
		FromEnvironmentID: stagingEnv.ID,
		ToEnvironmentID:   prodEnv.ID,
	})

	require.NoError(t, err)
	assert.Equal(t, PromotionStatusPending, promo.Status)
	assert.Equal(t, stagingRel.ID, promo.ReleaseID)
	assert.Equal(t, stagingEnv.ID, promo.FromEnvironmentID)
	assert.Equal(t, prodEnv.ID, promo.ToEnvironmentID)
	assert.Equal(t, userID, promo.RequestedBy)
	assert.Len(t, promos.promos, 1)
}

func TestRequestPromotion_SameEnvironment(t *testing.T) {
	svc, _, _, _, orgID, userID, stagingEnv, _, _, stagingRel := setupTwoEnvironments(t)
	ctx := context.Background()

	_, err := svc.RequestPromotion(ctx, orgID, userID, RequestPromotionRequest{
		ReleaseID:         stagingRel.ID,
		FromEnvironmentID: stagingEnv.ID,
		ToEnvironmentID:   stagingEnv.ID, // same!
	})
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeValidation, apperrors.From(err).Code)
}

func TestApprovePromotion_CreatesReleaseInExistingTargetDeployment(t *testing.T) {
	svc, deps, rels, promos, orgID, userID, stagingEnv, prodEnv, stagingDep, stagingRel := setupTwoEnvironments(t)
	ctx := context.Background()
	_ = stagingDep

	// Create an existing deployment in production.
	prodDep := &Deployment{
		ApplicationID: stagingDep.ApplicationID,
		ClusterID:     uuid.NewString(),
		Image:         "registry/myapp:old",
		Replicas:      1,
		EnvVars:       []byte("[]"),
		Status:        StatusSucceeded,
		EnvironmentID: &prodEnv.ID,
	}
	prodDep.OrgID = orgID
	require.NoError(t, deps.Create(ctx, prodDep))

	promo, _ := svc.RequestPromotion(ctx, orgID, userID, RequestPromotionRequest{
		ReleaseID:         stagingRel.ID,
		FromEnvironmentID: stagingEnv.ID,
		ToEnvironmentID:   prodEnv.ID,
	})

	updatedPromo, dep, rel, err := svc.ApprovePromotion(ctx, orgID, userID, promo.ID)

	require.NoError(t, err)
	assert.Equal(t, PromotionStatusApplied, updatedPromo.Status)
	assert.NotNil(t, updatedPromo.ApprovedBy)
	assert.Equal(t, userID, *updatedPromo.ApprovedBy)

	// Release in target deployment carries source image (digest-pinned).
	assert.Equal(t, "registry/myapp@sha256:abc123", rel.Image)
	assert.Equal(t, prodDep.ID, dep.ID, "approval reuses the existing production deployment")

	// A new release was created in the production deployment.
	assert.Len(t, rels.byDep[prodDep.ID], 1)
	assert.Equal(t, "registry/myapp@sha256:abc123", rels.byDep[prodDep.ID][0].Image)

	// Promotion is marked applied in the store.
	assert.Equal(t, PromotionStatusApplied, promos.promos[promo.ID].Status)
}

func TestApprovePromotion_CreatesNewDeploymentWhenNoneInTarget(t *testing.T) {
	svc, deps, rels, _, orgID, userID, stagingEnv, prodEnv, stagingDep, stagingRel := setupTwoEnvironments(t)
	ctx := context.Background()
	_ = stagingDep

	// Production environment has NO existing deployment yet.
	initialDepCount := len(deps.deps)

	promo, err := svc.RequestPromotion(ctx, orgID, userID, RequestPromotionRequest{
		ReleaseID:         stagingRel.ID,
		FromEnvironmentID: stagingEnv.ID,
		ToEnvironmentID:   prodEnv.ID,
	})
	require.NoError(t, err)

	updatedPromo, newDep, newRel, err := svc.ApprovePromotion(ctx, orgID, userID, promo.ID)
	require.NoError(t, err)

	// A brand-new deployment was created in production.
	assert.Equal(t, len(deps.deps), initialDepCount+1,
		"ApprovePromotion must create a new Deployment in the target environment when none exists")
	assert.Equal(t, prodEnv.ClusterID, newDep.ClusterID)
	assert.NotNil(t, newDep.EnvironmentID)
	assert.Equal(t, prodEnv.ID, *newDep.EnvironmentID)

	// Release carries the promoted image.
	assert.Equal(t, "registry/myapp@sha256:abc123", newRel.Image)
	assert.Len(t, rels.byDep[newDep.ID], 1)

	assert.Equal(t, PromotionStatusApplied, updatedPromo.Status)
}

func TestApprovePromotion_NotPending(t *testing.T) {
	svc, _, _, promos, orgID, userID, stagingEnv, prodEnv, _, stagingRel := setupTwoEnvironments(t)
	ctx := context.Background()

	promo, err := svc.RequestPromotion(ctx, orgID, userID, RequestPromotionRequest{
		ReleaseID:         stagingRel.ID,
		FromEnvironmentID: stagingEnv.ID,
		ToEnvironmentID:   prodEnv.ID,
	})
	require.NoError(t, err)

	// Manually mark rejected.
	promos.promos[promo.ID].Status = PromotionStatusRejected

	_, _, _, err = svc.ApprovePromotion(ctx, orgID, userID, promo.ID)
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeConflict, apperrors.From(err).Code)
}

func TestRejectPromotion(t *testing.T) {
	svc, _, _, promos, orgID, userID, stagingEnv, prodEnv, _, stagingRel := setupTwoEnvironments(t)
	ctx := context.Background()

	promo, err := svc.RequestPromotion(ctx, orgID, userID, RequestPromotionRequest{
		ReleaseID:         stagingRel.ID,
		FromEnvironmentID: stagingEnv.ID,
		ToEnvironmentID:   prodEnv.ID,
	})
	require.NoError(t, err)

	rejected, err := svc.RejectPromotion(ctx, orgID, userID, promo.ID, RejectPromotionRequest{
		Reason: "not ready for production yet",
	})
	require.NoError(t, err)
	assert.Equal(t, PromotionStatusRejected, rejected.Status)
	assert.NotNil(t, rejected.RejectionReason)
	assert.Equal(t, "not ready for production yet", *rejected.RejectionReason)
	assert.Equal(t, PromotionStatusRejected, promos.promos[promo.ID].Status)
}

// TestOnlyProjectAdminCanApprovePromotion verifies the authorization boundary:
// a ProjectDeveloper can request but NOT approve a promotion.
// ActionManageProject is required for approval; ActionDeploy is not sufficient.
func TestOnlyProjectAdminCanApprovePromotion(t *testing.T) {
	apps := newFakeApplicationStore()
	deps := newFakeDeploymentStore()
	rels := newFakeReleaseStore()
	envs := newFakeEnvironmentStore()
	promos := newFakePromotionStore()
	outbox := &fakeOutbox{}

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	developerID := uuid.NewString()
	adminID := uuid.NewString()

	svc := NewService(Deps{
		Applications: apps,
		Deployments:  deps,
		Releases:     rels,
		Environments: envs,
		Promotions:   promos,
		OrgMembers:   fakeOrgMemberStoreRole{orgID: orgID, role: authz.RoleMember},
		ProjectMembers: fakeProjectMemberStore{members: map[string]authz.ProjectRole{
			projectID + "/" + developerID: authz.ProjectDeveloper,
			projectID + "/" + adminID:     authz.ProjectAdmin,
		}},
		Outbox: outbox,
		Tenant: &fakeTenant{},
	})
	ctx := context.Background()

	app := &Application{ProjectID: projectID, Name: "App", Slug: "app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	clusterID := uuid.NewString()
	stagingEnv, err := svc.CreateEnvironment(ctx, orgID, adminID, app.ID, CreateEnvironmentRequest{
		Name: "staging", Namespace: "staging-ns", ClusterID: clusterID,
	})
	require.NoError(t, err)

	prodEnv, err := svc.CreateEnvironment(ctx, orgID, adminID, app.ID, CreateEnvironmentRequest{
		Name: "production", Namespace: "prod-ns", ClusterID: clusterID,
	})
	require.NoError(t, err)

	// Developer creates a deployment+release in staging.
	stagingDep := &Deployment{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "registry/myapp@sha256:abc",
		Replicas:      1,
		EnvVars:       []byte("[]"),
		Status:        StatusSucceeded,
		EnvironmentID: &stagingEnv.ID,
	}
	stagingDep.OrgID = orgID
	require.NoError(t, deps.Create(ctx, stagingDep))
	stagingRel := &Release{
		OrgID:        orgID,
		DeploymentID: stagingDep.ID,
		Revision:     1,
		Image:        stagingDep.Image,
		Replicas:     1,
		ConfigHash:   "x",
		Config:       []byte(`{}`),
		Status:       ReleaseStatusSucceeded,
	}
	require.NoError(t, rels.Create(ctx, stagingRel))

	// Developer requests the promotion (ActionDeploy — OK).
	promo, err := svc.RequestPromotion(ctx, orgID, developerID, RequestPromotionRequest{
		ReleaseID:         stagingRel.ID,
		FromEnvironmentID: stagingEnv.ID,
		ToEnvironmentID:   prodEnv.ID,
	})
	require.NoError(t, err)

	// Developer tries to approve their own promotion request — must be denied.
	_, _, _, err = svc.ApprovePromotion(ctx, orgID, developerID, promo.ID)
	require.Error(t, err, "ProjectDeveloper must NOT be able to approve a promotion")
	assert.Equal(t, apperrors.CodeForbidden, apperrors.From(err).Code,
		"developer approval should return 403 Forbidden, not any other error code")

	// ProjectAdmin can approve.
	_, _, _, err = svc.ApprovePromotion(ctx, orgID, adminID, promo.ID)
	require.NoError(t, err, "ProjectAdmin must be able to approve a promotion")
}

// ----------------------------------------------------------------------------
// Gap 2 regression tests — CAS miss behaviour and transaction rollback
// ----------------------------------------------------------------------------

// buildPromotionScenario creates a minimal but fully wired promotion scenario:
//
//	app → stagingEnv → prodEnv
//	existingDep (bound to stagingEnv) → stagingRel (the release to promote)
//	promo (pending, from staging → prod)
//
// All objects are pre-inserted into the given stores so that ApprovePromotion
// can be called immediately.
func buildPromotionScenario(
	ctx context.Context, t *testing.T,
	apps *fakeApplicationStore,
	deps *fakeDeploymentStore,
	rels *fakeReleaseStore,
	envs *fakeEnvironmentStore,
	promos *fakePromotionStore,
) (orgID, adminID, promoID string) {
	t.Helper()
	orgID = uuid.NewString()
	adminID = uuid.NewString()
	projID := uuid.NewString()
	clusterID := uuid.NewString()

	app := &Application{ProjectID: projID, Name: "Promo App", Slug: "promo-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	stagingEnv := &Environment{
		ApplicationID: app.ID, OrgID: orgID,
		Name: "staging", Namespace: "staging-ns", ClusterID: clusterID,
	}
	require.NoError(t, envs.Create(ctx, stagingEnv))

	prodEnv := &Environment{
		ApplicationID: app.ID, OrgID: orgID,
		Name: "production", Namespace: "prod-ns", ClusterID: clusterID,
	}
	require.NoError(t, envs.Create(ctx, prodEnv))

	envIDStr := stagingEnv.ID
	dep := &Deployment{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "my-image:1",
		Replicas:      1,
		EnvironmentID: &envIDStr,
	}
	dep.OrgID = orgID
	require.NoError(t, deps.Create(ctx, dep))

	rel := &Release{
		DeploymentID: dep.ID,
		Image:        dep.Image,
		Replicas:     dep.Replicas,
		Revision:     1,
		Status:       ReleaseStatusSucceeded,
	}
	rel.OrgID = orgID
	require.NoError(t, rels.Create(ctx, rel))

	promo := &PromotionRequest{
		OrgID: orgID, ApplicationID: app.ID,
		ReleaseID:         rel.ID,
		FromEnvironmentID: stagingEnv.ID,
		ToEnvironmentID:   prodEnv.ID,
		RequestedBy:       adminID,
		Status:            PromotionStatusPending,
	}
	require.NoError(t, promos.Create(ctx, promo))

	return orgID, adminID, promo.ID
}

// TestApprovePromotion_CASMiss_TransactionRollsBackApplyBlock verifies that when
// UpdateIfPending returns applied=false (the "losing side" of a concurrent race),
// the entire apply block is rolled back — the dep create, rel create, and dep update
// that happened earlier in the same WithTenant closure must all be undone.
//
// The test uses snapshotableTenant (simulates DB ROLLBACK for in-memory stores) and
// alwaysMissPromotionStore (deterministically injects a CAS miss) so no actual
// concurrency is needed.
func TestApprovePromotion_CASMiss_TransactionRollsBackApplyBlock(t *testing.T) {
	ctx := context.Background()

	apps := newFakeApplicationStore()
	deps := newFakeDeploymentStore()
	rels := newFakeReleaseStore()
	envs := newFakeEnvironmentStore()
	innerPromos := newFakePromotionStore()
	missPromos := &alwaysMissPromotionStore{innerPromos}

	snap := &snapshotableTenant{deps: deps, rels: rels, promos: innerPromos}

	svc := NewService(Deps{
		Applications: apps,
		Deployments:  deps,
		Releases:     rels,
		Environments: envs,
		Promotions:   missPromos,
		OrgMembers:   fakeOrgMemberStoreAllow{},
		Outbox:       &fakeOutbox{},
		Tenant:       snap,
	})

	orgID, adminID, promoID := buildPromotionScenario(ctx, t, apps, deps, rels, envs, innerPromos)

	// Snapshot the store sizes before the failing approve so we can assert rollback.
	deps.mu.Lock()
	depsBefore := len(deps.deps)
	deps.mu.Unlock()
	rels.mu.Lock()
	relsBefore := len(rels.releases)
	rels.mu.Unlock()

	_, _, _, err := svc.ApprovePromotion(ctx, orgID, adminID, promoID)
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeConflict, apperrors.From(err).Code,
		"CAS miss must surface as Conflict, not any other error code")

	// Assert rollback: dep and release counts must be exactly as before the call.
	deps.mu.Lock()
	depsAfter := len(deps.deps)
	deps.mu.Unlock()
	rels.mu.Lock()
	relsAfter := len(rels.releases)
	rels.mu.Unlock()

	assert.Equal(t, depsBefore, depsAfter,
		"no new deployment must be persisted when the CAS misses (transaction was rolled back)")
	assert.Equal(t, relsBefore, relsAfter,
		"no new release must be persisted when the CAS misses (transaction was rolled back)")
}

// TestApprovePromotion_ConcurrentApproval_ExactlyOneWins proves the CAS guarantee
// at the goroutine level: two concurrent calls to ApprovePromotion for the same
// promotion ID must resolve to exactly one success and exactly one conflict error —
// not two successes (double-deployment) and not two failures.
//
// barrierPromotionStore synchronises both goroutines so that they have both passed
// the initial pending-status read and completed their dep/rel creates before either
// reaches the UpdateIfPending write. This guarantees the race actually occurs every
// run, making -count=5 and -race reliable.
func TestApprovePromotion_ConcurrentApproval_ExactlyOneWins(t *testing.T) {
	ctx := context.Background()

	apps := newFakeApplicationStore()
	deps := newFakeDeploymentStore()
	rels := newFakeReleaseStore()
	envs := newFakeEnvironmentStore()
	innerPromos := newFakePromotionStore()
	barrierPromos := newBarrierPromotionStore(innerPromos, 2)

	svc := NewService(Deps{
		Applications: apps,
		Deployments:  deps,
		Releases:     rels,
		Environments: envs,
		Promotions:   barrierPromos,
		OrgMembers:   fakeOrgMemberStoreAllow{},
		Outbox:       &fakeOutbox{},
		Tenant:       &fakeTenant{},
	})

	orgID, adminID, promoID := buildPromotionScenario(ctx, t, apps, deps, rels, envs, innerPromos)

	type result struct{ err error }
	results := make(chan result, 2)

	for i := 0; i < 2; i++ {
		go func() {
			_, _, _, err := svc.ApprovePromotion(ctx, orgID, adminID, promoID)
			results <- result{err}
		}()
	}

	r1 := <-results
	r2 := <-results

	var successes, conflicts int
	for _, r := range []result{r1, r2} {
		if r.err == nil {
			successes++
		} else if apperrors.From(r.err).Code == apperrors.CodeConflict {
			conflicts++
		} else {
			t.Errorf("unexpected error (expected nil or Conflict): %v", r.err)
		}
	}

	assert.Equal(t, 1, successes, "exactly one goroutine must succeed in applying the promotion")
	assert.Equal(t, 1, conflicts, "exactly one goroutine must receive a Conflict error (CAS miss)")

	// The winning goroutine created exactly one new deployment + one new release.
	// Count only the prod-env bound objects (not the pre-existing staging ones).
	deps.mu.Lock()
	defer deps.mu.Unlock()
	var prodDeps int
	for _, d := range deps.deps {
		if d.EnvironmentID != nil {
			// The prod env namespace is "prod-ns"; find it by checking the envs store.
			if env, _ := envs.GetByID(ctx, *d.EnvironmentID); env != nil && env.Namespace == "prod-ns" {
				prodDeps++
			}
		}
	}
	assert.Equal(t, 1, prodDeps, "exactly one deployment must be bound to the prod environment")
}
