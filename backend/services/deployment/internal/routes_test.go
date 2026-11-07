package deployment

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/middleware"
)

func TestAgentDesiredState_UnauthenticatedByUserJWTReachesAgentAuth(t *testing.T) {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler(),
	})

	userHandler := NewHandler(nil, NewTokenVerifier(config.AuthConfig{JWTSigningKey: "test-signing-key"}))
	store := &fakeDesiredStateStore{}
	agentHandler := NewAgentHandler(AgentHandlerDeps{
		DesiredState: store,
		Tenant:       &fakeTenantQuerier{},
	})
	validator := &fakeClusterValidator{orgID: "org-123"}

	RegisterRoutes(app, userHandler)
	RegisterAgentRoutes(app, agentHandler, AgentAuthMiddleware(validator), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/agent/clusters/cluster-1/desired-state", nil)
	req.Header.Set("X-Cluster-ID", "cluster-1")
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("desired-state status = %d, body = %s; expected 200 (AgentAuthMiddleware, not user JWT)", resp.StatusCode, body)
	}
	if store.lastClusterID != "cluster-1" || store.lastOrgID != "org-123" {
		t.Fatalf("store called with org=%q cluster=%q", store.lastOrgID, store.lastClusterID)
	}

	orgReq := httptest.NewRequest(http.MethodGet, "/v1/organizations/org-123/deployments", nil)
	orgResp, err := app.Test(orgReq)
	if err != nil {
		t.Fatalf("org list app.Test: %v", err)
	}
	defer orgResp.Body.Close()
	if orgResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("org deployment list status = %d, want 401 (RequireAuth still applied)", orgResp.StatusCode)
	}
}
