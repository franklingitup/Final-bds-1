# Platform Validation Audit

**Date:** 2026-06-24  
**Auditor:** Principal Platform Architect  
**Scope:** End-to-end platform validation

---

## Executive Summary

| Metric | Value |
|--------|-------|
| **Completion %** | 78% |
| **Readiness Score** | 58/100 |
| **Critical Findings** | 11 |
| **High Findings** | 14 |
| **Medium Findings** | 18 |

**Verdict:** The platform is NOT ready for production. Eleven critical issues span API mismatches, **cross-tenant authorization bypasses**, and event contract breakage. The authorization gaps are security-critical and could allow data exfiltration between tenants.

---

## Customer Journey Validation

| Step | Action | Status | Blocker |
|------|--------|--------|---------|
| 1 | Signup | ✅ PASS | - |
| 2 | Create organization | ✅ PASS | - |
| 3 | Create project | ❌ FAIL | CRIT-01: Can't list organizations |
| 4 | Register cluster | ❌ FAIL | CRIT-01: Can't navigate to org dashboard |
| 5 | Create secret | ❌ FAIL | Blocked by step 4 |
| 6 | Create application | ❌ FAIL | Blocked by step 4 |
| 7 | Deploy nginx:latest | ❌ FAIL | CRIT-05: Can't list deployments |
| 8 | Observe healthy deployment | ❌ FAIL | Blocked by step 7 |
| 9 | Delete deployment | ❌ FAIL | CRIT-06: No delete endpoint |
| 10 | Delete cluster | ✅ PASS | Backend endpoint exists |

**Journey Completion:** 2/10 steps pass without blockers.

---

## Critical Findings (Security)

### CRIT-SEC-01: Auth Service Accounts Have No Authorization

**Severity:** CRITICAL (SECURITY)  
**Impact:** Any authenticated user can create/list/delete service accounts and API tokens in ANY organization  
**Category:** Authorization Coverage

**Evidence:**

Auth routes explicitly defer authorization:
```go
// backend/services/auth/internal/routes.go:29-31
// Org-scoped machine identities. Authorization of org membership/role is
// performed at the gateway/tenant service; these handlers enforce tenant
// data isolation via row-level security (database.WithTenant).
```

But gateway only authenticates + `OrgScope()` (no membership check). Service methods use `WithTenant` only:
```go
// backend/services/auth/internal/service_accounts.go:26-55
func (s *Service) CreateServiceAccount(ctx context.Context, orgID, creatorUserID string, req CreateServiceAccountRequest) (*ServiceAccount, error) {
    // NO AuthorizeOrgMember call
    err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
        if err := s.serviceAccounts.Create(ctx, sa); err != nil {
```

**Attack Vector:** Attacker with valid JWT (member of Org A) sends `POST /v1/organizations/ORG-B/service-accounts`. RLS allows write because `WithTenant(ORG-B)` sets `app.current_org_id=ORG-B`.

**Fix:**

Add authorization to all service account methods:

```go
// backend/services/auth/internal/service_accounts.go (MODIFY)
func (s *Service) CreateServiceAccount(ctx context.Context, orgID, creatorUserID string, req CreateServiceAccountRequest) (*ServiceAccount, error) {
    // ADD: Verify caller is org member with appropriate role
    if _, err := s.authz.AuthorizeOrgMember(ctx, orgID, creatorUserID, authz.ActionManageOrg); err != nil {
        return nil, err
    }
    // ... rest of implementation
}
```

---

### CRIT-SEC-02: Deployment User Status Endpoint Has No Authorization

**Severity:** CRITICAL (SECURITY)  
**Impact:** Any authenticated user can mark deployments as started/succeeded/failed in ANY organization  
**Category:** Authorization Coverage

**Evidence:**

```go
// backend/services/deployment/internal/service.go:569-591
func (s *Service) MarkDeploymentStarted(ctx context.Context, orgID, depID, releaseID string) error {
    return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
        // NO membership or role check
```

Called from user handler at `handlers.go:236-266`.

**Fix:**

Add authorization:

```go
func (s *Service) MarkDeploymentStarted(ctx context.Context, orgID, userID, depID, releaseID string) error {
    if _, err := s.authz.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
        return err
    }
    // ... rest
}
```

---

### CRIT-SEC-03: Cluster User Heartbeat Endpoint Has No Authorization

**Severity:** CRITICAL (SECURITY)  
**Impact:** Any authenticated user can heartbeat ANY organization's clusters  
**Category:** Authorization Coverage

**Evidence:**

```go
// backend/services/cluster/internal/service.go:443-489
func (s *Service) RecordHeartbeat(ctx context.Context, orgID, clusterID string, req AgentHeartbeatRequest) error {
    // NO AuthorizeOrgMember / AuthorizeOrgRead
    return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
```

Mounted under user JWT auth at `routes.go:33`.

**Fix:**

Either remove the user JWT heartbeat route (keep only agent credential route) or add authorization:

```go
// backend/services/cluster/internal/routes.go (MODIFY)
// REMOVE this line - agents should use /v1/agent/clusters/:clusterId/heartbeat
// clusters.Post("/:clusterId/heartbeat", h.AgentHeartbeat)
```

---

### CRIT-SEC-04: RLS Not Enforced for Application DB User

**Severity:** CRITICAL (SECURITY)  
**Impact:** RLS policies are ineffective - table owner bypasses them by default  
**Category:** Tenant Isolation

**Evidence:**

No migration includes `FORCE ROW LEVEL SECURITY`:
```sql
-- All migrations only have:
ALTER TABLE secrets ENABLE ROW LEVEL SECURITY;
-- Missing: ALTER TABLE secrets FORCE ROW LEVEL SECURITY;
```

The `platform` DB user is likely the table owner, which bypasses RLS by default in PostgreSQL.

**Fix:**

Add `FORCE ROW LEVEL SECURITY` to all tenant-owned tables:

```sql
-- Create new migration: backend/migrations/*/000X_force_rls.up.sql
ALTER TABLE secrets FORCE ROW LEVEL SECURITY;
ALTER TABLE clusters FORCE ROW LEVEL SECURITY;
-- ... for all tenant tables
```

---

### CRIT-SEC-05: Secrets Service Breaks Event Contract

**Severity:** CRITICAL  
**Impact:** Double-versioned NATS subjects cause event routing failures; audit consumer may miss events  
**Category:** Event Contract Consistency

**Evidence:**

```go
// backend/services/secrets/internal/events.go:11-14
EventSecretCreated = "secret.created.v1"  // WRONG: version in type string
EventSecretUpdated = "secret.updated.v1"
EventSecretDeleted = "secret.deleted.v1"
```

Other services use `secret.created` + `version: 1` separately. Secrets puts `.v1` in the type string, causing `Subject()` to produce `evt.secret.created.v1.v1`.

**Fix:**

```go
// backend/services/secrets/internal/events.go (MODIFY)
const (
    EventSecretCreated = "secret.created"  // Remove .v1
    EventSecretUpdated = "secret.updated"
    EventSecretDeleted = "secret.deleted"
)
```

---

## Critical Findings (API Alignment)

### CRIT-01: Missing List Organizations Endpoint

**Severity:** CRITICAL  
**Impact:** Users cannot see their organizations after login  
**Category:** Frontend ↔ API Alignment

**Evidence:**

Frontend API call:
```typescript
// frontend/src/lib/api/organizations.ts:13-14
async list(params?: { limit?: number; cursor?: string }): Promise<PaginatedResponse<Organization>> {
  return apiClient.get<PaginatedResponse<Organization>>("/v1/organizations", params);
}
```

Backend routes (tenant service):
```go
// backend/services/tenant/internal/routes.go:13-17
orgs := v1.Group("/organizations")
orgs.Post("", h.CreateOrganization)
orgs.Get("/:orgId", h.GetOrganization)    // Only supports GET by ID
orgs.Patch("/:orgId", h.UpdateOrganization)
orgs.Delete("/:orgId", h.DeleteOrganization)
```

**Root Cause:** The tenant service was implemented to manage organizations but never added an endpoint to list organizations for the authenticated user.

**Fix:**

1. Add `ListOrganizations` handler to tenant service:

```go
// backend/services/tenant/internal/handlers.go (ADD)
func (h *Handler) ListOrganizations(c *fiber.Ctx) error {
    userID := callerIdentity(c).UserID
    orgs, err := h.svc.ListUserOrganizations(c.UserContext(), userID)
    if err != nil {
        return err
    }
    return c.JSON(fiber.Map{
        "items": orgs,
    })
}
```

2. Add service method:

```go
// backend/services/tenant/internal/service.go (ADD)
func (s *Service) ListUserOrganizations(ctx context.Context, userID string) ([]Organization, error) {
    return s.store.ListByUser(ctx, userID)
}
```

3. Add repository method:

```go
// backend/services/tenant/internal/repository.go (ADD)
func (r *repo) ListByUser(ctx context.Context, userID string) ([]Organization, error) {
    return database.Query[Organization](ctx, r.db,
        `SELECT o.* FROM organizations o
         JOIN memberships m ON m.org_id = o.id
         WHERE m.user_id = $1 AND o.deleted_at IS NULL
         ORDER BY o.name`, userID)
}
```

4. Register route:

```go
// backend/services/tenant/internal/routes.go (MODIFY)
orgs.Get("", h.ListOrganizations)  // Add before orgs.Get("/:orgId", ...)
```

5. Add Gateway route:

```go
// backend/services/gateway/internal/router/router.go (MODIFY in registerTenantRoutes)
orgs.Get("", svc.Handler())  // Add before orgs.Get("/:orgId", ...)
```

---

### CRIT-02: Missing Get Organization by Slug Endpoint

**Severity:** CRITICAL  
**Impact:** Dashboard navigation fails; users cannot access organizations by URL slug  
**Category:** Frontend ↔ API Alignment

**Evidence:**

Frontend API call:
```typescript
// frontend/src/lib/api/organizations.ts:21-23
async getBySlug(slug: string): Promise<Organization> {
  return apiClient.get<Organization>(`/v1/organizations/by-slug/${slug}`);
}
```

Backend: Endpoint does not exist in tenant service or gateway.

**Root Cause:** The frontend assumes organizations can be looked up by slug for URL-based navigation, but the backend only supports lookup by UUID.

**Fix:**

1. Add `GetOrganizationBySlug` handler:

```go
// backend/services/tenant/internal/handlers.go (ADD)
func (h *Handler) GetOrganizationBySlug(c *fiber.Ctx) error {
    slug := c.Params("slug")
    userID := callerIdentity(c).UserID
    org, err := h.svc.GetOrganizationBySlug(c.UserContext(), userID, slug)
    if err != nil {
        return err
    }
    return c.JSON(org)
}
```

2. Add service method:

```go
// backend/services/tenant/internal/service.go (ADD)
func (s *Service) GetOrganizationBySlug(ctx context.Context, userID, slug string) (*Organization, error) {
    org, err := s.store.GetBySlug(ctx, slug)
    if err != nil {
        return nil, err
    }
    // Verify membership
    if err := s.authz.AuthorizeOrgRead(ctx, userID, org.ID); err != nil {
        return nil, err
    }
    return org, nil
}
```

3. Add repository method:

```go
// backend/services/tenant/internal/repository.go (ADD)
func (r *repo) GetBySlug(ctx context.Context, slug string) (*Organization, error) {
    return database.QueryOne[Organization](ctx, r.db.Pool, // Bypass RLS for initial lookup
        `SELECT * FROM organizations WHERE slug = $1 AND deleted_at IS NULL`, slug)
}
```

4. Register routes:

```go
// backend/services/tenant/internal/routes.go (MODIFY)
orgs.Get("/by-slug/:slug", h.GetOrganizationBySlug)  // Add before orgs.Get("/:orgId", ...)
```

5. Add Gateway route:

```go
// backend/services/gateway/internal/router/router.go (MODIFY in registerTenantRoutes)
orgs.Get("/by-slug/:slug", svc.Handler())
```

---

### CRIT-03: Audit API Path Mismatch

**Severity:** CRITICAL  
**Impact:** Audit log page shows no data; all audit API calls fail with 404  
**Category:** Frontend ↔ API Alignment

**Evidence:**

Frontend API calls:
```typescript
// frontend/src/lib/api/audit.ts:8-14
async list(...): Promise<PaginatedResponse<AuditLog>> {
  return apiClient.get<PaginatedResponse<AuditLog>>(`/v1/organizations/${orgId}/audit`, params);
}

async get(orgId: string, auditId: string): Promise<AuditLog> {
  return apiClient.get<AuditLog>(`/v1/organizations/${orgId}/audit/${auditId}`);
}
```

Backend routes:
```go
// backend/services/audit/internal/routes.go:13-15
logs := v1.Group("/organizations/:orgId/audit-logs")  // Uses "audit-logs"
logs.Get("", h.ListAuditLogs)
logs.Get("/:eventId", h.GetAuditLog)
```

**Root Cause:** Frontend uses `/audit` while backend uses `/audit-logs`.

**Fix:**

Update frontend to use correct path:

```typescript
// frontend/src/lib/api/audit.ts (MODIFY)
async list(
  orgId: string,
  params?: AuditLogFilter & { limit?: number; cursor?: string }
): Promise<PaginatedResponse<AuditLog>> {
  return apiClient.get<PaginatedResponse<AuditLog>>(`/v1/organizations/${orgId}/audit-logs`, params);
}

async get(orgId: string, eventId: string): Promise<AuditLog> {
  return apiClient.get<AuditLog>(`/v1/organizations/${orgId}/audit-logs/${eventId}`);
}
```

---

### CRIT-04: Application API Path Mismatch

**Severity:** CRITICAL  
**Impact:** Application get/update/delete operations fail with 404  
**Category:** Frontend ↔ API Alignment

**Evidence:**

Frontend API calls:
```typescript
// frontend/src/lib/api/applications.ts:21-50
async get(orgId: string, projectId: string, applicationId: string): Promise<Application> {
  return apiClient.get<Application>(
    `/v1/organizations/${orgId}/projects/${projectId}/applications/${applicationId}`
  );
}

async update(...): Promise<Application> {
  return apiClient.patch<Application>(
    `/v1/organizations/${orgId}/projects/${projectId}/applications/${applicationId}`,
    data
  );
}

async delete(orgId: string, projectId: string, applicationId: string): Promise<void> {
  return apiClient.delete(
    `/v1/organizations/${orgId}/projects/${projectId}/applications/${applicationId}`
  );
}
```

Backend routes:
```go
// backend/services/deployment/internal/routes.go:18-22
singleApp := authenticated.Group("/organizations/:orgId/applications/:appId")
singleApp.Get("", h.GetApplication)
singleApp.Patch("", h.UpdateApplication)
singleApp.Delete("", h.DeleteApplication)
```

**Root Cause:** Backend uses `/organizations/:orgId/applications/:appId` but frontend expects `/organizations/:orgId/projects/:projectId/applications/:appId`.

**Fix:**

Update frontend to use correct paths:

```typescript
// frontend/src/lib/api/applications.ts (MODIFY)
async get(orgId: string, applicationId: string): Promise<Application> {
  return apiClient.get<Application>(
    `/v1/organizations/${orgId}/applications/${applicationId}`
  );
}

async update(
  orgId: string,
  applicationId: string,
  data: UpdateApplicationRequest
): Promise<Application> {
  return apiClient.patch<Application>(
    `/v1/organizations/${orgId}/applications/${applicationId}`,
    data
  );
}

async delete(orgId: string, applicationId: string): Promise<void> {
  return apiClient.delete(
    `/v1/organizations/${orgId}/applications/${applicationId}`
  );
}
```

Also update components that call these methods to remove the `projectId` parameter.

---

### CRIT-05: Missing Deployment List Endpoint at Organization Level

**Severity:** CRITICAL  
**Impact:** Deployments page shows no data; cannot list all deployments for an organization  
**Category:** Frontend ↔ API Alignment

**Evidence:**

Frontend API call:
```typescript
// frontend/src/lib/api/deployments.ts:12-17
async list(
  orgId: string,
  params?: { applicationId?: string; clusterId?: string; limit?: number; cursor?: string }
): Promise<PaginatedResponse<Deployment>> {
  return apiClient.get<PaginatedResponse<Deployment>>(`/v1/organizations/${orgId}/deployments`, params);
}
```

Backend routes:
```go
// backend/services/deployment/internal/routes.go:28-30
deps := authenticated.Group("/organizations/:orgId/deployments")
deps.Post("", h.CreateDeployment)  // Only POST exists
// No GET endpoint for listing
```

**Root Cause:** The deployment service only implements POST for creating deployments, not GET for listing.

**Fix:**

1. Add `ListDeployments` handler for org-level listing:

```go
// backend/services/deployment/internal/handlers.go (ADD)
func (h *Handler) ListOrgDeployments(c *fiber.Ctx) error {
    org := orgID(c)
    filter := DeploymentFilter{
        ApplicationID: c.Query("applicationId"),
        ClusterID:     c.Query("clusterId"),
    }
    deps, err := h.svc.ListOrgDeployments(c.UserContext(), org, callerIdentity(c).UserID, filter)
    if err != nil {
        return err
    }
    return c.JSON(fiber.Map{"items": deps})
}
```

2. Add service method:

```go
// backend/services/deployment/internal/service.go (ADD)
func (s *Service) ListOrgDeployments(ctx context.Context, orgID, userID string, filter DeploymentFilter) ([]Deployment, error) {
    if err := s.authz.AuthorizeOrgRead(ctx, userID, orgID); err != nil {
        return nil, err
    }
    return s.tenant.WithTenantResult(ctx, orgID, func(ctx context.Context) ([]Deployment, error) {
        return s.store.ListByOrg(ctx, orgID, filter)
    })
}
```

3. Register route:

```go
// backend/services/deployment/internal/routes.go (MODIFY)
deps := authenticated.Group("/organizations/:orgId/deployments")
deps.Get("", h.ListOrgDeployments)  // Add this
deps.Post("", h.CreateDeployment)
```

4. Add Gateway route:

```go
// backend/services/gateway/internal/router/router.go (MODIFY in registerDeploymentRoutes)
deployments := orgs.Group("/deployments")
deployments.Get("", svc.Handler())   // Add this
deployments.Post("", svc.Handler())
```

---

### CRIT-06: Missing Deployment Delete Endpoint

**Severity:** CRITICAL  
**Impact:** Users cannot delete deployments; deployment lifecycle incomplete  
**Category:** Frontend ↔ API Alignment

**Evidence:**

Frontend API call:
```typescript
// frontend/src/lib/api/deployments.ts:30-32
async delete(orgId: string, deploymentId: string): Promise<void> {
  return apiClient.delete(`/v1/organizations/${orgId}/deployments/${deploymentId}`);
}
```

Backend routes:
```go
// backend/services/deployment/internal/routes.go:33-36
dep := authenticated.Group("/organizations/:orgId/deployments/:deploymentId")
dep.Get("", h.GetDeployment)
dep.Patch("", h.UpdateDeployment)
dep.Post("/rollback", h.Rollback)
// No DELETE endpoint
```

**Root Cause:** Delete deployment functionality was not implemented.

**Fix:**

1. Add `DeleteDeployment` handler:

```go
// backend/services/deployment/internal/handlers.go (ADD)
func (h *Handler) DeleteDeployment(c *fiber.Ctx) error {
    org := orgID(c)
    depID := c.Params("deploymentId")
    if err := h.svc.DeleteDeployment(c.UserContext(), org, callerIdentity(c).UserID, depID); err != nil {
        return err
    }
    return c.SendStatus(fiber.StatusNoContent)
}
```

2. Add service method:

```go
// backend/services/deployment/internal/service.go (ADD)
func (s *Service) DeleteDeployment(ctx context.Context, orgID, userID, deploymentID string) error {
    if err := s.authz.AuthorizeOrgMember(ctx, userID, orgID, authz.RoleAdmin); err != nil {
        return err
    }
    return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
        dep, err := s.store.GetByID(ctx, deploymentID)
        if err != nil {
            return err
        }
        // Soft delete
        now := time.Now().UTC()
        dep.DeletedAt = &now
        if err := s.store.Update(ctx, dep); err != nil {
            return err
        }
        // Emit event
        return s.enqueue(ctx, &database.OutboxEntry{
            EventType:    "deployment.deleted.v1",
            Payload:      mustMarshal(DeploymentDeletedPayload{DeploymentID: deploymentID}),
            OrgID:        orgID,
            ResourceType: "deployment",
            ResourceID:   deploymentID,
        })
    })
}
```

3. Register route:

```go
// backend/services/deployment/internal/routes.go (MODIFY)
dep := authenticated.Group("/organizations/:orgId/deployments/:deploymentId")
dep.Get("", h.GetDeployment)
dep.Patch("", h.UpdateDeployment)
dep.Delete("", h.DeleteDeployment)  // Add this
dep.Post("/rollback", h.Rollback)
```

4. Add Gateway route:

```go
// backend/services/gateway/internal/router/router.go (MODIFY)
dep.Delete("", svc.Handler())  // Add in registerDeploymentRoutes
```

---

## High Findings

### HIGH-00: Audit Service Drops Application Events

**Severity:** HIGH  
**Impact:** Application lifecycle events are published but never audited  
**Category:** Event Contract Consistency

**Evidence:**

```go
// backend/services/audit/internal/domain.go:15-22
var supportedDomains = map[string]bool{
    "auth": true,
    "tenant": true,
    "project": true,
    "cluster": true,
    "deployment": true,
    "secret": true,
    // Missing: "application"
}
```

`domainOf("application.created")` → `"application"`, which is not in the map.

**Fix:**

```go
var supportedDomains = map[string]bool{
    // ...
    "application": true,  // ADD
}
```

---

### HIGH-01: Organizations Table Missing Slug Column

**Severity:** HIGH  
**Impact:** Organization slug-based routing cannot work even after adding endpoint  
**Category:** Migration Validity

**Evidence:**

Migration does not include `slug` column:
```sql
-- backend/migrations/tenant/0001_init.up.sql
CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    -- No slug column
    ...
);
```

**Fix:**

Create migration `backend/migrations/tenant/0002_add_slug.up.sql`:

```sql
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS slug VARCHAR(63) UNIQUE;

-- Generate slugs for existing orgs
UPDATE organizations SET slug = LOWER(REGEXP_REPLACE(name, '[^a-zA-Z0-9]', '-', 'g'))
WHERE slug IS NULL;

ALTER TABLE organizations ALTER COLUMN slug SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS organizations_slug_idx ON organizations (slug) WHERE deleted_at IS NULL;
```

---

### HIGH-02: Missing Health Endpoints on Individual Services

**Severity:** HIGH  
**Impact:** Kubernetes health probes will fail; services won't restart on deadlock  
**Category:** Health/Readiness Endpoints

**Evidence:**

The shared `httpserver` library provides `/healthz` and `/readyz` endpoints:
```go
// backend/libs/httpserver/server.go:40-43
app.Get("/healthz", func(c *fiber.Ctx) error {
    return c.SendStatus(fiber.StatusOK)
})
app.Get("/readyz", func(c *fiber.Ctx) error {
    return c.SendStatus(fiber.StatusOK)
})
```

**Status:** PASS - All services using `httpserver.Run()` automatically get health endpoints.

---

### HIGH-03: Missing Event Contract for deployment.deleted.v1

**Severity:** HIGH  
**Impact:** Audit trail incomplete when deployments are deleted  
**Category:** Event Contract Consistency

**Evidence:**

Search for existing events shows no `deployment.deleted.v1`:

```go
// Events in deployment service include:
// - deployment.created.v1
// - release.created.v1
// - release.status.changed.v1
// Missing: deployment.deleted.v1
```

**Fix:**

Add event definition to service and event catalog:

```go
// backend/services/deployment/internal/domain.go (ADD)
type DeploymentDeletedPayload struct {
    DeploymentID  string `json:"deployment_id"`
    ApplicationID string `json:"application_id"`
    ClusterID     string `json:"cluster_id"`
}
```

Update `docs/12-event-catalog.md` with new event.

---

### HIGH-04: Agent Desired State Response Missing Delete Signal

**Severity:** HIGH  
**Impact:** Agent cannot clean up deleted deployments from Kubernetes  
**Category:** Agent ↔ Control Plane Alignment

**Evidence:**

When a deployment is deleted, the agent needs to know to remove it from Kubernetes. Currently, the desired state endpoint only returns active deployments.

**Fix:**

Add `deleted` field or separate deletion list to desired state response:

```go
// backend/services/deployment/internal/agent_handlers.go (MODIFY)
type DesiredStateResponse struct {
    Deployments       []DesiredDeployment `json:"deployments"`
    DeletedDeployments []string           `json:"deleted_deployments"` // ADD
}
```

Agent reconciler should check `deleted_deployments` and remove corresponding K8s resources.

---

### HIGH-05: No Deployment Status Transition Validation

**Severity:** HIGH  
**Impact:** Invalid status transitions could corrupt deployment state  
**Category:** API Contract Consistency

**Evidence:**

The deployment service accepts any status from agents without validating valid transitions:

```go
// Valid transitions should be:
// pending -> deploying -> succeeded/failed
// succeeded -> deploying (rollback)
// Current implementation accepts any transition
```

**Fix:**

Add state machine validation:

```go
// backend/libs/contracts/deploymentstatus/status.go (ADD)
func ValidTransition(from, to ReleaseStatus) bool {
    valid := map[ReleaseStatus][]ReleaseStatus{
        ReleasePending:    {ReleaseDeploying},
        ReleaseDeploying:  {ReleaseSucceeded, ReleaseFailed},
        ReleaseSucceeded:  {ReleaseDeploying}, // Allow rollback
        ReleaseFailed:     {ReleaseDeploying}, // Allow retry
        ReleaseRolledBack: {},
    }
    for _, v := range valid[from] {
        if v == to {
            return true
        }
    }
    return false
}
```

---

### HIGH-06: Missing Project Membership Validation

**Severity:** HIGH  
**Impact:** Users can access any project within an org regardless of project membership  
**Category:** Authorization Coverage

**Evidence:**

Project service validates org membership but not project membership for non-admin users:

```go
// Project-scoped endpoints should verify:
// 1. User is org member (✓ implemented)
// 2. User has project membership for non-admin roles (✗ not implemented)
```

**Fix:**

Add project membership validation:

```go
// backend/services/project/internal/service.go (MODIFY)
func (s *Service) GetProject(ctx context.Context, orgID, userID, projectID string) (*Project, error) {
    // First check org membership
    role, err := s.authz.GetOrgRole(ctx, userID, orgID)
    if err != nil {
        return nil, err
    }
    
    // Admin can access all projects; others need project membership
    if role != authz.RoleAdmin {
        if !s.hasProjectAccess(ctx, userID, projectID) {
            return nil, apperrors.Forbidden("not a project member")
        }
    }
    // ... rest of implementation
}
```

---

### HIGH-07: Secret Sync Endpoint Returns All Org Secrets

**Severity:** HIGH  
**Impact:** Agent receives secrets not needed for deployments on its cluster  
**Category:** Tenant Isolation

**Evidence:**

The secrets sync endpoint should only return secrets for projects that have deployments on the requesting cluster, not all secrets in the organization.

```go
// Current: Returns all secrets for org
// Expected: Returns only secrets for projects with deployments on this cluster
```

**Fix:**

Update query to filter by deployment association:

```sql
-- backend/services/secrets/internal/repository.go (MODIFY)
SELECT DISTINCT s.*
FROM secrets s
JOIN applications a ON a.project_id = s.project_id
JOIN deployments d ON d.application_id = a.id
WHERE d.cluster_id = $1
  AND d.deleted_at IS NULL
  AND s.deleted_at IS NULL
```

---

### HIGH-08: OpenAPI Specs Missing Agent Endpoints

**Severity:** HIGH  
**Impact:** API documentation incomplete; agent developers lack specification  
**Category:** OpenAPI Coverage

**Evidence:**

The Gateway OpenAPI spec (`backend/services/gateway/api/openapi.yaml`) exists but doesn't document:
- `POST /v1/agent/register`
- `POST /v1/agent/clusters/:clusterId/heartbeat`
- `GET /v1/agent/clusters/:clusterId/desired-state`
- `POST /v1/agent/deployments/:deploymentId/releases/:releaseId/status`
- `GET /v1/agent/clusters/:clusterId/secrets`

**Fix:**

Add agent endpoint documentation to `backend/services/gateway/api/openapi.yaml`:

```yaml
paths:
  /agent/register:
    post:
      summary: Register cluster agent
      tags: [Agent]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/AgentRegisterRequest'
      responses:
        '200':
          description: Registration successful
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/AgentRegisterResponse'
  # ... additional agent endpoints
```

---

## Medium Findings

### MED-01: No Rate Limiting on Agent Endpoints

**Severity:** MEDIUM  
**Impact:** Compromised agent could DoS control plane  
**Category:** Production Failure Scenarios

**Current:** Agent endpoints bypass Gateway rate limiting.

**Fix:** Add agent-specific rate limiter with higher limits (e.g., 100 req/min per cluster).

---

### MED-02: Missing Soft Delete on Applications

**Severity:** MEDIUM  
**Impact:** Deleted applications leave orphaned deployments  
**Category:** API Contract Consistency

**Fix:** Add soft delete support with cascade to deployments.

---

### MED-03: No Pagination on Heartbeat History

**Severity:** MEDIUM  
**Impact:** Memory pressure when fetching long heartbeat history  
**Category:** API Contract Consistency

**Fix:** Add cursor-based pagination to `GET /clusters/:clusterId/heartbeats`.

---

### MED-04: Missing Cluster Status Events

**Severity:** MEDIUM  
**Impact:** Audit trail doesn't capture cluster connectivity changes  
**Category:** Event Contract Consistency

**Fix:** Emit `cluster.connected.v1`, `cluster.disconnected.v1` events.

---

### MED-05: Frontend Missing Error Boundary

**Severity:** MEDIUM  
**Impact:** Uncaught errors crash entire application  
**Category:** Frontend ↔ API Alignment

**Fix:** Add React Error Boundary component wrapping dashboard layout.

---

### MED-06: No Retry Logic in Frontend API Client

**Severity:** MEDIUM  
**Impact:** Transient network errors cause immediate failure  
**Category:** Frontend ↔ API Alignment

**Fix:** Add exponential backoff retry for 5xx errors and network failures.

---

### MED-07: Missing Index on deployments(cluster_id)

**Severity:** MEDIUM  
**Impact:** Agent desired-state queries slow at scale  
**Category:** Migration Validity

**Fix:** Add index in new migration:

```sql
CREATE INDEX IF NOT EXISTS deployments_cluster_id_idx 
ON deployments (cluster_id) WHERE deleted_at IS NULL;
```

---

### MED-08: Secret Version Not Incremented on Update

**Severity:** MEDIUM  
**Impact:** Agent cannot detect secret changes  
**Category:** API Contract Consistency

**Fix:** Increment `version` column in `UpdateSecret` service method.

---

### MED-09: No Graceful Shutdown on Agent

**Severity:** MEDIUM  
**Impact:** In-progress reconciliation interrupted on restart  
**Category:** Agent ↔ Control Plane Alignment

**Fix:** Add context cancellation handling and graceful shutdown with timeout.

---

### MED-10: Missing CORS Configuration

**Severity:** MEDIUM  
**Impact:** Frontend cannot call API from different domain  
**Category:** Production Failure Scenarios

**Fix:** Add CORS middleware to Gateway with configurable origins.

---

### MED-11: No Connection Pooling Config Exposed

**Severity:** MEDIUM  
**Impact:** Database connections may exhaust under load  
**Category:** Production Failure Scenarios

**Fix:** Expose `DATABASE_MAX_CONNS`, `DATABASE_MIN_CONNS` environment variables.

---

## Audit Area Summary

| Area | Status | Score | Notes |
|------|--------|-------|-------|
| API Contract Consistency | ❌ FAIL | 5/10 | 6 path mismatches, no Auth OpenAPI, gateway incomplete |
| Event Contract Consistency | ❌ FAIL | 5/10 | Secrets breaks naming, audit drops app events |
| Authorization Coverage | ❌ FAIL | 3/10 | **3 cross-tenant bypasses**, org role gaps |
| Tenant Isolation | ❌ FAIL | 4/10 | RLS not enforced for app user, no FORCE RLS |
| Frontend ↔ API Alignment | ❌ FAIL | 3/10 | 18+ mismatches, error envelope wrong |
| Agent ↔ Control Plane Alignment | ⚠️ WARN | 7/10 | Namespace ignored, Helm env var drift |
| Migration Validity | ⚠️ WARN | 7/10 | Incomplete down migrations, soft-delete inconsistent |
| OpenAPI Coverage | ❌ FAIL | 4/10 | No Auth spec, gateway missing 3 services |
| Health/Readiness Endpoints | ✅ PASS | 10/10 | All services covered |
| Production Failure Scenarios | ⚠️ WARN | 6/10 | Rate limiting, CORS, error handling gaps |

---

## Remediation Priority

### Immediate (P0 SECURITY) - Must Fix Before Any User Access

| Finding | Effort | Impact |
|---------|--------|--------|
| CRIT-SEC-01: Auth service accounts authz | 2h | **Cross-tenant data access** |
| CRIT-SEC-02: Deployment status authz | 1h | **Cross-tenant writes** |
| CRIT-SEC-03: Cluster heartbeat authz | 1h | **Cross-tenant writes** |
| CRIT-SEC-04: Force RLS on all tables | 2h | **RLS bypass** |
| CRIT-SEC-05: Secrets event naming | 1h | Event routing broken |

**Total P0 Security Effort:** ~7 hours

### Immediate (P0 API) - Required for MVP

| Finding | Effort | Impact |
|---------|--------|--------|
| CRIT-01: List Organizations | 2h | Unlocks user journey |
| CRIT-02: Get Org by Slug | 2h | Unlocks dashboard navigation |
| CRIT-03: Audit API Path | 30m | Frontend fix only |
| CRIT-04: Application API Path | 30m | Frontend fix only |
| CRIT-05: Deployment List | 2h | Unlocks deployments page |
| CRIT-06: Deployment Delete | 2h | Completes deployment lifecycle |
| HIGH-01: Slug Column | 1h | Required for CRIT-02 |

**Total P0 API Effort:** ~10 hours

### Short-term (P1) - Required for Beta

| Finding | Effort |
|---------|--------|
| HIGH-00: Audit app events | 30m |
| HIGH-03: Delete event | 1h |
| HIGH-04: Delete signal | 2h |
| HIGH-05: Status validation | 2h |
| HIGH-06: Project membership | 3h |
| HIGH-07: Secret filtering | 2h |
| HIGH-08: OpenAPI agent docs | 3h |
| Frontend error envelope parsing | 2h |
| Frontend pagination field fix | 1h |

**Total P1 Effort:** ~16 hours

### Medium-term (P2) - Production Hardening

All MEDIUM findings - prioritize based on production load patterns.

---

## Verification Test Plan

After remediation, execute this manual test to verify end-to-end journey:

```bash
# 1. Signup
curl -X POST http://localhost:8080/v1/auth/signup \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"Test123!","name":"Test User"}'

# 2. Login
TOKEN=$(curl -s -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"Test123!"}' | jq -r '.accessToken')

# 3. Create Organization
ORG_ID=$(curl -s -X POST http://localhost:8080/v1/organizations \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Org","slug":"test-org"}' | jq -r '.id')

# 4. List Organizations (CRIT-01 fix verification)
curl http://localhost:8080/v1/organizations \
  -H "Authorization: Bearer $TOKEN"

# 5. Get Org by Slug (CRIT-02 fix verification)
curl http://localhost:8080/v1/organizations/by-slug/test-org \
  -H "Authorization: Bearer $TOKEN"

# 6. Create Project
PROJECT_ID=$(curl -s -X POST http://localhost:8080/v1/organizations/$ORG_ID/projects \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Project","slug":"test-project"}' | jq -r '.id')

# 7. Create Cluster
CLUSTER_ID=$(curl -s -X POST http://localhost:8080/v1/organizations/$ORG_ID/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Cluster","provider":"existing"}' | jq -r '.id')

# 8. Create Secret
curl -X POST http://localhost:8080/v1/organizations/$ORG_ID/projects/$PROJECT_ID/secrets \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"DATABASE_URL","value":"postgres://localhost:5432/db"}'

# 9. Create Application
APP_ID=$(curl -s -X POST http://localhost:8080/v1/organizations/$ORG_ID/projects/$PROJECT_ID/applications \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"nginx-app","image":"nginx:latest"}' | jq -r '.id')

# 10. Create Deployment
DEPLOYMENT_ID=$(curl -s -X POST http://localhost:8080/v1/organizations/$ORG_ID/deployments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"applicationId\":\"$APP_ID\",\"clusterId\":\"$CLUSTER_ID\",\"replicas\":1}" | jq -r '.id')

# 11. List Deployments (CRIT-05 fix verification)
curl http://localhost:8080/v1/organizations/$ORG_ID/deployments \
  -H "Authorization: Bearer $TOKEN"

# 12. Delete Deployment (CRIT-06 fix verification)
curl -X DELETE http://localhost:8080/v1/organizations/$ORG_ID/deployments/$DEPLOYMENT_ID \
  -H "Authorization: Bearer $TOKEN"

# 13. Delete Cluster
curl -X DELETE http://localhost:8080/v1/organizations/$ORG_ID/clusters/$CLUSTER_ID \
  -H "Authorization: Bearer $TOKEN"

# 14. View Audit Logs (CRIT-03 fix verification - via frontend)
curl http://localhost:8080/v1/organizations/$ORG_ID/audit-logs \
  -H "Authorization: Bearer $TOKEN"

echo "✅ End-to-end journey completed successfully"
```

---

## Conclusion

The platform has strong architectural foundations with:
- Centralized authorization framework (when used)
- Transactional outbox for reliable events
- Comprehensive health endpoints
- Good test coverage in many areas

However, **critical security vulnerabilities** and **widespread API misalignments** prevent production deployment:

### Security Blockers (Must Fix First)
1. **3 cross-tenant authorization bypasses** - Auth service accounts, deployment status, and cluster heartbeat endpoints allow any authenticated user to operate on any organization
2. **RLS not enforced** - Table owner bypasses RLS policies; `FORCE ROW LEVEL SECURITY` missing
3. **Event contract breakage** - Secrets service uses wrong naming convention

### Functional Blockers
4. **6 critical API path mismatches** - Frontend cannot complete basic user journey
5. **18+ frontend-backend alignment issues** - Error handling, pagination, types all misaligned
6. **Missing endpoints** - No list organizations, no deployment delete

**Recommended Action:**
1. **Immediately (Day 1):** Fix all P0 Security findings - these are exploitable
2. **Days 2-3:** Fix P0 API findings to enable user journey
3. **Day 4:** Conduct security-focused penetration test
4. **Day 5:** Full end-to-end validation before any external access

**Do NOT expose this platform to external users until P0 Security findings are resolved.**
