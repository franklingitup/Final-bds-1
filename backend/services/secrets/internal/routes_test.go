package secrets

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/middleware"
)

type rejectVerifier struct{}

func (rejectVerifier) Verify(string) (Identity, error) {
	return Identity{}, errInvalidToken
}

type fakeClusterValidator struct {
	orgID string
}

func (f *fakeClusterValidator) ValidateCluster(_ context.Context, _, _ string) (string, error) {
	return f.orgID, nil
}

func TestAgentSecrets_UnauthenticatedByUserJWTReachesAgentAuth(t *testing.T) {
	key, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey: %v", err)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	svc := NewService(Deps{
		Secrets:   newFakeSecretStore(),
		Encryptor: enc,
		Tenant:    &fakeTenantRunner{},
	})

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler(),
	})
	userHandler := NewHandler(svc, rejectVerifier{})
	agentHandler := NewAgentHandler(svc, &fakeClusterValidator{orgID: "org-123"}, nil)

	RegisterRoutes(app, userHandler)
	RegisterAgentRoutes(app, agentHandler)

	req := httptest.NewRequest(http.MethodGet, "/v1/agent/clusters/cluster-1/secrets", nil)
	req.Header.Set("X-Cluster-ID", "cluster-1")
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agent secrets status = %d, body = %s; expected 200 (AgentAuthMiddleware, not user JWT)", resp.StatusCode, body)
	}

	orgReq := httptest.NewRequest(http.MethodGet, "/v1/organizations/org-123/projects/proj-1/secrets", nil)
	orgResp, err := app.Test(orgReq)
	if err != nil {
		t.Fatalf("org secrets app.Test: %v", err)
	}
	defer orgResp.Body.Close()
	if orgResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("user secrets list status = %d, want 401 (RequireAuth still applied)", orgResp.StatusCode)
	}
}
