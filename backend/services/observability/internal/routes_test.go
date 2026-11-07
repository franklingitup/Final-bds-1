package observability

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/middleware"
)

// fakeClusterValidator is a test implementation of ClusterValidator.
type fakeClusterValidator struct {
	clusters map[string]struct {
		orgID   string
		agentID string
		status  string
	}
}

func newFakeClusterValidator() *fakeClusterValidator {
	return &fakeClusterValidator{
		clusters: make(map[string]struct {
			orgID   string
			agentID string
			status  string
		}),
	}
}

func (v *fakeClusterValidator) addCluster(clusterID, agentID, orgID, status string) {
	v.clusters[clusterID] = struct {
		orgID   string
		agentID string
		status  string
	}{orgID: orgID, agentID: agentID, status: status}
}

func (v *fakeClusterValidator) ValidateCluster(ctx context.Context, clusterID, agentID string) (string, error) {
	cluster, ok := v.clusters[clusterID]
	if !ok {
		return "", apperrors.Unauthorized("authentication failed")
	}
	if cluster.status != "connected" {
		return "", apperrors.Unauthorized("authentication failed")
	}
	if cluster.agentID != agentID {
		return "", apperrors.Unauthorized("authentication failed")
	}
	return cluster.orgID, nil
}

// TestAgentIngestRoutesRejectMissingCredentials verifies that agent ingest routes
// reject requests that lack proper X-Cluster-ID and X-Agent-ID credentials.
func TestAgentIngestRoutesRejectMissingCredentials(t *testing.T) {
	validator := newFakeClusterValidator()
	validator.addCluster("cluster-1", "agent-1", "org-1", "connected")

	// Create app with the agent auth middleware applied to test routes
	app := fiber.New(fiber.Config{
		ErrorHandler: middleware.ErrorHandler(),
	})

	// Apply agent auth middleware to /v1/agent group
	agentGroup := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agentGroup.Post("/metrics", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	agentGroup.Post("/logs", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	agentGroup.Post("/health", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	routes := []string{
		"/v1/agent/metrics",
		"/v1/agent/logs",
		"/v1/agent/health",
	}

	for _, route := range routes {
		t.Run("POST "+route+" missing all credentials", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, route, bytes.NewReader([]byte("{}")))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("expected 401, got %d: %s", resp.StatusCode, body)
			}
		})

		t.Run("POST "+route+" missing X-Agent-ID", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, route, bytes.NewReader([]byte("{}")))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Cluster-ID", "cluster-1")

			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("expected 401, got %d: %s", resp.StatusCode, body)
			}
		})

		t.Run("POST "+route+" invalid credentials", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, route, bytes.NewReader([]byte("{}")))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Cluster-ID", "cluster-1")
			req.Header.Set("X-Agent-ID", "wrong-agent")

			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("expected 401, got %d: %s", resp.StatusCode, body)
			}
		})
	}
}

// TestAgentIngestRoutesAcceptValidCredentials verifies that agent ingest routes
// accept requests with valid X-Cluster-ID and X-Agent-ID credentials.
func TestAgentIngestRoutesAcceptValidCredentials(t *testing.T) {
	validator := newFakeClusterValidator()
	validator.addCluster("cluster-1", "agent-1", "org-1", "connected")

	// Create app with the agent auth middleware applied to test routes
	app := fiber.New(fiber.Config{
		ErrorHandler: middleware.ErrorHandler(),
	})

	// Apply agent auth middleware and create handlers that verify context
	agentGroup := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agentGroup.Post("/metrics", func(c *fiber.Ctx) error {
		agent := AgentFromContext(c.UserContext())
		if agent == nil {
			return apperrors.Unauthorized("agent context missing")
		}
		if agent.ClusterID != "cluster-1" {
			return apperrors.Internal("wrong cluster ID in context")
		}
		if agent.OrganizationID != "org-1" {
			return apperrors.Internal("wrong org ID in context")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	agentGroup.Post("/logs", func(c *fiber.Ctx) error {
		agent := AgentFromContext(c.UserContext())
		if agent == nil {
			return apperrors.Unauthorized("agent context missing")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	agentGroup.Post("/health", func(c *fiber.Ctx) error {
		agent := AgentFromContext(c.UserContext())
		if agent == nil {
			return apperrors.Unauthorized("agent context missing")
		}
		return c.SendStatus(fiber.StatusOK)
	})

	t.Run("POST /v1/agent/metrics with valid credentials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/agent/metrics", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Cluster-ID", "cluster-1")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			respBody, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 204, got %d: %s", resp.StatusCode, respBody)
		}
	})

	t.Run("POST /v1/agent/logs with valid credentials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/agent/logs", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Cluster-ID", "cluster-1")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			respBody, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 204, got %d: %s", resp.StatusCode, respBody)
		}
	})

	t.Run("POST /v1/agent/health with valid credentials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/agent/health", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Cluster-ID", "cluster-1")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 200, got %d: %s", resp.StatusCode, respBody)
		}
	})
}

// TestAgentIngestRoutesRejectDisconnectedCluster verifies that agent ingest routes
// reject requests from clusters that are not in "connected" status.
func TestAgentIngestRoutesRejectDisconnectedCluster(t *testing.T) {
	validator := newFakeClusterValidator()
	validator.addCluster("cluster-1", "agent-1", "org-1", "disconnected")

	app := fiber.New(fiber.Config{
		ErrorHandler: middleware.ErrorHandler(),
	})

	agentGroup := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agentGroup.Post("/metrics", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	t.Run("disconnected cluster is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/agent/metrics", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Cluster-ID", "cluster-1")
		req.Header.Set("X-Agent-ID", "agent-1")

		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			respBody, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 401, got %d: %s", resp.StatusCode, respBody)
		}
	})
}

// TestAgentIngestRoutesContextContainsIdentity verifies that after successful
// authentication, the agent identity is available in both fiber.Locals and context.
func TestAgentIngestRoutesContextContainsIdentity(t *testing.T) {
	validator := newFakeClusterValidator()
	validator.addCluster("cluster-1", "agent-1", "org-1", "connected")

	app := fiber.New(fiber.Config{
		ErrorHandler: middleware.ErrorHandler(),
	})

	agentGroup := app.Group("/v1/agent", AgentAuthMiddleware(validator))
	agentGroup.Post("/metrics", func(c *fiber.Ctx) error {
		// Check Locals
		orgID := c.Locals(LocalAgentOrgID)
		clusterID := c.Locals(LocalAgentClusterID)
		agentID := c.Locals(LocalAgentID)

		if orgID != "org-1" {
			t.Errorf("LocalAgentOrgID = %v, want org-1", orgID)
		}
		if clusterID != "cluster-1" {
			t.Errorf("LocalAgentClusterID = %v, want cluster-1", clusterID)
		}
		if agentID != "agent-1" {
			t.Errorf("LocalAgentID = %v, want agent-1", agentID)
		}

		// Check context
		agent := AgentFromContext(c.UserContext())
		if agent == nil {
			t.Error("AgentFromContext returned nil")
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		if agent.OrganizationID != "org-1" {
			t.Errorf("agent.OrganizationID = %v, want org-1", agent.OrganizationID)
		}
		if agent.ClusterID != "cluster-1" {
			t.Errorf("agent.ClusterID = %v, want cluster-1", agent.ClusterID)
		}
		if agent.AgentID != "agent-1" {
			t.Errorf("agent.AgentID = %v, want agent-1", agent.AgentID)
		}

		return c.SendStatus(fiber.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/agent/metrics", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", "cluster-1")
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 204, got %d: %s", resp.StatusCode, respBody)
	}
}
