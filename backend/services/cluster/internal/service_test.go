package cluster

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// ----------------------------------------------------------------------------
// Fakes
// ----------------------------------------------------------------------------

type fakeRunner struct{}

func (fakeRunner) WithTenant(ctx context.Context, _ string, fn database.TxFunc) error { return fn(ctx) }

type fakeOutbox struct {
	mu     sync.Mutex
	events []events.Event
}

func (f *fakeOutbox) Enqueue(_ context.Context, e events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeOutbox) list() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]events.Event, len(f.events))
	copy(cp, f.events)
	return cp
}

type fakeClusterStore struct {
	mu       sync.Mutex
	clusters map[string]*Cluster
}

func newFakeClusterStore() *fakeClusterStore {
	return &fakeClusterStore{clusters: make(map[string]*Cluster)}
}

func (f *fakeClusterStore) Create(_ context.Context, c *Cluster) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	for _, existing := range f.clusters {
		if existing.OrgID == c.OrgID && existing.Slug == c.Slug {
			return apperrors.Conflict("slug taken")
		}
	}
	c.CreatedAt = time.Now()
	c.UpdatedAt = c.CreatedAt
	c.Version = 1
	f.clusters[c.ID] = c
	return nil
}

func (f *fakeClusterStore) GetByID(_ context.Context, id string) (*Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clusters[id]
	if !ok {
		return nil, apperrors.NotFound("cluster not found")
	}
	cp := *c
	return &cp, nil
}

func (f *fakeClusterStore) GetByIDWithoutTenant(_ context.Context, id string) (*Cluster, error) {
	return f.GetByID(context.Background(), id)
}

func (f *fakeClusterStore) GetBySlug(_ context.Context, slug string) (*Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.clusters {
		if c.Slug == slug {
			cp := *c
			return &cp, nil
		}
	}
	return nil, apperrors.NotFound("cluster not found")
}

func (f *fakeClusterStore) List(_ context.Context, req database.PageRequest, status string) (database.Page[Cluster], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []Cluster
	for _, c := range f.clusters {
		if c.Status == StatusDeleted {
			continue
		}
		if status != "" && c.Status != status {
			continue
		}
		items = append(items, *c)
	}
	return database.Page[Cluster]{Items: items}, nil
}

func (f *fakeClusterStore) Update(_ context.Context, c *Cluster) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.clusters[c.ID]
	if !ok {
		return apperrors.NotFound("cluster not found")
	}
	if existing.Version != c.Version {
		return database.ErrOptimisticLock
	}
	c.Version++
	c.UpdatedAt = time.Now()
	f.clusters[c.ID] = c
	return nil
}

func (f *fakeClusterStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clusters[id]
	if !ok {
		return apperrors.NotFound("cluster not found")
	}
	c.Status = StatusDeleted
	return nil
}

func (f *fakeClusterStore) UpdateStatus(_ context.Context, id, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clusters[id]
	if !ok {
		return apperrors.NotFound("cluster not found")
	}
	c.Status = status
	return nil
}

func (f *fakeClusterStore) UpdateHeartbeat(_ context.Context, id string, at time.Time, k8sVersion string, nodeCount int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clusters[id]
	if !ok {
		return apperrors.NotFound("cluster not found")
	}
	c.LastHeartbeatAt = &at
	c.KubernetesVersion = &k8sVersion
	c.NodeCount = &nodeCount
	c.Status = StatusConnected
	return nil
}

func (f *fakeClusterStore) RegisterAgent(_ context.Context, id string, agentID, k8sVersion string, nodeCount int, cloudProvider, region *string, registeredAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clusters[id]
	if !ok {
		return apperrors.NotFound("cluster not found")
	}
	c.AgentID = &agentID
	c.KubernetesVersion = &k8sVersion
	c.NodeCount = &nodeCount
	if cloudProvider != nil {
		c.CloudProvider = cloudProvider
	}
	if region != nil {
		c.Region = region
	}
	c.RegisteredAt = &registeredAt
	c.LastHeartbeatAt = &registeredAt
	c.Status = StatusConnected
	return nil
}

type fakeTokenStore struct {
	mu     sync.Mutex
	tokens map[string]*RegistrationToken
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{tokens: make(map[string]*RegistrationToken)}
}

func (f *fakeTokenStore) Create(_ context.Context, t *RegistrationToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.CreatedAt = time.Now()
	t.UpdatedAt = t.CreatedAt
	t.Version = 1
	f.tokens[t.ID] = t
	return nil
}

func (f *fakeTokenStore) GetByHash(_ context.Context, hash string) (*RegistrationToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tokens {
		if t.TokenHash == hash {
			cp := *t
			return &cp, nil
		}
	}
	return nil, apperrors.NotFound("token not found")
}

func (f *fakeTokenStore) GetActiveByCluster(_ context.Context, clusterID string) (*RegistrationToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tokens {
		if t.ClusterID == clusterID && t.Status == TokenStatusActive {
			cp := *t
			return &cp, nil
		}
	}
	return nil, apperrors.NotFound("token not found")
}

func (f *fakeTokenStore) MarkUsed(_ context.Context, id, agentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokens[id]
	if !ok || t.Status != TokenStatusActive {
		return apperrors.NotFound("token not found or not active")
	}
	t.Status = TokenStatusUsed
	t.UsedByAgent = &agentID
	now := time.Now()
	t.UsedAt = &now
	return nil
}

func (f *fakeTokenStore) Revoke(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokens[id]
	if !ok || t.Status != TokenStatusActive {
		return apperrors.NotFound("token not found or not active")
	}
	t.Status = TokenStatusRevoked
	return nil
}

type fakeHeartbeatStore struct {
	mu         sync.Mutex
	heartbeats []Heartbeat
}

func newFakeHeartbeatStore() *fakeHeartbeatStore {
	return &fakeHeartbeatStore{}
}

func (f *fakeHeartbeatStore) Create(_ context.Context, h *Heartbeat) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h.ID == "" {
		h.ID = uuid.NewString()
	}
	f.heartbeats = append(f.heartbeats, *h)
	return nil
}

func (f *fakeHeartbeatStore) ListByCluster(_ context.Context, clusterID string, limit int) ([]Heartbeat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []Heartbeat
	for _, h := range f.heartbeats {
		if h.ClusterID == clusterID {
			result = append(result, h)
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

type fakeNotifier struct {
	mu        sync.Mutex
	deliveries []TokenDelivery
}

func (f *fakeNotifier) DeliverToken(_ context.Context, d TokenDelivery) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deliveries = append(f.deliveries, d)
}

func (f *fakeNotifier) list() []TokenDelivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]TokenDelivery, len(f.deliveries))
	copy(cp, f.deliveries)
	return cp
}

// ----------------------------------------------------------------------------
// Test harness
// ----------------------------------------------------------------------------

type testEnv struct {
	svc        *Service
	clusters   *fakeClusterStore
	tokens     *fakeTokenStore
	heartbeats *fakeHeartbeatStore
	outbox     *fakeOutbox
	notifier   *fakeNotifier
	now        time.Time
}

func newTestEnv() *testEnv {
	clusters := newFakeClusterStore()
	tokens := newFakeTokenStore()
	heartbeats := newFakeHeartbeatStore()
	outbox := &fakeOutbox{}
	notifier := &fakeNotifier{}
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	svc := NewService(Deps{
		Clusters:   clusters,
		Tokens:     tokens,
		Heartbeats: heartbeats,
		Outbox:     outbox,
		Tenant:     fakeRunner{},
		Notifier:   notifier,
		Now:        func() time.Time { return now },
	})

	return &testEnv{
		svc:        svc,
		clusters:   clusters,
		tokens:     tokens,
		heartbeats: heartbeats,
		outbox:     outbox,
		notifier:   notifier,
		now:        now,
	}
}

func (e *testEnv) createCluster(t *testing.T, orgID, userID, name, slug string) *Cluster {
	t.Helper()
	c, err := e.svc.CreateCluster(context.Background(), orgID, userID, CreateClusterRequest{
		Name: name,
		Slug: slug,
	})
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	return c
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestCreateCluster(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c, err := env.svc.CreateCluster(ctx, orgID, userID, CreateClusterRequest{
		Name: "Production",
		Slug: "production",
	})
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	if c.Status != StatusPending {
		t.Errorf("status = %q, want pending", c.Status)
	}
	if c.Name != "Production" {
		t.Errorf("name = %q, want Production", c.Name)
	}

	evts := env.outbox.list()
	if len(evts) != 1 || evts[0].Type != EventClusterCreated {
		t.Errorf("expected cluster.created event, got %v", evts)
	}
}

func TestCreateCluster_DuplicateSlug(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	_, err := env.svc.CreateCluster(ctx, orgID, userID, CreateClusterRequest{
		Name: "Production",
		Slug: "production",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = env.svc.CreateCluster(ctx, orgID, userID, CreateClusterRequest{
		Name: "Another Cluster",
		Slug: "production",
	})
	if !errors.Is(err, errSlugTaken) {
		t.Errorf("expected errSlugTaken, got %v", err)
	}
}

func TestCreateCluster_InvalidSlug(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	_, err := env.svc.CreateCluster(ctx, orgID, userID, CreateClusterRequest{
		Name: "Test",
		Slug: "INVALID SLUG",
	})
	if err == nil || apperrors.From(err).Code != apperrors.CodeValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestGetCluster(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()

	created := env.createCluster(t, orgID, uuid.NewString(), "Test", "test")

	got, err := env.svc.GetCluster(ctx, orgID, created.ID)
	if err != nil {
		t.Fatalf("GetCluster: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
}

func TestListClusters(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()

	env.createCluster(t, orgID, uuid.NewString(), "Cluster 1", "cluster-1")
	env.createCluster(t, orgID, uuid.NewString(), "Cluster 2", "cluster-2")

	page, err := env.svc.ListClusters(ctx, orgID, database.PageRequest{Limit: 10}, "")
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(page.Items) != 2 {
		t.Errorf("got %d items, want 2", len(page.Items))
	}
}

func TestUpdateCluster(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()

	c := env.createCluster(t, orgID, uuid.NewString(), "Original", "original")

	newName := "Updated"
	updated, err := env.svc.UpdateCluster(ctx, orgID, c.ID, UpdateClusterRequest{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("UpdateCluster: %v", err)
	}
	if updated.Name != newName {
		t.Errorf("name = %q, want %q", updated.Name, newName)
	}
}

func TestDeleteCluster(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c := env.createCluster(t, orgID, userID, "ToDelete", "to-delete")

	if err := env.svc.DeleteCluster(ctx, orgID, userID, c.ID); err != nil {
		t.Fatalf("DeleteCluster: %v", err)
	}

	// Should be soft-deleted.
	got, _ := env.clusters.GetByID(ctx, c.ID)
	if got.Status != StatusDeleted {
		t.Errorf("status = %q, want deleted", got.Status)
	}

	// Check event.
	evts := env.outbox.list()
	found := false
	for _, e := range evts {
		if e.Type == EventClusterDeleted {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cluster.deleted event")
	}
}

func TestGenerateRegistrationToken(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c := env.createCluster(t, orgID, userID, "Test", "test")

	token, err := env.svc.GenerateRegistrationToken(ctx, orgID, userID, c.ID, GenerateTokenRequest{})
	if err != nil {
		t.Fatalf("GenerateRegistrationToken: %v", err)
	}
	if token.Token == "" {
		t.Error("expected plaintext token")
	}
	if token.Status != TokenStatusActive {
		t.Errorf("status = %q, want active", token.Status)
	}

	// Check notifier received the token.
	deliveries := env.notifier.list()
	if len(deliveries) != 1 || deliveries[0].Token != token.Token {
		t.Errorf("expected notifier to receive token")
	}

	// Check event.
	evts := env.outbox.list()
	found := false
	for _, e := range evts {
		if e.Type == EventRegistrationTokenCreated {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cluster.registration.token.created event")
	}
}

func TestRevokeRegistrationToken(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c := env.createCluster(t, orgID, userID, "Test", "test")
	token, _ := env.svc.GenerateRegistrationToken(ctx, orgID, userID, c.ID, GenerateTokenRequest{})

	if err := env.svc.RevokeRegistrationToken(ctx, orgID, c.ID, token.ID); err != nil {
		t.Fatalf("RevokeRegistrationToken: %v", err)
	}

	// Verify revoked.
	t2, _ := env.tokens.GetByHash(ctx, hashToken(token.Token))
	if t2.Status != TokenStatusRevoked {
		t.Errorf("status = %q, want revoked", t2.Status)
	}
}

func TestRegisterAgent(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c := env.createCluster(t, orgID, userID, "Test", "test")
	token, _ := env.svc.GenerateRegistrationToken(ctx, orgID, userID, c.ID, GenerateTokenRequest{})

	registered, err := env.svc.RegisterAgent(ctx, AgentRegisterRequest{
		Token:             token.Token,
		AgentID:           "agent-001",
		KubernetesVersion: "1.28.5",
		NodeCount:         3,
		CloudProvider:     "aws",
		Region:            "us-west-2",
	})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if registered.Status != StatusConnected {
		t.Errorf("status = %q, want connected", registered.Status)
	}
	if deref(registered.AgentID) != "agent-001" {
		t.Errorf("agentId = %q, want agent-001", deref(registered.AgentID))
	}

	// Check event.
	evts := env.outbox.list()
	found := false
	for _, e := range evts {
		if e.Type == EventClusterRegistered {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cluster.registered event")
	}
}

func TestRegisterAgent_TokenAlreadyUsed(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c := env.createCluster(t, orgID, userID, "Test", "test")
	token, _ := env.svc.GenerateRegistrationToken(ctx, orgID, userID, c.ID, GenerateTokenRequest{})

	// First registration.
	_, _ = env.svc.RegisterAgent(ctx, AgentRegisterRequest{
		Token:             token.Token,
		AgentID:           "agent-001",
		KubernetesVersion: "1.28.5",
		NodeCount:         3,
	})

	// Second attempt should fail.
	_, err := env.svc.RegisterAgent(ctx, AgentRegisterRequest{
		Token:             token.Token,
		AgentID:           "agent-002",
		KubernetesVersion: "1.28.5",
		NodeCount:         2,
	})
	if !errors.Is(err, errTokenUsed) {
		t.Errorf("expected errTokenUsed, got %v", err)
	}
}

// TestRegisterAgent_ReturnsCompleteClusterData verifies that RegisterAgent
// returns a cluster with all required fields populated, without requiring
// a secondary fetch. This is critical for agent credential persistence.
// (BLOCKER-4 fix verification)
func TestRegisterAgent_ReturnsCompleteClusterData(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c := env.createCluster(t, orgID, userID, "Test Cluster", "test-cluster")
	token, _ := env.svc.GenerateRegistrationToken(ctx, orgID, userID, c.ID, GenerateTokenRequest{})

	registered, err := env.svc.RegisterAgent(ctx, AgentRegisterRequest{
		Token:             token.Token,
		AgentID:           "agent-test-123",
		KubernetesVersion: "1.28.5",
		NodeCount:         5,
		CloudProvider:     "gcp",
		Region:            "us-central1",
	})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Verify all critical fields are present for credential persistence.
	// The agent needs these fields to authenticate subsequent API calls.
	if registered.ID == "" {
		t.Error("cluster ID is empty")
	}
	if registered.OrgID != orgID {
		t.Errorf("orgID = %q, want %q", registered.OrgID, orgID)
	}
	if registered.Name != "Test Cluster" {
		t.Errorf("name = %q, want %q", registered.Name, "Test Cluster")
	}
	if registered.Slug != "test-cluster" {
		t.Errorf("slug = %q, want %q", registered.Slug, "test-cluster")
	}
	if registered.Status != StatusConnected {
		t.Errorf("status = %q, want %q", registered.Status, StatusConnected)
	}
	if registered.AgentID == nil || *registered.AgentID != "agent-test-123" {
		t.Errorf("agentID = %v, want %q", registered.AgentID, "agent-test-123")
	}
	if registered.KubernetesVersion == nil || *registered.KubernetesVersion != "1.28.5" {
		t.Errorf("kubernetesVersion = %v, want %q", registered.KubernetesVersion, "1.28.5")
	}
	if registered.NodeCount == nil || *registered.NodeCount != 5 {
		t.Errorf("nodeCount = %v, want 5", registered.NodeCount)
	}
	if registered.CloudProvider == nil || *registered.CloudProvider != "gcp" {
		t.Errorf("cloudProvider = %v, want %q", registered.CloudProvider, "gcp")
	}
	if registered.Region == nil || *registered.Region != "us-central1" {
		t.Errorf("region = %v, want %q", registered.Region, "us-central1")
	}
	if registered.RegisteredAt == nil {
		t.Error("registeredAt is nil")
	}
	if registered.LastHeartbeatAt == nil {
		t.Error("lastHeartbeatAt is nil")
	}

	// Verify the response can be used for agent credential persistence.
	// The agent stores: ClusterID, OrganizationID, AgentID.
	creds := struct {
		ClusterID      string
		OrganizationID string
		AgentID        string
	}{
		ClusterID:      registered.ID,
		OrganizationID: registered.OrgID,
		AgentID:        *registered.AgentID,
	}
	if creds.ClusterID == "" || creds.OrganizationID == "" || creds.AgentID == "" {
		t.Errorf("incomplete credentials: %+v", creds)
	}
}

func TestRegisterAgent_ExpiredToken(t *testing.T) {
	clusters := newFakeClusterStore()
	tokens := newFakeTokenStore()
	heartbeats := newFakeHeartbeatStore()
	outbox := &fakeOutbox{}
	notifier := &fakeNotifier{}

	// Start with now in the past.
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	svc := NewService(Deps{
		Clusters:   clusters,
		Tokens:     tokens,
		Heartbeats: heartbeats,
		Outbox:     outbox,
		Tenant:     fakeRunner{},
		Notifier:   notifier,
		Now:        func() time.Time { return now },
	})

	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c, _ := svc.CreateCluster(ctx, orgID, userID, CreateClusterRequest{
		Name: "Test",
		Slug: "test",
	})
	token, _ := svc.GenerateRegistrationToken(ctx, orgID, userID, c.ID, GenerateTokenRequest{
		ExpiresIn: "1h",
	})

	// Advance time past expiration.
	now = now.Add(2 * time.Hour)

	_, err := svc.RegisterAgent(ctx, AgentRegisterRequest{
		Token:             token.Token,
		AgentID:           "agent-001",
		KubernetesVersion: "1.28.5",
		NodeCount:         3,
	})
	if !errors.Is(err, errTokenExpired) {
		t.Errorf("expected errTokenExpired, got %v", err)
	}
}

func TestRecordHeartbeat(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c := env.createCluster(t, orgID, userID, "Test", "test")
	token, _ := env.svc.GenerateRegistrationToken(ctx, orgID, userID, c.ID, GenerateTokenRequest{})
	_, _ = env.svc.RegisterAgent(ctx, AgentRegisterRequest{
		Token:             token.Token,
		AgentID:           "agent-001",
		KubernetesVersion: "1.28.5",
		NodeCount:         3,
	})

	err := env.svc.RecordHeartbeat(ctx, orgID, c.ID, AgentHeartbeatRequest{
		AgentID:           "agent-001",
		KubernetesVersion: "1.28.6",
		NodeCount:         4,
		APIServerHealthy:  true,
	})
	if err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	// Check cluster updated.
	got, _ := env.svc.GetCluster(ctx, orgID, c.ID)
	if *got.NodeCount != 4 {
		t.Errorf("nodeCount = %d, want 4", *got.NodeCount)
	}

	// Check heartbeat history.
	heartbeats, _ := env.svc.GetHeartbeats(ctx, orgID, c.ID, 10)
	if len(heartbeats) != 1 {
		t.Errorf("got %d heartbeats, want 1", len(heartbeats))
	}

	// Check event.
	evts := env.outbox.list()
	found := false
	for _, e := range evts {
		if e.Type == EventClusterHeartbeatReceived {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cluster.heartbeat.received event")
	}
}

func TestRecordHeartbeat_AgentMismatch(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c := env.createCluster(t, orgID, userID, "Test", "test")
	token, _ := env.svc.GenerateRegistrationToken(ctx, orgID, userID, c.ID, GenerateTokenRequest{})
	_, _ = env.svc.RegisterAgent(ctx, AgentRegisterRequest{
		Token:             token.Token,
		AgentID:           "agent-001",
		KubernetesVersion: "1.28.5",
		NodeCount:         3,
	})

	err := env.svc.RecordHeartbeat(ctx, orgID, c.ID, AgentHeartbeatRequest{
		AgentID:           "agent-002", // Wrong agent.
		KubernetesVersion: "1.28.6",
		NodeCount:         4,
		APIServerHealthy:  true,
	})
	if !errors.Is(err, errAgentMismatch) {
		t.Errorf("expected errAgentMismatch, got %v", err)
	}
}

func TestMarkDisconnected(t *testing.T) {
	env := newTestEnv()
	ctx := context.Background()
	orgID := uuid.NewString()
	userID := uuid.NewString()

	c := env.createCluster(t, orgID, userID, "Test", "test")
	token, _ := env.svc.GenerateRegistrationToken(ctx, orgID, userID, c.ID, GenerateTokenRequest{})
	_, _ = env.svc.RegisterAgent(ctx, AgentRegisterRequest{
		Token:             token.Token,
		AgentID:           "agent-001",
		KubernetesVersion: "1.28.5",
		NodeCount:         3,
	})

	// Manually set stale heartbeat.
	stale := env.now.Add(-10 * time.Minute)
	env.clusters.mu.Lock()
	env.clusters.clusters[c.ID].LastHeartbeatAt = &stale
	env.clusters.mu.Unlock()

	count, err := env.svc.MarkDisconnected(ctx, orgID, 5*time.Minute)
	if err != nil {
		t.Fatalf("MarkDisconnected: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	// Check status.
	got, _ := env.svc.GetCluster(ctx, orgID, c.ID)
	if got.Status != StatusDisconnected {
		t.Errorf("status = %q, want disconnected", got.Status)
	}
}
