//go:build integration_buildconsumer

// This DB-backed integration test lives under a dedicated build tag with a
// self-contained harness so it can be exercised independently of the package's
// legacy `integration`-tagged suite. Run it with:
//
//	TEST_DATABASE_URL=postgres://... go test -tags integration_buildconsumer \
//	    ./services/deployment/internal/ -run TestBuildConsumerIntegration -v

package deployment

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/migrations"
)

func bcTestDB(t *testing.T) *database.DB {
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
		for _, table := range []string{"deployment_processed_events", "releases", "deployments", "applications", "outbox"} {
			if _, err := db.Pool.Exec(ctx, "DELETE FROM "+table+" WHERE true"); err != nil {
				t.Logf("cleanup %s: %v", table, err)
			}
		}
		db.Close()
	})

	return db
}

func bcExec(t *testing.T, db *database.DB, sql string, args ...any) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

// TestBuildConsumerIntegration exercises the full Build -> Deployment flow
// against a real database: a build.succeeded event creates a new immutable
// release pinned to the built digest, repoints the deployment, surfaces the
// image in the agent's desired state, and is idempotent on redelivery.
func TestBuildConsumerIntegration(t *testing.T) {
	db := bcTestDB(t)
	ctx := context.Background()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	appID := uuid.NewString()
	clusterID := uuid.NewString()
	depID := uuid.NewString()

	bcExec(t, db, `INSERT INTO organizations (id, name, slug) VALUES ($1, 'Org', 'org-'||substr($1::text,1,8))`, orgID)
	bcExec(t, db, `INSERT INTO projects (id, org_id, name, slug, status) VALUES ($1,$2,'Proj','proj-'||substr($1::text,1,8),'active')`, projectID, orgID)
	bcExec(t, db, `INSERT INTO clusters (id, org_id, name, slug, status) VALUES ($1,$2,'Cl','cl-'||substr($1::text,1,8),'connected')`, clusterID, orgID)

	// Seed an application + deployment + initial release directly (RLS-scoped).
	require.NoError(t, db.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := NewApplicationStore(db).Create(ctx, &Application{
			TenantModel: database.TenantModel{Model: database.Model{ID: appID}, OrgID: orgID},
			ProjectID:   projectID, Name: "API", Slug: "api-" + uuid.NewString()[:8], RuntimeType: RuntimeContainer,
		}); err != nil {
			return err
		}
		dep := &Deployment{
			TenantModel: database.TenantModel{Model: database.Model{ID: depID}, OrgID: orgID},
			ApplicationID: appID, ClusterID: clusterID, Image: "ghcr.io/acme/api:v1", Replicas: 2,
			Status: StatusSucceeded, EnvVars: []byte("[]"),
		}
		if err := NewDeploymentStore(db).Create(ctx, dep); err != nil {
			return err
		}
		return NewReleaseStore(db).Create(ctx, &Release{
			OrgID: orgID, DeploymentID: depID, Revision: 1, Image: "ghcr.io/acme/api:v1",
			Replicas: 2, ConfigHash: "seed", Config: []byte(`{}`), Status: ReleaseStatusSucceeded,
		})
	}))

	consumer := NewBuildConsumer(BuildConsumerDeps{
		Deployments: NewDeploymentStore(db),
		Releases:    NewReleaseStore(db),
		Processed:   NewProcessedEventStore(db),
		Outbox:      events.NewPostgresOutbox(db, "outbox"),
		Tenant:      db,
	})

	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	pinned := "ghcr.io/acme/api@" + digest
	evt, err := events.New(eventBuildSucceeded, 1, orgID, buildSucceededEvent{
		BuildID: uuid.NewString(), Image: "acme/api", Registry: "ghcr.io", ImageTag: "v2", ImageDigest: digest,
	})
	require.NoError(t, err)

	require.NoError(t, consumer.handle(ctx, evt))

	var latest *Release
	var updated *Deployment
	require.NoError(t, db.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var e error
		if latest, e = NewReleaseStore(db).GetLatest(ctx, depID); e != nil {
			return e
		}
		updated, e = NewDeploymentStore(db).GetByID(ctx, depID)
		return e
	}))

	assert.Equal(t, 2, latest.Revision)
	assert.Equal(t, pinned, latest.Image)
	assert.Equal(t, ReleaseStatusPending, latest.Status)
	assert.Equal(t, pinned, updated.Image)
	assert.Equal(t, StatusPending, updated.Status)

	// The agent's derived desired state serves the freshly built image.
	desired, err := NewDesiredStateStore(db).GetDesiredState(ctx, orgID, clusterID)
	require.NoError(t, err)
	require.Len(t, desired, 1)
	assert.Equal(t, pinned, desired[0].Image)

	// Redelivery of the same event is idempotent.
	require.NoError(t, consumer.handle(ctx, evt))
	require.NoError(t, db.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var e error
		latest, e = NewReleaseStore(db).GetLatest(ctx, depID)
		return e
	}))
	assert.Equal(t, 2, latest.Revision, "duplicate build.succeeded must not create another release")
}
