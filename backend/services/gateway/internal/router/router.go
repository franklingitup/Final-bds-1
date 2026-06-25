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

	// Future services.
	ClusterService    proxy.ServiceConfig
	DeploymentService proxy.ServiceConfig
	SecretsService    proxy.ServiceConfig
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
	}
}

// Router manages all gateway routes.
type Router struct {
	validator   *auth.Validator
	rateLimiter *middleware.RateLimiter
	services    map[string]*proxy.Service
	log         *slog.Logger
}

// New creates a new router with the given configuration.
func New(validator *auth.Validator, rateLimiter *middleware.RateLimiter, cfg Config, log *slog.Logger) (*Router, error) {
	r := &Router{
		validator:   validator,
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
	app.Use(r.rateLimiter.Middleware())

	// API version group.
	v1 := app.Group("/v1")

	// Auth routes (mostly public).
	r.registerAuthRoutes(v1)

	// Agent registration route (public, capability-based).
	r.registerAgentRoutes(v1)

	// Authenticated routes.
	authenticated := v1.Group("", middleware.Authentication(r.validator))

	// Tenant routes.
	r.registerTenantRoutes(authenticated)

	// Organization-scoped routes.
	orgs := authenticated.Group("/organizations/:orgId", middleware.OrgScope())

	// Project routes.
	r.registerProjectRoutes(orgs)

	// Audit routes.
	r.registerAuditRoutes(orgs)

	// Cluster, Deployment, Secrets routes.
	r.registerClusterRoutes(orgs)
	r.registerDeploymentRoutes(orgs)
	r.registerSecretsRoutes(orgs)
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
	auth.Post("/password-reset", svc.Handler())
	auth.Post("/password-reset/confirm", svc.Handler())

	// Authenticated auth endpoints.
	authProtected := auth.Group("", middleware.Authentication(r.validator))
	authProtected.Post("/logout", svc.Handler())
	authProtected.Get("/me", svc.Handler())
	authProtected.Post("/mfa/setup", svc.Handler())
	authProtected.Post("/mfa/enable", svc.Handler())
	authProtected.Post("/mfa/disable", svc.Handler())

	// Service account management (org-scoped).
	serviceAccounts := g.Group("/organizations/:orgId/service-accounts",
		middleware.Authentication(r.validator),
		middleware.OrgScope(),
	)
	serviceAccounts.Post("", svc.Handler())
	serviceAccounts.Get("", svc.Handler())
	serviceAccounts.Get("/:accountId", svc.Handler())
	serviceAccounts.Delete("/:accountId", svc.Handler())

	// API tokens (org-scoped).
	apiTokens := g.Group("/organizations/:orgId/api-tokens",
		middleware.Authentication(r.validator),
		middleware.OrgScope(),
	)
	apiTokens.Post("", svc.Handler())
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

// Services returns the map of registered backend services.
func (r *Router) Services() map[string]*proxy.Service {
	return r.services
}
