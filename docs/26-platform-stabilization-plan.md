# Platform Stabilization Plan

**Date:** 2026-06-24  
**Author:** Principal Platform Architect  
**Status:** Stabilization Required Before Release

---

## Executive Summary

This document consolidates all findings from platform audits (docs 22-25, validation audits) into a prioritized stabilization plan. The focus is exclusively on fixing existing functionality—no new features or roadmap items.

### Readiness Assessment

| Metric | Current | Target |
|--------|---------|--------|
| **MVP Readiness** | 45% | 100% |
| **Beta Readiness** | 35% | 100% |
| **Production Readiness** | 25% | 100% |

### Issue Summary

| Severity | Count | Estimated Effort |
|----------|-------|------------------|
| **Critical** | 14 | 18-22 hours |
| **High** | 19 | 28-36 hours |
| **Medium** | 16 | 20-28 hours |
| **Low** | 8 | 12-16 hours |

**Total Stabilization Effort:** 78-102 hours (~2-3 weeks)

---

## Critical Issues (14)

### SEC-CRIT-01: Auth Service Accounts Have No Authorization

**Impact:** Any authenticated user can create/list/delete service accounts and API tokens in ANY organization. Cross-tenant data access vulnerability.

**Root Cause:** Auth service routes defer authorization to Gateway, but Gateway only validates JWT authenticity, not org membership. `WithTenant()` sets RLS context but doesn't verify membership.

**Files:**
- `backend/services/auth/internal/service_accounts.go`
- `backend/services/auth/internal/handlers.go`
- `backend/services/auth/cmd/server/main.go`

**Remediation:**
1. Add `AuthorizationService` dependency to Auth service
2. Add `AuthorizeOrgMember(ctx, orgID, userID, authz.ActionManageOrg)` before `WithTenant` in all service account methods
3. Add `AuthorizeOrgMember` to all API token methods
4. Add unit tests for authorization

**Effort:** 3-4 hours

---

### SEC-CRIT-02: Deployment User Status Endpoint Has No Authorization

**Impact:** Any authenticated user can mark deployments as started/succeeded/failed in ANY organization. Can corrupt deployment state across tenants.

**Root Cause:** `MarkDeploymentStarted/Succeeded/Failed` methods call `WithTenant()` without membership validation.

**Files:**
- `backend/services/deployment/internal/service.go` (lines 569-639)
- `backend/services/deployment/internal/handlers.go` (lines 236-266)

**Remediation:**
1. Add `userID` parameter to `MarkDeploymentStarted`, `MarkDeploymentSucceeded`, `MarkDeploymentFailed`
2. Add `AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy)` before `WithTenant`
3. Update handler to pass `callerIdentity(c).UserID`

**Effort:** 1-2 hours

---

### SEC-CRIT-03: Cluster User Heartbeat Route Has No Authorization

**Impact:** Any authenticated user can send heartbeats to ANY organization's clusters. Can manipulate cluster connection status.

**Root Cause:** Legacy user-facing heartbeat route exists alongside agent credential route. User route lacks authorization.

**Files:**
- `backend/services/cluster/internal/routes.go` (line 33)
- `backend/services/cluster/internal/handlers.go`

**Remediation:**
1. Remove `clusters.Post("/:clusterId/heartbeat", h.AgentHeartbeat)` from user routes
2. Keep only agent credential route at `/v1/agent/clusters/:clusterId/heartbeat`
3. Update any tests that use the user heartbeat route

**Effort:** 1 hour

---

### SEC-CRIT-04: RLS Not Enforced for Application DB User

**Impact:** PostgreSQL RLS policies are ineffective because table owner bypasses RLS by default. All tenant isolation depends only on application-level checks.

**Root Cause:** Migrations use `ENABLE ROW LEVEL SECURITY` but not `FORCE ROW LEVEL SECURITY`. The `platform` DB user is table owner.

**Files:**
- `backend/migrations/tenant/0001_init.up.sql`
- `backend/migrations/tenant/0002_memberships.up.sql`
- `backend/migrations/project/0001_init.up.sql`
- `backend/migrations/cluster/0001_init.up.sql`
- `backend/migrations/deployment/0001_init.up.sql`
- `backend/migrations/secrets/0001_init.up.sql`
- `backend/migrations/audit/0001_init.up.sql`

**Remediation:**
1. Create migration `backend/migrations/*/0002_force_rls.up.sql` for each service
2. Add `ALTER TABLE <table> FORCE ROW LEVEL SECURITY;` for all tenant tables
3. Create corresponding down migrations
4. Test that RLS is enforced even for table owner

**Effort:** 2-3 hours

---

### SEC-CRIT-05: Secrets Service Breaks Event Contract

**Impact:** Event routing failures; audit consumer may miss secret events; double-versioned NATS subjects (`evt.secret.created.v1.v1`).

**Root Cause:** Secrets service embeds version in event type string (`secret.created.v1`) instead of using separate version field like other services.

**Files:**
- `backend/services/secrets/internal/events.go` (lines 11-14)
- `backend/services/secrets/internal/contract_test.go`

**Remediation:**
1. Change `EventSecretCreated = "secret.created.v1"` to `EventSecretCreated = "secret.created"`
2. Same for `EventSecretUpdated` and `EventSecretDeleted`
3. Update contract tests
4. Verify audit consumer receives events correctly

**Effort:** 1-2 hours

---

### API-CRIT-01: Missing List Organizations Endpoint

**Impact:** Frontend cannot display user's organizations after login. User journey blocked at step 3.

**Root Cause:** Tenant service implements org CRUD but no list endpoint for authenticated user's organizations.

**Files:**
- `backend/services/tenant/internal/routes.go`
- `backend/services/tenant/internal/handlers.go`
- `backend/services/tenant/internal/service.go`
- `backend/services/tenant/internal/repository.go`
- `backend/services/gateway/internal/router/router.go`

**Remediation:**
1. Add `ListOrganizations` handler that queries memberships for user
2. Add `ListUserOrganizations(ctx, userID)` service method
3. Add `ListByUser(ctx, userID)` repository method
4. Register route `GET /v1/organizations` in tenant service and gateway

**Effort:** 2-3 hours

---

### API-CRIT-02: Missing Get Organization by Slug Endpoint

**Impact:** Frontend URL-based navigation fails. Dashboard routes like `/acme/projects` cannot resolve.

**Root Cause:** No endpoint to look up organization by slug (only by UUID).

**Files:**
- `backend/services/tenant/internal/routes.go`
- `backend/services/tenant/internal/handlers.go`
- `backend/services/tenant/internal/service.go`
- `backend/services/tenant/internal/repository.go`
- `backend/services/gateway/internal/router/router.go`

**Remediation:**
1. Add `GetOrganizationBySlug` handler
2. Add service method that fetches by slug then validates membership
3. Add `GetBySlug(ctx, slug)` repository method
4. Register route `GET /v1/organizations/by-slug/:slug`

**Effort:** 2-3 hours

---

### API-CRIT-03: Missing List Deployments at Organization Level

**Impact:** Deployments page shows empty/error. No way to list all deployments across applications.

**Root Cause:** Deployment service only has list at application level, not org level.

**Files:**
- `backend/services/deployment/internal/routes.go`
- `backend/services/deployment/internal/handlers.go`
- `backend/services/deployment/internal/service.go`
- `backend/services/deployment/internal/repository.go`
- `backend/services/gateway/internal/router/router.go`

**Remediation:**
1. Add `ListOrgDeployments` handler with optional `applicationId` and `clusterId` filters
2. Add service method with authorization
3. Add repository method
4. Register route `GET /v1/organizations/:orgId/deployments`

**Effort:** 2-3 hours

---

### API-CRIT-04: Missing Delete Deployment Endpoint

**Impact:** Users cannot delete deployments. Deployment lifecycle incomplete. Orphaned resources accumulate.

**Root Cause:** Delete deployment not implemented despite repository method existing.

**Files:**
- `backend/services/deployment/internal/routes.go`
- `backend/services/deployment/internal/handlers.go`
- `backend/services/deployment/internal/service.go`
- `backend/services/gateway/internal/router/router.go`

**Remediation:**
1. Add `DeleteDeployment` handler
2. Add service method with soft delete and event emission
3. Register route `DELETE /v1/organizations/:orgId/deployments/:deploymentId`
4. Emit `deployment.deleted.v1` event

**Effort:** 2-3 hours

---

### API-CRIT-05: Frontend Error Envelope Parsing Fails

**Impact:** All API error messages are unparseable. Users see generic errors instead of specific messages.

**Root Cause:** Frontend expects flat `{ error: string }`, backend returns nested `{ error: { code, message } }`.

**Files:**
- `frontend/src/types/api.ts` (lines 3-7)
- `frontend/src/lib/api/client.ts` (lines 177-189)

**Remediation:**
1. Update `ApiError` interface to match backend envelope
2. Update error parsing in `client.ts` to read `error.message` and `error.code`

**Effort:** 1 hour

---

### API-CRIT-06: Frontend Pagination Field Mismatch

**Impact:** Pagination broken on all list endpoints. Next page never loads.

**Root Cause:** Frontend expects `cursor`, backend returns `nextCursor`.

**Files:**
- `frontend/src/types/api.ts` (lines 9-13)
- All hooks in `frontend/src/hooks/`

**Remediation:**
1. Update `PaginatedResponse` to use `nextCursor`
2. Update all hook usages of `.cursor` to `.nextCursor`

**Effort:** 1-2 hours

---

### API-CRIT-07: Audit API Path Mismatch

**Impact:** Audit log page returns 404. Compliance features unavailable.

**Root Cause:** Frontend uses `/audit`, backend uses `/audit-logs`.

**Files:**
- `frontend/src/lib/api/audit.ts`

**Remediation:**
1. Change `/v1/organizations/${orgId}/audit` to `/v1/organizations/${orgId}/audit-logs`
2. Update parameter name `auditId` to `eventId`

**Effort:** 30 minutes

---

### API-CRIT-08: Application API Path Mismatch (Get/Update/Delete)

**Impact:** Application detail, edit, and delete operations return 404.

**Root Cause:** Frontend uses project-scoped paths (`/projects/:projectId/applications/:appId`), backend uses org-scoped paths (`/applications/:appId`).

**Files:**
- `frontend/src/lib/api/applications.ts` (lines 21-49)
- `frontend/src/components/applications/*`

**Remediation:**
1. Update `get`, `update`, `delete` methods to use `/v1/organizations/${orgId}/applications/${applicationId}`
2. Remove `projectId` parameter from these methods
3. Update calling components

**Effort:** 1-2 hours

---

### API-CRIT-09: Logout Requires Request Body

**Impact:** Logout fails with 422. Users cannot sign out properly.

**Root Cause:** Frontend sends empty POST, backend requires `{ refreshToken }`.

**Files:**
- `frontend/src/lib/api/auth.ts` (lines 25-30)

**Remediation:**
1. Update logout to send `{ refreshToken }` from localStorage

**Effort:** 30 minutes

---

## High Issues (19)

### SEC-HIGH-01: Project/Secrets Services Ignore Org Admin Role

**Impact:** Org admins cannot manage projects they're not explicitly members of, despite RBAC policy granting them `ActionManageProject`.

**Root Cause:** `authorize()` methods in Project and Secrets services only populate `ProjectRoles`, never `OrgRoles`.

**Files:**
- `backend/services/project/internal/service.go` (lines 363-391)
- `backend/services/secrets/internal/service.go` (lines 392-415)

**Remediation:**
1. Load org role in addition to project role
2. Populate both `OrgRoles` and `ProjectRoles` on Principal
3. Add tests for org admin access to project operations

**Effort:** 2-3 hours

---

### SEC-HIGH-02: Tenant Service Skips Suspended Member Check

**Impact:** Suspended members can still read org details and list members.

**Root Cause:** `loadMember` in tenant service doesn't check `member.Status == "active"`.

**Files:**
- `backend/services/tenant/internal/authorize.go` (lines 12-28)

**Remediation:**
1. Add status check to `loadMember`
2. Return forbidden error for non-active members

**Effort:** 1 hour

---

### API-HIGH-01: Frontend Member Path Parameter Mismatch

**Impact:** Update/remove member operations fail with 404.

**Root Cause:** Frontend uses `memberId`, backend expects `userId`.

**Files:**
- `frontend/src/lib/api/organizations.ts` (lines 42-47)
- `frontend/src/lib/api/projects.ts` (lines 44-57)

**Remediation:**
1. Rename parameter from `memberId` to `userId`
2. Update calling components

**Effort:** 1 hour

---

### API-HIGH-02: Deployment Mutation Response Wrapper

**Impact:** Create/update/rollback deployment fails to parse response.

**Root Cause:** Frontend expects `Deployment`, backend returns `{ deployment, release }`.

**Files:**
- `frontend/src/lib/api/deployments.ts`
- `frontend/src/types/api.ts`

**Remediation:**
1. Add `DeploymentCreateResponse` type
2. Update `create`, `update`, `rollback` return types
3. Update mutation handlers

**Effort:** 1-2 hours

---

### API-HIGH-03: User DTO Field Mismatch

**Impact:** User profile displays incorrect/missing data.

**Root Cause:** Frontend expects `avatarUrl`, `createdAt`; backend returns `emailVerified`, `mfaEnabled`.

**Files:**
- `frontend/src/types/api.ts` (lines 16-22)

**Remediation:**
1. Update `User` interface to match `UserProfile` from backend

**Effort:** 30 minutes

---

### API-HIGH-04: Organization DTO Field Mismatch

**Impact:** Organization pages missing plan/status, expect nonexistent description/logo fields.

**Root Cause:** Frontend type doesn't match `OrganizationView`.

**Files:**
- `frontend/src/types/api.ts` (lines 42-50)

**Remediation:**
1. Update `Organization` interface to match backend `OrganizationView`
2. Add `plan` and `status` fields, remove `description`, `logoUrl`, `updatedAt`

**Effort:** 30 minutes

---

### API-HIGH-05: OrganizationMember DTO Mismatch

**Impact:** Member list displays incorrect structure.

**Root Cause:** Frontend expects nested `user` object and `"member"` role; backend returns flat structure with `"developer"` role.

**Files:**
- `frontend/src/types/api.ts` (lines 52-59, 72-75)

**Remediation:**
1. Update `OrganizationMember` to match `MemberView`
2. Change role enum: replace `"member"` with `"developer"`

**Effort:** 1 hour

---

### API-HIGH-06: AuditLog DTO Complete Mismatch

**Impact:** Audit log page cannot display any data correctly.

**Root Cause:** Frontend/backend field names completely different (`action` vs `eventType`, `createdAt` vs `timestamp`, etc.).

**Files:**
- `frontend/src/types/api.ts` (lines 278-301)

**Remediation:**
1. Completely rewrite `AuditLog` interface to match `AuditLogView`
2. Update `AuditLogFilter` query params (`startDate`→`from`, `endDate`→`to`)

**Effort:** 1-2 hours

---

### API-HIGH-07: Secret DTO Missing organizationId

**Impact:** Secret operations may fail or have incorrect context.

**Root Cause:** Frontend expects `organizationId`, backend only returns `projectId`.

**Files:**
- `frontend/src/types/api.ts` (lines 162-171)

**Remediation:**
1. Remove `organizationId` from `Secret` interface

**Effort:** 30 minutes

---

### API-HIGH-08: Invitation Response Shape Mismatch

**Impact:** Invite member returns wrong data structure.

**Root Cause:** Frontend expects full `Invitation`, backend returns `{ invitationId, status }`.

**Files:**
- `frontend/src/types/api.ts`
- `frontend/src/lib/api/organizations.ts`

**Remediation:**
1. Add `InvitationCreateResponse` type
2. Update `inviteMember` return type

**Effort:** 30 minutes

---

### API-HIGH-09: Accept Invitation Response Mismatch

**Impact:** Accept invitation flow fails.

**Root Cause:** Frontend expects `OrganizationMember`, backend returns `{ orgId, role }`.

**Files:**
- `frontend/src/lib/api/organizations.ts`

**Remediation:**
1. Add `AcceptInvitationResponse` type
2. Update `acceptInvitation` return type

**Effort:** 30 minutes

---

### EVENT-HIGH-01: Audit Service Drops Application Events

**Impact:** Application lifecycle events not recorded in audit trail.

**Root Cause:** `supportedDomains` map doesn't include `"application"`.

**Files:**
- `backend/services/audit/internal/domain.go` (lines 15-22)

**Remediation:**
1. Add `"application": true` to `supportedDomains`

**Effort:** 15 minutes

---

### AGENT-HIGH-01: Agent Ignores Namespace Field

**Impact:** Multi-namespace deployments don't work; all deployments go to single namespace.

**Root Cause:** Server returns per-application namespace, but reconciler uses configured namespace.

**Files:**
- `agents/platform-agent/internal/reconciler/reconciler.go`
- `agents/platform-agent/internal/k8s/manager.go`

**Remediation:**
1. Use `desired.Namespace` from API response instead of configured namespace
2. Update K8s manager to accept namespace per deployment

**Effort:** 2-3 hours

---

### AGENT-HIGH-02: Agent Error Envelope Parsing Wrong

**Impact:** Agent cannot extract error messages from control plane responses.

**Root Cause:** Agent expects flat `{ error: string }`, control plane returns nested envelope.

**Files:**
- `agents/platform-agent/internal/controlplane/client.go` (lines 153-157)

**Remediation:**
1. Update `ErrorResponse` struct to match backend envelope
2. Update error extraction in all client methods

**Effort:** 1-2 hours

---

### AGENT-HIGH-03: Helm Env Var Naming Mismatch

**Impact:** Helm-deployed agents fail to start due to missing config.

**Root Cause:** Helm uses `CONTROL_PLANE_ENDPOINT`, agent expects `CONTROL_PLANE_URL`.

**Files:**
- `helm/agent/templates/deployment.yaml`
- `agents/platform-agent/internal/config/config.go`

**Remediation:**
1. Align Helm env var names with agent config expectations
2. Or add env var aliases in config loader

**Effort:** 1 hour

---

### MIGRATE-HIGH-01: Incomplete Down Migrations

**Impact:** Database rollbacks fail or leave schema drift.

**Root Cause:** Several down migrations are incomplete.

**Files:**
- `backend/migrations/project/0001_init.down.sql` - doesn't revert `status` column
- `backend/migrations/tenant/0001_init.down.sql` - drops shared `set_updated_at()` function

**Remediation:**
1. Add `ALTER TABLE projects DROP COLUMN IF EXISTS status` to project down migration
2. Don't drop shared trigger function in tenant down migration (or add dependency check)

**Effort:** 1-2 hours

---

### MIGRATE-HIGH-02: Missing Database Indices

**Impact:** Query performance degrades at scale.

**Root Cause:** Some query patterns lack supporting indices.

**Files:**
- `backend/migrations/cluster/0001_init.up.sql`
- `backend/migrations/deployment/0001_init.up.sql`

**Remediation:**
1. Add partial index on clusters for non-deleted list queries
2. Add `(org_id, status)` index on deployments

**Effort:** 1 hour

---

### LIFECYCLE-HIGH-01: Secrets Not Auto-Mounted in Pods

**Impact:** Customers must manually configure `envFrom.secretRef`. Secrets sync is incomplete.

**Root Cause:** Reconciler creates K8s Secrets but doesn't add `envFrom.secretRef` to deployment spec.

**Files:**
- `agents/platform-agent/internal/k8s/manager.go`
- `agents/platform-agent/internal/reconciler/reconciler.go`

**Remediation:**
1. Modify `ApplyDeployment` to add `envFrom.secretRef` for project secrets
2. Secret name follows pattern `bds-secret-{projectId}`

**Effort:** 2-3 hours

---

### LIFECYCLE-HIGH-02: Agent Delete Signal Missing

**Impact:** Agent cannot clean up deleted deployments from Kubernetes.

**Root Cause:** Desired state endpoint only returns active deployments; no deletion list.

**Files:**
- `backend/services/deployment/internal/agent_handlers.go`
- `backend/services/deployment/internal/agent_dto.go`
- `agents/platform-agent/internal/reconciler/reconciler.go`

**Remediation:**
1. Add `deleted_deployments` field to `DesiredStateResponse`
2. Include recently soft-deleted deployment IDs
3. Update reconciler to delete K8s resources for deleted deployments

**Effort:** 3-4 hours

---

## Medium Issues (16)

### API-MED-01: Missing updatedAt on Multiple Types

**Impact:** UI shows stale timestamps or missing fields.

**Files:**
- `frontend/src/types/api.ts` - Cluster, Application, Deployment, Project

**Remediation:**
1. Remove `updatedAt` from frontend types (backend doesn't return it)

**Effort:** 30 minutes

---

### API-MED-02: ProjectMember DTO Mismatch

**Impact:** Project member list displays incorrectly.

**Files:**
- `frontend/src/types/api.ts` (lines 97-104)

**Remediation:**
1. Update to match backend `MemberView` structure

**Effort:** 30 minutes

---

### API-MED-03: Heartbeat DTO Missing id Field

**Impact:** Heartbeat list may not render correctly.

**Files:**
- `frontend/src/types/api.ts` (lines 152-159)

**Remediation:**
1. Replace `id` with `clusterId` to match backend

**Effort:** 30 minutes

---

### API-MED-04: CreateOrganizationRequest Has Extra description

**Impact:** Request includes field backend ignores.

**Files:**
- `frontend/src/types/api.ts` (lines 61-65)

**Remediation:**
1. Remove `description` from request type

**Effort:** 15 minutes

---

### API-MED-05: Missing Project by Slug Endpoint

**Impact:** Project URL navigation fails.

**Files:**
- `frontend/src/lib/api/projects.ts`

**Remediation:**
1. Either implement backend endpoint or remove frontend method

**Effort:** 2 hours (if implementing) or 30 minutes (if removing)

---

### EVENT-MED-01: No correlationId/traceparent in Events

**Impact:** Cannot trace events across services for debugging.

**Files:**
- All service event emission points

**Remediation:**
1. Pass correlation ID from HTTP request context to event envelope
2. Update all `enqueue()` calls to include correlation options

**Effort:** 3-4 hours

---

### EVENT-MED-02: Missing cluster.updated Event

**Impact:** Audit trail incomplete for cluster metadata changes.

**Files:**
- `backend/services/cluster/internal/service.go`
- `backend/services/cluster/internal/events.go`

**Remediation:**
1. Emit `cluster.updated.v1` event in `UpdateCluster` method

**Effort:** 1 hour

---

### MIGRATE-MED-01: Soft-Delete Inconsistency

**Impact:** Different deletion patterns across services; inconsistent behavior.

**Root Cause:** Only secrets uses `deleted_at`; clusters use status-based; projects hard delete.

**Files:**
- Various migration and service files

**Remediation:**
1. Document the intentional differences
2. Or standardize on `deleted_at` pattern

**Effort:** 4-6 hours (if standardizing) or 1 hour (if documenting)

---

### AGENT-MED-01: Inconsistent Auth Status Codes

**Impact:** Agent error handling is confused by different status codes for same condition.

**Root Cause:** Cluster validator returns 403 for "not connected", deployment/secrets return 401.

**Files:**
- `backend/services/cluster/internal/agent_handlers.go`
- `backend/services/deployment/internal/agent_middleware.go`
- `backend/services/secrets/internal/agent_middleware.go`

**Remediation:**
1. Standardize: 401 for invalid credentials, 403 for valid creds but unauthorized

**Effort:** 1-2 hours

---

### AGENT-MED-02: Missing Agent Graceful Shutdown

**Impact:** In-progress reconciliation interrupted on restart; potential duplicate operations.

**Files:**
- `agents/platform-agent/internal/agent/agent.go`
- `agents/platform-agent/cmd/agent/main.go`

**Remediation:**
1. Add context cancellation handling
2. Save state on SIGTERM before exit

**Effort:** 2-3 hours

---

### API-MED-06: Auth Refresh Method Missing Body

**Impact:** Public `refreshToken()` method doesn't send required body.

**Files:**
- `frontend/src/lib/api/auth.ts` (lines 37-39)

**Remediation:**
1. Add `refreshToken` to request body

**Effort:** 30 minutes

---

### OPENAPI-MED-01: Auth Service Has No OpenAPI Spec

**Impact:** API documentation incomplete; no schema validation for auth endpoints.

**Files:**
- `backend/services/auth/api/` (missing)

**Remediation:**
1. Create `backend/services/auth/api/openapi.yaml`
2. Document all auth endpoints with schemas

**Effort:** 3-4 hours

---

### OPENAPI-MED-02: Gateway OpenAPI Missing Services

**Impact:** API documentation incomplete for cluster, deployment, secrets routes.

**Files:**
- `backend/services/gateway/api/openapi.yaml`

**Remediation:**
1. Add all cluster, deployment, secrets routes to gateway OpenAPI
2. Document agent routes

**Effort:** 2-3 hours

---

### TEST-MED-01: Test Fakes Out of Sync

**Impact:** Some tests may not accurately reflect production behavior.

**Files:**
- `backend/services/cluster/internal/service_test.go`
- `backend/services/deployment/internal/service_test.go`

**Remediation:**
1. Update `fakeOutbox` to match current `events.Outbox` interface
2. Fix type mismatches in test doubles

**Effort:** 1-2 hours

---

### DEPLOY-MED-01: Status Enum Documentation Inconsistency

**Impact:** Confusion about valid status values.

**Root Cause:** User handler accepts `"started"`, agent handler accepts `"deploying"`.

**Files:**
- `backend/services/deployment/internal/handlers.go` (line 249)
- `agents/platform-agent/README.md`

**Remediation:**
1. Deprecate `"started"` in user handler; use `"deploying"`
2. Update documentation

**Effort:** 1 hour

---

### DEPLOY-MED-02: No Deployment Status Transition Validation

**Impact:** Invalid status transitions could corrupt deployment state.

**Files:**
- `backend/libs/contracts/deploymentstatus/status.go`
- `backend/services/deployment/internal/agent_handlers.go`

**Remediation:**
1. Add `ValidTransition(from, to)` function
2. Validate transitions before applying

**Effort:** 2 hours

---

## Low Issues (8)

### API-LOW-01: Missing Labels in Cluster Frontend Type

**Impact:** Cluster labels not displayed in UI.

**Files:**
- `frontend/src/types/api.ts`

**Remediation:**
1. Add `labels?: Record<string, string>` to Cluster type

**Effort:** 15 minutes

---

### API-LOW-02: RegistrationToken Response Varies

**Impact:** Token field only present on create, may cause confusion.

**Files:**
- `frontend/src/types/api.ts`

**Remediation:**
1. Document that `token` is only returned on create

**Effort:** 15 minutes

---

### EVENT-LOW-01: Heartbeat Event Volume

**Impact:** High event volume for heartbeats floods NATS and audit.

**Files:**
- `backend/services/cluster/internal/service.go`

**Remediation:**
1. Sample heartbeat events (e.g., 1 in 10) or disable heartbeat events

**Effort:** 1 hour

---

### MIGRATE-LOW-01: Redundant Index on secrets(org_id)

**Impact:** Unnecessary storage overhead.

**Files:**
- `backend/migrations/secrets/0001_init.up.sql`

**Remediation:**
1. Remove redundant `secrets_org_id_idx` (covered by composite index)

**Effort:** 30 minutes

---

### OPENAPI-LOW-01: Inconsistent Security Scheme Names

**Impact:** Schema drift across services.

**Files:**
- Various OpenAPI specs

**Remediation:**
1. Standardize on `bearerAuth` (lowercase)

**Effort:** 1 hour

---

### OPENAPI-LOW-02: Inconsistent Server URLs

**Impact:** OpenAPI spec portability issues.

**Files:**
- Various OpenAPI specs

**Remediation:**
1. Use relative `/v1` in all specs

**Effort:** 30 minutes

---

### OPENAPI-LOW-03: Inconsistent OpenAPI Versions

**Impact:** Tooling compatibility issues.

**Files:**
- `backend/services/secrets/api/openapi.yaml` (uses 3.1.0, others use 3.0.3)

**Remediation:**
1. Standardize on 3.0.3 or upgrade all to 3.1.0

**Effort:** 30 minutes

---

### AGENT-LOW-01: Stale Test References

**Impact:** Tests reference deprecated methods.

**Files:**
- `agents/platform-agent/internal/controlplane/client_test.go`
- `agents/platform-agent/internal/controlplane/deployment_test.go`

**Remediation:**
1. Update tests to use current method signatures

**Effort:** 1-2 hours

---

## Remediation Priority Matrix

### Phase 1: Security (Days 1-3)

| Issue | Effort | Blocker |
|-------|--------|---------|
| SEC-CRIT-01: Auth service accounts authz | 3-4h | Yes |
| SEC-CRIT-02: Deployment status authz | 1-2h | Yes |
| SEC-CRIT-03: Cluster heartbeat route | 1h | Yes |
| SEC-CRIT-04: Force RLS on all tables | 2-3h | Yes |
| SEC-CRIT-05: Secrets event naming | 1-2h | Yes |
| SEC-HIGH-01: Project/secrets org role | 2-3h | No |
| SEC-HIGH-02: Suspended member check | 1h | No |

**Total Phase 1:** 11-16 hours

### Phase 2: API Contracts (Days 4-6)

| Issue | Effort | Blocker |
|-------|--------|---------|
| API-CRIT-01: List organizations | 2-3h | Yes |
| API-CRIT-02: Get org by slug | 2-3h | Yes |
| API-CRIT-03: List deployments | 2-3h | Yes |
| API-CRIT-04: Delete deployment | 2-3h | Yes |
| API-CRIT-05: Error envelope | 1h | Yes |
| API-CRIT-06: Pagination field | 1-2h | Yes |
| API-CRIT-07: Audit path | 30m | Yes |
| API-CRIT-08: Application path | 1-2h | Yes |
| API-CRIT-09: Logout body | 30m | Yes |

**Total Phase 2:** 13-18 hours

### Phase 3: Frontend DTOs (Days 7-8)

| Issue | Effort |
|-------|--------|
| API-HIGH-01 through API-HIGH-11 | 8-10h |

**Total Phase 3:** 8-10 hours

### Phase 4: Agent & Events (Days 9-10)

| Issue | Effort |
|-------|--------|
| AGENT-HIGH-01 through AGENT-HIGH-03 | 4-6h |
| EVENT-HIGH-01 | 15m |
| LIFECYCLE-HIGH-01, HIGH-02 | 5-7h |

**Total Phase 4:** 9-13 hours

### Phase 5: Medium & Low (Days 11-14)

| Issue | Effort |
|-------|--------|
| All Medium issues | 20-28h |
| All Low issues | 5-8h |

**Total Phase 5:** 25-36 hours

---

## Readiness Scores

### Current State

| Metric | Score | Rationale |
|--------|-------|-----------|
| **MVP Readiness** | 45% | Critical security and API gaps block basic user journey |
| **Beta Readiness** | 35% | Additional DTO mismatches and agent issues |
| **Production Readiness** | 25% | Needs all issues fixed plus observability/ops |

### After Phase 1 (Security)

| Metric | Score |
|--------|-------|
| **MVP Readiness** | 55% |
| **Beta Readiness** | 45% |
| **Production Readiness** | 40% |

### After Phase 2 (API Contracts)

| Metric | Score |
|--------|-------|
| **MVP Readiness** | 75% |
| **Beta Readiness** | 60% |
| **Production Readiness** | 50% |

### After Phase 3 (Frontend DTOs)

| Metric | Score |
|--------|-------|
| **MVP Readiness** | 90% |
| **Beta Readiness** | 75% |
| **Production Readiness** | 55% |

### After Phase 4 (Agent & Events)

| Metric | Score |
|--------|-------|
| **MVP Readiness** | 95% |
| **Beta Readiness** | 85% |
| **Production Readiness** | 65% |

### After Phase 5 (Medium & Low)

| Metric | Score |
|--------|-------|
| **MVP Readiness** | 100% |
| **Beta Readiness** | 95% |
| **Production Readiness** | 80% |

---

## Final Readiness Assessment

| Milestone | Current | After Stabilization | Gap |
|-----------|---------|---------------------|-----|
| **MVP Readiness** | 45% | 100% | 78-102h effort |
| **Beta Readiness** | 35% | 95% | Same effort |
| **Production Readiness** | 25% | 80% | +20% needs ops/monitoring |

### Production Readiness Gap (Remaining 20%)

Items NOT in this stabilization plan but required for production:

1. **Observability** - Prometheus metrics, Grafana dashboards, alerting
2. **CI/CD Pipeline** - GitHub Actions, automated testing
3. **Backup Strategy** - Database backup/restore procedures
4. **Key Rotation** - Master encryption key rotation mechanism
5. **Load Testing** - Performance baseline and capacity planning
6. **Runbooks** - Operational procedures documentation

These are operational concerns, not stabilization items, and should be addressed in a separate production readiness plan.

---

## Conclusion

The platform requires **78-102 hours of stabilization work** across 5 phases to reach MVP readiness. The most critical issues are:

1. **3 cross-tenant authorization bypasses** (SEC-CRIT-01, 02, 03)
2. **RLS enforcement gap** (SEC-CRIT-04)
3. **6 missing/mismatched API endpoints** (API-CRIT-01 through 09)

**Recommendation:** Do NOT expose this platform to external users until at least Phases 1 and 2 are complete. The security issues are exploitable and could lead to data exfiltration between tenants.

**Timeline:**
- Phase 1-2 (Security + API): 3-4 days
- Phase 3-4 (Frontend + Agent): 3-4 days
- Phase 5 (Polish): 3-4 days
- **Total:** 9-12 working days
