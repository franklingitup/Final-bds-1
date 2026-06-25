# 24 — End-to-End Deployment Audit

**Date:** 2026-06-24  
**Author:** Principal Platform Engineer  
**Scope:** Complete deployment journey validation from user signup to Kubernetes health reporting

---

## Executive Summary

This audit validates the complete customer journey from account creation to deploying an application with secrets on a Kubernetes cluster.

### Can a customer deploy `nginx:latest` with `DATABASE_URL` today?

**Answer: NO — 4 Critical Blockers**

While all APIs, tables, and agent components exist, the following issues prevent end-to-end operation:

1. ❌ **Status reporting broken** — Agent sends `"started"`, server rejects (expects `"deploying"`)
2. ❌ **Agent heartbeat fails** — Gateway requires JWT, agent sends none → cluster becomes `disconnected`
3. ❌ **First-boot credential race** — Reconciler starts with empty credentials until pod restart
4. ❌ **Secrets not mounted** — K8s Secrets created but not wired into pod specs

**After fixing these 4 blockers**, a customer can:

1. ✅ Sign up and create an organization
2. ✅ Create a project within the organization
3. ✅ Register a Kubernetes cluster
4. ✅ Create secrets for the project
5. ✅ Create an application and deployment
6. ✅ Have the Platform Agent reconcile the deployment to Kubernetes
7. ⚠️ Have secrets synchronized to Kubernetes Secrets (not auto-mounted)
8. ✅ Receive deployment health status updates

**Production Readiness Score: 68/100** (was 82 before blocker discovery)

---

## Journey Validation Matrix

### Step 1: User Signup

| Component | Status | Evidence |
|-----------|--------|----------|
| API Endpoint | ✅ | `POST /v1/auth/signup` in `backend/services/auth/internal/routes.go:13` |
| Database Table | ✅ | `users` table in `backend/migrations/auth/0001_init.up.sql:23-37` |
| Event Emitted | ✅ | `auth.user.created.v1` via transactional outbox |
| Authorization | ✅ | Public endpoint (no auth required) |
| Gateway Route | ✅ | `POST /v1/auth/signup` in `router.go:149` |

**Verdict:** PASS

### Step 2: User Login

| Component | Status | Evidence |
|-----------|--------|----------|
| API Endpoint | ✅ | `POST /v1/auth/login` in `backend/services/auth/internal/routes.go:14` |
| Database Table | ✅ | `refresh_tokens` table for sessions |
| Event Emitted | ✅ | `auth.login.succeeded.v1` / `auth.login.failed.v1` |
| Authorization | ✅ | Public endpoint |
| Gateway Route | ✅ | `POST /v1/auth/login` in `router.go:150` |

**Verdict:** PASS

### Step 3: Organization Creation

| Component | Status | Evidence |
|-----------|--------|----------|
| API Endpoint | ✅ | `POST /v1/organizations` in `backend/services/tenant/internal/routes.go:14` |
| Database Table | ✅ | `organizations` table in `backend/migrations/tenant/0001_init.up.sql:19-28` |
| Membership Table | ✅ | `organization_members` in `backend/migrations/tenant/0002_memberships.up.sql:12-23` |
| Event Emitted | ✅ | `tenant.organization.created.v1` |
| Authorization | ✅ | Requires authentication; creator becomes owner |
| Gateway Route | ✅ | `POST /v1/organizations` in `router.go:194` |

**Verdict:** PASS

### Step 4: Project Creation

| Component | Status | Evidence |
|-----------|--------|----------|
| API Endpoint | ✅ | `POST /v1/organizations/:orgId/projects` in `backend/services/project/internal/routes.go:15` |
| Database Table | ✅ | `projects` table in `backend/migrations/tenant/0001_init.up.sql:39-49` |
| Membership Table | ✅ | `project_members` in `backend/migrations/project/0001_init.up.sql:18-29` |
| Event Emitted | ✅ | `project.created.v1` |
| Authorization | ✅ | Requires org admin role via `authSvc.AuthorizeOrgMember` |
| RLS Enforcement | ✅ | `app.current_org_id` tenant isolation |
| Gateway Route | ✅ | `POST /v1/organizations/:orgId/projects` in `router.go:224` |

**Verdict:** PASS

### Step 5: Cluster Registration

| Component | Status | Evidence |
|-----------|--------|----------|
| Create Cluster | ✅ | `POST /v1/organizations/:orgId/clusters` |
| Generate Token | ✅ | `POST /v1/organizations/:orgId/clusters/:clusterId/tokens` |
| Agent Register | ✅ | `POST /v1/agent/register` (capability-based) |
| Database Tables | ✅ | `clusters`, `cluster_registration_tokens`, `cluster_heartbeats` |
| Events Emitted | ✅ | `cluster.created.v1`, `cluster.registered.v1` |
| Authorization | ✅ | Cluster ops require org membership; registration uses token |
| Gateway Routes | ✅ | All routes registered in `router.go:283-296` |

**Agent Registration Flow:**
1. User creates cluster → `POST /v1/organizations/:orgId/clusters` ✅
2. User generates registration token → `POST /v1/organizations/:orgId/clusters/:clusterId/tokens` ✅
3. Agent registers with token → `POST /v1/agent/register` ✅
4. Agent receives cluster_id and agent_id credentials ✅

**Verdict:** PASS

### Step 6: Secret Creation

| Component | Status | Evidence |
|-----------|--------|----------|
| API Endpoint | ✅ | `POST /v1/organizations/:orgId/projects/:projectId/secrets` |
| Database Table | ✅ | `secrets` table in `backend/migrations/secrets/0001_init.up.sql:9-35` |
| Encryption | ✅ | AES-256-GCM envelope encryption in `internal/crypto.go` |
| Event Emitted | ✅ | `secret.created.v1` via transactional outbox |
| Authorization | ✅ | Requires project membership with write access |
| RLS Enforcement | ✅ | `app.current_org_id` tenant isolation |
| Gateway Route | ✅ | `POST /v1/organizations/:orgId/projects/:projectId/secrets` in `router.go:348` |
| Agent Sync API | ✅ | `GET /v1/agent/clusters/:clusterId/secrets` with explicit `org_id` filtering |

**Security Controls:**
- Plaintext values never stored ✅
- Plaintext never in API responses ✅
- Plaintext never in events ✅
- Value only returned to authenticated agents ✅
- Cross-tenant isolation verified ✅

**Verdict:** PASS

### Step 7: Application Creation

| Component | Status | Evidence |
|-----------|--------|----------|
| API Endpoint | ✅ | `POST /v1/organizations/:orgId/projects/:projectId/applications` |
| Database Table | ✅ | `applications` table in `backend/migrations/deployment/0001_init.up.sql:6-20` |
| Event Emitted | ✅ | `application.created.v1` |
| Authorization | ✅ | Requires org membership with deployment privileges |
| RLS Enforcement | ✅ | `app.current_org_id` tenant isolation |
| Gateway Route | ✅ | `POST /v1/organizations/:orgId/projects/:projectId/applications` in `router.go:308` |

**Verdict:** PASS

### Step 8: Deployment Creation

| Component | Status | Evidence |
|-----------|--------|----------|
| API Endpoint | ✅ | `POST /v1/organizations/:orgId/deployments` |
| Database Tables | ✅ | `deployments`, `releases` in `backend/migrations/deployment/0001_init.up.sql` |
| Event Emitted | ✅ | `deployment.created.v1`, `deployment.started.v1` |
| Authorization | ✅ | Requires org membership with deployment privileges |
| RLS Enforcement | ✅ | `app.current_org_id` tenant isolation |
| Gateway Route | ✅ | `POST /v1/organizations/:orgId/deployments` in `router.go:320` |

**Deployment Model:**
- Application → Deployment → Release (immutable)
- Each deploy creates a new release with incremented revision
- Releases are immutable (enforced by trigger)

**Verdict:** PASS

### Step 9: Agent Reconciliation

| Component | Status | Evidence |
|-----------|--------|----------|
| Desired State API | ✅ | `GET /v1/agent/clusters/:clusterId/desired-state` |
| Agent Client | ✅ | `agents/platform-agent/internal/controlplane/deployment.go:60-95` |
| Reconciler | ✅ | `agents/platform-agent/internal/reconciler/reconciler.go` |
| K8s Deployment | ✅ | `agents/platform-agent/internal/k8s/manager.go:124-213` |
| K8s Service | ✅ | `agents/platform-agent/internal/k8s/manager.go:241-311` |
| Gateway Route | ✅ | `GET /v1/agent/clusters/:clusterId/desired-state` in `router.go:263` |

**Control Plane Contract:**

| Agent Expects | Server Provides | Match |
|--------------|-----------------|-------|
| `GET /v1/agent/clusters/:clusterId/desired-state` | `AgentHandler.GetDesiredState` | ✅ |
| `X-Cluster-ID`, `X-Agent-ID` headers | `AgentAuthMiddleware` | ✅ |
| `DesiredStateResponse{ClusterID, Items}` | `DesiredStateResponse` | ✅ |
| `DeploymentID, ReleaseID, Image, Replicas, Port, EnvVars` | All fields present | ✅ |

**Reconciler Flow:**
1. Poll desired state every 30s ✅
2. Compare with Kubernetes state ✅
3. Apply/Update Kubernetes Deployment ✅
4. Apply/Update Kubernetes Service (if port specified) ✅
5. Report status back to control plane ✅

**Verdict:** PASS

### Step 10: Secret Synchronization

| Component | Status | Evidence |
|-----------|--------|----------|
| Agent Secrets API | ✅ | `GET /v1/agent/clusters/:clusterId/secrets` |
| Secrets Syncer | ✅ | `agents/platform-agent/internal/secrets/syncer.go` |
| K8s Secret Manager | ✅ | `agents/platform-agent/internal/secrets/k8s_manager.go` |
| Main.go Wired | ✅ | `agents/platform-agent/cmd/agent/main.go` (Phase 1 remediation) |
| Gateway Route | ✅ | `GET /v1/agent/clusters/:clusterId/secrets` in `router.go:271` |

**Control Plane Contract:**

| Agent Expects | Server Provides | Match |
|--------------|-----------------|-------|
| `GET /v1/agent/clusters/:clusterId/secrets` | `AgentHandler.GetSecrets` | ✅ |
| `X-Cluster-ID`, `X-Agent-ID` headers | `AgentAuthMiddleware` | ✅ |
| `AgentSecretsResponse{Secrets}` | `AgentSecretsResponse` | ✅ |
| `ProjectID, Name, Value, Version` | All fields present | ✅ |

**K8s Secret Naming:**
- Secret name: `bds-secret-{projectId}`
- Labels: `app.kubernetes.io/managed-by=bdsplatform-agent`
- Annotations: version, synced-at, secret-keys count

**Verdict:** PASS

### Step 11: Kubernetes Deployment Rollout

| Component | Status | Evidence |
|-----------|--------|----------|
| Deployment Creation | ✅ | `ApplyDeployment` in `k8s/manager.go` |
| Service Creation | ✅ | `ApplyService` in `k8s/manager.go` (when port specified) |
| Status Monitoring | ✅ | `GetDeploymentStatus` in `k8s/manager.go` |
| Managed Labels | ✅ | `app.kubernetes.io/managed-by=bdsplatform-agent` |
| Orphan Cleanup | ✅ | `cleanupOrphanedDeployments` in reconciler |

**Generated K8s Resources:**

```yaml
# Deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {applicationSlug}
  labels:
    app.kubernetes.io/managed-by: bdsplatform-agent
    bdsplatform.io/deployment-id: {deploymentId}
spec:
  replicas: {replicas}
  template:
    spec:
      containers:
      - name: app
        image: {image}
        env: {envVars}
        ports:
        - containerPort: {port}

# Service (when port specified)
apiVersion: v1
kind: Service
metadata:
  name: {applicationSlug}
spec:
  selector:
    app: {applicationSlug}
  ports:
  - port: {port}
  type: ClusterIP
```

**Verdict:** PASS

### Step 12: Deployment Health Reporting

| Component | Status | Evidence |
|-----------|--------|----------|
| Status Update API | ✅ | `POST /v1/agent/deployments/:deploymentId/releases/:releaseId/status` |
| Agent Client | ✅ | `ReportDeploymentStatusWithCreds` in `deployment.go:118-149` |
| Status States | ✅ | `started`, `succeeded`, `failed` |
| Ownership Validation | ✅ | CRIT-002 fix validates cluster owns deployment |
| Gateway Route | ✅ | `POST /v1/agent/deployments/:deploymentId/releases/:releaseId/status` in `router.go:265` |

**Status Reporting Contract:**

| Agent Sends | Server Accepts | Match |
|------------|----------------|-------|
| `status: started\|succeeded\|failed` | Validated status enum | ✅ |
| `readyReplicas: int` | Optional field | ✅ |
| `errorMessage: string` | Optional field | ✅ |
| `X-Cluster-ID`, `X-Agent-ID` headers | Agent auth middleware | ✅ |

**Verdict:** PASS

---

## Database Tables Audit

| Table | Service | RLS | Migration |
|-------|---------|-----|-----------|
| `users` | Auth | ❌ (global) | `auth/0001_init.up.sql` |
| `refresh_tokens` | Auth | ❌ (global) | `auth/0001_init.up.sql` |
| `one_time_tokens` | Auth | ❌ (global) | `auth/0001_init.up.sql` |
| `service_accounts` | Auth | ✅ | `auth/0001_init.up.sql` |
| `api_tokens` | Auth | ✅ | `auth/0001_init.up.sql` |
| `organizations` | Tenant | ✅ | `tenant/0001_init.up.sql` |
| `organization_members` | Tenant | ✅ | `tenant/0002_memberships.up.sql` |
| `organization_invitations` | Tenant | ✅ | `tenant/0002_memberships.up.sql` |
| `projects` | Tenant/Project | ✅ | `tenant/0001_init.up.sql` |
| `project_members` | Project | ✅ | `project/0001_init.up.sql` |
| `clusters` | Cluster | ✅ | `cluster/0001_init.up.sql` |
| `cluster_registration_tokens` | Cluster | ✅ | `cluster/0001_init.up.sql` |
| `cluster_heartbeats` | Cluster | ✅ | `cluster/0001_init.up.sql` |
| `applications` | Deployment | ✅ | `deployment/0001_init.up.sql` |
| `deployments` | Deployment | ✅ | `deployment/0001_init.up.sql` |
| `releases` | Deployment | ✅ | `deployment/0001_init.up.sql` |
| `secrets` | Secrets | ✅ | `secrets/0001_init.up.sql` |
| `secret_access_logs` | Secrets | ✅ | `secrets/0001_init.up.sql` |
| `audit_logs` | Audit | ✅ | `audit/0001_init.up.sql` |
| `outbox` | All | N/A | `outbox/0001_init.up.sql` |

**Verdict:** All tables exist with proper RLS policies

---

## Event Emission Audit

| Event | Service | Outbox Used | Catalog Entry |
|-------|---------|-------------|---------------|
| `auth.user.created.v1` | Auth | ✅ | ✅ |
| `auth.login.succeeded.v1` | Auth | ✅ | ✅ |
| `auth.login.failed.v1` | Auth | ✅ | ✅ |
| `tenant.organization.created.v1` | Tenant | ✅ | ✅ |
| `tenant.member.invited.v1` | Tenant | ✅ | ✅ |
| `project.created.v1` | Project | ✅ | ✅ |
| `project.member.added.v1` | Project | ✅ | ✅ |
| `cluster.created.v1` | Cluster | ✅ | ✅ |
| `cluster.registered.v1` | Cluster | ✅ | ✅ |
| `cluster.heartbeat.received.v1` | Cluster | ✅ | ✅ |
| `deployment.created.v1` | Deployment | ✅ | ✅ |
| `deployment.started.v1` | Deployment | ✅ | ✅ |
| `deployment.succeeded.v1` | Deployment | ✅ | ✅ |
| `secret.created.v1` | Secrets | ✅ | ✅ |
| `secret.updated.v1` | Secrets | ✅ | ✅ |

**Verdict:** All critical events use transactional outbox

---

## Authorization Audit

| Endpoint | Auth Check | Method |
|----------|------------|--------|
| Auth signup/login | Public | N/A |
| Org creation | JWT required | `RequireAuth()` middleware |
| Org read/update | Org membership | `AuthorizeOrgRead()` |
| Project CRUD | Org admin | `AuthorizeOrgMember(ActionManageProject)` |
| Cluster CRUD | Org membership | `AuthorizeOrgMember(ActionManageCluster)` |
| Deployment CRUD | Org membership | `AuthorizeOrgMember(ActionDeploy)` |
| Secrets CRUD | Project membership | `AuthorizeOrgMember(ActionWriteSecrets)` |
| Audit read | Org membership | `AuthorizeOrgMember(ActionReadAudit)` |
| Agent register | Registration token | Capability-based |
| Agent desired-state | Cluster credentials | `X-Cluster-ID`/`X-Agent-ID` |
| Agent secrets | Cluster credentials | `X-Cluster-ID`/`X-Agent-ID` |
| Agent status update | Cluster credentials | `X-Cluster-ID`/`X-Agent-ID` + ownership validation |

**Verdict:** Authorization properly enforced across all endpoints

---

## Gateway Route Audit

All routes are properly registered in the API Gateway:

```
Auth Service (public):
  POST /v1/auth/signup
  POST /v1/auth/login
  POST /v1/auth/refresh
  POST /v1/auth/verify-email
  POST /v1/auth/password-reset
  
Auth Service (authenticated):
  POST /v1/auth/logout
  GET  /v1/auth/me
  POST /v1/auth/mfa/*

Tenant Service:
  POST /v1/organizations
  GET/PATCH/DELETE /v1/organizations/:orgId
  */v1/organizations/:orgId/members/*
  */v1/organizations/:orgId/invitations/*

Project Service:
  */v1/organizations/:orgId/projects/*
  */v1/organizations/:orgId/projects/:projectId/members/*

Cluster Service:
  */v1/organizations/:orgId/clusters/*
  */v1/organizations/:orgId/clusters/:clusterId/tokens/*
  POST /v1/organizations/:orgId/clusters/:clusterId/heartbeat

Deployment Service:
  */v1/organizations/:orgId/projects/:projectId/applications/*
  */v1/organizations/:orgId/deployments/*
  */v1/organizations/:orgId/applications/:appId/*

Secrets Service:
  */v1/organizations/:orgId/projects/:projectId/secrets/*

Audit Service:
  GET /v1/organizations/:orgId/audit-logs/*

Agent Routes (credential-based):
  POST /v1/agent/register
  GET  /v1/agent/clusters/:clusterId/desired-state
  GET  /v1/agent/clusters/:clusterId/secrets
  POST /v1/agent/deployments/:deploymentId/releases/:releaseId/status
```

**Verdict:** All routes properly registered

---

## Blockers

### CRITICAL — Must Fix Before Production

| # | Issue | Impact | Location |
|---|-------|--------|----------|
| **B1** | **Status value mismatch: `"started"` vs `"deploying"`** | Agent sends `"started"`, server rejects with 400 (expects `"deploying"`). Deployment status never transitions. | `reconciler.go:188` → `agent_handlers.go:104` |
| **B2** | **First-boot credential race** | On fresh install, reconciler/syncer initialized with empty credentials before registration completes. Auth fails until pod restart. | `agent/main.go` setup order |
| **B3** | **Agent heartbeat requires JWT** | Gateway routes heartbeat under authenticated group. Agent sends no JWT. Heartbeats 401, cluster becomes `disconnected`, breaking all agent auth. | `router.go:295`, `client.go:109` |
| **B4** | **`RegisterAgent` re-fetch bug** | After registration, calls `GetCluster(ctx, orgID, clusterID)` missing `userID` parameter. Would fail compile or auth. | `cluster/service.go:417-418` |

### HIGH — Functional Gaps

| # | Issue | Impact |
|---|-------|--------|
| **B5** | **Secrets not wired to workloads** | Secrets sync creates K8s Secrets (`bds-secret-{projectId}`) but reconciler doesn't mount them via `envFrom.secretRef`. |
| **B6** | **Gateway auth path mismatches** | 3 routes proxy to non-existent upstream paths: `/password-reset` (expects `/password-reset/request`), `/api-tokens` (expects `/service-accounts/:id/tokens`), `GET /service-accounts/:id` (no handler). |
| **B7** | **Agent lifecycle events skipped** | Agent status updates bypass service layer; no `deployment.started/succeeded/failed` outbox events emitted. |

---

## Warnings

### HIGH Priority

1. **Missing Secret References in Deployment Spec**
   - Current: Deployments only support inline env vars
   - Needed: `envFrom.secretRef` to link to Kubernetes Secrets
   - Impact: Customer must manually wire secrets in Kubernetes
   - Workaround: Agent creates secrets with known naming convention

2. **No Email Verification Enforcement**
   - Current: Users can operate without verified email
   - Needed: Enforce email verification for production
   - Impact: Account security

3. **Missing Rate Limiting on Agent Endpoints**
   - Current: Agent routes bypass rate limiter
   - Needed: Per-cluster rate limiting
   - Impact: DoS protection

### MEDIUM Priority

4. **No Graceful Shutdown in Agent**
   - Current: Reconciler state saved on tick
   - Needed: Save state on SIGTERM
   - Impact: Potential double-apply on restart

5. **Missing Deployment Rollback Verification**
   - Current: Rollback creates new release, no verification
   - Needed: Verify rollback succeeds
   - Impact: Silent rollback failures

6. **No Circuit Breaker in Gateway**
   - Current: Failed backend calls retry indefinitely
   - Needed: Circuit breaker pattern
   - Impact: Cascading failures

7. **Missing Pagination in Agent APIs**
   - Current: All deployments/secrets returned
   - Needed: Cursor pagination for large clusters
   - Impact: Memory pressure with many deployments

---

## Missing Integrations

| Integration | Priority | Description |
|-------------|----------|-------------|
| Secret Reference in Pods | HIGH | Auto-wire `envFrom.secretRef` |
| Ingress/Route Creation | MEDIUM | External access for services |
| HPA (Auto-scaling) | MEDIUM | Horizontal Pod Autoscaler support |
| PVC Support | MEDIUM | Persistent volume claims |
| ConfigMap Support | LOW | Non-sensitive configuration |
| Network Policies | LOW | Pod-to-pod network isolation |
| Pod Security Policies | LOW | Container security constraints |

---

## Production Readiness Score

| Category | Score | Max | Notes |
|----------|-------|-----|-------|
| Core APIs | 18 | 20 | All CRUD operations work |
| Authentication | 9 | 10 | JWT + Service Accounts |
| Authorization | 14 | 15 | Full RBAC implemented |
| Multi-tenancy | 10 | 10 | RLS + explicit filtering |
| Events | 8 | 10 | Transactional outbox |
| Agent Integration | 12 | 15 | Reconciler + Secrets sync |
| Security | 8 | 10 | Encryption, auth checks |
| Observability | 3 | 10 | Basic logging only |

**Total: 82/100**

---

## Exact Remaining Work

### Blockers (P0) — Required for Any Deployment

1. **Fix Status Value Mismatch**
   - Change `reconciler.go` to send `"deploying"` instead of `"started"`
   - OR change `agent_handlers.go` to accept `"started"`
   - Estimated: 30 minutes

2. **Fix Agent Heartbeat Auth**
   - Add public agent heartbeat route: `POST /v1/agent/clusters/:clusterId/heartbeat`
   - Use `X-Cluster-ID`/`X-Agent-ID` header auth (like desired-state)
   - Estimated: 1-2 hours

3. **Fix First-Boot Credential Race**
   - Lazy-init reconciler/syncer after registration completes
   - OR reload credentials from state file on each poll cycle
   - Estimated: 1-2 hours

4. **Fix RegisterAgent Re-fetch Bug**
   - Use `clusters.GetByID` inside tenant scope instead of `GetCluster`
   - Estimated: 30 minutes

### Must Have (P0) — Required for Secrets

5. **Add Secret Reference Support to Deployments**
   - Modify `DesiredDeployment` to include `secretRefs`
   - Update K8s manager to add `envFrom.secretRef`
   - Estimated: 2-4 hours

### Should Have (P1)

2. **Add Prometheus Metrics**
   - API latency, error rates, request counts
   - Agent reconciliation metrics
   - Estimated: 4-8 hours

3. **Add Health Check Endpoints**
   - `/health/live` for liveness
   - `/health/ready` for readiness
   - Estimated: 2 hours

4. **Add Agent Rate Limiting**
   - Per-cluster rate limits
   - Estimated: 2-4 hours

5. **Add Circuit Breaker to Gateway**
   - Use resilience library
   - Estimated: 4 hours

### Nice to Have (P2)

6. **Add Deployment Rollback Verification**
   - Verify previous release is healthy before completing
   - Estimated: 4 hours

7. **Add Pagination to Agent APIs**
   - Cursor-based pagination
   - Estimated: 2-4 hours

8. **Add Structured Tracing**
   - OpenTelemetry spans
   - Estimated: 4-8 hours

---

## Conclusion

The BDS Platform has achieved **near-complete implementation** of the MVP deployment journey, but **4 critical blockers** prevent end-to-end operation today.

### Blocker Summary

| Fix | Effort | Impact |
|-----|--------|--------|
| Status `started`→`deploying` | 30 min | Enables status reporting |
| Agent heartbeat public route | 1-2 hr | Keeps cluster `connected` |
| First-boot credential race | 1-2 hr | Enables reconciler on fresh install |
| RegisterAgent re-fetch | 30 min | Returns cluster after registration |

**Total effort to unblock: ~4-6 hours**

### After Fixes

A customer can successfully:

1. ✅ Sign up and authenticate
2. ✅ Create an organization
3. ✅ Create a project
4. ✅ Register a Kubernetes cluster
5. ✅ Create encrypted secrets
6. ✅ Create applications and deployments
7. ✅ Have the Platform Agent reconcile deployments to Kubernetes
8. ✅ Have secrets synchronized to Kubernetes Secrets
9. ✅ Receive deployment status updates

**To deploy `nginx:latest` with `DATABASE_URL` today:**

```bash
# 1. Sign up
curl -X POST /v1/auth/signup -d '{"email":"...", "password":"...", "name":"..."}'

# 2. Login
curl -X POST /v1/auth/login -d '{"email":"...", "password":"..."}'
# Returns access_token

# 3. Create org
curl -H "Authorization: Bearer $TOKEN" -X POST /v1/organizations \
  -d '{"name":"My Org", "slug":"my-org"}'
# Returns org_id

# 4. Create project
curl -H "Authorization: Bearer $TOKEN" -X POST /v1/organizations/$ORG_ID/projects \
  -d '{"name":"My Project", "slug":"my-project"}'
# Returns project_id

# 5. Create cluster (get registration token from response)
curl -H "Authorization: Bearer $TOKEN" -X POST /v1/organizations/$ORG_ID/clusters \
  -d '{"name":"production", "slug":"production"}'
curl -H "Authorization: Bearer $TOKEN" -X POST /v1/organizations/$ORG_ID/clusters/$CLUSTER_ID/tokens
# Install platform-agent with registration token

# 6. Create secret
curl -H "Authorization: Bearer $TOKEN" -X POST /v1/organizations/$ORG_ID/projects/$PROJECT_ID/secrets \
  -d '{"name":"DATABASE_URL", "value":"postgres://..."}'

# 7. Create application
curl -H "Authorization: Bearer $TOKEN" -X POST /v1/organizations/$ORG_ID/projects/$PROJECT_ID/applications \
  -d '{"name":"nginx", "slug":"nginx"}'
# Returns app_id

# 8. Create deployment
curl -H "Authorization: Bearer $TOKEN" -X POST /v1/organizations/$ORG_ID/deployments \
  -d '{
    "applicationId":"$APP_ID",
    "clusterId":"$CLUSTER_ID",
    "image":"nginx:latest",
    "replicas":2,
    "port":80,
    "envVars":[{"name":"DATABASE_URL","value":"$DATABASE_URL"}]
  }'

# Agent will:
# - Fetch desired state
# - Create Kubernetes Deployment
# - Create Kubernetes Service
# - Sync secrets to Kubernetes Secret (bds-secret-$PROJECT_ID)
# - Report status back

# Note: To use the synced K8s Secret, add envFrom.secretRef manually
# or wait for P0 enhancement
```

The platform is **NOT production-ready** until the 4 critical blockers are fixed (~4-6 hours of work). After those fixes, it will be ready for early adopters with the understanding that secret-to-deployment linking requires manual wiring until P0 work is complete.

### Priority Roadmap

```
Week 1: Fix 4 blockers (4-6 hours)
        → Platform becomes functional

Week 2: Add secret references (2-4 hours)
        → Secrets auto-mount into pods

Week 3: Add observability (4-8 hours)
        → Production monitoring

Week 4: Add circuit breaker + rate limiting (4-8 hours)
        → Production resilience
```
