package deployment

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// ----------------------------------------------------------------------------
// Fake Stores
// ----------------------------------------------------------------------------

type fakeApplicationStore struct {
	apps     map[string]*Application
	bySlug   map[string]*Application
	sequence int
}

func newFakeApplicationStore() *fakeApplicationStore {
	return &fakeApplicationStore{
		apps:   make(map[string]*Application),
		bySlug: make(map[string]*Application),
	}
}

func (f *fakeApplicationStore) Create(ctx context.Context, a *Application) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	key := a.ProjectID + "/" + a.Slug
	if _, exists := f.bySlug[key]; exists {
		return apperrors.Conflict("slug taken")
	}
	a.CreatedAt = time.Now()
	a.UpdatedAt = time.Now()
	a.Version = 1
	f.apps[a.ID] = a
	f.bySlug[key] = a
	return nil
}

func (f *fakeApplicationStore) GetByID(ctx context.Context, id string) (*Application, error) {
	a, ok := f.apps[id]
	if !ok {
		return nil, apperrors.NotFound("application not found")
	}
	return a, nil
}

func (f *fakeApplicationStore) GetBySlug(ctx context.Context, projectID, slug string) (*Application, error) {
	a, ok := f.bySlug[projectID+"/"+slug]
	if !ok {
		return nil, apperrors.NotFound("application not found")
	}
	return a, nil
}

func (f *fakeApplicationStore) List(ctx context.Context, projectID string, req database.PageRequest) (database.Page[Application], error) {
	var items []Application
	for _, a := range f.apps {
		if a.ProjectID == projectID {
			items = append(items, *a)
		}
	}
	return database.Page[Application]{Items: items}, nil
}

func (f *fakeApplicationStore) Update(ctx context.Context, a *Application) error {
	if _, ok := f.apps[a.ID]; !ok {
		return apperrors.NotFound("application not found")
	}
	a.Version++
	a.UpdatedAt = time.Now()
	f.apps[a.ID] = a
	return nil
}

func (f *fakeApplicationStore) Delete(ctx context.Context, id string) error {
	if _, ok := f.apps[id]; !ok {
		return apperrors.NotFound("application not found")
	}
	delete(f.apps, id)
	return nil
}

// ----------------------------------------------------------------------------

type fakeDeploymentStore struct {
	deps map[string]*Deployment
}

func newFakeDeploymentStore() *fakeDeploymentStore {
	return &fakeDeploymentStore{deps: make(map[string]*Deployment)}
}

func (f *fakeDeploymentStore) Create(ctx context.Context, d *Deployment) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	d.CreatedAt = time.Now()
	d.UpdatedAt = time.Now()
	d.Version = 1
	d.DesiredReplicas = d.Replicas
	f.deps[d.ID] = d
	return nil
}

func (f *fakeDeploymentStore) GetByID(ctx context.Context, id string) (*Deployment, error) {
	d, ok := f.deps[id]
	if !ok {
		return nil, apperrors.NotFound("deployment not found")
	}
	return d, nil
}

func (f *fakeDeploymentStore) List(ctx context.Context, appID string, req database.PageRequest) (database.Page[Deployment], error) {
	var items []Deployment
	for _, d := range f.deps {
		if d.ApplicationID == appID {
			items = append(items, *d)
		}
	}
	return database.Page[Deployment]{Items: items}, nil
}

func (f *fakeDeploymentStore) ListByCluster(ctx context.Context, clusterID string, req database.PageRequest) (database.Page[Deployment], error) {
	var items []Deployment
	for _, d := range f.deps {
		if d.ClusterID == clusterID {
			items = append(items, *d)
		}
	}
	return database.Page[Deployment]{Items: items}, nil
}

func (f *fakeDeploymentStore) Update(ctx context.Context, d *Deployment) error {
	if _, ok := f.deps[d.ID]; !ok {
		return apperrors.NotFound("deployment not found")
	}
	d.Version++
	d.UpdatedAt = time.Now()
	f.deps[d.ID] = d
	return nil
}

func (f *fakeDeploymentStore) UpdateStatus(ctx context.Context, id, status string, readyReplicas *int, errorMsg *string) error {
	d, ok := f.deps[id]
	if !ok {
		return apperrors.NotFound("deployment not found")
	}
	d.Status = status
	if readyReplicas != nil {
		d.ReadyReplicas = *readyReplicas
	}
	d.Version++
	return nil
}

func (f *fakeDeploymentStore) Delete(ctx context.Context, id string) error {
	if _, ok := f.deps[id]; !ok {
		return apperrors.NotFound("deployment not found")
	}
	delete(f.deps, id)
	return nil
}

func (f *fakeDeploymentStore) ListByOrg(ctx context.Context, req database.PageRequest) (database.Page[Deployment], error) {
	var items []Deployment
	for _, d := range f.deps {
		items = append(items, *d)
	}
	return database.Page[Deployment]{Items: items}, nil
}

func (f *fakeDeploymentStore) SoftDelete(ctx context.Context, id string) error {
	if _, ok := f.deps[id]; !ok {
		return apperrors.NotFound("deployment not found")
	}
	f.deps[id].Status = "deleted"
	return nil
}

func (f *fakeDeploymentStore) ListAllActive(ctx context.Context) ([]Deployment, error) {
	var items []Deployment
	for _, d := range f.deps {
		if d.Status != "deleted" {
			items = append(items, *d)
		}
	}
	return items, nil
}

// ----------------------------------------------------------------------------

type fakeReleaseStore struct {
	releases map[string]*Release
	byDep    map[string][]*Release
}

func newFakeReleaseStore() *fakeReleaseStore {
	return &fakeReleaseStore{
		releases: make(map[string]*Release),
		byDep:    make(map[string][]*Release),
	}
}

func (f *fakeReleaseStore) Create(ctx context.Context, r *Release) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt = time.Now()
	f.releases[r.ID] = r
	f.byDep[r.DeploymentID] = append(f.byDep[r.DeploymentID], r)
	return nil
}

func (f *fakeReleaseStore) GetByID(ctx context.Context, id string) (*Release, error) {
	r, ok := f.releases[id]
	if !ok {
		return nil, apperrors.NotFound("release not found")
	}
	return r, nil
}

func (f *fakeReleaseStore) GetByRevision(ctx context.Context, depID string, rev int) (*Release, error) {
	for _, r := range f.byDep[depID] {
		if r.Revision == rev {
			return r, nil
		}
	}
	return nil, apperrors.NotFound("release not found")
}

func (f *fakeReleaseStore) GetLatest(ctx context.Context, depID string) (*Release, error) {
	rels := f.byDep[depID]
	if len(rels) == 0 {
		return nil, apperrors.NotFound("no releases")
	}
	return rels[len(rels)-1], nil
}

func (f *fakeReleaseStore) GetPreviousSuccessful(ctx context.Context, depID string, beforeRev int) (*Release, error) {
	rels := f.byDep[depID]
	for i := len(rels) - 1; i >= 0; i-- {
		if rels[i].Revision < beforeRev && rels[i].Status == ReleaseStatusSucceeded {
			return rels[i], nil
		}
	}
	return nil, apperrors.NotFound("no previous successful release")
}

func (f *fakeReleaseStore) List(ctx context.Context, depID string, req database.PageRequest) (database.Page[Release], error) {
	var items []Release
	for _, r := range f.byDep[depID] {
		items = append(items, *r)
	}
	return database.Page[Release]{Items: items}, nil
}

func (f *fakeReleaseStore) UpdateStatus(ctx context.Context, id, status string, errorMsg *string) error {
	r, ok := f.releases[id]
	if !ok {
		return apperrors.NotFound("release not found")
	}
	r.Status = status
	if errorMsg != nil {
		r.ErrorMessage = errorMsg
	}
	return nil
}

func (f *fakeReleaseStore) MarkStarted(ctx context.Context, id string) error {
	r, ok := f.releases[id]
	if !ok {
		return apperrors.NotFound("release not found")
	}
	r.Status = ReleaseStatusDeploying
	now := time.Now()
	r.StartedAt = &now
	return nil
}

func (f *fakeReleaseStore) MarkFinished(ctx context.Context, id, status string, errorMsg *string) error {
	r, ok := f.releases[id]
	if !ok {
		return apperrors.NotFound("release not found")
	}
	r.Status = status
	now := time.Now()
	r.FinishedAt = &now
	if errorMsg != nil {
		r.ErrorMessage = errorMsg
	}
	return nil
}

// ----------------------------------------------------------------------------
// Fake Outbox & Tenant
// ----------------------------------------------------------------------------

type fakeOutbox struct {
	events []*events.Envelope
}

// Enqueue matches events.Outbox (value receiver of Envelope). A copy is captured
// so callers cannot mutate the recorded event afterwards.
func (f *fakeOutbox) Enqueue(ctx context.Context, e events.Envelope) error {
	captured := e
	f.events = append(f.events, &captured)
	return nil
}

func (f *fakeOutbox) FetchUnpublished(context.Context, int) ([]events.OutboxRecord, error) {
	return nil, nil
}

func (f *fakeOutbox) MarkPublished(context.Context, []string) error { return nil }

type fakeTenant struct{}

func (f *fakeTenant) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	return fn(ctx)
}

// fakeOrgMemberStoreAllow treats every caller as an active org owner so that
// service-layer org authorization passes in these unit tests. Negative
// authorization cases are covered in service_authz_test.go with a non-member
// store.
type fakeOrgMemberStoreAllow struct{}

func (fakeOrgMemberStoreAllow) GetOrgMember(_ context.Context, userID string) (*authz.OrgMember, error) {
	return &authz.OrgMember{UserID: userID, Role: authz.OrgOwner, Status: "active"}, nil
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func newTestService() (*Service, *fakeApplicationStore, *fakeDeploymentStore, *fakeReleaseStore, *fakeOutbox) {
	apps := newFakeApplicationStore()
	deps := newFakeDeploymentStore()
	rels := newFakeReleaseStore()
	outbox := &fakeOutbox{}

	svc := NewService(Deps{
		Applications: apps,
		Deployments:  deps,
		Releases:     rels,
		OrgMembers:   fakeOrgMemberStoreAllow{},
		Outbox:       outbox,
		Tenant:       &fakeTenant{},
	})

	return svc, apps, deps, rels, outbox
}

func TestCreateApplication(t *testing.T) {
	svc, apps, _, _, outbox := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	userID := uuid.NewString()

	app, err := svc.CreateApplication(ctx, orgID, projID, userID, CreateApplicationRequest{
		Name:        "My App",
		Slug:        "my-app",
		RuntimeType: RuntimeContainer,
	})

	require.NoError(t, err)
	assert.Equal(t, "My App", app.Name)
	assert.Equal(t, "my-app", app.Slug)
	assert.Equal(t, RuntimeContainer, app.RuntimeType)
	assert.Len(t, apps.apps, 1)
	assert.Len(t, outbox.events, 1)
	assert.Equal(t, EventApplicationCreated, outbox.events[0].Type)
}

func TestCreateApplication_DuplicateSlug(t *testing.T) {
	svc, _, _, _, _ := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	userID := uuid.NewString()

	_, err := svc.CreateApplication(ctx, orgID, projID, userID, CreateApplicationRequest{
		Name: "App 1",
		Slug: "my-app",
	})
	require.NoError(t, err)

	_, err = svc.CreateApplication(ctx, orgID, projID, userID, CreateApplicationRequest{
		Name: "App 2",
		Slug: "my-app",
	})
	require.Error(t, err)
	assert.Equal(t, apperrors.CodeConflict, apperrors.From(err).Code)
}

func TestCreateDeployment(t *testing.T) {
	svc, apps, deps, rels, outbox := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	clusterID := uuid.NewString()
	userID := uuid.NewString()

	// Create application first.
	app := &Application{
		ProjectID:   projID,
		Name:        "Test App",
		Slug:        "test-app",
		RuntimeType: RuntimeContainer,
	}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	dep, rel, err := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "nginx:latest",
		Replicas:      3,
	})

	require.NoError(t, err)
	assert.Equal(t, app.ID, dep.ApplicationID)
	assert.Equal(t, clusterID, dep.ClusterID)
	assert.Equal(t, "nginx:latest", dep.Image)
	assert.Equal(t, 3, dep.Replicas)
	assert.Equal(t, StatusPending, dep.Status)
	assert.Equal(t, 1, rel.Revision)
	assert.Equal(t, ReleaseStatusPending, rel.Status)

	assert.Len(t, deps.deps, 1)
	assert.Len(t, rels.releases, 1)
	assert.Len(t, outbox.events, 1)
	assert.Equal(t, EventDeploymentCreated, outbox.events[0].Type)
}

func TestUpdateDeployment_CreatesNewRelease(t *testing.T) {
	svc, apps, _, rels, outbox := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	clusterID := uuid.NewString()
	userID := uuid.NewString()

	// Create application.
	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	// Create initial deployment.
	dep, _, err := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "nginx:1.0",
		Replicas:      2,
	})
	require.NoError(t, err)

	// Update deployment.
	newImage := "nginx:2.0"
	newReplicas := 5
	updatedDep, newRel, err := svc.UpdateDeployment(ctx, orgID, userID, dep.ID, UpdateDeploymentRequest{
		Image:    &newImage,
		Replicas: &newReplicas,
	})

	require.NoError(t, err)
	assert.Equal(t, "nginx:2.0", updatedDep.Image)
	assert.Equal(t, 5, updatedDep.Replicas)
	assert.Equal(t, 2, newRel.Revision)
	assert.Len(t, rels.releases, 2)
	assert.Len(t, outbox.events, 2) // create + update
}

func TestRollback(t *testing.T) {
	svc, apps, _, rels, _ := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	clusterID := uuid.NewString()
	userID := uuid.NewString()

	// Create application.
	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	// Create initial deployment.
	dep, rel1, err := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "nginx:1.0",
		Replicas:      2,
	})
	require.NoError(t, err)

	// Mark first release as successful.
	rel1.Status = ReleaseStatusSucceeded

	// Update deployment (revision 2).
	newImage := "nginx:2.0"
	_, _, err = svc.UpdateDeployment(ctx, orgID, userID, dep.ID, UpdateDeploymentRequest{
		Image: &newImage,
	})
	require.NoError(t, err)

	// Rollback to previous.
	rolledBack, newRel, err := svc.Rollback(ctx, orgID, userID, dep.ID, RollbackRequest{})

	require.NoError(t, err)
	assert.Equal(t, "nginx:1.0", rolledBack.Image) // Back to revision 1 image
	assert.Equal(t, 3, newRel.Revision)
	assert.Len(t, rels.releases, 3)
}

func TestRollback_NoTarget(t *testing.T) {
	svc, apps, _, _, _ := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	clusterID := uuid.NewString()
	userID := uuid.NewString()

	// Create application.
	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	// Create initial deployment (no successful release yet).
	dep, _, err := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "nginx:1.0",
		Replicas:      2,
	})
	require.NoError(t, err)

	// Rollback should fail - no previous successful release.
	_, _, err = svc.Rollback(ctx, orgID, userID, dep.ID, RollbackRequest{})

	require.Error(t, err)
	assert.Equal(t, apperrors.CodeConflict, apperrors.From(err).Code)
}

func TestMarkDeploymentLifecycle(t *testing.T) {
	svc, apps, deps, rels, outbox := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	clusterID := uuid.NewString()
	userID := uuid.NewString()

	// Create application.
	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	// Create deployment.
	dep, rel, err := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "nginx:1.0",
		Replicas:      2,
	})
	require.NoError(t, err)

	// Mark started.
	err = svc.MarkDeploymentStarted(ctx, orgID, userID, dep.ID, rel.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, deps.deps[dep.ID].Status)
	assert.Equal(t, ReleaseStatusDeploying, rels.releases[rel.ID].Status)

	// Mark succeeded.
	err = svc.MarkDeploymentSucceeded(ctx, orgID, userID, dep.ID, rel.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, deps.deps[dep.ID].Status)
	assert.Equal(t, ReleaseStatusSucceeded, rels.releases[rel.ID].Status)
	assert.Equal(t, 2, deps.deps[dep.ID].ReadyReplicas)

	// Should have 3 events: created, started, succeeded.
	assert.Len(t, outbox.events, 3)
}

func TestMarkDeploymentFailed(t *testing.T) {
	svc, apps, deps, rels, outbox := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	clusterID := uuid.NewString()
	userID := uuid.NewString()

	// Create application.
	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	// Create deployment.
	dep, rel, err := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "nginx:1.0",
		Replicas:      2,
	})
	require.NoError(t, err)

	// Mark failed.
	err = svc.MarkDeploymentFailed(ctx, orgID, userID, dep.ID, rel.ID, "image pull failed")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, deps.deps[dep.ID].Status)
	assert.Equal(t, ReleaseStatusFailed, rels.releases[rel.ID].Status)
	assert.NotNil(t, rels.releases[rel.ID].ErrorMessage)

	// Should have 2 events: created, failed.
	assert.Len(t, outbox.events, 2)
	assert.Equal(t, EventDeploymentFailed, outbox.events[1].Type)

	var payload deploymentFailedPayload
	err = json.Unmarshal(outbox.events[1].Payload, &payload)
	require.NoError(t, err)
	assert.Equal(t, "image pull failed", payload.ErrorMessage)
}

func TestCreateApplication_ValidationErrors(t *testing.T) {
	svc, _, _, _, _ := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	userID := uuid.NewString()

	tests := []struct {
		name string
		req  CreateApplicationRequest
	}{
		{
			name: "empty name",
			req:  CreateApplicationRequest{Name: "", Slug: "valid-slug"},
		},
		{
			name: "short name",
			req:  CreateApplicationRequest{Name: "A", Slug: "valid-slug"},
		},
		{
			name: "invalid slug",
			req:  CreateApplicationRequest{Name: "Valid Name", Slug: "INVALID_SLUG!"},
		},
		{
			name: "invalid runtime type",
			req:  CreateApplicationRequest{Name: "Valid Name", Slug: "valid-slug", RuntimeType: "invalid"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateApplication(ctx, orgID, projID, userID, tc.req)
			require.Error(t, err)
			assert.Equal(t, apperrors.CodeValidation, apperrors.From(err).Code)
		})
	}
}

func TestCreateDeployment_ValidationErrors(t *testing.T) {
	svc, apps, _, _, _ := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	projID := uuid.NewString()
	userID := uuid.NewString()

	app := &Application{ProjectID: projID, Name: "Test App", Slug: "test-app"}
	app.OrgID = orgID
	require.NoError(t, apps.Create(ctx, app))

	tests := []struct {
		name string
		req  CreateDeploymentRequest
	}{
		{
			name: "empty image",
			req:  CreateDeploymentRequest{ApplicationID: app.ID, ClusterID: uuid.NewString(), Image: "", Replicas: 1},
		},
		{
			name: "zero replicas",
			req:  CreateDeploymentRequest{ApplicationID: app.ID, ClusterID: uuid.NewString(), Image: "nginx", Replicas: 0},
		},
		{
			name: "negative replicas",
			req:  CreateDeploymentRequest{ApplicationID: app.ID, ClusterID: uuid.NewString(), Image: "nginx", Replicas: -1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.CreateDeployment(ctx, orgID, userID, tc.req)
			require.Error(t, err)
			assert.Equal(t, apperrors.CodeValidation, apperrors.From(err).Code)
		})
	}
}

func TestCreateDeployment_AppNotFound(t *testing.T) {
	svc, _, _, _, _ := newTestService()
	ctx := context.Background()

	orgID := uuid.NewString()
	userID := uuid.NewString()

	_, _, err := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: uuid.NewString(), // Nonexistent
		ClusterID:     uuid.NewString(),
		Image:         "nginx",
		Replicas:      1,
	})

	require.Error(t, err)
	assert.Equal(t, apperrors.CodeNotFound, apperrors.From(err).Code)
}
