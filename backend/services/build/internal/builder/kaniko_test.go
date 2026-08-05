package builder

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	build "github.com/bdsplatform/platform/backend/services/build/internal"
)

// ----------------------------------------------------------------------------
// Pure helper tests
// ----------------------------------------------------------------------------

func strp(s string) *string { return &s }

func newTestBuild() *build.Build {
	b := &build.Build{
		GitURL:         strp("https://github.com/acme/app.git"),
		GitRef:         "main",
		ContextPath:    ".",
		DockerfilePath: "Dockerfile",
		BuildArgs:      json.RawMessage(`{"VERSION":"1.0","ARCH":"amd64"}`),
		TargetImage:    "acme/app:latest",
		TargetRegistry: "registry.example.com",
		PushToRegistry: true,
		BuilderType:    build.BuilderKaniko,
		TimeoutSeconds: 900,
	}
	b.ID = "BUILD-abc123"
	b.OrgID = "org-xyz"
	return b
}

func TestBuildKanikoArgs_PushWithBuildArgs(t *testing.T) {
	args, err := buildKanikoArgs(newTestBuild())
	require.NoError(t, err)
	joined := strings.Join(args, " ")

	assert.Contains(t, joined, "--context=git://github.com/acme/app.git#refs/heads/main")
	assert.Contains(t, joined, "--dockerfile=Dockerfile")
	assert.Contains(t, joined, "--digest-file=/dev/termination-log")
	assert.Contains(t, joined, "--destination=registry.example.com/acme/app:latest")
	// Build args are sorted deterministically (ARCH before VERSION).
	assert.Contains(t, joined, "--build-arg=ARCH=amd64 --build-arg=VERSION=1.0")
	assert.NotContains(t, joined, "--no-push")
}

func TestBuildKanikoArgs_NoPushAndSubPath(t *testing.T) {
	b := newTestBuild()
	b.PushToRegistry = false
	b.ContextPath = "services/api"
	args, err := buildKanikoArgs(b)
	require.NoError(t, err)
	joined := strings.Join(args, " ")

	assert.Contains(t, joined, "--no-push")
	assert.Contains(t, joined, "--context-sub-path=services/api")
	assert.NotContains(t, joined, "--destination=")
}

func TestBuildKanikoArgs_RejectsNonHTTPGit(t *testing.T) {
	b := newTestBuild()
	b.GitURL = strp("git@github.com:acme/app.git")
	_, err := buildKanikoArgs(b)
	require.Error(t, err, "ssh git URLs are not supported by the kaniko git context")
}

func TestGitContext(t *testing.T) {
	ctx, err := gitContext("https://github.com/acme/app.git", "main", nil)
	require.NoError(t, err)
	assert.Equal(t, "git://github.com/acme/app.git#refs/heads/main", ctx)

	// Explicit refs/ ref is preserved; commit pin appended.
	ctx, err = gitContext("https://github.com/acme/app.git", "refs/tags/v1", strp("deadbeef"))
	require.NoError(t, err)
	assert.Equal(t, "git://github.com/acme/app.git#refs/tags/v1#deadbeef", ctx)

	_, err = gitContext("git@github.com:acme/app.git", "main", nil)
	require.Error(t, err)
}

func TestDockerConfigJSON(t *testing.T) {
	raw, err := dockerConfigJSON("registry.example.com", "user", "pass")
	require.NoError(t, err)

	var cfg struct {
		Auths map[string]struct {
			Username, Password, Auth string
		} `json:"auths"`
	}
	require.NoError(t, json.Unmarshal(raw, &cfg))
	entry, ok := cfg.Auths["registry.example.com"]
	require.True(t, ok)
	assert.Equal(t, "user", entry.Username)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("user:pass")), entry.Auth)
}

func TestDockerConfigJSON_DockerHubHost(t *testing.T) {
	raw, err := dockerConfigJSON("docker.io", "u", "p")
	require.NoError(t, err)
	assert.Contains(t, string(raw), "https://index.docker.io/v1/")
}

func TestComposeImageRef(t *testing.T) {
	assert.Equal(t, "reg.io/app:1", composeImageRef("reg.io", "app:1"))
	assert.Equal(t, "reg.io/app:1", composeImageRef("reg.io", "reg.io/app:1"))
	assert.Equal(t, "app:1", composeImageRef("", "app:1"))
}

func TestKanikoJobName_DeterministicAndSafe(t *testing.T) {
	n1 := kanikoJobName("BUILD-abc123")
	n2 := kanikoJobName("BUILD-abc123")
	assert.Equal(t, n1, n2, "job name must be deterministic for idempotent re-claims")
	assert.Equal(t, "bdsplatform-build-build-abc123", n1)
	assert.LessOrEqual(t, len(n1), 63)
	assert.NotContains(t, n1, "_")
}

func TestTruncateName_LongInput(t *testing.T) {
	long := "bdsplatform-build-" + strings.Repeat("a", 100)
	got := truncateName(long)
	assert.LessOrEqual(t, len(got), 63)
	// Deterministic hash suffix.
	assert.Equal(t, got, truncateName(long))
}

func TestBuildKanikoJobSpec_InjectsGitAndRegistryAuth(t *testing.T) {
	svc := newFakeService(t)
	cfg := DefaultConfig()
	cfg.RegistryAuth = map[string]RegistryCredentials{
		"registry.example.com": {Username: "u", Password: "p"},
	}
	mb := NewMultiBuilder(cfg, svc.svc).
		WithCredentialProvider(fakeCredProvider{token: "ghs_secret"}).
		WithKubeBackend(nil, DefaultKanikoConfig())

	b := newTestBuild()
	job := &build.BuildJob{Build: b, OrgID: b.OrgID, LogWriter: noopLog}
	spec, err := mb.buildKanikoJobSpec(context.Background(), job)
	require.NoError(t, err)

	assert.Equal(t, "x-access-token", spec.Env["GIT_USERNAME"])
	assert.Equal(t, "ghs_secret", spec.Env["GIT_PASSWORD"])
	assert.NotEmpty(t, spec.SecretName, "registry secret name must be set when pushing with creds")
	assert.NotEmpty(t, spec.DockerConfigJSON)
	assert.Equal(t, dockerConfigMount, spec.Env["DOCKER_CONFIG"])
	assert.Equal(t, int64(900), spec.ActiveDeadlineSeconds)
	assert.Equal(t, managedByValue, spec.Labels[labelManagedBy])
}

func TestBuildKanikoJobSpec_NoRegistrySecretWhenNoPush(t *testing.T) {
	svc := newFakeService(t)
	mb := NewMultiBuilder(DefaultConfig(), svc.svc)
	b := newTestBuild()
	b.PushToRegistry = false
	job := &build.BuildJob{Build: b, OrgID: b.OrgID, LogWriter: noopLog}
	spec, err := mb.buildKanikoJobSpec(context.Background(), job)
	require.NoError(t, err)
	assert.Empty(t, spec.SecretName)
	assert.Nil(t, spec.DockerConfigJSON)
}

// ----------------------------------------------------------------------------
// Orchestration tests (fake KubeBackend)
// ----------------------------------------------------------------------------

type fakeBackend struct {
	result JobResult
	err    error
	gotLog bool
}

func (f *fakeBackend) Run(ctx context.Context, spec JobSpec, logSink func(stream, line string)) (JobResult, error) {
	logSink(build.StreamStdout, "building layer 1/3")
	f.gotLog = true
	return f.result, f.err
}

func newKanikoBuilder(t *testing.T, backend KubeBackend) (*MultiBuilder, *fakeService) {
	svc := newFakeService(t)
	mb := NewMultiBuilder(DefaultConfig(), svc.svc).WithKubeBackend(backend, DefaultKanikoConfig())
	return mb, svc
}

func runJob(t *testing.T, fs *fakeService) (*build.BuildJob, []string) {
	t.Helper()
	b := newTestBuild()
	// Seed the build so MarkBuildSucceeded can load it.
	fs.builds.builds[b.ID] = b
	var logs []string
	job := &build.BuildJob{
		Build: b, OrgID: b.OrgID,
		LogWriter:  func(level, stream, msg string, meta map[string]any) { logs = append(logs, msg) },
		OnProgress: func(string) {},
	}
	return job, logs
}

func TestBuildWithKanikoBackend_Success(t *testing.T) {
	backend := &fakeBackend{result: JobResult{Succeeded: true, Digest: "sha256:abcdef"}}
	mb, fs := newKanikoBuilder(t, backend)
	job, _ := runJob(t, fs)

	err := mb.buildWithKanikoBackend(context.Background(), job)
	require.NoError(t, err)
	assert.True(t, backend.gotLog)

	art := fs.artifacts.created
	require.NotNil(t, art, "artifact must be persisted on success")
	assert.Equal(t, "sha256:abcdef", art.ImageDigest)
	assert.Equal(t, "registry.example.com/acme/app:latest", art.ImageTag)
	assert.Equal(t, build.StatusSucceeded, fs.builds.builds[job.Build.ID].Status)
}

func TestBuildWithKanikoBackend_BackendError(t *testing.T) {
	backend := &fakeBackend{err: assertAnErr{}}
	mb, fs := newKanikoBuilder(t, backend)
	job, _ := runJob(t, fs)

	err := mb.buildWithKanikoBackend(context.Background(), job)
	require.Error(t, err)
	assert.Nil(t, fs.artifacts.created, "no artifact on backend error")
}

func TestBuildWithKanikoBackend_BuildFailed(t *testing.T) {
	backend := &fakeBackend{result: JobResult{Succeeded: false, FailureReason: "dockerfile not found"}}
	mb, fs := newKanikoBuilder(t, backend)
	job, _ := runJob(t, fs)

	err := mb.buildWithKanikoBackend(context.Background(), job)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dockerfile not found")
	assert.Nil(t, fs.artifacts.created)
}

func TestBuildWithKanikoBackend_MissingDigest(t *testing.T) {
	backend := &fakeBackend{result: JobResult{Succeeded: true, Digest: ""}}
	mb, fs := newKanikoBuilder(t, backend)
	job, _ := runJob(t, fs)

	err := mb.buildWithKanikoBackend(context.Background(), job)
	require.Error(t, err, "success without a digest must be treated as failure")
	assert.Nil(t, fs.artifacts.created)
}

type assertAnErr struct{}

func (assertAnErr) Error() string { return "backend boom" }

func noopLog(level, stream, msg string, meta map[string]any) {}

type fakeCredProvider struct{ token string }

func (f fakeCredProvider) GetGitToken(ctx context.Context, orgID, repoURL string) (string, error) {
	return f.token, nil
}

// ----------------------------------------------------------------------------
// Minimal build.Service test double (only the methods the orchestrator touches
// are meaningful; the rest satisfy the interfaces and are inert).
// ----------------------------------------------------------------------------

type fakeService struct {
	svc       *build.Service
	builds    *fakeBuildStore
	artifacts *fakeArtifactStore
}

func newFakeService(t *testing.T) *fakeService {
	t.Helper()
	builds := &fakeBuildStore{builds: map[string]*build.Build{}}
	artifacts := &fakeArtifactStore{}
	svc := build.NewService(build.Deps{
		Builds:         builds,
		BuildArtifacts: artifacts,
		BuildQueue:     &fakeQueueStore{},
		BuildLogs:      &fakeLogStore{},
		Outbox:         &fakeOutbox{},
		Tenant:         fakeTenant{},
		Now:            time.Now,
	})
	return &fakeService{svc: svc, builds: builds, artifacts: artifacts}
}

// service returns the wrapped *build.Service so it can be handed to NewMultiBuilder.
func (f *fakeService) service() *build.Service { return f.svc }

type fakeTenant struct{}

func (fakeTenant) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	return fn(ctx)
}

type fakeBuildStore struct{ builds map[string]*build.Build }

func (m *fakeBuildStore) Create(ctx context.Context, b *build.Build) error {
	if b.ID == "" {
		b.ID = "b-1"
	}
	m.builds[b.ID] = b
	return nil
}
func (m *fakeBuildStore) GetByID(ctx context.Context, id string) (*build.Build, error) {
	if b, ok := m.builds[id]; ok {
		return b, nil
	}
	return nil, database.ErrNotFound
}
func (m *fakeBuildStore) List(ctx context.Context, r database.PageRequest) (database.Page[build.Build], error) {
	return database.Page[build.Build]{}, nil
}
func (m *fakeBuildStore) ListByRepository(ctx context.Context, repoID string, r database.PageRequest) (database.Page[build.Build], error) {
	return database.Page[build.Build]{}, nil
}
func (m *fakeBuildStore) Update(ctx context.Context, b *build.Build) error { m.builds[b.ID] = b; return nil }
func (m *fakeBuildStore) UpdateStatus(ctx context.Context, id, status string, e *string) error {
	if b, ok := m.builds[id]; ok {
		b.Status = status
	}
	return nil
}
func (m *fakeBuildStore) MarkStarted(ctx context.Context, id string, commit *string) error {
	if b, ok := m.builds[id]; ok {
		b.Status = build.StatusBuilding
		b.GitCommit = commit
	}
	return nil
}
func (m *fakeBuildStore) MarkFinished(ctx context.Context, id, status string, e *string) error {
	if b, ok := m.builds[id]; ok {
		b.Status = status
		b.ErrorMessage = e
	}
	return nil
}
func (m *fakeBuildStore) IncrementRetryCount(ctx context.Context, id string) error { return nil }

type fakeArtifactStore struct{ created *build.BuildArtifact }

func (m *fakeArtifactStore) Create(ctx context.Context, a *build.BuildArtifact) error {
	m.created = a
	return nil
}
func (m *fakeArtifactStore) GetByBuildID(ctx context.Context, id string) (*build.BuildArtifact, error) {
	return nil, database.ErrNotFound
}
func (m *fakeArtifactStore) GetByDigest(ctx context.Context, d string) (*build.BuildArtifact, error) {
	return nil, database.ErrNotFound
}

type fakeQueueStore struct{}

func (fakeQueueStore) Enqueue(ctx context.Context, id string, p int) error { return nil }
func (fakeQueueStore) Dequeue(ctx context.Context, w string) (*build.BuildQueueItem, error) {
	return nil, nil
}
func (fakeQueueStore) Heartbeat(ctx context.Context, id, w string) error { return nil }
func (fakeQueueStore) Remove(ctx context.Context, id string) error       { return nil }
func (fakeQueueStore) GetStaleClaims(ctx context.Context, t time.Duration) ([]build.BuildQueueItem, error) {
	return nil, nil
}
func (fakeQueueStore) ReleaseStaleClaims(ctx context.Context, t time.Duration) error { return nil }

type fakeLogStore struct{}

func (fakeLogStore) Append(ctx context.Context, l *build.BuildLog) error       { return nil }
func (fakeLogStore) AppendBatch(ctx context.Context, l []*build.BuildLog) error { return nil }
func (fakeLogStore) List(ctx context.Context, id string, r database.PageRequest) (database.Page[build.BuildLog], error) {
	return database.Page[build.BuildLog]{}, nil
}
func (fakeLogStore) GetNextSequence(ctx context.Context, id string) (int, error) { return 1, nil }

type fakeOutbox struct{ enqueued []events.Envelope }

func (m *fakeOutbox) Enqueue(ctx context.Context, e events.Envelope) error {
	m.enqueued = append(m.enqueued, e)
	return nil
}
func (m *fakeOutbox) FetchUnpublished(ctx context.Context, limit int) ([]events.OutboxRecord, error) {
	return nil, nil
}
func (m *fakeOutbox) MarkPublished(ctx context.Context, ids []string) error { return nil }

var _ authz.OrgMemberStore = (*inertOrgMembers)(nil)

type inertOrgMembers struct{}

func (inertOrgMembers) GetOrgMember(ctx context.Context, userID string) (*authz.OrgMember, error) {
	return nil, database.ErrNotFound
}
