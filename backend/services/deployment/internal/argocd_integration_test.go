//go:build integration_argocd

// DB-backed integration + migration test for the GitOps (Argo CD) binding.
// Verifies the 0005_argo_applications migration applies, the repository upserts
// / reads / updates observed status, and RLS isolates tenants. Run with:
//
//	TEST_DATABASE_URL=postgres://... go test -tags integration_argocd \
//	    ./services/deployment/internal/ -run TestArgoApplications -v

package deployment

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/argocd"
	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/migrations"
)

func argoTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	cfg := config.Config{}
	cfg.Database.URL = dsn

	ctx := context.Background()
	db, err := database.Connect(ctx, cfg)
	require.NoError(t, err)

	// Applying the deployment migrations must include 0005_argo_applications.
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
		for _, table := range []string{"argo_applications", "rollout_status", "releases", "deployments", "applications"} {
			_, _ = db.Pool.Exec(ctx, "DELETE FROM "+table+" WHERE true")
		}
		db.Close()
	})
	return db
}

func argoExec(t *testing.T, db *database.DB, sql string, args ...any) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

func TestArgoApplicationsMigrationAndRepo(t *testing.T) {
	db := argoTestDB(t)
	ctx := context.Background()
	store := NewArgoApplicationStore(db)

	orgID := uuid.NewString()
	otherOrg := uuid.NewString()
	projectID := uuid.NewString()
	appID := uuid.NewString()
	clusterID := uuid.NewString()
	depID := uuid.NewString()

	argoExec(t, db, `INSERT INTO organizations (id, name, slug) VALUES ($1,'Org','org-'||substr($1::text,1,8))`, orgID)
	argoExec(t, db, `INSERT INTO organizations (id, name, slug) VALUES ($1,'Org2','org-'||substr($1::text,1,8))`, otherOrg)
	argoExec(t, db, `INSERT INTO projects (id, org_id, name, slug, status) VALUES ($1,$2,'P','p-'||substr($1::text,1,8),'active')`, projectID, orgID)
	argoExec(t, db, `INSERT INTO clusters (id, org_id, name, slug, status) VALUES ($1,$2,'C','c-'||substr($1::text,1,8),'connected')`, clusterID, orgID)

	require.NoError(t, db.WithTenant(ctx, orgID, func(ctx context.Context) error {
		app := &Application{ProjectID: projectID, Name: "app", Slug: "app", RuntimeType: RuntimeContainer}
		app.ID = appID
		app.OrgID = orgID
		require.NoError(t, NewApplicationStore(db).Create(ctx, app))

		dep := &Deployment{ApplicationID: appID, ClusterID: clusterID, Image: "nginx:1", Replicas: 1, EnvVars: []byte("[]"), Status: StatusPending}
		dep.ID = depID
		dep.OrgID = orgID
		require.NoError(t, NewDeploymentStore(db).Create(ctx, dep))
		return nil
	}))

	// Upsert + Get.
	require.NoError(t, db.WithTenant(ctx, orgID, func(ctx context.Context) error {
		rec := &ArgoApplication{
			DeploymentID:  depID,
			OrgID:         orgID,
			AppName:       "app-" + depID[:8],
			RepoURL:       "https://github.com/acme/config",
			Path:          "prod",
			TargetRevision: "main",
			SourceType:    SourceTypeDirectory,
			DestNamespace: "app",
			AutoSync:      true,
			SelfHeal:      true,
			Prune:         true,
		}
		require.NoError(t, store.Upsert(ctx, rec))

		got, err := store.Get(ctx, depID)
		require.NoError(t, err)
		assert.Equal(t, "https://github.com/acme/config", got.RepoURL)
		assert.Equal(t, argocd.SyncStatusUnknown, got.SyncStatus)

		// UpdateObserved mirrors Argo status.
		got.SyncStatus = argocd.SyncStatusSynced
		got.HealthStatus = argocd.HealthStatusHealthy
		got.OperationPhase = argocd.OperationSucceeded
		got.SyncedRevision = "abc123"
		got.Drift = false
		require.NoError(t, store.UpdateObserved(ctx, got))

		reread, err := store.Get(ctx, depID)
		require.NoError(t, err)
		assert.Equal(t, argocd.SyncStatusSynced, reread.SyncStatus)
		assert.Equal(t, "abc123", reread.SyncedRevision)
		require.NotNil(t, reread.ObservedAt)
		return nil
	}))

	// RLS: another tenant cannot see the row.
	require.NoError(t, db.WithTenant(ctx, otherOrg, func(ctx context.Context) error {
		_, err := store.Get(ctx, depID)
		assert.True(t, database.IsNotFound(err), "cross-tenant read must be denied by RLS")
		return nil
	}))
}
