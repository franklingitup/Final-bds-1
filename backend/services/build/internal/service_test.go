package build

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// mockRepositoryStore implements RepositoryStore for testing.
type mockRepositoryStore struct {
	repos map[string]*GitRepository
}

func newMockRepositoryStore() *mockRepositoryStore {
	return &mockRepositoryStore{repos: make(map[string]*GitRepository)}
}

func (m *mockRepositoryStore) Create(ctx context.Context, r *GitRepository) error {
	if r.ID == "" {
		r.ID = "repo-123"
	}
	r.CreatedAt = time.Now()
	r.UpdatedAt = time.Now()
	r.Version = 1
	m.repos[r.ID] = r
	return nil
}

func (m *mockRepositoryStore) GetByID(ctx context.Context, id string) (*GitRepository, error) {
	if repo, ok := m.repos[id]; ok {
		return repo, nil
	}
	return nil, errRepositoryNotFound
}

func (m *mockRepositoryStore) GetByName(ctx context.Context, projectID, name string) (*GitRepository, error) {
	for _, r := range m.repos {
		if r.ProjectID == projectID && r.Name == name {
			return r, nil
		}
	}
	return nil, errRepositoryNotFound
}

func (m *mockRepositoryStore) List(ctx context.Context, projectID string, req database.PageRequest) (database.Page[GitRepository], error) {
	var items []GitRepository
	for _, r := range m.repos {
		if r.ProjectID == projectID {
			items = append(items, *r)
		}
	}
	return database.Page[GitRepository]{Items: items}, nil
}

func (m *mockRepositoryStore) Update(ctx context.Context, r *GitRepository) error {
	m.repos[r.ID] = r
	return nil
}

func (m *mockRepositoryStore) Delete(ctx context.Context, id string) error {
	delete(m.repos, id)
	return nil
}

// mockBuildStore implements BuildStore for testing.
type mockBuildStore struct {
	builds map[string]*Build
}

func newMockBuildStore() *mockBuildStore {
	return &mockBuildStore{builds: make(map[string]*Build)}
}

func (m *mockBuildStore) Create(ctx context.Context, b *Build) error {
	if b.ID == "" {
		b.ID = "build-123"
	}
	b.QueuedAt = time.Now()
	b.CreatedAt = time.Now()
	b.UpdatedAt = time.Now()
	b.Version = 1
	m.builds[b.ID] = b
	return nil
}

func (m *mockBuildStore) GetByID(ctx context.Context, id string) (*Build, error) {
	if b, ok := m.builds[id]; ok {
		return b, nil
	}
	return nil, errBuildNotFound
}

func (m *mockBuildStore) List(ctx context.Context, req database.PageRequest) (database.Page[Build], error) {
	var items []Build
	for _, b := range m.builds {
		items = append(items, *b)
	}
	return database.Page[Build]{Items: items}, nil
}

func (m *mockBuildStore) ListByRepository(ctx context.Context, repoID string, req database.PageRequest) (database.Page[Build], error) {
	var items []Build
	for _, b := range m.builds {
		if b.RepositoryID != nil && *b.RepositoryID == repoID {
			items = append(items, *b)
		}
	}
	return database.Page[Build]{Items: items}, nil
}

func (m *mockBuildStore) Update(ctx context.Context, b *Build) error {
	m.builds[b.ID] = b
	return nil
}

func (m *mockBuildStore) UpdateStatus(ctx context.Context, id, status string, errorMsg *string) error {
	if b, ok := m.builds[id]; ok {
		b.Status = status
		b.ErrorMessage = errorMsg
		return nil
	}
	return errBuildNotFound
}

func (m *mockBuildStore) MarkStarted(ctx context.Context, id string, commit *string) error {
	if b, ok := m.builds[id]; ok {
		b.Status = StatusBuilding
		b.GitCommit = commit
		now := time.Now()
		b.StartedAt = &now
		return nil
	}
	return errBuildNotFound
}

func (m *mockBuildStore) MarkFinished(ctx context.Context, id, status string, errorMsg *string) error {
	if b, ok := m.builds[id]; ok {
		b.Status = status
		b.ErrorMessage = errorMsg
		now := time.Now()
		b.FinishedAt = &now
		return nil
	}
	return errBuildNotFound
}

func (m *mockBuildStore) IncrementRetryCount(ctx context.Context, id string) error {
	if b, ok := m.builds[id]; ok {
		b.RetryCount++
		return nil
	}
	return errBuildNotFound
}

// mockBuildLogStore implements BuildLogStore for testing.
type mockBuildLogStore struct {
	logs map[string][]*BuildLog
}

func newMockBuildLogStore() *mockBuildLogStore {
	return &mockBuildLogStore{logs: make(map[string][]*BuildLog)}
}

func (m *mockBuildLogStore) Append(ctx context.Context, log *BuildLog) error {
	m.logs[log.BuildID] = append(m.logs[log.BuildID], log)
	return nil
}

func (m *mockBuildLogStore) AppendBatch(ctx context.Context, logs []*BuildLog) error {
	for _, log := range logs {
		m.logs[log.BuildID] = append(m.logs[log.BuildID], log)
	}
	return nil
}

func (m *mockBuildLogStore) List(ctx context.Context, buildID string, req database.PageRequest) (database.Page[BuildLog], error) {
	var items []BuildLog
	for _, log := range m.logs[buildID] {
		items = append(items, *log)
	}
	return database.Page[BuildLog]{Items: items}, nil
}

func (m *mockBuildLogStore) GetNextSequence(ctx context.Context, buildID string) (int, error) {
	return len(m.logs[buildID]) + 1, nil
}

// mockBuildArtifactStore implements BuildArtifactStore for testing.
type mockBuildArtifactStore struct {
	artifacts map[string]*BuildArtifact
}

func newMockBuildArtifactStore() *mockBuildArtifactStore {
	return &mockBuildArtifactStore{artifacts: make(map[string]*BuildArtifact)}
}

func (m *mockBuildArtifactStore) Create(ctx context.Context, a *BuildArtifact) error {
	if a.ID == "" {
		a.ID = "artifact-123"
	}
	a.CreatedAt = time.Now()
	m.artifacts[a.BuildID] = a
	return nil
}

func (m *mockBuildArtifactStore) GetByBuildID(ctx context.Context, buildID string) (*BuildArtifact, error) {
	if a, ok := m.artifacts[buildID]; ok {
		return a, nil
	}
	return nil, errBuildNotFound
}

func (m *mockBuildArtifactStore) GetByDigest(ctx context.Context, digest string) (*BuildArtifact, error) {
	for _, a := range m.artifacts {
		if a.ImageDigest == digest {
			return a, nil
		}
	}
	return nil, errBuildNotFound
}

// mockBuildQueueStore implements BuildQueueStore for testing.
type mockBuildQueueStore struct {
	items map[string]*BuildQueueItem
}

func newMockBuildQueueStore() *mockBuildQueueStore {
	return &mockBuildQueueStore{items: make(map[string]*BuildQueueItem)}
}

func (m *mockBuildQueueStore) Enqueue(ctx context.Context, buildID string, priority int) error {
	m.items[buildID] = &BuildQueueItem{
		BuildID:   buildID,
		Priority:  priority,
		CreatedAt: time.Now(),
	}
	return nil
}

func (m *mockBuildQueueStore) Dequeue(ctx context.Context, workerID string) (*BuildQueueItem, error) {
	for _, item := range m.items {
		if item.WorkerID == nil {
			item.WorkerID = &workerID
			now := time.Now()
			item.ClaimedAt = &now
			item.HeartbeatAt = &now
			return item, nil
		}
	}
	return nil, nil
}

func (m *mockBuildQueueStore) Heartbeat(ctx context.Context, buildID, workerID string) error {
	if item, ok := m.items[buildID]; ok {
		now := time.Now()
		item.HeartbeatAt = &now
		return nil
	}
	return errBuildNotFound
}

func (m *mockBuildQueueStore) Remove(ctx context.Context, buildID string) error {
	delete(m.items, buildID)
	return nil
}

func (m *mockBuildQueueStore) GetStaleClaims(ctx context.Context, timeout time.Duration) ([]BuildQueueItem, error) {
	return nil, nil
}

func (m *mockBuildQueueStore) ReleaseStaleClaims(ctx context.Context, timeout time.Duration) error {
	return nil
}

// mockOrgMemberStore implements authz.OrgMemberStore for testing.
type mockOrgMemberStore struct{}

func (m *mockOrgMemberStore) GetMembership(ctx context.Context, orgID, userID string) (any, error) {
	return struct {
		Role string
	}{Role: "admin"}, nil
}

func (m *mockOrgMemberStore) HasMembership(ctx context.Context, orgID, userID string) (bool, error) {
	return true, nil
}

// mockOutbox implements events.Outbox for testing.
type mockOutbox struct{}

func (m *mockOutbox) Enqueue(ctx context.Context, e any) error { return nil }

func (m *mockOutbox) FetchUnpublished(ctx context.Context, limit int) (any, error) { return nil, nil }

func (m *mockOutbox) MarkPublished(ctx context.Context, ids []string) error { return nil }

// mockTenant implements TenantRunner for testing.
type mockTenant struct{}

func (m *mockTenant) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	return fn(ctx)
}

type testOrgMemberStore struct {
	orgID string
	role  authz.OrgRole
}

func (s testOrgMemberStore) GetOrgMember(_ context.Context, userID string) (*authz.OrgMember, error) {
	return &authz.OrgMember{OrgID: s.orgID, UserID: userID, Role: s.role, Status: "active"}, nil
}

type testProjectMemberStore struct {
	members map[string]authz.ProjectRole
}

func (s testProjectMemberStore) GetProjectMember(_ context.Context, projectID, userID string) (*authz.ProjectMember, error) {
	role, ok := s.members[projectID+"/"+userID]
	if !ok {
		return nil, apperrors.NotFound("project member not found")
	}
	return &authz.ProjectMember{ProjectID: projectID, UserID: userID, Role: role}, nil
}

type testOutbox struct{}

func (testOutbox) Enqueue(context.Context, events.Envelope) error { return nil }
func (testOutbox) FetchUnpublished(context.Context, int) ([]events.OutboxRecord, error) {
	return nil, nil
}
func (testOutbox) MarkPublished(context.Context, []string) error { return nil }

func TestBuild_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"queued is not terminal", StatusQueued, false},
		{"building is not terminal", StatusBuilding, false},
		{"succeeded is terminal", StatusSucceeded, true},
		{"failed is terminal", StatusFailed, true},
		{"cancelled is terminal", StatusCancelled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Build{Status: tt.status}
			if got := b.IsTerminal(); got != tt.want {
				t.Errorf("Build.IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuild_CanRetry(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		retryCount int
		maxRetries int
		want       bool
	}{
		{"succeeded cannot retry", StatusSucceeded, 0, 3, false},
		{"failed with retries left can retry", StatusFailed, 1, 3, true},
		{"failed at max retries cannot retry", StatusFailed, 3, 3, false},
		{"queued cannot retry", StatusQueued, 0, 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Build{
				Status:     tt.status,
				RetryCount: tt.retryCount,
				MaxRetries: tt.maxRetries,
			}
			if got := b.CanRetry(); got != tt.want {
				t.Errorf("Build.CanRetry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuild_CanCancel(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"queued can cancel", StatusQueued, true},
		{"building can cancel", StatusBuilding, true},
		{"succeeded cannot cancel", StatusSucceeded, false},
		{"failed cannot cancel", StatusFailed, false},
		{"cancelled cannot cancel", StatusCancelled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Build{Status: tt.status}
			if got := b.CanCancel(); got != tt.want {
				t.Errorf("Build.CanCancel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProjectDeveloperCanCreateAndCancelRepositoryBuild(t *testing.T) {
	const (
		orgID     = "org-1"
		projectID = "project-1"
		userID    = "user-1"
		repoID    = "repo-1"
	)
	repos := newMockRepositoryStore()
	repos.repos[repoID] = &GitRepository{
		ProjectID:     projectID,
		Name:          "app",
		URL:           "https://github.com/acme/app.git",
		DefaultBranch: "main",
	}
	repos.repos[repoID].ID = repoID
	repos.repos[repoID].OrgID = orgID

	svc := NewService(Deps{
		Repositories:   repos,
		Builds:         newMockBuildStore(),
		BuildLogs:      newMockBuildLogStore(),
		BuildArtifacts: newMockBuildArtifactStore(),
		BuildQueue:     newMockBuildQueueStore(),
		OrgMembers:     testOrgMemberStore{orgID: orgID, role: authz.RoleMember},
		ProjectMembers: testProjectMemberStore{members: map[string]authz.ProjectRole{
			projectID + "/" + userID: authz.ProjectDeveloper,
		}},
		Outbox: testOutbox{},
		Tenant: &mockTenant{},
	})

	build, err := svc.CreateBuild(context.Background(), orgID, userID, CreateBuildRequest{
		RepositoryID:   ptr(repoID),
		TargetImage:    "acme/app",
		TargetRegistry: "registry.example.com",
	})
	if err != nil {
		t.Fatalf("project developer should create repository build: %v", err)
	}

	if _, err := svc.CancelBuild(context.Background(), orgID, userID, build.ID); err != nil {
		t.Fatalf("project developer should cancel repository build: %v", err)
	}
}

func ptr[T any](value T) *T { return &value }

// TestValidateGitURL_ServiceLayer exercises ValidateGitURL from the perspective
// of the service callers. It uses a stubbed LookupHostFunc so the tests never
// hit real DNS and pass in offline / CI environments.
func TestValidateGitURL_ServiceLayer(t *testing.T) {
	const pubIP = "140.82.121.4" // representative public GitHub IP

	tests := []struct {
		name    string
		url     string
		lookup  map[string][]string // nil ⇒ no stub needed (raw IP in URL)
		wantOK  bool
	}{
		{
			name:   "https with public hostname",
			url:    "https://github.com/org/repo.git",
			lookup: map[string][]string{"github.com": {pubIP}},
			wantOK: true,
		},
		{
			name:   "http with public hostname",
			url:    "http://github.com/org/repo.git",
			lookup: map[string][]string{"github.com": {pubIP}},
			wantOK: true,
		},
		{
			name:   "ssh shorthand with public hostname",
			url:    "git@github.com:org/repo.git",
			lookup: map[string][]string{"github.com": {pubIP}},
			wantOK: true,
		},
		{
			name:    "raw public IP",
			url:     "http://8.8.8.8/repo.git",
			lookup:  nil,
			wantOK:  true,
		},
		{
			name:    "invalid scheme ftp",
			url:     "ftp://github.com/repo.git",
			lookup:  nil,
			wantOK:  false,
		},
		{
			name:    "not a URL",
			url:     "not-a-url",
			lookup:  nil,
			wantOK:  false,
		},
		{
			name:    "empty",
			url:     "",
			lookup:  nil,
			wantOK:  false,
		},
		{
			name:    "loopback raw IP",
			url:     "http://127.0.0.1/repo.git",
			lookup:  nil,
			wantOK:  false,
		},
		{
			name:    "cloud metadata endpoint raw IP",
			url:     "http://169.254.169.254/latest/meta-data/",
			lookup:  nil,
			wantOK:  false,
		},
		{
			name:    "RFC1918 raw IP",
			url:     "http://10.0.0.1/repo.git",
			lookup:  nil,
			wantOK:  false,
		},
		{
			name:   "hostname resolving to private IP",
			url:    "https://evil.internal.example.com/repo.git",
			lookup: map[string][]string{"evil.internal.example.com": {"192.168.1.1"}},
			wantOK: false,
		},
	}

	orig := LookupHostFunc
	defer func() { LookupHostFunc = orig }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.lookup != nil {
				LookupHostFunc = func(_ context.Context, host string) ([]string, error) {
					if ips, ok := tt.lookup[host]; ok {
						return ips, nil
					}
					return nil, fmt.Errorf("stubLookup: no record for %q", host)
				}
			} else {
				// Reset to a stub that panics if called — raw IP tests must
				// not trigger DNS.
				LookupHostFunc = func(_ context.Context, host string) ([]string, error) {
					t.Errorf("DNS lookup unexpectedly called for host %q in test %q", host, tt.name)
					return nil, fmt.Errorf("unexpected lookup")
				}
			}

			err := ValidateGitURL(context.Background(), tt.url)
			if tt.wantOK && err != nil {
				t.Errorf("ValidateGitURL(%q) returned unexpected error: %v", tt.url, err)
			}
			if !tt.wantOK && err == nil {
				t.Errorf("ValidateGitURL(%q) should have returned an error but did not", tt.url)
			}
		})
	}

	// Restore after the loop (the defer above also handles this, but be explicit).
	LookupHostFunc = orig
}

func TestToBuildView(t *testing.T) {
	now := time.Now()
	finished := now.Add(5 * time.Minute)
	repoID := "repo-123"
	gitURL := "https://github.com/org/repo.git"

	b := &Build{
		RepositoryID:   &repoID,
		GitURL:         &gitURL,
		GitRef:         "main",
		ContextPath:    ".",
		DockerfilePath: "Dockerfile",
		BuildArgs:      json.RawMessage(`{"VERSION":"1.0"}`),
		TargetImage:    "myapp:latest",
		TargetRegistry: "docker.io",
		PushToRegistry: true,
		BuilderType:    BuilderKaniko,
		Status:         StatusSucceeded,
		QueuedAt:       now,
		StartedAt:      &now,
		FinishedAt:     &finished,
		RetryCount:     0,
		MaxRetries:     3,
	}
	b.ID = "build-123"
	b.OrgID = "org-123"
	b.CreatedAt = now

	view := toBuildView(b)

	if view.ID != b.ID {
		t.Errorf("ID = %v, want %v", view.ID, b.ID)
	}
	if view.Status != b.Status {
		t.Errorf("Status = %v, want %v", view.Status, b.Status)
	}
	if view.DurationMs == nil {
		t.Error("DurationMs should be set when both StartedAt and FinishedAt are present")
	}
	if view.BuildArgs == nil {
		t.Error("BuildArgs should be parsed from JSON")
	}
	if view.BuildArgs["VERSION"] != "1.0" {
		t.Errorf("BuildArgs[VERSION] = %v, want 1.0", view.BuildArgs["VERSION"])
	}
}
