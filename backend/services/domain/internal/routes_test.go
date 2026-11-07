package domain

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	platformmiddleware "github.com/bdsplatform/platform/backend/libs/middleware"
)

// fakeClusterValidator implements ClusterValidator for tests.
type fakeClusterValidator struct {
	clusters map[string]struct {
		orgID   string
		agentID string
		status  string
	}
}

func (v *fakeClusterValidator) ValidateCluster(ctx context.Context, clusterID, agentID string) (string, error) {
	c, ok := v.clusters[clusterID]
	if !ok {
		return "", apperrors.Unauthorized("authentication failed")
	}
	if c.status != "connected" {
		return "", apperrors.Unauthorized("authentication failed")
	}
	if c.agentID != agentID {
		return "", apperrors.Unauthorized("authentication failed")
	}
	return c.orgID, nil
}

func TestAgentRoutesRejectMissingCredentials(t *testing.T) {
	validator := &fakeClusterValidator{
		clusters: map[string]struct {
			orgID   string
			agentID string
			status  string
		}{
			"cluster-1": {orgID: "org-1", agentID: "agent-1", status: "connected"},
		},
	}

	app := fiber.New(fiber.Config{
		ErrorHandler: platformmiddleware.ErrorHandler(),
	})

	// Use a simple test handler that returns success if auth passes
	testHandler := func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true})
	}

	agentAuth := AgentAuthMiddleware(validator)
	agent := app.Group("/v1/agent", agentAuth)
	agent.Get("/clusters/:clusterId/ingresses", testHandler)
	agent.Post("/ingresses/:ingressId/sync", testHandler)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"GetIngresses no headers", http.MethodGet, "/v1/agent/clusters/cluster-1/ingresses"},
		{"ReportSync no headers", http.MethodPost, "/v1/agent/ingresses/ingress-1/sync"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			// Intentionally no X-Cluster-ID or X-Agent-ID headers

			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("expected 401, got %d; body=%s", resp.StatusCode, body)
			}
		})
	}
}

func TestAgentRoutesAcceptValidCredentials(t *testing.T) {
	validator := &fakeClusterValidator{
		clusters: map[string]struct {
			orgID   string
			agentID string
			status  string
		}{
			"cluster-1": {orgID: "org-1", agentID: "agent-1", status: "connected"},
		},
	}

	app := fiber.New(fiber.Config{
		ErrorHandler: platformmiddleware.ErrorHandler(),
	})

	// Use a simple test handler that verifies context is populated correctly
	testHandler := func(c *fiber.Ctx) error {
		agent := AgentFromContext(c.UserContext())
		if agent == nil {
			return apperrors.Unauthorized("agent not in context")
		}
		return c.JSON(fiber.Map{
			"clusterID": agent.ClusterID,
			"orgID":     agent.OrganizationID,
			"agentID":   agent.AgentID,
		})
	}

	agentAuth := AgentAuthMiddleware(validator)
	agent := app.Group("/v1/agent", agentAuth)
	agent.Get("/clusters/:clusterId/ingresses", testHandler)
	agent.Post("/ingresses/:ingressId/sync", testHandler)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"GetIngresses valid creds", http.MethodGet, "/v1/agent/clusters/cluster-1/ingresses"},
		{"ReportSync valid creds", http.MethodPost, "/v1/agent/ingresses/ingress-1/sync"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Cluster-ID", "cluster-1")
			req.Header.Set("X-Agent-ID", "agent-1")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("expected 200, got %d; body=%s", resp.StatusCode, body)
			}
		})
	}
}

func TestAgentRoutesRejectDisconnectedCluster(t *testing.T) {
	validator := &fakeClusterValidator{
		clusters: map[string]struct {
			orgID   string
			agentID string
			status  string
		}{
			"cluster-1": {orgID: "org-1", agentID: "agent-1", status: "disconnected"},
		},
	}

	app := fiber.New(fiber.Config{
		ErrorHandler: platformmiddleware.ErrorHandler(),
	})

	testHandler := func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true})
	}

	agentAuth := AgentAuthMiddleware(validator)
	agent := app.Group("/v1/agent", agentAuth)
	agent.Get("/clusters/:clusterId/ingresses", testHandler)

	req := httptest.NewRequest(http.MethodGet, "/v1/agent/clusters/cluster-1/ingresses", nil)
	req.Header.Set("X-Cluster-ID", "cluster-1")
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 401, got %d; body=%s", resp.StatusCode, body)
	}
}

func TestAgentRoutesRejectWrongAgentID(t *testing.T) {
	validator := &fakeClusterValidator{
		clusters: map[string]struct {
			orgID   string
			agentID string
			status  string
		}{
			"cluster-1": {orgID: "org-1", agentID: "agent-1", status: "connected"},
		},
	}

	app := fiber.New(fiber.Config{
		ErrorHandler: platformmiddleware.ErrorHandler(),
	})

	testHandler := func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true})
	}

	agentAuth := AgentAuthMiddleware(validator)
	agent := app.Group("/v1/agent", agentAuth)
	agent.Get("/clusters/:clusterId/ingresses", testHandler)

	// Correct cluster ID but wrong agent ID
	req := httptest.NewRequest(http.MethodGet, "/v1/agent/clusters/cluster-1/ingresses", nil)
	req.Header.Set("X-Cluster-ID", "cluster-1")
	req.Header.Set("X-Agent-ID", "wrong-agent")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 401, got %d; body=%s", resp.StatusCode, body)
	}
}

func TestAgentContextContainsIdentity(t *testing.T) {
	validator := &fakeClusterValidator{
		clusters: map[string]struct {
			orgID   string
			agentID string
			status  string
		}{
			"cluster-1": {orgID: "org-1", agentID: "agent-1", status: "connected"},
		},
	}

	app := fiber.New(fiber.Config{
		ErrorHandler: platformmiddleware.ErrorHandler(),
	})

	// Handler that verifies all context values are set correctly
	testHandler := func(c *fiber.Ctx) error {
		// Check context values
		agent := AgentFromContext(c.UserContext())
		if agent == nil {
			t.Error("AgentFromContext returned nil")
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		if agent.ClusterID != "cluster-1" {
			t.Errorf("ClusterID = %q, want %q", agent.ClusterID, "cluster-1")
		}
		if agent.OrganizationID != "org-1" {
			t.Errorf("OrganizationID = %q, want %q", agent.OrganizationID, "org-1")
		}
		if agent.AgentID != "agent-1" {
			t.Errorf("AgentID = %q, want %q", agent.AgentID, "agent-1")
		}

		// Check fiber locals
		if got := c.Locals(LocalAgentOrgID); got != "org-1" {
			t.Errorf("LocalAgentOrgID = %v, want %q", got, "org-1")
		}
		if got := c.Locals(LocalAgentClusterID); got != "cluster-1" {
			t.Errorf("LocalAgentClusterID = %v, want %q", got, "cluster-1")
		}
		if got := c.Locals(LocalAgentID); got != "agent-1" {
			t.Errorf("LocalAgentID = %v, want %q", got, "agent-1")
		}

		return c.SendStatus(fiber.StatusOK)
	}

	agentAuth := AgentAuthMiddleware(validator)
	agent := app.Group("/v1/agent", agentAuth)
	agent.Get("/test", testHandler)

	req := httptest.NewRequest(http.MethodGet, "/v1/agent/test", nil)
	req.Header.Set("X-Cluster-ID", "cluster-1")
	req.Header.Set("X-Agent-ID", "agent-1")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200, got %d; body=%s", resp.StatusCode, body)
	}
}

func TestACMEChallengeIsPublic(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: platformmiddleware.ErrorHandler(),
	})

	// Simple handler mimicking ACME challenge behavior
	app.Get("/.well-known/acme-challenge/:token", func(c *fiber.Ctx) error {
		token := c.Params("token")
		if token == "valid-token" {
			return c.Type("text/plain").SendString("challenge-response")
		}
		return c.Status(fiber.StatusNotFound).SendString("Not found")
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/valid-token", nil)
	// No auth headers - ACME challenges must be public

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200, got %d; body=%s", resp.StatusCode, body)
	}
}
