package deployment

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/argocd"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// ----------------------------------------------------------------------------
// Fake ArgoApplicationStore
// ----------------------------------------------------------------------------

type fakeArgoAppStore struct {
	mu    sync.Mutex
	byDep map[string]*ArgoApplication
}

func newFakeArgoAppStore() *fakeArgoAppStore {
	return &fakeArgoAppStore{byDep: make(map[string]*ArgoApplication)}
}

func (f *fakeArgoAppStore) Upsert(_ context.Context, a *ArgoApplication) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *a
	f.byDep[a.DeploymentID] = &cp
	return nil
}

func (f *fakeArgoAppStore) Get(_ context.Context, deploymentID string) (*ArgoApplication, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byDep[deploymentID]
	if !ok {
		return nil, apperrors.NotFound("argo application not found")
	}
	cp := *a
	return &cp, nil
}

func (f *fakeArgoAppStore) GetByAppName(_ context.Context, appName string) (*ArgoApplication, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.byDep {
		if a.AppName == appName {
			cp := *a
			return &cp, nil
		}
	}
	return nil, apperrors.NotFound("argo application not found")
}

func (f *fakeArgoAppStore) UpdateObserved(_ context.Context, a *ArgoApplication) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.byDep[a.DeploymentID]
	if !ok {
		return apperrors.NotFound("argo application not found")
	}
	existing.SyncStatus = a.SyncStatus
	existing.HealthStatus = a.HealthStatus
	existing.OperationPhase = a.OperationPhase
	existing.SyncedRevision = a.SyncedRevision
	existing.Drift = a.Drift
	now := time.Now()
	existing.ObservedAt = &now
	a.ObservedAt = &now
	return nil
}

func (f *fakeArgoAppStore) Delete(_ context.Context, deploymentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byDep, deploymentID)
	return nil
}

// ----------------------------------------------------------------------------
// Fake Argo CD client
// ----------------------------------------------------------------------------

type fakeArgoClient struct {
	mu         sync.Mutex
	created    []*argocd.Application
	synced     []syncCall
	terminated []string
	rolledBack []int64
	getApp     *argocd.Application
	listApps   []argocd.Application
	listErr    error
	createErr  error
	syncErr    error
	rollbackErr error
	getErr     error
}

type syncCall struct {
	name string
	req  argocd.SyncRequest
}

func (c *fakeArgoClient) CreateApplication(_ context.Context, app *argocd.Application) (*argocd.Application, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.createErr != nil {
		return nil, c.createErr
	}
	cp := *app
	c.created = append(c.created, &cp)
	return &cp, nil
}

func (c *fakeArgoClient) UpdateApplication(_ context.Context, app *argocd.Application) (*argocd.Application, error) {
	return app, nil
}

func (c *fakeArgoClient) DeleteApplication(_ context.Context, name string, cascade bool) error { return nil }

func (c *fakeArgoClient) GetApplication(_ context.Context, name string) (*argocd.Application, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.getErr != nil {
		return nil, c.getErr
	}
	if c.getApp == nil {
		return nil, argocd.ErrNotFound
	}
	return c.getApp, nil
}

func (c *fakeArgoClient) RefreshApplication(ctx context.Context, name string, hard bool) (*argocd.Application, error) {
	return c.GetApplication(ctx, name)
}

func (c *fakeArgoClient) ListApplications(_ context.Context, opts argocd.ListOptions) ([]argocd.Application, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listErr != nil {
		return nil, c.listErr
	}
	return c.listApps, nil
}

func (c *fakeArgoClient) SyncApplication(_ context.Context, name string, req argocd.SyncRequest) (*argocd.Application, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.syncErr != nil {
		return nil, c.syncErr
	}
	c.synced = append(c.synced, syncCall{name: name, req: req})
	return &argocd.Application{Metadata: argocd.ObjectMeta{Name: name}}, nil
}

func (c *fakeArgoClient) TerminateOperation(_ context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.terminated = append(c.terminated, name)
	return nil
}

func (c *fakeArgoClient) RollbackApplication(_ context.Context, name string, id int64) (*argocd.Application, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rollbackErr != nil {
		return nil, c.rollbackErr
	}
	c.rolledBack = append(c.rolledBack, id)
	return &argocd.Application{Metadata: argocd.ObjectMeta{Name: name}}, nil
}

func (c *fakeArgoClient) WaitForSync(ctx context.Context, name string, opts argocd.WaitOptions) (*argocd.Application, error) {
	return c.GetApplication(ctx, name)
}

func (c *fakeArgoClient) WaitForHealthy(ctx context.Context, name string, opts argocd.WaitOptions) (*argocd.Application, error) {
	return c.GetApplication(ctx, name)
}

func (c *fakeArgoClient) RetryOperation(ctx context.Context, opts argocd.RetryOptions, fn func(context.Context) error) error {
	return fn(ctx)
}

func (c *fakeArgoClient) createdCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.created)
}

func (c *fakeArgoClient) syncCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.synced)
}

// ----------------------------------------------------------------------------
// Test env
// ----------------------------------------------------------------------------

type argoTestEnv struct {
	svc      *ArgoService
	apps     *fakeApplicationStore
	deps     *fakeDeploymentStore
	rels     *fakeReleaseStore
	argoApps *fakeArgoAppStore
	rollouts *fakeRolloutStore
	outbox   *fakeOutbox
	client   *fakeArgoClient
}

func newArgoTestEnv() *argoTestEnv {
	apps := newFakeApplicationStore()
	deps := newFakeDeploymentStore()
	rels := newFakeReleaseStore()
	argoApps := newFakeArgoAppStore()
	rollouts := newFakeRolloutStore()
	outbox := &fakeOutbox{}
	client := &fakeArgoClient{}

	svc := NewArgoService(ArgoServiceDeps{
		Client:       client,
		ArgoApps:     argoApps,
		Applications: apps,
		Deployments:  deps,
		Releases:     rels,
		Rollouts:     rollouts,
		OrgMembers:   fakeOrgMemberStoreAllow{},
		Outbox:       outbox,
		Tenant:       &fakeTenant{},
	})
	return &argoTestEnv{svc: svc, apps: apps, deps: deps, rels: rels, argoApps: argoApps, rollouts: rollouts, outbox: outbox, client: client}
}

// seedGitOpsDeployment creates an application + deployment + release and returns
// the deployment ID.
func (e *argoTestEnv) seed(orgID, slug string) string {
	app := &Application{ProjectID: "proj-1", Name: slug, Slug: slug, RuntimeType: RuntimeContainer}
	app.OrgID = orgID
	_ = e.apps.Create(context.Background(), app)

	dep := &Deployment{ApplicationID: app.ID, ClusterID: "cluster-1", Image: "nginx:1", Replicas: 2, EnvVars: []byte("[]")}
	dep.OrgID = orgID
	_ = e.deps.Create(context.Background(), dep)

	rel := &Release{OrgID: orgID, DeploymentID: dep.ID, Revision: 1, Image: "nginx:1", Replicas: 2, Status: ReleaseStatusSucceeded}
	_ = e.rels.Create(context.Background(), rel)
	return dep.ID
}

// ----------------------------------------------------------------------------
// RegisterApplication
// ----------------------------------------------------------------------------

func TestRegisterApplication_Success(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")

	rec, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, GitOpsSource{
		RepoURL:        "https://github.com/acme/config",
		Path:           "prod",
		TargetRevision: "main",
	})
	require.NoError(t, err)
	assert.Equal(t, depID, rec.DeploymentID)
	assert.Contains(t, rec.AppName, "my-app-")
	assert.Equal(t, "my-app", rec.DestNamespace)
	assert.True(t, rec.AutoSync)

	// Persisted.
	stored, err := e.argoApps.Get(context.Background(), depID)
	require.NoError(t, err)
	assert.Equal(t, rec.AppName, stored.AppName)

	// Created in Argo CD with identity labels.
	require.Equal(t, 1, e.client.createdCount())
	created := e.client.created[0]
	assert.Equal(t, orgID, created.Metadata.Labels[LabelOrgID])
	assert.Equal(t, depID, created.Metadata.Labels[LabelDeploymentID])

	// Rollout seeded to Pending.
	rs, err := e.rollouts.Get(context.Background(), depID, e.rels.byDep[depID][0].ID)
	require.NoError(t, err)
	assert.Equal(t, RolloutPhasePending, rs.Phase)
}

func TestRegisterApplication_RequiresRepoURL(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")

	_, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, GitOpsSource{})
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeValidationFailed, apperrors.From(err).Code)
}

func TestRegisterApplication_InvalidSourceType(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")

	_, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, GitOpsSource{
		RepoURL: "https://x", SourceType: "bogus",
	})
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeValidationFailed, apperrors.From(err).Code)
}

func TestRegisterApplication_Idempotent(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")

	src := GitOpsSource{RepoURL: "https://github.com/acme/config"}
	_, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, src)
	require.NoError(t, err)
	_, err = e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, src)
	require.NoError(t, err)

	// Still exactly one persisted binding for the deployment.
	assert.Len(t, e.argoApps.byDep, 1)
}

func TestRegisterApplication_RemoteFailureSurfaced(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")
	e.client.createErr = &argocd.APIError{StatusCode: 502, Message: "argocd down"}

	_, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, GitOpsSource{
		RepoURL: "https://github.com/acme/config",
	})
	require.Error(t, err)
	// The binding is still persisted so a retry can converge.
	_, gerr := e.argoApps.Get(context.Background(), depID)
	require.NoError(t, gerr)
}

func TestRegisterApplication_RBACDenied(t *testing.T) {
	apps := newFakeApplicationStore()
	deps := newFakeDeploymentStore()
	client := &fakeArgoClient{}
	svc := NewArgoService(ArgoServiceDeps{
		Client:       client,
		ArgoApps:     newFakeArgoAppStore(),
		Applications: apps,
		Deployments:  deps,
		Releases:     newFakeReleaseStore(),
		OrgMembers:   &fakeOrgMemberStore{}, // non-member
		Outbox:       &fakeOutbox{},
		Tenant:       &fakeTenantRunner{},
	})

	_, err := svc.RegisterApplication(context.Background(), "org-1", "user-1", "dep-1", GitOpsSource{RepoURL: "https://x"})
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeForbidden, apperrors.From(err).Code)
	assert.Equal(t, 0, client.createdCount(), "no remote call when RBAC denies")
}

// ----------------------------------------------------------------------------
// Sync
// ----------------------------------------------------------------------------

func TestSync_Success(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")
	rec, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, GitOpsSource{RepoURL: "https://x"})
	require.NoError(t, err)

	_, err = e.svc.Sync(context.Background(), orgID, "user-1", depID, ArgoSyncRequest{Revision: "abc"})
	require.NoError(t, err)
	require.Equal(t, 1, e.client.syncCount())
	assert.Equal(t, rec.AppName, e.client.synced[0].name)
	assert.Equal(t, "abc", e.client.synced[0].req.Revision)
}

func TestSync_NotRegistered(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")

	_, err := e.svc.Sync(context.Background(), orgID, "user-1", depID, ArgoSyncRequest{})
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeNotFound, apperrors.From(err).Code)
}

// ----------------------------------------------------------------------------
// Rollback
// ----------------------------------------------------------------------------

func TestRollback_ByRevision(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")
	rec, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, GitOpsSource{RepoURL: "https://x"})
	require.NoError(t, err)

	e.client.getApp = &argocd.Application{
		Metadata: argocd.ObjectMeta{Name: rec.AppName},
		Status: argocd.ApplicationStatus{History: []argocd.RevisionHistory{
			{ID: 1, Revision: "v1"},
			{ID: 2, Revision: "v2"},
		}},
	}

	_, err = e.svc.Rollback(context.Background(), orgID, "user-1", depID, ArgoRollbackRequest{Revision: "v1"})
	require.NoError(t, err)
	require.Len(t, e.client.rolledBack, 1)
	assert.Equal(t, int64(1), e.client.rolledBack[0])

	// A rollback.started event was emitted.
	assertEventEmitted(t, e.outbox, EventDeploymentRollbackStarted)
}

func TestRollback_ByHistoryID(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")
	_, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, GitOpsSource{RepoURL: "https://x"})
	require.NoError(t, err)

	id := int64(5)
	_, err = e.svc.Rollback(context.Background(), orgID, "user-1", depID, ArgoRollbackRequest{HistoryID: &id})
	require.NoError(t, err)
	require.Len(t, e.client.rolledBack, 1)
	assert.Equal(t, int64(5), e.client.rolledBack[0])
}

func TestRollback_RevisionNotInHistory(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")
	rec, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, GitOpsSource{RepoURL: "https://x"})
	require.NoError(t, err)
	e.client.getApp = &argocd.Application{Metadata: argocd.ObjectMeta{Name: rec.AppName}}

	_, err = e.svc.Rollback(context.Background(), orgID, "user-1", depID, ArgoRollbackRequest{Revision: "missing"})
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeValidationFailed, apperrors.From(err).Code)
}

func TestRollback_RequiresRevisionOrHistory(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()
	depID := e.seed(orgID, "my-app")
	_, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, GitOpsSource{RepoURL: "https://x"})
	require.NoError(t, err)

	_, err = e.svc.Rollback(context.Background(), orgID, "user-1", depID, ArgoRollbackRequest{})
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeValidationFailed, apperrors.From(err).Code)
}

// ----------------------------------------------------------------------------
// Concurrency
// ----------------------------------------------------------------------------

func TestRegisterApplication_Concurrent(t *testing.T) {
	e := newArgoTestEnv()
	orgID := uuid.NewString()

	const n = 20
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = e.seed(orgID, "app-"+uuid.NewString()[:8])
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(depID string) {
			defer wg.Done()
			_, err := e.svc.RegisterApplication(context.Background(), orgID, "user-1", depID, GitOpsSource{RepoURL: "https://github.com/acme/config"})
			errs <- err
		}(ids[i])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Len(t, e.argoApps.byDep, n)
	assert.Equal(t, n, e.client.createdCount())
}

func assertEventEmitted(t *testing.T, outbox *fakeOutbox, eventType string) {
	t.Helper()
	for _, ev := range outbox.events {
		if ev.Type == eventType {
			return
		}
	}
	t.Fatalf("expected event %q to be emitted; got %d events", eventType, len(outbox.events))
}
