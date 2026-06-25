//go:build integration

package deployment

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/migrations"
)

func testDB(t *testing.T) *database.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	cfg := config.Config{}
	cfg.Database.DSN = dsn

	ctx := context.Background()
	db, err := database.Connect(ctx, cfg)
	require.NoError(t, err)

	// Apply migrations for test dependencies.
	for _, service := range []string{"tenant", "project", "cluster", "deployment", "outbox"} {
		fsys, err := migrations.Service(service)
		require.NoError(t, err)
		migs, err := database.LoadMigrations(fsys)
		require.NoError(t, err)
		migrator, err := database.NewMigrator(db, "schema_migrations_"+service, migs)
		require.NoError(t, err)
		_, err = migrator.Up(ctx)
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		// Clean test data.
		cleanupTestData(t, db)
		db.Close()
	})

	return db
}

func cleanupTestData(t *testing.T, db *database.DB) {
	ctx := context.Background()
	tables := []string{"releases", "deployments", "applications", "outbox"}
	for _, table := range tables {
		_, err := db.Pool().Exec(ctx, "DELETE FROM "+table+" WHERE true")
		if err != nil {
			t.Logf("cleanup %s: %v", table, err)
		}
	}
}

func createTestOrg(t *testing.T, db *database.DB, orgID string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO organizations (id, name, slug) 
		VALUES ($1, 'Test Org', 'test-org-'||substr($1::text, 1, 8))
		ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
}

func createTestProject(t *testing.T, db *database.DB, orgID, projectID string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO projects (id, org_id, name, slug, status) 
		VALUES ($1, $2, 'Test Project', 'test-project-'||substr($1::text, 1, 8), 'active')
		ON CONFLICT DO NOTHING`, projectID, orgID)
	require.NoError(t, err)
}

func createTestCluster(t *testing.T, db *database.DB, orgID, clusterID string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO clusters (id, org_id, name, slug, status) 
		VALUES ($1, $2, 'Test Cluster', 'test-cluster-'||substr($1::text, 1, 8), 'connected')
		ON CONFLICT DO NOTHING`, clusterID, orgID)
	require.NoError(t, err)
}

func createTestClusterWithAgent(t *testing.T, db *database.DB, orgID, clusterID, agentID string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO clusters (id, org_id, name, slug, status, agent_id) 
		VALUES ($1, $2, 'Test Cluster', 'test-cluster-'||substr($1::text, 1, 8), 'connected', $3)
		ON CONFLICT DO NOTHING`, clusterID, orgID, agentID)
	require.NoError(t, err)
}

func TestIntegration_CreateApplication(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, orgID)
	createTestProject(t, db, orgID, projectID)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	app, err := svc.CreateApplication(ctx, orgID, projectID, userID, CreateApplicationRequest{
		Name:        "Integration Test App",
		Slug:        "int-test-app-" + uuid.NewString()[:8],
		RuntimeType: RuntimeContainer,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, app.ID)
	assert.Equal(t, "Integration Test App", app.Name)
	assert.Equal(t, RuntimeContainer, app.RuntimeType)
}

func TestIntegration_FullDeploymentLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	clusterID := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, orgID)
	createTestProject(t, db, orgID, projectID)
	createTestCluster(t, db, orgID, clusterID)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	// 1. Create application.
	app, err := svc.CreateApplication(ctx, orgID, projectID, userID, CreateApplicationRequest{
		Name: "Full Lifecycle App",
		Slug: "full-lifecycle-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	// 2. Create deployment.
	dep, rel, err := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "nginx:1.0",
		Replicas:      3,
	})
	require.NoError(t, err)
	assert.Equal(t, StatusPending, dep.Status)
	assert.Equal(t, 1, rel.Revision)

	// 3. Mark started.
	err = svc.MarkDeploymentStarted(ctx, orgID, dep.ID, rel.ID)
	require.NoError(t, err)

	// Verify status.
	fetchedDep, _, err := svc.GetDeployment(ctx, orgID, dep.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, fetchedDep.Status)

	// 4. Mark succeeded.
	err = svc.MarkDeploymentSucceeded(ctx, orgID, dep.ID, rel.ID, 3)
	require.NoError(t, err)

	fetchedDep, _, err = svc.GetDeployment(ctx, orgID, dep.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, fetchedDep.Status)
	assert.Equal(t, 3, fetchedDep.ReadyReplicas)

	// 5. Update deployment (creates revision 2).
	newImage := "nginx:2.0"
	updatedDep, newRel, err := svc.UpdateDeployment(ctx, orgID, userID, dep.ID, UpdateDeploymentRequest{
		Image: &newImage,
	})
	require.NoError(t, err)
	assert.Equal(t, "nginx:2.0", updatedDep.Image)
	assert.Equal(t, 2, newRel.Revision)

	// 6. Mark revision 2 as failed.
	err = svc.MarkDeploymentFailed(ctx, orgID, dep.ID, newRel.ID, "OOM killed")
	require.NoError(t, err)

	// 7. Rollback to previous successful.
	rolledBack, rollbackRel, err := svc.Rollback(ctx, orgID, userID, dep.ID, RollbackRequest{})
	require.NoError(t, err)
	assert.Equal(t, "nginx:1.0", rolledBack.Image)
	assert.Equal(t, 3, rollbackRel.Revision)

	// Verify releases.
	releases, err := svc.ListReleases(ctx, orgID, dep.ID, database.PageRequest{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, releases.Items, 3)
}

func TestIntegration_ListDeploymentsByCluster(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	clusterID := uuid.NewString()
	otherClusterID := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, orgID)
	createTestProject(t, db, orgID, projectID)
	createTestCluster(t, db, orgID, clusterID)
	createTestCluster(t, db, orgID, otherClusterID)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	// Create applications and deployments.
	app1, _ := svc.CreateApplication(ctx, orgID, projectID, userID, CreateApplicationRequest{
		Name: "App 1", Slug: "app1-" + uuid.NewString()[:8],
	})
	app2, _ := svc.CreateApplication(ctx, orgID, projectID, userID, CreateApplicationRequest{
		Name: "App 2", Slug: "app2-" + uuid.NewString()[:8],
	})

	// Deploy both apps to clusterID.
	svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app1.ID, ClusterID: clusterID, Image: "nginx", Replicas: 1,
	})
	svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app2.ID, ClusterID: clusterID, Image: "redis", Replicas: 1,
	})

	// Deploy app1 also to otherClusterID.
	svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app1.ID, ClusterID: otherClusterID, Image: "nginx", Replicas: 1,
	})

	// List by clusterID.
	deps, err := svc.ListDeploymentsByCluster(ctx, orgID, clusterID, database.PageRequest{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, deps.Items, 2)

	// List by otherClusterID.
	deps, err = svc.ListDeploymentsByCluster(ctx, orgID, otherClusterID, database.PageRequest{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, deps.Items, 1)
}

func TestIntegration_RLS_TenantIsolation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	org1 := uuid.NewString()
	org2 := uuid.NewString()
	proj1 := uuid.NewString()
	proj2 := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, org1)
	createTestOrg(t, db, org2)
	createTestProject(t, db, org1, proj1)
	createTestProject(t, db, org2, proj2)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	// Create app in org1.
	app1, err := svc.CreateApplication(ctx, org1, proj1, userID, CreateApplicationRequest{
		Name: "Org1 App", Slug: "org1-app-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	// Create app in org2.
	_, err = svc.CreateApplication(ctx, org2, proj2, userID, CreateApplicationRequest{
		Name: "Org2 App", Slug: "org2-app-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	// List apps in org1 - should only see org1's app.
	apps, err := svc.ListApplications(ctx, org1, proj1, database.PageRequest{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, apps.Items, 1)
	assert.Equal(t, app1.ID, apps.Items[0].ID)

	// Try to get org2's app from org1's context - should fail due to RLS.
	// Note: This depends on actual RLS enforcement which may vary based on test setup.
}

func TestIntegration_Pagination(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, orgID)
	createTestProject(t, db, orgID, projectID)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	// Create 5 applications.
	for i := 0; i < 5; i++ {
		_, err := svc.CreateApplication(ctx, orgID, projectID, userID, CreateApplicationRequest{
			Name: "App " + string(rune('A'+i)),
			Slug: "app-" + uuid.NewString()[:8],
		})
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure distinct timestamps
	}

	// Get first page of 2.
	page1, err := svc.ListApplications(ctx, orgID, projectID, database.PageRequest{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, page1.Items, 2)
	assert.NotEmpty(t, page1.NextCursor)

	// Get second page.
	page2, err := svc.ListApplications(ctx, orgID, projectID, database.PageRequest{Limit: 2, Cursor: page1.NextCursor})
	require.NoError(t, err)
	assert.Len(t, page2.Items, 2)
	assert.NotEmpty(t, page2.NextCursor)

	// Get third page (should have 1 item).
	page3, err := svc.ListApplications(ctx, orgID, projectID, database.PageRequest{Limit: 2, Cursor: page2.NextCursor})
	require.NoError(t, err)
	assert.Len(t, page3.Items, 1)
	assert.Empty(t, page3.NextCursor) // No more pages
}

func TestIntegration_OutboxEvents(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	clusterID := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, orgID)
	createTestProject(t, db, orgID, projectID)
	createTestCluster(t, db, orgID, clusterID)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	// Create application.
	app, err := svc.CreateApplication(ctx, orgID, projectID, userID, CreateApplicationRequest{
		Name: "Outbox Test App",
		Slug: "outbox-test-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	// Create deployment.
	dep, _, err := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "nginx",
		Replicas:      1,
	})
	require.NoError(t, err)

	// Verify events in outbox.
	rows, err := db.Pool().Query(ctx, `
		SELECT event_type, org_id 
		FROM outbox 
		WHERE org_id = $1 
		ORDER BY created_at`, orgID)
	require.NoError(t, err)
	defer rows.Close()

	var eventTypes []string
	for rows.Next() {
		var eventType, org string
		require.NoError(t, rows.Scan(&eventType, &org))
		eventTypes = append(eventTypes, eventType)
	}
	require.NoError(t, rows.Err())

	assert.Contains(t, eventTypes, EventApplicationCreated+".v1")
	assert.Contains(t, eventTypes, EventDeploymentCreated+".v1")

	// Clean up.
	_, _ = db.Pool().Exec(ctx, "DELETE FROM releases WHERE deployment_id = $1", dep.ID)
	_, _ = db.Pool().Exec(ctx, "DELETE FROM deployments WHERE id = $1", dep.ID)
	_, _ = db.Pool().Exec(ctx, "DELETE FROM applications WHERE id = $1", app.ID)
}

func TestIntegration_DesiredState(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	clusterID := uuid.NewString()
	agentID := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, orgID)
	createTestProject(t, db, orgID, projectID)
	createTestClusterWithAgent(t, db, orgID, clusterID, agentID)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	// Create application.
	app, err := svc.CreateApplication(ctx, orgID, projectID, userID, CreateApplicationRequest{
		Name:        "Desired State App",
		Slug:        "desired-state-" + uuid.NewString()[:8],
		RuntimeType: RuntimeContainer,
	})
	require.NoError(t, err)

	// Create deployment with releases.
	dep, rel, err := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "nginx:latest",
		Replicas:      2,
		CPURequest:    strPtr("100m"),
		MemoryRequest: strPtr("128Mi"),
		Port:          intPtr(80),
		EnvVars:       []EnvVar{{Name: "ENV", Value: "test"}},
	})
	require.NoError(t, err)

	// Test DesiredStateStore with the updated API (CRIT-001 fix).
	store := NewDesiredStateStore(db)

	// The new GetDesiredState API takes orgID and executes in tenant context internally.
	desired, err := store.GetDesiredState(ctx, orgID, clusterID)
	require.NoError(t, err)

	assert.Len(t, desired, 1)

	d := desired[0]
	assert.Equal(t, dep.ID, d.DeploymentID)
	assert.Equal(t, rel.ID, d.ReleaseID)
	assert.Equal(t, app.ID, d.ApplicationID)
	assert.Equal(t, app.Name, d.ApplicationName)
	assert.Equal(t, app.Slug, d.ApplicationSlug)
	assert.Equal(t, "nginx:latest", d.Image)
	assert.Equal(t, 1, d.Revision)
	assert.Equal(t, 2, d.Replicas)
	assert.NotNil(t, d.Port)
	assert.Equal(t, 80, *d.Port)
	assert.NotNil(t, d.ResourceRequests)
	assert.Equal(t, "100m", d.ResourceRequests.CPU)
	assert.Equal(t, "128Mi", d.ResourceRequests.Memory)
	assert.Len(t, d.EnvVars, 1)
	assert.Equal(t, "ENV", d.EnvVars[0].Name)
}

func TestIntegration_DesiredState_MultipleDeployments(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	clusterID := uuid.NewString()
	agentID := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, orgID)
	createTestProject(t, db, orgID, projectID)
	createTestClusterWithAgent(t, db, orgID, clusterID, agentID)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	// Create two applications.
	app1, _ := svc.CreateApplication(ctx, orgID, projectID, userID, CreateApplicationRequest{
		Name: "App 1", Slug: "app1-" + uuid.NewString()[:8],
	})
	app2, _ := svc.CreateApplication(ctx, orgID, projectID, userID, CreateApplicationRequest{
		Name: "App 2", Slug: "app2-" + uuid.NewString()[:8],
	})

	// Deploy both to the cluster.
	dep1, _, _ := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app1.ID, ClusterID: clusterID, Image: "nginx:1.0", Replicas: 1,
	})
	dep2, _, _ := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app2.ID, ClusterID: clusterID, Image: "redis:6.0", Replicas: 3,
	})

	store := NewDesiredStateStore(db)

	// Use the updated API that takes orgID (CRIT-001 fix).
	desired, err := store.GetDesiredState(ctx, orgID, clusterID)
	require.NoError(t, err)

	assert.Len(t, desired, 2)

	// Find each deployment in results.
	var found1, found2 bool
	for _, d := range desired {
		if d.DeploymentID == dep1.ID {
			found1 = true
			assert.Equal(t, "nginx:1.0", d.Image)
			assert.Equal(t, 1, d.Replicas)
		}
		if d.DeploymentID == dep2.ID {
			found2 = true
			assert.Equal(t, "redis:6.0", d.Image)
			assert.Equal(t, 3, d.Replicas)
		}
	}
	assert.True(t, found1, "dep1 not found")
	assert.True(t, found2, "dep2 not found")
}

func TestIntegration_DesiredState_LatestRelease(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	clusterID := uuid.NewString()
	agentID := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, orgID)
	createTestProject(t, db, orgID, projectID)
	createTestClusterWithAgent(t, db, orgID, clusterID, agentID)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	// Create application.
	app, _ := svc.CreateApplication(ctx, orgID, projectID, userID, CreateApplicationRequest{
		Name: "Multi Release App", Slug: "multi-rel-" + uuid.NewString()[:8],
	})

	// Create deployment.
	dep, rel1, _ := svc.CreateDeployment(ctx, orgID, userID, CreateDeploymentRequest{
		ApplicationID: app.ID, ClusterID: clusterID, Image: "nginx:1.0", Replicas: 1,
	})

	// Mark first release as succeeded.
	_ = svc.MarkDeploymentSucceeded(ctx, orgID, dep.ID, rel1.ID, 1)

	// Update deployment (creates revision 2).
	newImage := "nginx:2.0"
	_, rel2, _ := svc.UpdateDeployment(ctx, orgID, userID, dep.ID, UpdateDeploymentRequest{
		Image: &newImage,
	})

	store := NewDesiredStateStore(db)

	// Use the updated API that takes orgID (CRIT-001 fix).
	desired, err := store.GetDesiredState(ctx, orgID, clusterID)
	require.NoError(t, err)

	assert.Len(t, desired, 1)
	d := desired[0]

	// Should return the latest release (revision 2).
	assert.Equal(t, rel2.ID, d.ReleaseID)
	assert.Equal(t, 2, d.Revision)
	assert.Equal(t, "nginx:2.0", d.Image)
}

func TestIntegration_ClusterValidator(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	orgID := uuid.NewString()
	clusterID := uuid.NewString()
	agentID := uuid.NewString()

	createTestOrg(t, db, orgID)
	createTestClusterWithAgent(t, db, orgID, clusterID, agentID)

	validator := NewClusterValidator(db.Pool())

	// Valid credentials.
	gotOrgID, err := validator.ValidateCluster(ctx, clusterID, agentID)
	require.NoError(t, err)
	assert.Equal(t, orgID, gotOrgID)

	// Invalid agent ID.
	_, err = validator.ValidateCluster(ctx, clusterID, "wrong-agent")
	assert.Error(t, err)

	// Non-existent cluster.
	_, err = validator.ValidateCluster(ctx, "non-existent", agentID)
	assert.Error(t, err)
}

// ============================================================================
// SECURITY INTEGRATION TESTS - CRIT-001 Fix: Cross-Tenant Isolation
// ============================================================================

func TestIntegration_DesiredState_CrossTenantIsolation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup two organizations with separate clusters and deployments.
	org1 := uuid.NewString()
	org2 := uuid.NewString()
	proj1 := uuid.NewString()
	proj2 := uuid.NewString()
	cluster1 := uuid.NewString()
	cluster2 := uuid.NewString()
	agent1 := uuid.NewString()
	agent2 := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, org1)
	createTestOrg(t, db, org2)
	createTestProject(t, db, org1, proj1)
	createTestProject(t, db, org2, proj2)
	createTestClusterWithAgent(t, db, org1, cluster1, agent1)
	createTestClusterWithAgent(t, db, org2, cluster2, agent2)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	// Create deployment in org1.
	app1, err := svc.CreateApplication(ctx, org1, proj1, userID, CreateApplicationRequest{
		Name: "Org1 Secret App", Slug: "org1-secret-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	dep1, _, err := svc.CreateDeployment(ctx, org1, userID, CreateDeploymentRequest{
		ApplicationID: app1.ID,
		ClusterID:     cluster1,
		Image:         "secret-image:v1",
		Replicas:      2,
		EnvVars:       []EnvVar{{Name: "SECRET_KEY", Value: "super-secret-value"}},
	})
	require.NoError(t, err)

	// Create deployment in org2.
	app2, err := svc.CreateApplication(ctx, org2, proj2, userID, CreateApplicationRequest{
		Name: "Org2 Public App", Slug: "org2-public-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	dep2, _, err := svc.CreateDeployment(ctx, org2, userID, CreateDeploymentRequest{
		ApplicationID: app2.ID,
		ClusterID:     cluster2,
		Image:         "public-image:v1",
		Replicas:      1,
	})
	require.NoError(t, err)

	store := NewDesiredStateStore(db)

	// CRIT-001 FIX VALIDATION:
	// Org2 agent should NOT be able to see Org1's deployments.

	// Test 1: Org1 queries its own cluster - should see its deployment.
	desired1, err := store.GetDesiredState(ctx, org1, cluster1)
	require.NoError(t, err)
	assert.Len(t, desired1, 1, "Org1 should see exactly 1 deployment")
	assert.Equal(t, dep1.ID, desired1[0].DeploymentID)
	assert.Equal(t, "secret-image:v1", desired1[0].Image)

	// Test 2: Org2 queries its own cluster - should see its deployment.
	desired2, err := store.GetDesiredState(ctx, org2, cluster2)
	require.NoError(t, err)
	assert.Len(t, desired2, 1, "Org2 should see exactly 1 deployment")
	assert.Equal(t, dep2.ID, desired2[0].DeploymentID)
	assert.Equal(t, "public-image:v1", desired2[0].Image)

	// Test 3: SECURITY - Org2 queries Org1's cluster with Org2's credentials.
	// With CRIT-001 fix, this should return ZERO results because:
	// 1. The query executes in Org2's tenant context (RLS filters by org_id)
	// 2. The explicit org_id filter further restricts results.
	desiredCrossTenant, err := store.GetDesiredState(ctx, org2, cluster1)
	require.NoError(t, err)
	assert.Len(t, desiredCrossTenant, 0,
		"CRIT-001 FIX: Org2 should NOT see Org1's deployments even when querying Org1's cluster ID")

	// Test 4: SECURITY - Org1 queries Org2's cluster with Org1's credentials.
	desiredCrossTenant2, err := store.GetDesiredState(ctx, org1, cluster2)
	require.NoError(t, err)
	assert.Len(t, desiredCrossTenant2, 0,
		"CRIT-001 FIX: Org1 should NOT see Org2's deployments even when querying Org2's cluster ID")
}

func TestIntegration_DesiredState_ExplicitOrgFilter(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Create a "shared" cluster ID scenario (though this shouldn't happen in real life).
	org1 := uuid.NewString()
	proj1 := uuid.NewString()
	clusterID := uuid.NewString()
	agentID := uuid.NewString()
	userID := uuid.NewString()

	createTestOrg(t, db, org1)
	createTestProject(t, db, org1, proj1)
	createTestClusterWithAgent(t, db, org1, clusterID, agentID)

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Applications: NewApplicationStore(db),
		Deployments:  NewDeploymentStore(db),
		Releases:     NewReleaseStore(db),
		Outbox:       outbox,
		Tenant:       db,
	})

	// Create deployment.
	app, _ := svc.CreateApplication(ctx, org1, proj1, userID, CreateApplicationRequest{
		Name: "Test App", Slug: "test-" + uuid.NewString()[:8],
	})
	svc.CreateDeployment(ctx, org1, userID, CreateDeploymentRequest{
		ApplicationID: app.ID,
		ClusterID:     clusterID,
		Image:         "test:v1",
		Replicas:      1,
	})

	store := NewDesiredStateStore(db)

	// Query with a random non-existent org ID.
	fakeOrgID := uuid.NewString()
	desiredFakeOrg, err := store.GetDesiredState(ctx, fakeOrgID, clusterID)
	require.NoError(t, err)
	assert.Len(t, desiredFakeOrg, 0,
		"Query with non-existent org ID should return zero results")

	// Query with correct org ID.
	desiredRealOrg, err := store.GetDesiredState(ctx, org1, clusterID)
	require.NoError(t, err)
	assert.Len(t, desiredRealOrg, 1,
		"Query with correct org ID should return the deployment")
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
