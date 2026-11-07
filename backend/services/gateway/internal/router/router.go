// Package router defines the API Gateway routes and mounts backend services.
package router

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/services/gateway/internal/auth"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/middleware"
	"github.com/bdsplatform/platform/backend/services/gateway/internal/proxy"
)

// Config holds configuration for all backend services.
type Config struct {
	AuthService    proxy.ServiceConfig
	TenantService  proxy.ServiceConfig
	ProjectService proxy.ServiceConfig
	AuditService   proxy.ServiceConfig

	// Infrastructure services.
	ClusterService       proxy.ServiceConfig
	DeploymentService    proxy.ServiceConfig
	SecretsService       proxy.ServiceConfig
	DomainService        proxy.ServiceConfig
	NotificationService  proxy.ServiceConfig
	ProvisioningService  proxy.ServiceConfig
	BuildService         proxy.ServiceConfig
	ObservabilityService proxy.ServiceConfig
}

// DefaultConfig returns configuration for local development.
func DefaultConfig() Config {
	return Config{
		AuthService: proxy.ServiceConfig{
			Name:    "auth",
			BaseURL: "http://localhost:8081",
		},
		TenantService: proxy.ServiceConfig{
			Name:    "tenant",
			BaseURL: "http://localhost:8082",
		},
		ProjectService: proxy.ServiceConfig{
			Name:    "project",
			BaseURL: "http://localhost:8083",
		},
		AuditService: proxy.ServiceConfig{
			Name:    "audit",
			BaseURL: "http://localhost:8084",
		},
		ClusterService: proxy.ServiceConfig{
			Name:    "cluster",
			BaseURL: "http://localhost:8085",
		},
		DeploymentService: proxy.ServiceConfig{
			Name:    "deployment",
			BaseURL: "http://localhost:8086",
		},
		SecretsService: proxy.ServiceConfig{
			Name:    "secrets",
			BaseURL: "http://localhost:8087",
		},
		DomainService: proxy.ServiceConfig{
			Name:    "domain",
			BaseURL: "http://localhost:8088",
		},
		NotificationService: proxy.ServiceConfig{
			Name:    "notification",
			BaseURL: "http://localhost:8089",
		},
		ProvisioningService: proxy.ServiceConfig{
			Name:    "provisioning",
			BaseURL: "http://localhost:8090",
		},
		BuildService: proxy.ServiceConfig{
			Name:    "build",
			BaseURL: "http://localhost:8091",
		},
		ObservabilityService: proxy.ServiceConfig{
			Name:    "observability",
			BaseURL: "http://localhost:8092",
		},
	}
}

// Router manages all gateway routes.
type Router struct {
	validator   *auth.Validator
	revoker     *middleware.RevocationChecker
	rateLimiter middleware.Limiter
	services    map[string]*proxy.Service
	log         *slog.Logger
}

// New creates a new router with the given configuration. The revoker may be nil
// to disable token-revocation checks (signature-only auth). The rateLimiter may
// be any middleware.Limiter (in-memory or Redis-backed) or nil to disable rate
// limiting.
func New(validator *auth.Validator, revoker *middleware.RevocationChecker, rateLimiter middleware.Limiter, cfg Config, log *slog.Logger) (*Router, error) {
	r := &Router{
		validator:   validator,
		revoker:     revoker,
		rateLimiter: rateLimiter,
		services:    make(map[string]*proxy.Service),
		log:         log,
	}

	// Initialize enabled services.
	serviceConfigs := []proxy.ServiceConfig{
		cfg.AuthService,
		cfg.TenantService,
		cfg.ProjectService,
		cfg.AuditService,
		cfg.ClusterService,
		cfg.DeploymentService,
		cfg.SecretsService,
		cfg.DomainService,
		cfg.NotificationService,
		cfg.ProvisioningService,
		cfg.BuildService,
		cfg.ObservabilityService,
	}

	for _, svcCfg := range serviceConfigs {
		if svcCfg.BaseURL == "" {
			continue
		}
		svc, err := proxy.NewService(svcCfg, log)
		if err != nil {
			return nil, err
		}
		r.services[svcCfg.Name] = svc
		log.Info("registered backend service", slog.String("service", svcCfg.Name), slog.String("url", svcCfg.BaseURL))
	}

	return r, nil
}

// Register mounts all routes onto the Fiber app.
//
// Adding a new backend service route?
//   1. Register it in the service's routes.go (e.g. services/deployment/internal/routes.go).
//   2. Add a proxy rule in the appropriate register*Routes function below IN THE SAME PR.
//   3. Add the concrete URL to the corresponding TestGatewayCoverage* test in coverage_test.go.
//   4. Run: go run ./tools/check-gateway-coverage (from backend/) to verify counts match.
//
// Skipping step 2 causes a silent 404 for every request to that route — this has
// happened four times.  The convention and tests above are the automated guard.
func (r *Router) Register(app *fiber.App) {
	// Global middleware.
	app.Use(middleware.RequestID())
	if r.rateLimiter != nil {
		app.Use(r.rateLimiter.Middleware())
	}

	// API version group.
	v1 := app.Group("/v1")

	// Auth routes (mostly public).
	r.registerAuthRoutes(v1)

	// Agent registration route (public, capability-based).
	r.registerAgentRoutes(v1)

	// Tenant routes apply authentication to their concrete path groups. Do not
	// mount authentication on v1.Group(""), because Fiber treats that as a
	// blanket /v1 middleware and it intercepts capability-based routes such as
	// /v1/agent-install/:token/manifest.yaml.
	r.registerTenantRoutes(v1)

	// Organization-scoped routes.
	orgs := v1.Group(
		"/organizations/:orgId",
		middleware.Authentication(r.validator, r.revoker),
		middleware.OrgScope(),
	)

	// Project routes.
	r.registerProjectRoutes(orgs)

	// Audit routes.
	r.registerAuditRoutes(orgs)

	// Cluster, Deployment, Secrets, Domain, Notification, Provisioning, Build, Observability routes.
	r.registerClusterRoutes(orgs)
	r.registerDeploymentRoutes(orgs)
	r.registerSecretsRoutes(orgs)
	r.registerDomainRoutes(v1, orgs)
	r.registerNotificationRoutes(orgs)
	r.registerProvisioningRoutes(v1, orgs)
	r.registerBuildRoutes(v1, orgs)
	r.registerObservabilityRoutes(v1, orgs)
}

// registerAuthRoutes mounts auth service routes.
func (r *Router) registerAuthRoutes(g fiber.Router) {
	svc, ok := r.services["auth"]
	if !ok {
		return
	}

	// Public auth endpoints.
	auth := g.Group("/auth")
	auth.Post("/signup", svc.Handler())
	auth.Post("/login", svc.Handler())
	auth.Get("/sso/:orgSlug/login", svc.Handler())
	auth.Post("/sso/:orgId/acs", svc.Handler())
	auth.Get("/sso/:orgId/metadata", svc.Handler())
	auth.Post("/sso/exchange", svc.Handler())
	auth.Post("/refresh", svc.Handler())
	auth.Post("/verify-email", svc.Handler())
	auth.Post("/resend-verification", svc.Handler())
	// Backend route is /password-reset/request (auth/internal/routes.go).
	auth.Post("/password-reset/request", svc.Handler())
	auth.Post("/password-reset/confirm", svc.Handler())

	// Authenticated auth endpoints.
	authProtected := auth.Group("", middleware.Authentication(r.validator, r.revoker))
	authProtected.Post("/logout", svc.Handler())
	authProtected.Get("/me", svc.Handler())
	authProtected.Post("/mfa/setup", svc.Handler())
	authProtected.Post("/mfa/enable", svc.Handler())
	authProtected.Post("/mfa/disable", svc.Handler())

	// Service account management (org-scoped).
	serviceAccounts := g.Group("/organizations/:orgId/service-accounts",
		middleware.Authentication(r.validator, r.revoker),
		middleware.OrgScope(),
	)
	serviceAccounts.Post("", svc.Handler())
	serviceAccounts.Get("", svc.Handler())
	serviceAccounts.Delete("/:accountId", svc.Handler())
	// API token creation is nested under a service account to match the auth
	// backend route POST /v1/organizations/:orgId/service-accounts/:id/tokens.
	serviceAccounts.Post("/:accountId/tokens", svc.Handler())

	// API tokens (org-scoped): list and revoke. Creation lives under the owning
	// service account above; the backend has no flat POST /api-tokens route.
	apiTokens := g.Group("/organizations/:orgId/api-tokens",
		middleware.Authentication(r.validator, r.revoker),
		middleware.OrgScope(),
	)
	apiTokens.Get("", svc.Handler())
	apiTokens.Delete("/:tokenId", svc.Handler())

	// SSO configuration (org-scoped). Allows org admins to configure SAML/OIDC
	// identity providers. Maps to PUT/GET/DELETE /v1/organizations/:orgId/sso/config
	// in the auth service.
	ssoConfig := g.Group("/organizations/:orgId/sso",
		middleware.Authentication(r.validator, r.revoker),
		middleware.OrgScope(),
	)
	ssoConfig.Put("/config", svc.Handler())
	ssoConfig.Get("/config", svc.Handler())
	ssoConfig.Delete("/config", svc.Handler())
}

// registerTenantRoutes mounts tenant service routes.
func (r *Router) registerTenantRoutes(g fiber.Router) {
	svc, ok := r.services["tenant"]
	if !ok {
		return
	}

	requireAuth := middleware.Authentication(r.validator, r.revoker)

	// Organization management.
	orgs := g.Group("/organizations", requireAuth)
	orgs.Get("", svc.Handler()) // API-CRIT-01: List user's orgs
	orgs.Post("", svc.Handler())
	orgs.Get("/by-slug/:slug", svc.Handler()) // API-CRIT-02: Get by slug
	orgs.Get("/:orgId", middleware.OrgScope(), svc.Handler())
	orgs.Patch("/:orgId", middleware.OrgScope(), svc.Handler())
	orgs.Delete("/:orgId", middleware.OrgScope(), svc.Handler())

	// Member management.
	members := g.Group(
		"/organizations/:orgId/members",
		requireAuth,
		middleware.OrgScope(),
	)
	members.Get("", svc.Handler())
	members.Patch("/:userId", svc.Handler())
	members.Delete("/:userId", svc.Handler())

	// Invitation management.
	invitations := g.Group(
		"/organizations/:orgId/invitations",
		requireAuth,
		middleware.OrgScope(),
	)
	invitations.Post("", svc.Handler())
	invitations.Get("", svc.Handler())
	invitations.Delete("/:id", svc.Handler())

	// Accept invitation (cross-tenant, token-based).
	g.Post("/invitations/accept", requireAuth, svc.Handler())
}

// registerProjectRoutes mounts project service routes.
func (r *Router) registerProjectRoutes(orgs fiber.Router) {
	svc, ok := r.services["project"]
	if !ok {
		return
	}

	// Project management.
	projects := orgs.Group("/projects")
	projects.Post("", svc.Handler())
	projects.Get("", svc.Handler())
	// Lookup by slug must be registered before the :projectId param route.
	projects.Get("/by-slug/:slug", svc.Handler())
	projects.Get("/:projectId", middleware.ProjectScope(), svc.Handler())
	projects.Patch("/:projectId", middleware.ProjectScope(), svc.Handler())
	projects.Delete("/:projectId", middleware.ProjectScope(), svc.Handler())

	// Project member management.
	projectMembers := projects.Group("/:projectId/members", middleware.ProjectScope())
	projectMembers.Post("", svc.Handler())
	projectMembers.Get("", svc.Handler())
	projectMembers.Patch("/:userId", svc.Handler())
	projectMembers.Delete("/:userId", svc.Handler())
}

// registerAuditRoutes mounts audit service routes.
func (r *Router) registerAuditRoutes(orgs fiber.Router) {
	svc, ok := r.services["audit"]
	if !ok {
		return
	}

	auditLogs := orgs.Group("/audit-logs")
	auditLogs.Get("", svc.Handler())
	auditLogs.Get("/:eventId", svc.Handler())
}

// registerAgentRoutes mounts public agent routes.
// These are capability/credential-based endpoints that don't require user authentication.
func (r *Router) registerAgentRoutes(v1 fiber.Router) {
	// Agent registration (cluster service, capability-based via registration token).
	if svc, ok := r.services["cluster"]; ok {
		// Existing-cluster install manifest (registration-token capability).
		v1.Get("/agent-install/:token/manifest.yaml", svc.Handler())
		v1.Post("/agent/register", svc.Handler())
		// Agent recovery (cluster service, capability-based via installation token
		// header). Lets an agent rebuild lost local state without re-issuing a token.
		v1.Get("/agent/recover", svc.Handler())
		// Agent heartbeat (cluster service, credential-based via X-Cluster-ID/X-Agent-ID).
		// This endpoint allows agents to send heartbeats without user JWT.
		v1.Post("/agent/clusters/:clusterId/heartbeat", svc.Handler())
	}

	// Agent deployment routes (deployment service, credential-based via X-Cluster-ID/X-Agent-ID).
	// These endpoints allow registered agents to fetch desired state and report deployment status.
	if svc, ok := r.services["deployment"]; ok {
		agent := v1.Group("/agent")
		// Desired state endpoint for deployment reconciliation.
		agent.Get("/clusters/:clusterId/desired-state", svc.Handler())
		// Status reporting endpoint for deployment updates.
		agent.Post("/deployments/:deploymentId/releases/:releaseId/status", svc.Handler())
		// Rich rollout progress snapshots.
		agent.Post("/deployments/:deploymentId/releases/:releaseId/progress", svc.Handler())
	}

	// Agent secrets routes (secrets service, credential-based via X-Cluster-ID/X-Agent-ID).
	// This endpoint returns decrypted secrets for deployments on a cluster.
	if svc, ok := r.services["secrets"]; ok {
		v1.Get("/agent/clusters/:clusterId/secrets", svc.Handler())
	}
}

// registerClusterRoutes mounts cluster service routes.
func (r *Router) registerClusterRoutes(orgs fiber.Router) {
	svc, ok := r.services["cluster"]
	if !ok {
		return
	}

	// Clusters CRUD.
	clusters := orgs.Group("/clusters")
	clusters.Post("", svc.Handler())
	clusters.Get("", svc.Handler())
	clusters.Get("/:clusterId", svc.Handler())
	clusters.Patch("/:clusterId", svc.Handler())
	clusters.Delete("/:clusterId", svc.Handler())

	// Registration tokens.
	clusters.Post("/:clusterId/tokens", svc.Handler())
	clusters.Delete("/:clusterId/tokens/:tokenId", svc.Handler())

	// Heartbeat history (read-only for users).
	// NOTE: User-facing heartbeat POST removed (SEC-CRIT-03).
	// Agents use /v1/agent/clusters/:clusterId/heartbeat instead.
	clusters.Get("/:clusterId/heartbeats", svc.Handler())
}

// registerDeploymentRoutes mounts deployment service routes.
//
// CONVENTION: every route registered in services/deployment/internal/routes.go
// MUST have a corresponding proxy call here in the same PR.  The automated
// check is TestGatewayCoverageDeploymentRoutes in coverage_test.go and the
// source-level count tool in backend/tools/check-gateway-coverage/main.go.
// Forgetting to update this function is the single most common gateway bug.
func (r *Router) registerDeploymentRoutes(orgs fiber.Router) {
	svc, ok := r.services["deployment"]
	if !ok {
		return
	}

	// Applications within a project.
	apps := orgs.Group("/projects/:projectId/applications", middleware.ProjectScope())
	apps.Post("", svc.Handler())
	apps.Get("", svc.Handler())

	// Single application operations (org-scoped).
	app := orgs.Group("/applications/:appId")
	app.Get("", svc.Handler())
	app.Patch("", svc.Handler())
	app.Delete("", svc.Handler())
	app.Get("/deployments", svc.Handler())

	// Environments (scoped to an application — mirrors deployment service routes.go).
	appEnvs := app.Group("/environments")
	appEnvs.Post("", svc.Handler())
	appEnvs.Get("", svc.Handler())

	// Single environment operations (org-scoped).
	singleEnv := orgs.Group("/environments/:environmentId")
	singleEnv.Get("", svc.Handler())
	singleEnv.Patch("", svc.Handler())
	singleEnv.Delete("", svc.Handler())

	// Promotions (list at application scope; create/get/approve/reject at org scope).
	app.Get("/promotions", svc.Handler())

	promos := orgs.Group("/promotions")
	promos.Post("", svc.Handler())
	promos.Get("/:promotionId", svc.Handler())
	promos.Post("/:promotionId/approve", svc.Handler())
	promos.Post("/:promotionId/reject", svc.Handler())

	// Deployments at org level.
	deployments := orgs.Group("/deployments")
	deployments.Get("", svc.Handler()) // API-CRIT-03: List org deployments
	deployments.Post("", svc.Handler())

	// Single deployment operations.
	dep := deployments.Group("/:deploymentId")
	dep.Get("", svc.Handler())
	dep.Patch("", svc.Handler())
	dep.Delete("", svc.Handler()) // API-CRIT-04: Delete deployment
	dep.Post("/rollback", svc.Handler())

	// Releases for a deployment.
	releases := dep.Group("/releases")
	releases.Get("", svc.Handler())
	releases.Get("/:releaseId", svc.Handler())
	releases.Post("/:releaseId/status", svc.Handler())

	// Cluster-scoped deployment list (legacy endpoint, kept for backward compatibility).
	// Agents should use /v1/agent/clusters/:clusterId/desired-state instead.
	orgs.Get("/clusters/:clusterId/deployments", svc.Handler())
}

// registerSecretsRoutes mounts secrets service routes (future).
func (r *Router) registerSecretsRoutes(orgs fiber.Router) {
	svc, ok := r.services["secrets"]
	if !ok {
		return
	}

	// Secrets within a project.
	secrets := orgs.Group("/projects/:projectId/secrets", middleware.ProjectScope())
	secrets.Post("", svc.Handler())
	secrets.Get("", svc.Handler())
	secrets.Get("/:secretId", svc.Handler())
	secrets.Patch("/:secretId", svc.Handler())
	secrets.Delete("/:secretId", svc.Handler())
}

// registerDomainRoutes mounts domain service routes.
func (r *Router) registerDomainRoutes(v1, orgs fiber.Router) {
	svc, ok := r.services["domain"]
	if !ok {
		return
	}

	// Domain management within an organization.
	domains := orgs.Group("/domains")
	domains.Post("", svc.Handler())
	domains.Get("", svc.Handler())
	domains.Get("/:domainId", svc.Handler())
	domains.Patch("/:domainId", svc.Handler())
	domains.Delete("/:domainId", svc.Handler())

	// Domain verification.
	domains.Post("/:domainId/verify", svc.Handler())

	// TLS certificates.
	domains.Post("/:domainId/certificate", svc.Handler())
	domains.Get("/:domainId/certificate", svc.Handler())

	// Ingress management.
	domains.Post("/:domainId/ingress", svc.Handler())
	domains.Get("/:domainId/ingress", svc.Handler())

	// Domain events.
	domains.Get("/:domainId/events", svc.Handler())

	// Domains by deployment.
	orgs.Get("/deployments/:deploymentId/domains", svc.Handler())

	// Agent endpoints for ingress sync (no user auth, credential-based).
	agent := v1.Group("/agent")
	agent.Get("/clusters/:clusterId/ingresses", svc.Handler())
	agent.Post("/ingresses/:ingressId/sync", svc.Handler())

	// ACME HTTP-01 challenge (public, no auth - accessed by Let's Encrypt).
	v1.Get("/.well-known/acme-challenge/:token", svc.Handler())
}

// registerNotificationRoutes mounts notification service routes.
func (r *Router) registerNotificationRoutes(orgs fiber.Router) {
	svc, ok := r.services["notification"]
	if !ok {
		return
	}

	// Notification channels.
	channels := orgs.Group("/channels")
	channels.Post("", svc.Handler())
	channels.Get("", svc.Handler())
	channels.Get("/:channelId", svc.Handler())
	channels.Patch("/:channelId", svc.Handler())
	channels.Delete("/:channelId", svc.Handler())
	channels.Post("/:channelId/test", svc.Handler())

	// User preferences.
	prefs := orgs.Group("/preferences")
	prefs.Get("", svc.Handler())
	prefs.Put("", svc.Handler())

	// Notifications.
	notifs := orgs.Group("/notifications")
	notifs.Post("", svc.Handler())
	notifs.Get("", svc.Handler())
	notifs.Get("/:notificationId", svc.Handler())

	// Webhooks.
	webhooks := orgs.Group("/webhooks")
	webhooks.Post("", svc.Handler())
	webhooks.Get("", svc.Handler())
	webhooks.Delete("/:webhookId", svc.Handler())

	// Dead letter queue.
	dlq := orgs.Group("/dlq")
	dlq.Get("", svc.Handler())
	dlq.Post("/replay", svc.Handler())
	dlq.Post("/discard", svc.Handler())
}

// registerProvisioningRoutes mounts provisioning service routes.
func (r *Router) registerProvisioningRoutes(v1, orgs fiber.Router) {
	svc, ok := r.services["provisioning"]
	if !ok {
		return
	}

	// Public install script endpoint (session ID in URL, token in env).
	v1.Get("/install/:sessionId", svc.Handler())

	// NOTE: The old /v1/bootstrap/* endpoints are removed.
	// Agents now use the cluster service's /v1/agent-install/:token/manifest.yaml
	// endpoint for agent installation.

	// Session bundle and step updates (token-based auth).
	sessions := v1.Group("/sessions")
	sessions.Get("/:sessionToken/bundle", svc.Handler())
	sessions.Post("/:sessionToken/steps/:stepNumber", svc.Handler())

	// Cloud credentials.
	creds := orgs.Group("/credentials")
	creds.Post("", svc.Handler())
	creds.Get("", svc.Handler())
	creds.Post("/:credentialId/validate", svc.Handler())
	creds.Delete("/:credentialId", svc.Handler())

	// Cluster templates.
	templates := orgs.Group("/templates")
	templates.Get("", svc.Handler())

	// Provisioning requests.
	prov := orgs.Group("/provisioning")
	prov.Post("", svc.Handler())
	prov.Get("", svc.Handler())
	prov.Get("/:requestId", svc.Handler())
	prov.Post("/:requestId/terraform", svc.Handler())
	prov.Post("/:requestId/start", svc.Handler())
	prov.Get("/:requestId/events", svc.Handler())

	// Install sessions.
	installSessions := orgs.Group("/sessions")
	installSessions.Get("/:sessionId", svc.Handler())

	// Provider info.
	providers := orgs.Group("/providers")
	providers.Get("/:provider/regions", svc.Handler())
	providers.Get("/:provider/machine-types", svc.Handler())
	providers.Get("/:provider/kubernetes-versions", svc.Handler())
}

// registerBuildRoutes mounts build service routes.
func (r *Router) registerBuildRoutes(v1, orgs fiber.Router) {
	svc, ok := r.services["build"]
	if !ok {
		return
	}

	// Public routes (NO auth required - GitHub redirects/calls these).
	// These must NOT be wrapped in Authentication middleware.
	v1.Get("/github/oauth/callback", svc.Handler())
	v1.Post("/webhooks/github/:repositoryId", svc.Handler())

	// --- Authenticated routes (org-scoped) ---

	// Git Repositories (scoped to org/project).
	repos := orgs.Group("/projects/:projectId/repositories", middleware.ProjectScope())
	repos.Post("", svc.Handler())
	repos.Get("", svc.Handler())

	// Single repository operations.
	singleRepo := orgs.Group("/repositories/:repoId")
	singleRepo.Get("", svc.Handler())
	singleRepo.Patch("", svc.Handler())
	singleRepo.Delete("", svc.Handler())

	// Builds at org level.
	builds := orgs.Group("/builds")
	builds.Post("", svc.Handler())
	builds.Get("", svc.Handler())

	// Single build operations.
	build := builds.Group("/:buildId")
	build.Get("", svc.Handler())
	build.Post("/cancel", svc.Handler())
	build.Post("/retry", svc.Handler())
	build.Get("/logs", svc.Handler())
	build.Get("/artifact", svc.Handler())

	// --- GitHub integration routes (org-scoped) ---

	// OAuth initiation.
	orgs.Get("/github/oauth/authorize", svc.Handler())

	// Connections.
	connections := orgs.Group("/github/connections")
	connections.Post("", svc.Handler())
	connections.Get("", svc.Handler())

	// Single connection operations.
	singleConn := connections.Group("/:connectionId")
	singleConn.Get("", svc.Handler())
	singleConn.Delete("", svc.Handler())
	singleConn.Post("/validate", svc.Handler())
	singleConn.Get("/repositories", svc.Handler())

	// Connected GitHub repositories.
	ghRepos := orgs.Group("/github/repositories")
	ghRepos.Post("", svc.Handler())
	ghRepos.Get("", svc.Handler())

	// Single GitHub repository operations.
	singleGHRepo := ghRepos.Group("/:repositoryId")
	singleGHRepo.Get("", svc.Handler())
	singleGHRepo.Delete("", svc.Handler())
	singleGHRepo.Post("/sync", svc.Handler())
	singleGHRepo.Get("/branches", svc.Handler())
	singleGHRepo.Get("/commits", svc.Handler())
	singleGHRepo.Get("/default-branch", svc.Handler())
	singleGHRepo.Post("/webhook", svc.Handler())

	// Webhook operations.
	webhooks := orgs.Group("/github/webhooks/:webhookId")
	webhooks.Get("", svc.Handler())
	webhooks.Delete("", svc.Handler())
	webhooks.Get("/deliveries", svc.Handler())
}

// registerObservabilityRoutes mounts observability service routes.
func (r *Router) registerObservabilityRoutes(v1, orgs fiber.Router) {
	svc, ok := r.services["observability"]
	if !ok {
		return
	}

	// Metrics.
	metrics := orgs.Group("/metrics")
	metrics.Get("/query", svc.Handler())
	metrics.Post("/query", svc.Handler())
	metrics.Get("/query_instant", svc.Handler())
	metrics.Get("/:resourceType/:resourceId", svc.Handler())

	// Logs.
	logs := orgs.Group("/logs")
	logs.Get("/query", svc.Handler())
	logs.Post("/query", svc.Handler())
	logs.Get("/streams", svc.Handler())

	// Dashboards.
	dashboards := orgs.Group("/dashboards")
	dashboards.Post("", svc.Handler())
	dashboards.Get("", svc.Handler())
	dashboards.Get("/:dashboardId", svc.Handler())
	dashboards.Put("/:dashboardId", svc.Handler())
	dashboards.Delete("/:dashboardId", svc.Handler())

	// Health checks.
	health := orgs.Group("/health")
	health.Get("/summary", svc.Handler())
	health.Get("/:resourceType/:resourceId", svc.Handler())

	// Events.
	events := orgs.Group("/events")
	events.Get("", svc.Handler())

	// Alerts.
	alerts := orgs.Group("/alerts")
	alerts.Post("/rules", svc.Handler())
	alerts.Get("/rules", svc.Handler())
	alerts.Get("/firing", svc.Handler())

	// Observability overview.
	orgs.Get("/observability/overview", svc.Handler())

	// Agent ingest endpoints (credential-based, no user auth).
	agent := v1.Group("/agent")
	agent.Post("/metrics", svc.Handler())
	agent.Post("/logs", svc.Handler())
	agent.Post("/health", svc.Handler())
}

// Services returns the map of registered backend services.
func (r *Router) Services() map[string]*proxy.Service {
	return r.services
}
