package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

func tenantModel(id, orgID string) database.TenantModel {
	return database.TenantModel{Model: database.Model{ID: id}, OrgID: orgID}
}

// ----------------------------------------------------------------------------
// Fakes specific to the progress engine
// ----------------------------------------------------------------------------

// fakeRolloutStore emulates the rollout_status upsert semantics: is_rollback is
// preserved (OR-ed) across upserts and started_at is set once on first insert.
type fakeRolloutStore struct {
	mu        sync.Mutex
	snapshots map[string]*RolloutStatus
	upserts   int
}

func newFakeRolloutStore() *fakeRolloutStore {
	return &fakeRolloutStore{snapshots: make(map[string]*RolloutStatus)}
}

func key(deploymentID, releaseID string) string { return deploymentID + "/" + releaseID }

func (s *fakeRolloutStore) Upsert(ctx context.Context, rs *RolloutStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upserts++
	k := key(rs.DeploymentID, rs.ReleaseID)
	if existing := s.snapshots[k]; existing != nil {
		rs.IsRollback = existing.IsRollback || rs.IsRollback
		rs.StartedAt = existing.StartedAt
	} else {
		rs.StartedAt = time.Now()
	}
	rs.UpdatedAt = time.Now()
	cp := *rs
	s.snapshots[k] = &cp
	return nil
}

func (s *fakeRolloutStore) Get(ctx context.Context, deploymentID, releaseID string) (*RolloutStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rs, ok := s.snapshots[key(deploymentID, releaseID)]
	if !ok {
		return nil, apperrors.NotFound("rollout status not found")
	}
	cp := *rs
	return &cp, nil
}

// rollbackReleaseStore adds rollback behaviour to fakeReleaseStoreForAgent:
// GetPreviousSuccessful returns a configured target (or NotFound), and Create
// assigns an ID and records the created release.
type rollbackReleaseStore struct {
	*fakeReleaseStoreForAgent
	previousSuccessful *Release
	created            []*Release
	counter            int
}

func (s *rollbackReleaseStore) Create(ctx context.Context, r *Release) error {
	s.counter++
	if r.ID == "" {
		r.ID = fmt.Sprintf("rel-new-%d", s.counter)
	}
	s.created = append(s.created, r)
	if s.releases != nil {
		s.releases[r.ID] = r
	}
	return nil
}

func (s *rollbackReleaseStore) GetPreviousSuccessful(ctx context.Context, deploymentID string, beforeRevision int) (*Release, error) {
	if s.previousSuccessful != nil {
		return s.previousSuccessful, nil
	}
	return nil, apperrors.NotFound("no rollback target")
}

// ----------------------------------------------------------------------------
// Test harness
// ----------------------------------------------------------------------------

type progressTestEnv struct {
	handler  *AgentHandler
	app      *fiber.App
	outbox   *fakeOutbox
	rollouts *fakeRolloutStore
	releases ReleaseStore
	deps     DeploymentStore
}

func newProgressEnv(t *testing.T, releases ReleaseStore, deps DeploymentStore, autoRollback bool) *progressTestEnv {
	t.Helper()
	outbox := &fakeOutbox{}
	rollouts := newFakeRolloutStore()
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: &fakeDesiredStateStore{},
		Tenant:       &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}},
		Outbox:       outbox,
		Rollouts:     rollouts,
		AutoRollback: autoRollback,
		// Deterministic clock 60s ahead of release start so healthy duration > 0.
		Now:    func() time.Time { return time.Unix(1_000_060, 0) },
		Logger: slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"}
	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Post("/deployments/:deploymentId/releases/:releaseId/progress", func(c *fiber.Ctx) error {
		return handler.UpdateDeploymentProgress(c, releases, deps)
	})
	return &progressTestEnv{handler: handler, app: app, outbox: outbox, rollouts: rollouts, releases: releases, deps: deps}
}

func (e *progressTestEnv) post(t *testing.T, deploymentID, releaseID, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/v1/agent/deployments/%s/releases/%s/progress", deploymentID, releaseID),
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")
	resp, err := e.app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func (e *progressTestEnv) eventTypes() []string {
	types := make([]string, 0, len(e.outbox.events))
	for _, ev := range e.outbox.events {
		types = append(types, ev.Type)
	}
	return types
}

func countType(types []string, want string) int {
	n := 0
	for _, ty := range types {
		if ty == want {
			n++
		}
	}
	return n
}

func standardStores() (*fakeReleaseStoreForAgent, *fakeDeploymentStoreForAgent) {
	startedAt := time.Unix(1_000_000, 0)
	releaseStore := &fakeReleaseStoreForAgent{
		releases: map[string]*Release{
			"rel-1": {ID: "rel-1", OrgID: "org-123", DeploymentID: "dep-1", Revision: 2, Image: "nginx:2", StartedAt: &startedAt},
		},
	}
	deploymentStore := &fakeDeploymentStoreForAgent{
		deployments: map[string]*Deployment{
			"dep-1": {TenantModel: tenantModel("dep-1", "org-123"), ClusterID: "cluster-123"},
		},
	}
	return releaseStore, deploymentStore
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestProgress_RollingOut_EmitsProgress(t *testing.T) {
	rel, dep := standardStores()
	env := newProgressEnv(t, rel, dep, false)

	body := `{"phase":"RollingOut","revision":2,"image":"nginx:2","rolloutPercentage":50,"desiredReplicas":2,"readyReplicas":1,"updatedReplicas":2,"availableReplicas":1}`
	resp := env.post(t, "dep-1", "rel-1", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	types := env.eventTypes()
	if countType(types, EventDeploymentProgress) != 1 {
		t.Errorf("expected 1 progress event, got %v", types)
	}
	if countType(types, EventDeploymentHealthy) != 0 {
		t.Errorf("did not expect healthy event, got %v", types)
	}
	snap, err := env.rollouts.Get(context.Background(), "dep-1", "rel-1")
	if err != nil {
		t.Fatalf("expected snapshot persisted: %v", err)
	}
	if snap.Phase != RolloutPhaseRollingOut || snap.RolloutPercentage != 50 {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestProgress_Healthy_EmitsHealthy(t *testing.T) {
	rel, dep := standardStores()
	env := newProgressEnv(t, rel, dep, false)

	body := `{"phase":"Healthy","revision":2,"rolloutPercentage":100,"desiredReplicas":2,"readyReplicas":2,"updatedReplicas":2,"availableReplicas":2}`
	resp := env.post(t, "dep-1", "rel-1", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	types := env.eventTypes()
	if countType(types, EventDeploymentHealthy) != 1 {
		t.Errorf("expected 1 healthy event, got %v", types)
	}
	if countType(types, EventDeploymentProgress) != 1 {
		t.Errorf("expected 1 progress event, got %v", types)
	}

	// Duration should be captured in the healthy payload (clock is 60s ahead).
	for _, ev := range env.outbox.events {
		if ev.Type == EventDeploymentHealthy {
			var p deploymentHealthyPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatalf("unmarshal healthy payload: %v", err)
			}
			if p.DurationSeconds <= 0 {
				t.Errorf("expected positive duration, got %v", p.DurationSeconds)
			}
			if p.ReadyReplicas != 2 {
				t.Errorf("ready replicas = %d, want 2", p.ReadyReplicas)
			}
		}
	}
}

func TestProgress_Healthy_Idempotent(t *testing.T) {
	rel, dep := standardStores()
	env := newProgressEnv(t, rel, dep, false)

	body := `{"phase":"Healthy","revision":2,"rolloutPercentage":100,"desiredReplicas":2,"readyReplicas":2,"updatedReplicas":2,"availableReplicas":2}`
	env.post(t, "dep-1", "rel-1", body)
	env.post(t, "dep-1", "rel-1", body)

	if got := countType(env.eventTypes(), EventDeploymentHealthy); got != 1 {
		t.Errorf("expected exactly 1 healthy event across two reports, got %d", got)
	}
}

func TestProgress_Timeout_EmitsTimeout(t *testing.T) {
	rel, dep := standardStores()
	env := newProgressEnv(t, rel, dep, false)

	body := `{"phase":"Failed","revision":2,"timeout":true,"rolloutPercentage":50,"desiredReplicas":2,"readyReplicas":1,"errorMessage":"progress deadline exceeded"}`
	resp := env.post(t, "dep-1", "rel-1", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	types := env.eventTypes()
	if countType(types, EventDeploymentTimeout) != 1 {
		t.Errorf("expected 1 timeout event, got %v", types)
	}
}

func TestProgress_Failed_NoTimeout_NoAutoRollback(t *testing.T) {
	rel, dep := standardStores()
	env := newProgressEnv(t, rel, dep, false)

	body := `{"phase":"Failed","revision":2,"rolloutPercentage":0,"desiredReplicas":2,"errorMessage":"ImagePullBackOff"}`
	env.post(t, "dep-1", "rel-1", body)

	types := env.eventTypes()
	if countType(types, EventDeploymentTimeout) != 0 {
		t.Errorf("did not expect timeout event, got %v", types)
	}
	if countType(types, EventDeploymentRollbackStarted) != 0 {
		t.Errorf("did not expect rollback event without auto-rollback, got %v", types)
	}
	if countType(types, EventDeploymentProgress) != 1 {
		t.Errorf("expected 1 progress event, got %v", types)
	}
}

func TestProgress_Failed_TriggersAutoRollback(t *testing.T) {
	base, dep := standardStores()
	rel := &rollbackReleaseStore{
		fakeReleaseStoreForAgent: base,
		previousSuccessful: &Release{
			ID: "rel-prev", OrgID: "org-123", DeploymentID: "dep-1", Revision: 1,
			Image: "nginx:1", Replicas: 2, Config: json.RawMessage(`{}`),
		},
	}
	env := newProgressEnv(t, rel, dep, true)

	body := `{"phase":"Failed","revision":2,"rolloutPercentage":0,"desiredReplicas":2,"errorMessage":"CrashLoopBackOff"}`
	resp := env.post(t, "dep-1", "rel-1", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if countType(env.eventTypes(), EventDeploymentRollbackStarted) != 1 {
		t.Errorf("expected 1 rollback.started event, got %v", env.eventTypes())
	}
	if len(rel.created) != 1 {
		t.Fatalf("expected 1 rollback release created, got %d", len(rel.created))
	}
	newRel := rel.created[0]
	if newRel.Image != "nginx:1" || newRel.Revision != 3 {
		t.Errorf("rollback release = %+v, want image nginx:1 revision 3", newRel)
	}
	// The new release's rollout snapshot must carry the rollback marker.
	snap, err := env.rollouts.Get(context.Background(), "dep-1", newRel.ID)
	if err != nil {
		t.Fatalf("expected pre-seeded rollback snapshot: %v", err)
	}
	if !snap.IsRollback {
		t.Errorf("expected is_rollback=true on pre-seeded snapshot")
	}
}

func TestProgress_RollbackRelease_Healthy_EmitsCompleted(t *testing.T) {
	startedAt := time.Unix(1_000_000, 0)
	rel := &fakeReleaseStoreForAgent{
		releases: map[string]*Release{
			"rel-rb": {ID: "rel-rb", OrgID: "org-123", DeploymentID: "dep-1", Revision: 3, Image: "nginx:1", StartedAt: &startedAt},
		},
	}
	dep := &fakeDeploymentStoreForAgent{
		deployments: map[string]*Deployment{
			"dep-1": {TenantModel: tenantModel("dep-1", "org-123"), ClusterID: "cluster-123"},
		},
	}
	env := newProgressEnv(t, rel, dep, false)

	// Pre-seed the rollback marker as the auto-rollback path would.
	if err := env.rollouts.Upsert(context.Background(), &RolloutStatus{
		DeploymentID: "dep-1", ReleaseID: "rel-rb", OrgID: "org-123",
		Phase: RolloutPhasePending, Revision: 3, IsRollback: true,
	}); err != nil {
		t.Fatalf("seed rollback snapshot: %v", err)
	}

	body := `{"phase":"Healthy","revision":3,"rolloutPercentage":100,"desiredReplicas":2,"readyReplicas":2,"updatedReplicas":2,"availableReplicas":2}`
	resp := env.post(t, "dep-1", "rel-rb", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	types := env.eventTypes()
	if countType(types, EventDeploymentRollbackCompleted) != 1 {
		t.Errorf("expected 1 rollback.completed event, got %v", types)
	}
	if countType(types, EventDeploymentHealthy) != 1 {
		t.Errorf("expected 1 healthy event, got %v", types)
	}
}

func TestProgress_InvalidPhase(t *testing.T) {
	rel, dep := standardStores()
	env := newProgressEnv(t, rel, dep, false)

	body := `{"phase":"Bogus","revision":2}`
	resp := env.post(t, "dep-1", "rel-1", body)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestProgress_ClusterMismatch_Forbidden(t *testing.T) {
	base, _ := standardStores()
	dep := &fakeDeploymentStoreForAgent{
		deployments: map[string]*Deployment{
			"dep-1": {TenantModel: tenantModel("dep-1", "org-123"), ClusterID: "other-cluster"},
		},
	}
	env := newProgressEnv(t, base, dep, false)

	body := `{"phase":"Healthy","revision":2,"rolloutPercentage":100,"desiredReplicas":1,"readyReplicas":1}`
	resp := env.post(t, "dep-1", "rel-1", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestProgress_ConcurrentRollouts(t *testing.T) {
	// Multiple deployments/releases reported concurrently must not race and must
	// each produce a persisted snapshot and a healthy event.
	releases := &fakeReleaseStoreForAgent{releases: map[string]*Release{}}
	deps := &fakeDeploymentStoreForAgent{deployments: map[string]*Deployment{}}
	const n = 8
	for i := 0; i < n; i++ {
		did := fmt.Sprintf("dep-%d", i)
		rid := fmt.Sprintf("rel-%d", i)
		startedAt := time.Unix(1_000_000, 0)
		releases.releases[rid] = &Release{ID: rid, OrgID: "org-123", DeploymentID: did, Revision: 1, StartedAt: &startedAt}
		deps.deployments[did] = &Deployment{TenantModel: tenantModel(did, "org-123"), ClusterID: "cluster-123"}
	}
	env := newProgressEnv(t, releases, deps, false)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := `{"phase":"Healthy","revision":1,"rolloutPercentage":100,"desiredReplicas":1,"readyReplicas":1,"updatedReplicas":1,"availableReplicas":1}`
			resp := env.post(t, fmt.Sprintf("dep-%d", i), fmt.Sprintf("rel-%d", i), body)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("dep-%d status = %d, want 200", i, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()

	if got := countType(env.eventTypes(), EventDeploymentHealthy); got != n {
		t.Errorf("expected %d healthy events, got %d", n, got)
	}
}
