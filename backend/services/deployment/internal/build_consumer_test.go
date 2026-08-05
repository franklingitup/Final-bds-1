package deployment

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/events"
)

// ----------------------------------------------------------------------------
// Fake processed-event store (idempotency ledger)
// ----------------------------------------------------------------------------

type fakeProcessedEventStore struct {
	seen map[string]bool
	err  error
}

func newFakeProcessedEventStore() *fakeProcessedEventStore {
	return &fakeProcessedEventStore{seen: make(map[string]bool)}
}

func (f *fakeProcessedEventStore) MarkProcessed(ctx context.Context, consumer, eventID, orgID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	key := consumer + "|" + eventID
	if f.seen[key] {
		return false, nil
	}
	f.seen[key] = true
	return true, nil
}

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

func newTestBuildConsumer() (*BuildConsumer, *fakeDeploymentStore, *fakeReleaseStore, *fakeProcessedEventStore, *fakeOutbox) {
	deps := newFakeDeploymentStore()
	rels := newFakeReleaseStore()
	processed := newFakeProcessedEventStore()
	outbox := &fakeOutbox{}

	c := NewBuildConsumer(BuildConsumerDeps{
		Deployments: deps,
		Releases:    rels,
		Processed:   processed,
		Outbox:      outbox,
		Tenant:      &fakeTenant{},
	})
	return c, deps, rels, processed, outbox
}

func buildSucceededEnvelope(t *testing.T, orgID string, p buildSucceededEvent) events.Envelope {
	t.Helper()
	e, err := events.New(eventBuildSucceeded, 1, orgID, p)
	require.NoError(t, err)
	return e
}

func seedDeployment(deps *fakeDeploymentStore, id, orgID, image string) {
	d := &Deployment{
		ApplicationID: "app-" + id,
		ClusterID:     "cluster-" + id,
		Image:         image,
		Replicas:      2,
		Status:        StatusSucceeded,
		EnvVars:       []byte("[]"),
	}
	d.ID = id
	d.OrgID = orgID
	d.Version = 1
	deps.deps[id] = d
}

// ----------------------------------------------------------------------------
// Image reference helpers
// ----------------------------------------------------------------------------

func TestImageRepository(t *testing.T) {
	cases := map[string]string{
		"ghcr.io/acme/api:v1.2.3":                   "ghcr.io/acme/api",
		"ghcr.io/acme/api@sha256:deadbeef":          "ghcr.io/acme/api",
		"ghcr.io/acme/api:v1@sha256:deadbeef":       "ghcr.io/acme/api",
		"ghcr.io/acme/api":                          "ghcr.io/acme/api",
		"localhost:5000/acme/api:latest":            "localhost:5000/acme/api",
		"localhost:5000/acme/api":                   "localhost:5000/acme/api",
		"registry:5000/team/app@sha256:abc":         "registry:5000/team/app",
		"docker.io/library/nginx:1.27":              "docker.io/library/nginx",
	}
	for in, want := range cases {
		assert.Equalf(t, want, imageRepository(in), "imageRepository(%q)", in)
	}
}

func TestComposeImageRef(t *testing.T) {
	assert.Equal(t, "ghcr.io/acme/api", composeImageRef("ghcr.io", "acme/api"))
	assert.Equal(t, "ghcr.io/acme/api", composeImageRef("ghcr.io", "ghcr.io/acme/api"))
	assert.Equal(t, "acme/api", composeImageRef("", "acme/api"))
	// Image already carries its own registry host: leave untouched.
	assert.Equal(t, "docker.io/library/nginx", composeImageRef("ghcr.io", "docker.io/library/nginx"))
	assert.Equal(t, "localhost:5000/app", composeImageRef("ghcr.io", "localhost:5000/app"))
}

// ----------------------------------------------------------------------------
// Consumer behavior
// ----------------------------------------------------------------------------

func TestBuildConsumer_CreatesReleaseForMatchingDeployment(t *testing.T) {
	c, deps, rels, _, outbox := newTestBuildConsumer()
	ctx := context.Background()
	orgID := "org-1"

	seedDeployment(deps, "dep-1", orgID, "ghcr.io/acme/api:v1")
	seedDeployment(deps, "dep-2", orgID, "ghcr.io/acme/worker:v1") // different repo, must be skipped

	e := buildSucceededEnvelope(t, orgID, buildSucceededEvent{
		BuildID:     "build-1",
		Image:       "acme/api",
		Registry:    "ghcr.io",
		ImageTag:    "v2",
		ImageDigest: "sha256:cafebabe",
	})

	require.NoError(t, c.handle(ctx, e))

	// A new release pinned to the digest exists for the matching deployment only.
	relsForDep := rels.byDep["dep-1"]
	require.Len(t, relsForDep, 1)
	assert.Equal(t, 1, relsForDep[0].Revision)
	assert.Equal(t, "ghcr.io/acme/api@sha256:cafebabe", relsForDep[0].Image)
	assert.Equal(t, ReleaseStatusPending, relsForDep[0].Status)
	assert.Equal(t, orgID, relsForDep[0].OrgID)
	assert.Empty(t, rels.byDep["dep-2"], "non-matching deployment must not get a release")

	// The deployment now points at the immutable image and is pending again.
	assert.Equal(t, "ghcr.io/acme/api@sha256:cafebabe", deps.deps["dep-1"].Image)
	assert.Equal(t, StatusPending, deps.deps["dep-1"].Status)
	assert.Equal(t, "ghcr.io/acme/worker:v1", deps.deps["dep-2"].Image)

	// A deployment.created event was emitted for the new revision.
	require.Len(t, outbox.events, 1)
	assert.Equal(t, EventDeploymentCreated, outbox.events[0].Type)
	assert.Equal(t, orgID, outbox.events[0].OrgID)
	assert.Equal(t, "system", outbox.events[0].Actor.Type)
}

func TestBuildConsumer_NextRevisionFromLatest(t *testing.T) {
	c, deps, rels, _, _ := newTestBuildConsumer()
	ctx := context.Background()
	orgID := "org-1"

	seedDeployment(deps, "dep-1", orgID, "ghcr.io/acme/api:v1")
	// Existing release at revision 3.
	require.NoError(t, rels.Create(ctx, &Release{
		ID: "rel-existing", OrgID: orgID, DeploymentID: "dep-1", Revision: 3,
		Image: "ghcr.io/acme/api@sha256:old", Replicas: 2, Status: ReleaseStatusSucceeded,
	}))

	e := buildSucceededEnvelope(t, orgID, buildSucceededEvent{
		BuildID: "build-1", Image: "acme/api", Registry: "ghcr.io", ImageDigest: "sha256:new",
	})
	require.NoError(t, c.handle(ctx, e))

	relsForDep := rels.byDep["dep-1"]
	require.Len(t, relsForDep, 2)
	assert.Equal(t, 4, relsForDep[1].Revision, "revision should increment from the latest release")
}

func TestBuildConsumer_DeduplicatesByEventID(t *testing.T) {
	c, deps, rels, _, outbox := newTestBuildConsumer()
	ctx := context.Background()
	orgID := "org-1"

	seedDeployment(deps, "dep-1", orgID, "ghcr.io/acme/api:v1")
	e := buildSucceededEnvelope(t, orgID, buildSucceededEvent{
		BuildID: "build-1", Image: "acme/api", Registry: "ghcr.io", ImageDigest: "sha256:cafebabe",
	})

	require.NoError(t, c.handle(ctx, e))
	require.NoError(t, c.handle(ctx, e)) // redelivery of the same event id

	assert.Len(t, rels.byDep["dep-1"], 1, "duplicate delivery must not create a second release")
	assert.Len(t, outbox.events, 1, "duplicate delivery must not emit a second event")
}

func TestBuildConsumer_NoMatchingDeployment(t *testing.T) {
	c, deps, rels, _, outbox := newTestBuildConsumer()
	ctx := context.Background()
	orgID := "org-1"

	seedDeployment(deps, "dep-1", orgID, "ghcr.io/acme/other:v1")
	e := buildSucceededEnvelope(t, orgID, buildSucceededEvent{
		BuildID: "build-1", Image: "acme/api", Registry: "ghcr.io", ImageDigest: "sha256:cafebabe",
	})

	require.NoError(t, c.handle(ctx, e))
	assert.Empty(t, rels.byDep["dep-1"])
	assert.Empty(t, outbox.events)
}

func TestBuildConsumer_InvalidPayloadIsAcked(t *testing.T) {
	c, deps, _, _, _ := newTestBuildConsumer()
	ctx := context.Background()
	orgID := "org-1"
	seedDeployment(deps, "dep-1", orgID, "ghcr.io/acme/api:v1")

	// Missing image digest -> unfixable by retry -> acknowledged (nil error).
	e := buildSucceededEnvelope(t, orgID, buildSucceededEvent{
		BuildID: "build-1", Image: "acme/api", Registry: "ghcr.io",
	})
	require.NoError(t, c.handle(ctx, e))

	res, err := c.process(ctx, e)
	require.NoError(t, err)
	assert.Equal(t, outcomeInvalid, res.outcome)
}

func TestBuildConsumer_WrongEventTypeIgnored(t *testing.T) {
	c, _, _, _, _ := newTestBuildConsumer()
	ctx := context.Background()

	e, err := events.New("build.failed", 1, "org-1", map[string]string{"buildId": "b1"})
	require.NoError(t, err)
	require.NoError(t, c.handle(ctx, e))
}

func TestBuildConsumer_TransientErrorIsRetried(t *testing.T) {
	c, deps, _, processed, _ := newTestBuildConsumer()
	ctx := context.Background()
	orgID := "org-1"
	seedDeployment(deps, "dep-1", orgID, "ghcr.io/acme/api:v1")

	processed.err = errors.New("db unavailable")
	e := buildSucceededEnvelope(t, orgID, buildSucceededEvent{
		BuildID: "build-1", Image: "acme/api", Registry: "ghcr.io", ImageDigest: "sha256:cafebabe",
	})

	// A storage error must propagate so the framework retries / dead-letters.
	assert.Error(t, c.handle(ctx, e))
}
