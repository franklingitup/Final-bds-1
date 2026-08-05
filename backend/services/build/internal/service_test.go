package build

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
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

func TestIsValidGitURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"https url", "https://github.com/org/repo.git", true},
		{"http url", "http://github.com/org/repo.git", true},
		{"ssh url", "git@github.com:org/repo.git", true},
		{"invalid scheme", "ftp://github.com/repo.git", false},
		{"invalid url", "not-a-url", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidGitURL(tt.url); got != tt.want {
				t.Errorf("isValidGitURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
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
