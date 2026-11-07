package router

// TestGatewayCoverageDeploymentRoutes is the primary structural guard against
// the recurring pattern of "backend route exists, gateway never proxies it."
//
// Each sub-test makes a real HTTP request (via fiber.App.Test) to a fully
// configured gateway Fiber app. All backend services point to a single stub
// server that returns 200 OK for every request. A 404 from app.Test means the
// route was never registered in the gateway — that is the bug this test catches.
//
// # How to read a failure
//
//	FAIL: missing gateway proxy: POST /v1/organizations/org-1/applications/app-1/environments
//	→ the route exists in the backend service (services/deployment/internal/routes.go)
//	  but has no matching call in registerDeploymentRoutes in router.go.
//
// # How to fix a failure
//
//  1. Open backend/services/gateway/internal/router/router.go.
//  2. Find the register*Routes function for the affected service.
//  3. Add a proxy rule that mirrors the service route, e.g.
//     appEnvs.Post("", svc.Handler())
//
// # Convention
//
// Any new route added to a backend service's routes.go MUST have a corresponding
// proxy registration added in the same PR (see CONTRIBUTING.md §Gateway coverage).
// This test is the automated enforcement of that convention.

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/services/gateway/internal/auth"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/proxy"
)

// allServicesConfig returns a Config with every service pointing at stubURL.
func allServicesConfig(stubURL string) Config {
	svc := func(name string) proxy.ServiceConfig {
		return proxy.ServiceConfig{Name: name, BaseURL: stubURL}
	}
	return Config{
		AuthService:          svc("auth"),
		TenantService:        svc("tenant"),
		ProjectService:       svc("project"),
		AuditService:         svc("audit"),
		ClusterService:       svc("cluster"),
		DeploymentService:    svc("deployment"),
		SecretsService:       svc("secrets"),
		DomainService:        svc("domain"),
		NotificationService:  svc("notification"),
		ProvisioningService:  svc("provisioning"),
		BuildService:         svc("build"),
		ObservabilityService: svc("observability"),
	}
}

// gatewayApp builds a fully-configured Fiber test app with every backend service
// proxied to stubURL.
func gatewayApp(t *testing.T, stubURL string) *fiber.App {
	t.Helper()
	router, err := New(
		auth.NewValidator(testAuthConfig()),
		nil,  // no revocation checker
		nil,  // no rate limiter
		allServicesConfig(stubURL),
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("create router: %v", err)
	}
	app := fiber.New()
	router.Register(app)
	return app
}

// assertProxied sends METHOD path to app (with a signed user JWT) and fails the
// test if the gateway returns 404. Any other status code means the route was
// registered and the request reached (or was rejected by) the backend.
func assertProxied(t *testing.T, app *fiber.App, token, method, path string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: app.Test: %v", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf(
			"missing gateway proxy rule: %s %s\n"+
				"\tThis route is registered in the backend service but has no proxy rule in router.go.\n"+
				"\tFix: add the route to the appropriate register*Routes function.",
			method, path,
		)
	}
}

// assertPublicProxied is like assertProxied but omits the Authorization header.
// Used for public endpoints (GitHub OAuth callback, webhooks, ACME challenges).
func assertPublicProxied(t *testing.T, app *fiber.App, method, path string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: app.Test: %v", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf(
			"missing gateway proxy rule (public): %s %s\n"+
				"\tThis public route has no proxy rule in router.go.",
			method, path,
		)
	}
}

// TestGatewayCoverageDeploymentRoutes exhaustively verifies every route registered
// in backend/services/deployment/internal/routes.go has a gateway proxy rule.
//
// This is where the four regression incidents occurred on Oct 19 2026:
// environments and promotions routes were added to the service but
// registerDeploymentRoutes was never updated.
func TestGatewayCoverageDeploymentRoutes(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	app := gatewayApp(t, stub.URL)
	token := signUserToken("user-1", "test@example.com")
	check := func(method, path string) {
		t.Helper()
		assertProxied(t, app, token, method, path)
	}

	const (
		org    = "org-1"
		proj   = "proj-1"
		appID  = "app-1"
		dep    = "dep-1"
		rel    = "rel-1"
		env    = "env-1"
		promo  = "promo-1"
		clu    = "clu-1"
	)
	base := fmt.Sprintf("/v1/organizations/%s", org)

	t.Run("applications", func(t *testing.T) {
		// POST + GET scoped to project.
		check("POST", fmt.Sprintf("%s/projects/%s/applications", base, proj))
		check("GET", fmt.Sprintf("%s/projects/%s/applications", base, proj))
		// Single-app operations (org-scoped).
		check("GET", fmt.Sprintf("%s/applications/%s", base, appID))
		check("PATCH", fmt.Sprintf("%s/applications/%s", base, appID))
		check("DELETE", fmt.Sprintf("%s/applications/%s", base, appID))
		// Deployments for an application.
		check("GET", fmt.Sprintf("%s/applications/%s/deployments", base, appID))
	})

	t.Run("deployments", func(t *testing.T) {
		check("GET", fmt.Sprintf("%s/deployments", base))
		check("POST", fmt.Sprintf("%s/deployments", base))
		check("GET", fmt.Sprintf("%s/deployments/%s", base, dep))
		check("PATCH", fmt.Sprintf("%s/deployments/%s", base, dep))
		check("DELETE", fmt.Sprintf("%s/deployments/%s", base, dep))
		check("POST", fmt.Sprintf("%s/deployments/%s/rollback", base, dep))
		// Cluster-scoped legacy endpoint.
		check("GET", fmt.Sprintf("%s/clusters/%s/deployments", base, clu))
	})

	t.Run("releases", func(t *testing.T) {
		check("GET", fmt.Sprintf("%s/deployments/%s/releases", base, dep))
		check("GET", fmt.Sprintf("%s/deployments/%s/releases/%s", base, dep, rel))
		check("POST", fmt.Sprintf("%s/deployments/%s/releases/%s/status", base, dep, rel))
	})

	// ── The routes that were previously missing (regression guard) ──────────────
	// These 10 routes triggered the fourth "gateway never proxied it" incident.
	// Do NOT remove these sub-tests.

	t.Run("environments/app_scoped", func(t *testing.T) {
		check("POST", fmt.Sprintf("%s/applications/%s/environments", base, appID))
		check("GET", fmt.Sprintf("%s/applications/%s/environments", base, appID))
	})

	t.Run("environments/org_scoped", func(t *testing.T) {
		check("GET", fmt.Sprintf("%s/environments/%s", base, env))
		check("PATCH", fmt.Sprintf("%s/environments/%s", base, env))
		check("DELETE", fmt.Sprintf("%s/environments/%s", base, env))
	})

	t.Run("promotions/app_scoped", func(t *testing.T) {
		check("GET", fmt.Sprintf("%s/applications/%s/promotions", base, appID))
	})

	t.Run("promotions/org_scoped", func(t *testing.T) {
		check("POST", fmt.Sprintf("%s/promotions", base))
		check("GET", fmt.Sprintf("%s/promotions/%s", base, promo))
		check("POST", fmt.Sprintf("%s/promotions/%s/approve", base, promo))
		check("POST", fmt.Sprintf("%s/promotions/%s/reject", base, promo))
	})
}

// TestGatewayCoverageClusterRoutes spot-checks cluster service routes.
func TestGatewayCoverageClusterRoutes(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	app := gatewayApp(t, stub.URL)
	token := signUserToken("user-1", "test@example.com")
	check := func(method, path string) {
		t.Helper()
		assertProxied(t, app, token, method, path)
	}
	const org, clu = "org-1", "clu-1"
	base := fmt.Sprintf("/v1/organizations/%s", org)

	check("POST", fmt.Sprintf("%s/clusters", base))
	check("GET", fmt.Sprintf("%s/clusters", base))
	check("GET", fmt.Sprintf("%s/clusters/%s", base, clu))
	check("PATCH", fmt.Sprintf("%s/clusters/%s", base, clu))
	check("DELETE", fmt.Sprintf("%s/clusters/%s", base, clu))
	check("POST", fmt.Sprintf("%s/clusters/%s/tokens", base, clu))
	check("DELETE", fmt.Sprintf("%s/clusters/%s/tokens/tok-1", base, clu))
	check("GET", fmt.Sprintf("%s/clusters/%s/heartbeats", base, clu))
}

// TestGatewayCoverageBuildRoutes spot-checks build service routes, including
// the public GitHub OAuth callback and webhook endpoints.
func TestGatewayCoverageBuildRoutes(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	app := gatewayApp(t, stub.URL)
	token := signUserToken("user-1", "test@example.com")
	check := func(method, path string) {
		t.Helper()
		assertProxied(t, app, token, method, path)
	}
	checkPublic := func(method, path string) {
		t.Helper()
		assertPublicProxied(t, app, method, path)
	}

	const org, proj, repo, build = "org-1", "proj-1", "repo-1", "build-1"
	base := fmt.Sprintf("/v1/organizations/%s", org)

	// Public (no auth).
	checkPublic("GET", "/v1/github/oauth/callback?code=x&state=y")
	checkPublic("POST", fmt.Sprintf("/v1/webhooks/github/%s", repo))

	// Authenticated.
	check("POST", fmt.Sprintf("%s/projects/%s/repositories", base, proj))
	check("GET", fmt.Sprintf("%s/projects/%s/repositories", base, proj))
	check("GET", fmt.Sprintf("%s/repositories/%s", base, repo))
	check("POST", fmt.Sprintf("%s/builds", base))
	check("GET", fmt.Sprintf("%s/builds", base))
	check("GET", fmt.Sprintf("%s/builds/%s", base, build))
	check("POST", fmt.Sprintf("%s/builds/%s/cancel", base, build))
	check("GET", fmt.Sprintf("%s/github/oauth/authorize", base))
}

// TestGatewayCoverageSecretsRoutes spot-checks secrets service routes.
func TestGatewayCoverageSecretsRoutes(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	app := gatewayApp(t, stub.URL)
	token := signUserToken("user-1", "test@example.com")
	check := func(method, path string) {
		t.Helper()
		assertProxied(t, app, token, method, path)
	}

	const org, proj, secret = "org-1", "proj-1", "sec-1"
	base := fmt.Sprintf("/v1/organizations/%s", org)

	check("POST", fmt.Sprintf("%s/projects/%s/secrets", base, proj))
	check("GET", fmt.Sprintf("%s/projects/%s/secrets", base, proj))
	check("GET", fmt.Sprintf("%s/projects/%s/secrets/%s", base, proj, secret))
	check("PATCH", fmt.Sprintf("%s/projects/%s/secrets/%s", base, proj, secret))
	check("DELETE", fmt.Sprintf("%s/projects/%s/secrets/%s", base, proj, secret))
}

// TestGatewayCoverageObservabilityRoutes spot-checks observability service routes.
func TestGatewayCoverageObservabilityRoutes(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	app := gatewayApp(t, stub.URL)
	token := signUserToken("user-1", "test@example.com")
	check := func(method, path string) {
		t.Helper()
		assertProxied(t, app, token, method, path)
	}

	const org = "org-1"
	base := fmt.Sprintf("/v1/organizations/%s", org)

	check("GET", fmt.Sprintf("%s/metrics/query", base))
	check("GET", fmt.Sprintf("%s/logs/query", base))
	check("POST", fmt.Sprintf("%s/dashboards", base))
	check("GET", fmt.Sprintf("%s/dashboards", base))
	check("GET", fmt.Sprintf("%s/health/summary", base))
	check("GET", fmt.Sprintf("%s/alerts/rules", base))
	check("GET", fmt.Sprintf("%s/observability/overview", base))
}

// TestGatewayCoverageProvisioningRoutes spot-checks provisioning service routes.
func TestGatewayCoverageProvisioningRoutes(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	app := gatewayApp(t, stub.URL)
	token := signUserToken("user-1", "test@example.com")
	check := func(method, path string) {
		t.Helper()
		assertProxied(t, app, token, method, path)
	}

	const org, req = "org-1", "req-1"
	base := fmt.Sprintf("/v1/organizations/%s", org)

	check("POST", fmt.Sprintf("%s/credentials", base))
	check("GET", fmt.Sprintf("%s/credentials", base))
	check("GET", fmt.Sprintf("%s/templates", base))
	check("POST", fmt.Sprintf("%s/provisioning", base))
	check("GET", fmt.Sprintf("%s/provisioning", base))
	check("GET", fmt.Sprintf("%s/provisioning/%s", base, req))
	check("POST", fmt.Sprintf("%s/provisioning/%s/start", base, req))
	check("GET", fmt.Sprintf("%s/providers/aws/regions", base))
}

// TestGatewayCoverageNotificationRoutes spot-checks notification service routes.
func TestGatewayCoverageNotificationRoutes(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	app := gatewayApp(t, stub.URL)
	token := signUserToken("user-1", "test@example.com")
	check := func(method, path string) {
		t.Helper()
		assertProxied(t, app, token, method, path)
	}

	const org, ch = "org-1", "ch-1"
	base := fmt.Sprintf("/v1/organizations/%s", org)

	check("POST", fmt.Sprintf("%s/channels", base))
	check("GET", fmt.Sprintf("%s/channels", base))
	check("GET", fmt.Sprintf("%s/channels/%s", base, ch))
	check("PATCH", fmt.Sprintf("%s/channels/%s", base, ch))
	check("DELETE", fmt.Sprintf("%s/channels/%s", base, ch))
	check("POST", fmt.Sprintf("%s/channels/%s/test", base, ch))
	check("GET", fmt.Sprintf("%s/notifications", base))
	check("GET", fmt.Sprintf("%s/dlq", base))
}

// TestGatewayCoverageDomainRoutes spot-checks domain service routes.
func TestGatewayCoverageDomainRoutes(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	app := gatewayApp(t, stub.URL)
	token := signUserToken("user-1", "test@example.com")
	check := func(method, path string) {
		t.Helper()
		assertProxied(t, app, token, method, path)
	}
	checkPublic := func(method, path string) {
		t.Helper()
		assertPublicProxied(t, app, method, path)
	}

	const org, dom = "org-1", "dom-1"
	base := fmt.Sprintf("/v1/organizations/%s", org)

	check("POST", fmt.Sprintf("%s/domains", base))
	check("GET", fmt.Sprintf("%s/domains", base))
	check("GET", fmt.Sprintf("%s/domains/%s", base, dom))
	check("POST", fmt.Sprintf("%s/domains/%s/verify", base, dom))
	check("POST", fmt.Sprintf("%s/domains/%s/certificate", base, dom))
	check("POST", fmt.Sprintf("%s/domains/%s/ingress", base, dom))
	checkPublic("GET", "/v1/.well-known/acme-challenge/some-token")
}
