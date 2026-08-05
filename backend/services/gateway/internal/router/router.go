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

	// Authenticated routes.
	authenticated := v1.Group("", middleware.Authentication(r.validator, r.revoker))

	// Tenant routes.
	r.registerTenantRoutes(authenticated)

	// Organization-scoped routes.
	orgs := authenticated.Group("/organizations/:orgId", middleware.OrgScope())

	// Project routes.
	r.registerProjectRoutes(orgs)

	// Audit routes.
	r.registerAuditRoutes(orgs)

	// Cluster, Deployment, Secrets, Domain, Notification, Provisioning routes.
	r.registerClusterRoutes(orgs)
	r.registerDeploymentRoutes(orgs)
	r.registerSecretsRoutes(orgs)
	r.registerDomainRoutes(v1, orgs)
	r.registerNotificationRoutes(orgs)
	r.registerProvisioningRoutes(v1, orgs)
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
}

// registerTenantRoutes mounts tenant service routes.
func (r *Router) registerTenantRoutes(g fiber.Router) {
	svc, ok := r.services["tenant"]
	if !ok {
		return
	}

	// Organization management.
	orgs := g.Group("/organizations")
	orgs.Get("", svc.Handler())                             // API-CRIT-01: List user's orgs
	orgs.Post("", svc.Handler())
	orgs.Get("/by-slug/:slug", svc.Handler())               // API-CRIT-02: Get by slug
	orgs.Get("/:orgId", middleware.OrgScope(), svc.Handler())
	orgs.Patch("/:orgId", middleware.OrgScope(), svc.Handler())
	orgs.Delete("/:orgId", middleware.OrgScope(), svc.Handler())

	// Member management.
	members := g.Group("/organizations/:orgId/members", middleware.OrgScope())
	members.Get("", svc.Handler())
	members.Patch("/:userId", svc.Handler())
	members.Delete("/:userId", svc.Handler())

	// Invitation management.
	invitations := g.Group("/organizations/:orgId/invitations", middleware.OrgScope())
	invitations.Post("", svc.Handler())
	invitations.Get("", svc.Handler())
	invitations.Delete("/:id", svc.Handler())

	// Accept invitation (cross-tenant, token-based).
	g.Post("/invitations/accept", svc.Handler())
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

	// Deployments at org level.
	deployments := orgs.Group("/deployments")
	deployments.Get("", svc.Handler())     // API-CRIT-03: List org deployments
	deployments.Post("", svc.Handler())

	// Single deployment operations.
	dep := deployments.Group("/:deploymentId")
	dep.Get("", svc.Handler())
	dep.Patch("", svc.Handler())
	dep.Delete("", svc.Handler())          // API-CRIT-04: Delete deployment
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

	// Public bootstrap endpoints (token-based auth).
	bootstrap := v1.Group("/bootstrap")
	bootstrap.Get("/:token/manifest.yaml", svc.Handler())
	bootstrap.Post("/:token/agent", svc.Handler())

	// Session step updates (token-based auth).
	sessions := v1.Group("/sessions")
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

// Services returns the map of registered backend services.
func (r *Router) Services() map[string]*proxy.Service {
	return r.services
}
