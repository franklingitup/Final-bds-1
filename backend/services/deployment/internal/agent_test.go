package deployment

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/middleware"
)

// newTestApp creates a Fiber app with the standard error handler.
func newTestApp() *fiber.App {
	return fiber.New(fiber.Config{ErrorHandler: middleware.ErrorHandler()})
}

// fakeDesiredStateStore implements DesiredStateStore for testing.
type fakeDesiredStateStore struct {
	deployments   []DesiredDeployment
	err           error
	lastOrgID     string
	lastClusterID string
}

func (f *fakeDesiredStateStore) GetDesiredState(ctx context.Context, orgID, clusterID string) ([]DesiredDeployment, error) {
	f.lastOrgID = orgID
	f.lastClusterID = clusterID
	if f.err != nil {
		return nil, f.err
	}
	return f.deployments, nil
}

// fakeTenantQuerier implements TenantQuerier for testing.
type fakeTenantQuerier struct {
	allowedOrgs map[string]bool
}

func (f *fakeTenantQuerier) WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error {
	if f.allowedOrgs != nil && !f.allowedOrgs[orgID] {
		return apperrors.Forbidden("access denied")
	}
	return fn(ctx)
}

// fakeClusterValidator implements ClusterValidator for testing.
type fakeClusterValidator struct {
	orgID string
	err   error
}

func (f *fakeClusterValidator) ValidateCluster(ctx context.Context, clusterID, agentID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.orgID, nil
}

func TestAgentAuthMiddleware_MissingHeaders(t *testing.T) {
	app := newTestApp()
	validator := &fakeClusterValidator{orgID: "org-123"}
	app.Use(AgentAuthMiddleware(validator))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	tests := []struct {
		name      string
		clusterID string
		agentID   string
		wantCode  int
	}{
		{"missing both", "", "", http.StatusUnauthorized},
		{"missing cluster ID", "", "agent-123", http.StatusUnauthorized},
		{"missing agent ID", "cluster-123", "", http.StatusUnauthorized},
		{"both present", "cluster-123", "agent-123", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.clusterID != "" {
				req.Header.Set("X-Cluster-ID", tt.clusterID)
			}
			if tt.agentID != "" {
				req.Header.Set("X-Agent-ID", tt.agentID)
			}

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tt.wantCode {
				t.Errorf("got status %d, want %d", resp.StatusCode, tt.wantCode)
			}
		})
	}
}

func TestAgentAuthMiddleware_ValidationError(t *testing.T) {
	app := newTestApp()
	validator := &fakeClusterValidator{err: apperrors.Unauthorized("invalid credentials")}
	app.Use(AgentAuthMiddleware(validator))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAgentAuthMiddleware_SetsContext(t *testing.T) {
	app := newTestApp()
	validator := &fakeClusterValidator{orgID: "org-456"}
	app.Use(AgentAuthMiddleware(validator))
	app.Get("/test", func(c *fiber.Ctx) error {
		agent := AgentFromContext(c.UserContext())
		if agent == nil {
			return fiber.NewError(http.StatusInternalServerError, "agent not in context")
		}
		return c.JSON(fiber.Map{
			"clusterID": agent.ClusterID,
			"agentID":   agent.AgentID,
			"orgID":     agent.OrganizationID,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Cluster-ID", "cluster-789")
	req.Header.Set("X-Agent-ID", "agent-abc")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["clusterID"] != "cluster-789" {
		t.Errorf("clusterID = %q, want %q", body["clusterID"], "cluster-789")
	}
	if body["agentID"] != "agent-abc" {
		t.Errorf("agentID = %q, want %q", body["agentID"], "agent-abc")
	}
	if body["orgID"] != "org-456" {
		t.Errorf("orgID = %q, want %q", body["orgID"], "org-456")
	}
}

func TestAgentHandler_GetDesiredState(t *testing.T) {
	port := 8080
	deployments := []DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationID:   "app-1",
			ApplicationName: "My App",
			ApplicationSlug: "my-app",
			Image:           "nginx:latest",
			Revision:        1,
			Replicas:        2,
			Port:            &port,
			Status:          "pending",
			ResourceRequests: &ResourceSpec{
				CPU:    "100m",
				Memory: "128Mi",
			},
		},
	}

	store := &fakeDesiredStateStore{deployments: deployments}
	tenant := &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}}
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       tenant,
		Logger:       slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"}

	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Get("/clusters/:clusterId/desired-state", handler.GetDesiredState)

	req := httptest.NewRequest(http.MethodGet, "/v1/agent/clusters/cluster-123/desired-state", nil)
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body DesiredStateResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body.ClusterID != "cluster-123" {
		t.Errorf("clusterID = %q, want %q", body.ClusterID, "cluster-123")
	}
	if len(body.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(body.Items))
	}

	// Verify the store was called with the correct org ID (CRIT-001 fix validation).
	if store.lastOrgID != "org-123" {
		t.Errorf("store.lastOrgID = %q, want %q", store.lastOrgID, "org-123")
	}
	if store.lastClusterID != "cluster-123" {
		t.Errorf("store.lastClusterID = %q, want %q", store.lastClusterID, "cluster-123")
	}

	item := body.Items[0]
	if item.DeploymentID != "dep-1" {
		t.Errorf("deploymentId = %q, want %q", item.DeploymentID, "dep-1")
	}
	if item.ReleaseID != "rel-1" {
		t.Errorf("releaseId = %q, want %q", item.ReleaseID, "rel-1")
	}
	if item.ApplicationName != "My App" {
		t.Errorf("applicationName = %q, want %q", item.ApplicationName, "My App")
	}
	if item.ApplicationSlug != "my-app" {
		t.Errorf("applicationSlug = %q, want %q", item.ApplicationSlug, "my-app")
	}
	if item.Image != "nginx:latest" {
		t.Errorf("image = %q, want %q", item.Image, "nginx:latest")
	}
	if item.Revision != 1 {
		t.Errorf("revision = %d, want 1", item.Revision)
	}
	if item.Replicas != 2 {
		t.Errorf("replicas = %d, want 2", item.Replicas)
	}
	if item.ResourceRequests == nil || item.ResourceRequests.CPU != "100m" {
		t.Errorf("resourceRequests.cpu = %v, want 100m", item.ResourceRequests)
	}
}

func TestAgentHandler_GetDesiredState_ClusterMismatch(t *testing.T) {
	store := &fakeDesiredStateStore{deployments: nil}
	tenant := &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}}
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       tenant,
		Logger:       slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"}

	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Get("/clusters/:clusterId/desired-state", handler.GetDesiredState)

	req := httptest.NewRequest(http.MethodGet, "/v1/agent/clusters/other-cluster/desired-state", nil)
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestAgentHandler_UpdateDeploymentStatus(t *testing.T) {
	// Setup fake stores with valid deployment and release that match the agent's credentials.
	releaseStore := &fakeReleaseStoreForAgent{
		releases: map[string]*Release{
			"rel-1": {ID: "rel-1", OrgID: "org-123", DeploymentID: "dep-1"},
		},
	}
	deploymentStore := &fakeDeploymentStoreForAgent{
		deployments: map[string]*Deployment{
			"dep-1": {TenantModel: database.TenantModel{Model: database.Model{ID: "dep-1"}, OrgID: "org-123"}, ClusterID: "cluster-123"},
		},
	}
	store := &fakeDesiredStateStore{}
	tenant := &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}}
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       tenant,
		Logger:       slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"}

	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Post("/deployments/:deploymentId/releases/:releaseId/status", func(c *fiber.Ctx) error {
		return handler.UpdateDeploymentStatus(c, releaseStore, deploymentStore)
	})

	body := `{"status": "succeeded", "readyReplicas": 3}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/deployments/dep-1/releases/rel-1/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if releaseStore.finishedID != "rel-1" || releaseStore.finishedStatus != "succeeded" {
		t.Errorf("release not marked finished correctly: id=%q status=%q", releaseStore.finishedID, releaseStore.finishedStatus)
	}
	if deploymentStore.updatedID != "dep-1" || deploymentStore.updatedStatus != StatusSucceeded {
		t.Errorf("deployment not updated correctly: id=%q status=%q", deploymentStore.updatedID, deploymentStore.updatedStatus)
	}
}

func TestAgentHandler_UpdateDeploymentStatus_Invalid(t *testing.T) {
	releaseStore := &fakeReleaseStoreForAgent{
		releases: map[string]*Release{
			"rel-1": {ID: "rel-1", OrgID: "org-123", DeploymentID: "dep-1"},
		},
	}
	deploymentStore := &fakeDeploymentStoreForAgent{
		deployments: map[string]*Deployment{
			"dep-1": {TenantModel: database.TenantModel{Model: database.Model{ID: "dep-1"}, OrgID: "org-123"}, ClusterID: "cluster-123"},
		},
	}
	store := &fakeDesiredStateStore{}
	tenant := &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}}
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       tenant,
		Logger:       slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"}

	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Post("/deployments/:deploymentId/releases/:releaseId/status", func(c *fiber.Ctx) error {
		return handler.UpdateDeploymentStatus(c, releaseStore, deploymentStore)
	})

	body := `{"status": "invalid-status"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/deployments/dep-1/releases/rel-1/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
}

// ============================================================================
// SECURITY TESTS - CRIT-002 Fix: Ownership Validation
// ============================================================================

func TestAgentHandler_UpdateDeploymentStatus_DeploymentNotFound(t *testing.T) {
	releaseStore := &fakeReleaseStoreForAgent{releases: map[string]*Release{}}
	deploymentStore := &fakeDeploymentStoreForAgent{deployments: map[string]*Deployment{}}
	store := &fakeDesiredStateStore{}
	tenant := &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}}
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       tenant,
		Logger:       slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"}

	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Post("/deployments/:deploymentId/releases/:releaseId/status", func(c *fiber.Ctx) error {
		return handler.UpdateDeploymentStatus(c, releaseStore, deploymentStore)
	})

	body := `{"status": "succeeded"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/deployments/unknown-dep/releases/rel-1/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("got status %d, want %d (deployment not found)", resp.StatusCode, http.StatusForbidden)
	}
}

func TestAgentHandler_UpdateDeploymentStatus_ClusterMismatch(t *testing.T) {
	// Deployment belongs to a DIFFERENT cluster than the authenticated agent.
	releaseStore := &fakeReleaseStoreForAgent{
		releases: map[string]*Release{
			"rel-1": {ID: "rel-1", OrgID: "org-123", DeploymentID: "dep-1"},
		},
	}
	deploymentStore := &fakeDeploymentStoreForAgent{
		deployments: map[string]*Deployment{
			"dep-1": {TenantModel: database.TenantModel{Model: database.Model{ID: "dep-1"}, OrgID: "org-123"}, ClusterID: "other-cluster"}, // DIFFERENT CLUSTER
		},
	}
	store := &fakeDesiredStateStore{}
	tenant := &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}}
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       tenant,
		Logger:       slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"}

	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Post("/deployments/:deploymentId/releases/:releaseId/status", func(c *fiber.Ctx) error {
		return handler.UpdateDeploymentStatus(c, releaseStore, deploymentStore)
	})

	body := `{"status": "succeeded"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/deployments/dep-1/releases/rel-1/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", "cluster-123") // Agent claims to be cluster-123
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	// CRIT-002 fix: Agent should be FORBIDDEN from updating deployment on different cluster.
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("got status %d, want %d (cluster mismatch - CRIT-002 fix)", resp.StatusCode, http.StatusForbidden)
	}
}

func TestAgentHandler_UpdateDeploymentStatus_OrgMismatch(t *testing.T) {
	// Deployment belongs to a DIFFERENT organization than the authenticated agent.
	releaseStore := &fakeReleaseStoreForAgent{
		releases: map[string]*Release{
			"rel-1": {ID: "rel-1", OrgID: "org-999", DeploymentID: "dep-1"}, // DIFFERENT ORG
		},
	}
	deploymentStore := &fakeDeploymentStoreForAgent{
		deployments: map[string]*Deployment{
			"dep-1": {TenantModel: database.TenantModel{Model: database.Model{ID: "dep-1"}, OrgID: "org-999"}, ClusterID: "cluster-123"}, // DIFFERENT ORG
		},
	}
	store := &fakeDesiredStateStore{}
	tenant := &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}}
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       tenant,
		Logger:       slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"} // Agent authenticated as org-123

	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Post("/deployments/:deploymentId/releases/:releaseId/status", func(c *fiber.Ctx) error {
		return handler.UpdateDeploymentStatus(c, releaseStore, deploymentStore)
	})

	body := `{"status": "succeeded"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/deployments/dep-1/releases/rel-1/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	// CRIT-002 fix: Agent should be FORBIDDEN from updating deployment in different org.
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("got status %d, want %d (org mismatch - CRIT-002 fix)", resp.StatusCode, http.StatusForbidden)
	}
}

func TestAgentHandler_UpdateDeploymentStatus_ReleaseDeploymentMismatch(t *testing.T) {
	// Release belongs to a DIFFERENT deployment than specified in the URL.
	releaseStore := &fakeReleaseStoreForAgent{
		releases: map[string]*Release{
			"rel-1": {ID: "rel-1", OrgID: "org-123", DeploymentID: "dep-999"}, // DIFFERENT DEPLOYMENT
		},
	}
	deploymentStore := &fakeDeploymentStoreForAgent{
		deployments: map[string]*Deployment{
			"dep-1": {TenantModel: database.TenantModel{Model: database.Model{ID: "dep-1"}, OrgID: "org-123"}, ClusterID: "cluster-123"},
		},
	}
	store := &fakeDesiredStateStore{}
	tenant := &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}}
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       tenant,
		Logger:       slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"}

	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Post("/deployments/:deploymentId/releases/:releaseId/status", func(c *fiber.Ctx) error {
		return handler.UpdateDeploymentStatus(c, releaseStore, deploymentStore)
	})

	body := `{"status": "succeeded"}`
	// Request URL says dep-1, but rel-1 belongs to dep-999.
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/deployments/dep-1/releases/rel-1/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	// CRIT-002 fix: Agent should be FORBIDDEN from updating release that belongs to different deployment.
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("got status %d, want %d (release deployment mismatch - CRIT-002 fix)", resp.StatusCode, http.StatusForbidden)
	}
}

func TestAgentHandler_UpdateDeploymentStatus_ReleaseNotFound(t *testing.T) {
	releaseStore := &fakeReleaseStoreForAgent{releases: map[string]*Release{}}
	deploymentStore := &fakeDeploymentStoreForAgent{
		deployments: map[string]*Deployment{
			"dep-1": {TenantModel: database.TenantModel{Model: database.Model{ID: "dep-1"}, OrgID: "org-123"}, ClusterID: "cluster-123"},
		},
	}
	store := &fakeDesiredStateStore{}
	tenant := &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}}
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       tenant,
		Logger:       slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"}

	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Post("/deployments/:deploymentId/releases/:releaseId/status", func(c *fiber.Ctx) error {
		return handler.UpdateDeploymentStatus(c, releaseStore, deploymentStore)
	})

	body := `{"status": "succeeded"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/deployments/dep-1/releases/unknown-rel/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("got status %d, want %d (release not found)", resp.StatusCode, http.StatusForbidden)
	}
}

// TestAgentHandler_UpdateDeploymentStatus_EmitsEvent verifies that an
// agent-reported status transition emits the corresponding deployment domain
// event to the outbox, so the audit service records agent-driven transitions
// identically to user-driven ones (production-readiness fix for the agent path
// previously bypassing event emission).
func TestAgentHandler_UpdateDeploymentStatus_EmitsEvent(t *testing.T) {
	releaseStore := &fakeReleaseStoreForAgent{
		releases: map[string]*Release{
			"rel-1": {ID: "rel-1", OrgID: "org-123", DeploymentID: "dep-1", Revision: 1, Image: "nginx:latest"},
		},
	}
	deploymentStore := &fakeDeploymentStoreForAgent{
		deployments: map[string]*Deployment{
			"dep-1": {TenantModel: database.TenantModel{Model: database.Model{ID: "dep-1"}, OrgID: "org-123"}, ClusterID: "cluster-123"},
		},
	}
	store := &fakeDesiredStateStore{}
	tenant := &fakeTenantQuerier{allowedOrgs: map[string]bool{"org-123": true}}
	outbox := &fakeOutbox{}
	handler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       tenant,
		Outbox:       outbox,
		Logger:       slog.Default(),
	})
	validator := &fakeClusterValidator{orgID: "org-123"}

	app := newTestApp()
	agent := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agent.Post("/deployments/:deploymentId/releases/:releaseId/status", func(c *fiber.Ctx) error {
		return handler.UpdateDeploymentStatus(c, releaseStore, deploymentStore)
	})

	body := `{"status": "succeeded", "readyReplicas": 3}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/deployments/dep-1/releases/rel-1/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", "cluster-123")
	req.Header.Set("X-Agent-ID", "agent-123")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(outbox.events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(outbox.events))
	}
	if outbox.events[0].Type != EventDeploymentSucceeded {
		t.Errorf("event type = %q, want %q", outbox.events[0].Type, EventDeploymentSucceeded)
	}
	if outbox.events[0].Actor.Type != "agent" {
		t.Errorf("actor type = %q, want %q", outbox.events[0].Actor.Type, "agent")
	}
}

// Helper types for testing.
type testError struct {
	code int
	msg  string
}

func (e *testError) Error() string { return e.msg }

type fakeReleaseStoreForAgent struct {
	startedID      string
	finishedID     string
	finishedStatus string
	releases       map[string]*Release
}

func (f *fakeReleaseStoreForAgent) MarkStarted(ctx context.Context, id string) error {
	f.startedID = id
	return nil
}

func (f *fakeReleaseStoreForAgent) MarkFinished(ctx context.Context, id, status string, errMsg *string) error {
	f.finishedID = id
	f.finishedStatus = status
	return nil
}

func (f *fakeReleaseStoreForAgent) Create(ctx context.Context, r *Release) error { return nil }

func (f *fakeReleaseStoreForAgent) GetByID(ctx context.Context, id string) (*Release, error) {
	if f.releases == nil {
		return nil, apperrors.NotFound("release not found")
	}
	rel, ok := f.releases[id]
	if !ok {
		return nil, apperrors.NotFound("release not found")
	}
	return rel, nil
}

func (f *fakeReleaseStoreForAgent) GetByRevision(ctx context.Context, deploymentID string, revision int) (*Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreForAgent) GetLatest(ctx context.Context, deploymentID string) (*Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreForAgent) GetPreviousSuccessful(ctx context.Context, deploymentID string, beforeRevision int) (*Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreForAgent) List(ctx context.Context, deploymentID string, req database.PageRequest) (database.Page[Release], error) {
	return database.Page[Release]{}, nil
}

func (f *fakeReleaseStoreForAgent) UpdateStatus(ctx context.Context, id, status string, errMsg *string) error {
	return nil
}

type fakeDeploymentStoreForAgent struct {
	updatedID     string
	updatedStatus string
	deployments   map[string]*Deployment
}

func (f *fakeDeploymentStoreForAgent) UpdateStatus(ctx context.Context, id, status string, readyReplicas *int, errMsg *string) error {
	f.updatedID = id
	f.updatedStatus = status
	return nil
}

func (f *fakeDeploymentStoreForAgent) Create(ctx context.Context, d *Deployment) error { return nil }

func (f *fakeDeploymentStoreForAgent) GetByID(ctx context.Context, id string) (*Deployment, error) {
	if f.deployments == nil {
		return nil, apperrors.NotFound("deployment not found")
	}
	dep, ok := f.deployments[id]
	if !ok {
		return nil, apperrors.NotFound("deployment not found")
	}
	return dep, nil
}

func (f *fakeDeploymentStoreForAgent) List(ctx context.Context, appID string, req database.PageRequest) (database.Page[Deployment], error) {
	return database.Page[Deployment]{}, nil
}

func (f *fakeDeploymentStoreForAgent) ListByCluster(ctx context.Context, clusterID string, req database.PageRequest) (database.Page[Deployment], error) {
	return database.Page[Deployment]{}, nil
}

func (f *fakeDeploymentStoreForAgent) ListByOrg(ctx context.Context, req database.PageRequest) (database.Page[Deployment], error) {
	return database.Page[Deployment]{}, nil
}

func (f *fakeDeploymentStoreForAgent) SoftDelete(ctx context.Context, id string) error { return nil }
func (f *fakeDeploymentStoreForAgent) Update(ctx context.Context, d *Deployment) error { return nil }
func (f *fakeDeploymentStoreForAgent) Delete(ctx context.Context, id string) error     { return nil }
func (f *fakeDeploymentStoreForAgent) ListAllActive(ctx context.Context) ([]Deployment, error) {
	return nil, nil
}
