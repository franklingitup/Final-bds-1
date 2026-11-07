package pipeline

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/middleware"
	deployment "github.com/bdsplatform/platform/backend/services/deployment/internal"
)

const (
	testOrgID     = "org-1"
	testClusterID = "cluster-1"
	testAgentID   = "agent-1"
)

func rejectJWT(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid or expired token"})
}

type stubClusterValidator struct {
	orgID string
	err   error
}

func (s *stubClusterValidator) ValidateCluster(_ context.Context, _, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.orgID, nil
}

type passThroughTenant struct{}

func (passThroughTenant) WithTenant(ctx context.Context, _ string, fn database.TxFunc) error {
	return fn(ctx)
}

type fakeDesiredStates struct {
	byCluster    map[string][]DesiredState
	byDeployment map[string]*DesiredState
	syncUpdates  int
}

func (f *fakeDesiredStates) Create(_ context.Context, _ *DesiredState) error { return nil }

func (f *fakeDesiredStates) GetByID(_ context.Context, _ string) (*DesiredState, error) {
	return nil, apperrors.NotFound("desired state")
}

func (f *fakeDesiredStates) GetByDeploymentID(_ context.Context, deploymentID string) (*DesiredState, error) {
	if ds, ok := f.byDeployment[deploymentID]; ok {
		return ds, nil
	}
	return nil, apperrors.NotFound("desired state")
}

func (f *fakeDesiredStates) ListByCluster(_ context.Context, _ string, _ database.PageRequest) (database.Page[DesiredState], error) {
	return database.Page[DesiredState]{}, nil
}

func (f *fakeDesiredStates) ListPendingByCluster(_ context.Context, clusterID string) ([]DesiredState, error) {
	return f.byCluster[clusterID], nil
}

func (f *fakeDesiredStates) Update(_ context.Context, _ *DesiredState) error { return nil }

func (f *fakeDesiredStates) UpdateSyncStatus(_ context.Context, _, _ string, _ int64, _ *string) error {
	f.syncUpdates++
	return nil
}

func (f *fakeDesiredStates) Delete(_ context.Context, _ string) error { return nil }

type fakeMetrics struct {
	upserted int
}

func (f *fakeMetrics) Upsert(_ context.Context, _ *DeploymentMetrics) error {
	f.upserted++
	return nil
}

func (f *fakeMetrics) GetByDeploymentID(_ context.Context, _ string) (*DeploymentMetrics, error) {
	return nil, apperrors.NotFound("metrics")
}

func (f *fakeMetrics) List(_ context.Context, _ string, _ database.PageRequest) (database.Page[DeploymentMetrics], error) {
	return database.Page[DeploymentMetrics]{}, nil
}

func newAgentAuthApp(t *testing.T) *fiber.App {
	t.Helper()
	ds := &DesiredState{
		ID:           "ds-1",
		OrgID:        testOrgID,
		DeploymentID: "dep-1",
		ClusterID:    testClusterID,
		Namespace:    "default",
		SyncStatus:   SyncStatusPending,
	}
	h := NewHandler(NewService(Deps{
		DesiredStates: &fakeDesiredStates{
			byCluster:    map[string][]DesiredState{testClusterID: {*ds}},
			byDeployment: map[string]*DesiredState{"dep-1": ds},
		},
		DeploymentMetrics: &fakeMetrics{},
		Tenant:            passThroughTenant{},
	}))
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler(),
	})
	RegisterRoutes(app, h, rejectJWT, deployment.AgentAuthMiddleware(&stubClusterValidator{orgID: testOrgID}))
	return app
}

func agentPath(suffix string) string {
	return "/v1/agent/organizations/" + testOrgID + "/clusters/" + testClusterID + suffix
}

func setAgentHeaders(req *http.Request) {
	req.Header.Set("X-Cluster-ID", testClusterID)
	req.Header.Set("X-Agent-ID", testAgentID)
}

func TestPipelineAgentRoutesRejectMissingCredentials(t *testing.T) {
	app := newAgentAuthApp(t)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, agentPath("/desired-state"), ""},
		{http.MethodPost, agentPath("/sync"), `{"deploymentId":"dep-1","syncStatus":"synced"}`},
		{http.MethodPost, agentPath("/metrics"), `{"deploymentId":"dep-1","readyReplicas":1}`},
	}

	for _, tc := range cases {
		var body io.Reader
		if tc.body != "" {
			body = bytes.NewBufferString(tc.body)
		}
		req := httptest.NewRequest(tc.method, tc.path, body)
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401 (missing agent credentials)", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestPipelineAgentRoutesAcceptValidCredentials(t *testing.T) {
	app := newAgentAuthApp(t)

	get := httptest.NewRequest(http.MethodGet, agentPath("/desired-state"), nil)
	setAgentHeaders(get)
	resp, err := app.Test(get)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET desired-state: status = %d, want 200; body=%s", resp.StatusCode, body)
	}

	syncReq := httptest.NewRequest(http.MethodPost, agentPath("/sync"), bytes.NewBufferString(`{"deploymentId":"dep-1","syncStatus":"synced"}`))
	syncReq.Header.Set("Content-Type", "application/json")
	setAgentHeaders(syncReq)
	resp, err = app.Test(syncReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST sync: status = %d, want 204; body=%s", resp.StatusCode, body)
	}

	metricsReq := httptest.NewRequest(http.MethodPost, agentPath("/metrics"), bytes.NewBufferString(`{"deploymentId":"dep-1","readyReplicas":1}`))
	metricsReq.Header.Set("Content-Type", "application/json")
	setAgentHeaders(metricsReq)
	resp, err = app.Test(metricsReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST metrics: status = %d, want 204; body=%s", resp.StatusCode, body)
	}
}

func TestPipelineAgentRoutesRejectMismatchedPathIdentity(t *testing.T) {
	app := newAgentAuthApp(t)

	path := "/v1/agent/organizations/org-other/clusters/" + testClusterID + "/desired-state"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	setAgentHeaders(req)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("mismatched org path: status = %d, want 403", resp.StatusCode)
	}
}

func TestPipelineAgentRoutesDoNotInheritOrgJWTMiddleware(t *testing.T) {
	app := newAgentAuthApp(t)

	req := httptest.NewRequest(http.MethodGet, agentPath("/desired-state"), nil)
	setAgentHeaders(req)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == fiber.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("agent route hit user JWT middleware: status=%d body=%s", resp.StatusCode, body)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("GET desired-state with agent credentials: status = %d, want 200", resp.StatusCode)
	}
}

func TestPipelineOrgRoutesStillRequireJWT(t *testing.T) {
	app := newAgentAuthApp(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/organizations/"+testOrgID+"/deployments/dep-1/desired-state", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("org route status = %d, want 401", resp.StatusCode)
	}
}
