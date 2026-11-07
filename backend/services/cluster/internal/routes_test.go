package cluster

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/middleware"
)

// TestHeartbeat_UnauthenticatedByUserJWTReachesAgentAuth reproduces production
// route registration order (RegisterRoutes then RegisterAgentRoutes) and asserts
// a heartbeat authenticated only by X-Cluster-ID / X-Agent-ID is not rejected
// by the user-JWT middleware with "invalid or expired token".
func TestHeartbeat_UnauthenticatedByUserJWTReachesAgentAuth(t *testing.T) {
	env := newTestEnv()
	orgID := uuid.NewString()
	userID := uuid.NewString()
	c := env.createCluster(t, orgID, userID, "Prod", "prod")

	agentID := "agent-heartbeat-1"
	env.clusters.mu.Lock()
	stored := env.clusters.clusters[c.ID]
	stored.AgentID = &agentID
	stored.Status = StatusConnected
	env.clusters.mu.Unlock()

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler(),
	})
	userHandler := NewHandler(env.svc, NewTokenVerifier(config.AuthConfig{JWTSigningKey: "test-signing-key"}))
	agentHandler := NewAgentHandler(env.svc, NewClusterValidator(env.clusters, nil), nil)

	RegisterRoutes(app, userHandler, nil)
	RegisterAgentRoutes(app, agentHandler)

	body, err := json.Marshal(AgentHeartbeatRequest{
		AgentID:           agentID,
		KubernetesVersion: "1.28.0",
		NodeCount:         3,
		APIServerHealthy:  true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/agent/clusters/"+c.ID+"/heartbeat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", c.ID)
	req.Header.Set("X-Agent-ID", agentID)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat status = %d, body = %s; expected 200 (AgentAuthMiddleware, not user JWT)", resp.StatusCode, respBody)
	}

	orgReq := httptest.NewRequest(http.MethodGet, "/v1/organizations/"+orgID+"/clusters", nil)
	orgResp, err := app.Test(orgReq)
	if err != nil {
		t.Fatalf("org list app.Test: %v", err)
	}
	defer orgResp.Body.Close()
	if orgResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("org cluster list status = %d, want 401 (RequireAuth still applied)", orgResp.StatusCode)
	}
}

// TestProvisionedClusterAppearsInNormalClusterList covers the Phase 4/5 path:
// provisioning creates a pending cluster plus registration token through the
// internal API, the real agent consumes that token, and the normal user-facing
// cluster list returns the connected cluster.
func TestProvisionedClusterAppearsInNormalClusterList(t *testing.T) {
	env := newTestEnv()
	const (
		internalToken = "test-internal-token"
		jwtKey        = "test-signing-key"
	)
	orgID := uuid.NewString()
	userID := uuid.NewString()

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler(),
	})
	handler := NewHandler(env.svc, NewTokenVerifier(config.AuthConfig{JWTSigningKey: jwtKey})).
		WithInternalToken(internalToken)
	RegisterRoutes(app, handler, nil)

	createBody, err := json.Marshal(InternalCreateClusterRequest{
		Name:          "Provisioned Production",
		Slug:          "provisioned-production",
		CloudProvider: ProviderAWS,
		Region:        "us-east-1",
		CreatedBy:     userID,
	})
	if err != nil {
		t.Fatalf("marshal internal create request: %v", err)
	}
	createReq := httptest.NewRequest(
		http.MethodPost,
		"/internal/organizations/"+orgID+"/clusters",
		bytes.NewReader(createBody),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-Internal-Token", internalToken)
	createResp, err := app.Test(createReq)
	if err != nil {
		t.Fatalf("internal create app.Test: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("internal create status = %d, body = %s", createResp.StatusCode, body)
	}

	var created InternalCreateClusterResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode internal create response: %v", err)
	}
	if created.RegistrationToken == "" {
		t.Fatal("internal create returned an empty registration token")
	}

	registerBody, err := json.Marshal(AgentRegisterRequest{
		Token:             created.RegistrationToken,
		AgentID:           "provisioned-agent-1",
		KubernetesVersion: "1.30",
		NodeCount:         3,
		CloudProvider:     ProviderAWS,
		Region:            "us-east-1",
	})
	if err != nil {
		t.Fatalf("marshal agent registration: %v", err)
	}
	registerReq := httptest.NewRequest(http.MethodPost, "/v1/agent/register", bytes.NewReader(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp, err := app.Test(registerReq)
	if err != nil {
		t.Fatalf("agent register app.Test: %v", err)
	}
	defer registerResp.Body.Close()
	if registerResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(registerResp.Body)
		t.Fatalf("agent register status = %d, body = %s", registerResp.StatusCode, body)
	}

	claims := &accessClaims{
		Email: "owner@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    jwtIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	userToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtKey))
	if err != nil {
		t.Fatalf("sign user token: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/organizations/"+orgID+"/clusters", nil)
	listReq.Header.Set("Authorization", "Bearer "+userToken)
	listResp, err := app.Test(listReq)
	if err != nil {
		t.Fatalf("cluster list app.Test: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		t.Fatalf("cluster list status = %d, body = %s", listResp.StatusCode, body)
	}

	var listed struct {
		Items []ClusterView `json:"items"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode cluster list: %v", err)
	}
	for _, cluster := range listed.Items {
		if cluster.ID == created.ID {
			if cluster.Status != StatusConnected {
				t.Fatalf("provisioned cluster status = %q, want %q", cluster.Status, StatusConnected)
			}
			if cluster.AgentID != "provisioned-agent-1" {
				t.Fatalf("provisioned cluster agentId = %q", cluster.AgentID)
			}
			return
		}
	}
	t.Fatalf("provisioned cluster %s was absent from normal cluster list", created.ID)
}
