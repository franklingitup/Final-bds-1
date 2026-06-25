//go:build integration

package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/migrations"
)

type testGateway struct {
	app      *fiber.App
	db       *database.DB
	outbox   *events.PostgresOutbox
	orgID    string
	jwtKey   string
}

func setupGateway(t *testing.T) *testGateway {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	cfg := config.Config{}
	cfg.Database.URL = dsn
	cfg.Auth.JWTSigningKey = "test-secret-key-for-integration-tests"

	db, err := database.Connect(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Apply migrations: tenant (for organizations table), cluster, outbox.
	for _, m := range []struct {
		service string
		table   string
	}{
		{"tenant", "schema_migrations_tenant"},
		{"cluster", "schema_migrations_cluster"},
		{"outbox", "schema_migrations_outbox"},
	} {
		fsys, err := migrations.Service(m.service)
		if err != nil {
			t.Fatalf("migrations.Service(%s): %v", m.service, err)
		}
		migs, err := database.LoadMigrations(fsys)
		if err != nil {
			t.Fatalf("LoadMigrations: %v", err)
		}
		migrator, err := database.NewMigrator(db, m.table, migs)
		if err != nil {
			t.Fatalf("NewMigrator: %v", err)
		}
		if _, err := migrator.Up(ctx); err != nil {
			t.Fatalf("migrate %s: %v", m.service, err)
		}
	}

	// Seed an organization.
	orgID := uuid.NewString()
	_, err = db.Pool.Exec(ctx, `
INSERT INTO organizations (id, name, slug, owner_user_id) 
VALUES ($1, 'Test Org', 'test-org', $2)
ON CONFLICT (id) DO NOTHING`, orgID, uuid.NewString())
	if err != nil {
		t.Fatalf("seed org: %v", err)
	}

	outbox := events.NewPostgresOutbox(db, "outbox")
	svc := NewService(Deps{
		Clusters:   NewClusterStore(db),
		Tokens:     NewTokenStore(db),
		Heartbeats: NewHeartbeatStore(db),
		Outbox:     outbox,
		Tenant:     db,
	})
	handler := NewHandler(svc, NewTokenVerifier(cfg.Auth))

	app := fiber.New(fiber.Config{ErrorHandler: func(c *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}
		return c.Status(code).JSON(fiber.Map{"error": err.Error()})
	}})
	RegisterRoutes(app, handler)

	return &testGateway{
		app:    app,
		db:     db,
		outbox: outbox,
		orgID:  orgID,
		jwtKey: cfg.Auth.JWTSigningKey,
	}
}

func (g *testGateway) signToken(userID, email string) string {
	claims := &accessClaims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    jwtIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte(g.jwtKey))
	return signed
}

func (g *testGateway) request(t *testing.T, method, path string, body any, token string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := g.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	return resp
}

func (g *testGateway) assertOutboxHas(t *testing.T, eventType string) {
	t.Helper()
	ctx := context.Background()
	rows, err := g.db.Pool.Query(ctx, "SELECT event_type FROM outbox WHERE event_type = $1", eventType)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Errorf("expected outbox to have %s event", eventType)
	}
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestIntegration_ClusterLifecycle(t *testing.T) {
	g := setupGateway(t)
	ctx := context.Background()
	userID := uuid.NewString()
	token := g.signToken(userID, "test@example.com")
	slug := "cluster-" + uuid.NewString()[:8]

	// Create cluster.
	resp := g.request(t, "POST", "/v1/organizations/"+g.orgID+"/clusters", CreateClusterRequest{
		Name:          "Production",
		Slug:          slug,
		CloudProvider: strPtr("aws"),
		Region:        strPtr("us-west-2"),
	}, token)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}

	var created ClusterView
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	if created.Status != StatusPending {
		t.Errorf("status = %q, want pending", created.Status)
	}
	g.assertOutboxHas(t, EventClusterCreated)

	// Get cluster.
	resp = g.request(t, "GET", "/v1/organizations/"+g.orgID+"/clusters/"+created.ID, nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("get status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// List clusters.
	resp = g.request(t, "GET", "/v1/organizations/"+g.orgID+"/clusters", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("list status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Update cluster.
	newName := "Production Updated"
	resp = g.request(t, "PATCH", "/v1/organizations/"+g.orgID+"/clusters/"+created.ID, UpdateClusterRequest{
		Name: &newName,
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("update status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Delete cluster.
	resp = g.request(t, "DELETE", "/v1/organizations/"+g.orgID+"/clusters/"+created.ID, nil, token)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	g.assertOutboxHas(t, EventClusterDeleted)

	// Cleanup.
	_, _ = g.db.Pool.Exec(ctx, "DELETE FROM clusters WHERE id = $1", created.ID)
}

func TestIntegration_AgentRegistration(t *testing.T) {
	g := setupGateway(t)
	ctx := context.Background()
	userID := uuid.NewString()
	token := g.signToken(userID, "test@example.com")
	slug := "agent-test-" + uuid.NewString()[:8]

	// Create cluster.
	resp := g.request(t, "POST", "/v1/organizations/"+g.orgID+"/clusters", CreateClusterRequest{
		Name: "Agent Test",
		Slug: slug,
	}, token)
	var cluster ClusterView
	json.NewDecoder(resp.Body).Decode(&cluster)
	resp.Body.Close()

	// Generate registration token.
	resp = g.request(t, "POST", "/v1/organizations/"+g.orgID+"/clusters/"+cluster.ID+"/tokens", GenerateTokenRequest{
		ExpiresIn: "1h",
	}, token)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("generate token status = %d, body = %s", resp.StatusCode, body)
	}

	var regToken TokenWithSecret
	json.NewDecoder(resp.Body).Decode(&regToken)
	resp.Body.Close()

	if regToken.Token == "" {
		t.Fatal("expected plaintext token")
	}
	g.assertOutboxHas(t, EventRegistrationTokenCreated)

	// Register agent (no auth required).
	resp = g.request(t, "POST", "/v1/agent/register", AgentRegisterRequest{
		Token:             regToken.Token,
		AgentID:           "agent-int-test",
		KubernetesVersion: "1.28.5",
		NodeCount:         3,
		CloudProvider:     "aws",
		Region:            "us-west-2",
	}, "")
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("register agent status = %d, body = %s", resp.StatusCode, body)
	}

	var registered ClusterView
	json.NewDecoder(resp.Body).Decode(&registered)
	resp.Body.Close()

	if registered.Status != StatusConnected {
		t.Errorf("status = %q, want connected", registered.Status)
	}
	if registered.AgentID != "agent-int-test" {
		t.Errorf("agentId = %q, want agent-int-test", registered.AgentID)
	}
	g.assertOutboxHas(t, EventClusterRegistered)

	// Send heartbeat.
	resp = g.request(t, "POST", "/v1/organizations/"+g.orgID+"/clusters/"+cluster.ID+"/heartbeat", AgentHeartbeatRequest{
		AgentID:           "agent-int-test",
		KubernetesVersion: "1.28.6",
		NodeCount:         4,
		APIServerHealthy:  true,
	}, token)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("heartbeat status = %d, body = %s", resp.StatusCode, body)
	}
	resp.Body.Close()
	g.assertOutboxHas(t, EventClusterHeartbeatReceived)

	// Get heartbeat history.
	resp = g.request(t, "GET", "/v1/organizations/"+g.orgID+"/clusters/"+cluster.ID+"/heartbeats", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("get heartbeats status = %d", resp.StatusCode)
	}

	var heartbeats struct {
		Items []HeartbeatView `json:"items"`
	}
	json.NewDecoder(resp.Body).Decode(&heartbeats)
	resp.Body.Close()

	if len(heartbeats.Items) == 0 {
		t.Error("expected at least one heartbeat")
	}

	// Cleanup.
	_, _ = g.db.Pool.Exec(ctx, "DELETE FROM cluster_heartbeats WHERE cluster_id = $1", cluster.ID)
	_, _ = g.db.Pool.Exec(ctx, "DELETE FROM cluster_registration_tokens WHERE cluster_id = $1", cluster.ID)
	_, _ = g.db.Pool.Exec(ctx, "DELETE FROM clusters WHERE id = $1", cluster.ID)
}

func TestIntegration_CrossTenantIsolation(t *testing.T) {
	g := setupGateway(t)
	ctx := context.Background()

	// Create another org.
	otherOrgID := uuid.NewString()
	_, _ = g.db.Pool.Exec(ctx, `
INSERT INTO organizations (id, name, slug, owner_user_id) 
VALUES ($1, 'Other Org', 'other-org', $2)
ON CONFLICT (id) DO NOTHING`, otherOrgID, uuid.NewString())

	userID := uuid.NewString()
	token := g.signToken(userID, "test@example.com")
	slug := "tenant-iso-" + uuid.NewString()[:8]

	// Create cluster in first org.
	resp := g.request(t, "POST", "/v1/organizations/"+g.orgID+"/clusters", CreateClusterRequest{
		Name: "Isolated",
		Slug: slug,
	}, token)
	var cluster ClusterView
	json.NewDecoder(resp.Body).Decode(&cluster)
	resp.Body.Close()

	// Try to access from other org (should fail with not found due to RLS).
	resp = g.request(t, "GET", "/v1/organizations/"+otherOrgID+"/clusters/"+cluster.ID, nil, token)
	if resp.StatusCode == http.StatusOK {
		t.Error("expected cross-tenant access to fail")
	}
	resp.Body.Close()

	// Cleanup.
	_, _ = g.db.Pool.Exec(ctx, "DELETE FROM clusters WHERE id = $1", cluster.ID)
	_, _ = g.db.Pool.Exec(ctx, "DELETE FROM organizations WHERE id = $1", otherOrgID)
}

func TestIntegration_TokenRevocation(t *testing.T) {
	g := setupGateway(t)
	ctx := context.Background()
	userID := uuid.NewString()
	token := g.signToken(userID, "test@example.com")
	slug := "revoke-test-" + uuid.NewString()[:8]

	// Create cluster.
	resp := g.request(t, "POST", "/v1/organizations/"+g.orgID+"/clusters", CreateClusterRequest{
		Name: "Revoke Test",
		Slug: slug,
	}, token)
	var cluster ClusterView
	json.NewDecoder(resp.Body).Decode(&cluster)
	resp.Body.Close()

	// Generate token.
	resp = g.request(t, "POST", "/v1/organizations/"+g.orgID+"/clusters/"+cluster.ID+"/tokens", GenerateTokenRequest{}, token)
	var regToken TokenWithSecret
	json.NewDecoder(resp.Body).Decode(&regToken)
	resp.Body.Close()

	// Revoke token.
	resp = g.request(t, "DELETE", "/v1/organizations/"+g.orgID+"/clusters/"+cluster.ID+"/tokens/"+regToken.ID, nil, token)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("revoke status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Try to register with revoked token.
	resp = g.request(t, "POST", "/v1/agent/register", AgentRegisterRequest{
		Token:             regToken.Token,
		AgentID:           "agent-revoke-test",
		KubernetesVersion: "1.28.5",
		NodeCount:         3,
	}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for revoked token, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Cleanup.
	_, _ = g.db.Pool.Exec(ctx, "DELETE FROM cluster_registration_tokens WHERE cluster_id = $1", cluster.ID)
	_, _ = g.db.Pool.Exec(ctx, "DELETE FROM clusters WHERE id = $1", cluster.ID)
}

func strPtr(s string) *string { return &s }
