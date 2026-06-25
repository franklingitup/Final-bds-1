# 16 — Deployment Readiness Audit

**Document Version:** 1.0  
**Audit Date:** June 2026  
**Auditor Role:** Principal Platform Architect & Kubernetes Platform Engineer

---

## Executive Summary

This audit evaluates whether a customer can successfully deploy `nginx:latest` into a registered cluster using the current platform implementation.

### Critical Finding

**Answer: NO — A customer CANNOT successfully deploy `nginx:latest` today.**

The deployment workflow is **BROKEN** due to a critical API contract mismatch between the Deployment Service and the Platform Agent. The agent expects a response schema that the Deployment Service does not provide.

---

## A. Working Components

### ✅ Application Creation
- **API Route:** `POST /v1/organizations/:orgId/projects/:projectId/applications`
- **Database:** `applications` table with RLS policy
- **Events:** `application.created.v1` emitted via transactional outbox
- **Gateway:** Route properly proxied
- **Validation:** Name (2-64 chars), slug (DNS-compatible), runtime type

### ✅ Deployment Creation
- **API Route:** `POST /v1/organizations/:orgId/deployments`
- **Database:** `deployments` and `releases` tables with RLS policies
- **Events:** `deployment.created.v1` emitted via transactional outbox
- **FK Constraint:** `cluster_id` enforced (prevents deployment to non-existent cluster)

### ✅ Status Update Endpoint
- **API Route:** `POST /v1/organizations/:orgId/deployments/:deploymentId/releases/:releaseId/status`
- **Agent Client:** Correctly implements `ReportDeploymentStatus`
- **Service:** `MarkDeploymentStarted`, `MarkDeploymentSucceeded`, `MarkDeploymentFailed` implemented

### ✅ Database Schema
- Applications, Deployments, Releases tables properly designed
- RLS policies on all tables
- Immutability trigger on releases
- Proper foreign key constraints

### ✅ Event Contract
- Events follow naming convention
- No duplicate envelope metadata
- No sensitive data in payloads
- Contract tests exist

### ✅ Kubernetes Resource Manager
- Creates Deployments with proper labels/annotations
- Creates Services when port is specified
- Handles idempotent apply
- Drift detection implemented

---

## B. Missing Components

### ❌ B1. Desired State Response Schema (CRITICAL BLOCKER)

**Agent expects** (`controlplane.DesiredDeployment`):
```go
type DesiredDeployment struct {
    DeploymentID    string       `json:"deploymentId"`     // ❌ API returns "id"
    ReleaseID       string       `json:"releaseId"`        // ❌ NOT RETURNED
    ApplicationName string       `json:"applicationName"`  // ❌ NOT RETURNED
    ApplicationSlug string       `json:"applicationSlug"`  // ❌ NOT RETURNED
    Image           string       `json:"image"`            // ✅
    Replicas        int          `json:"replicas"`         // ✅
    Port            *int         `json:"port"`             // ✅
    EnvVars         []EnvVar     `json:"envVars"`          // ✅
    ResourceRequests *ResourceSpec `json:"resourceRequests"` // ❌ FLAT vs NESTED
    ResourceLimits   *ResourceSpec `json:"resourceLimits"`   // ❌ FLAT vs NESTED
    Revision        int          `json:"revision"`         // ❌ NOT RETURNED
    Status          string       `json:"status"`           // ✅
}
```

**Deployment Service returns** (`DeploymentView`):
```go
type DeploymentView struct {
    ID              string    `json:"id"`                  // ❌ Should be "deploymentId"
    ApplicationID   string    `json:"applicationId"`       // ❌ Agent needs name/slug
    // NO releaseId
    // NO applicationName
    // NO applicationSlug
    // NO revision
    CPURequest      string    `json:"cpuRequest"`          // ❌ Should be nested
    MemoryRequest   string    `json:"memoryRequest"`       // ❌ Should be nested
    // ...
}
```

### ❌ B2. Release ID in Cluster Deployments Response
The agent needs `releaseId` to report status back, but `ListDeploymentsByCluster` doesn't include it.

### ❌ B3. Application Details Join
The agent uses `ApplicationSlug` as the Kubernetes resource name, but the API doesn't join Application data.

### ❌ B4. Revision in Response
Agent uses revision for change detection, but it's not returned in the cluster deployments endpoint.

### ❌ B5. Cluster Status Validation
Deployment Service doesn't verify cluster is `connected` before accepting deployments.

---

## C. API Contract Mismatches

| Field | Agent Expects | Service Returns | Impact |
|-------|--------------|-----------------|--------|
| `deploymentId` | `deploymentId` | `id` | Agent can't correlate |
| `releaseId` | Required | Not returned | Can't report status |
| `applicationName` | Required | Not returned | K8s labeling fails |
| `applicationSlug` | Required | Not returned | K8s resource name fails |
| `revision` | Required | Not returned | Change detection fails |
| `resourceRequests` | `{cpu, memory}` | `cpuRequest` (flat) | JSON unmarshal fails |
| `resourceLimits` | `{cpu, memory}` | `cpuLimit` (flat) | JSON unmarshal fails |

---

## D. Security Issues

### 🔴 D1. No Cluster Status Validation
Deployments can be created targeting disconnected clusters, leading to:
- Resources stuck in `pending` forever
- No feedback to user
- Wasted agent poll cycles

### 🔴 D2. Agent Authentication Gap
The agent uses `ACCESS_TOKEN` (JWT) to call Deployment Service, but:
- No service account JWT generation endpoint exists
- Agent would need a user JWT (expires in 15m)
- No long-lived token mechanism for agents

### 🟡 D3. No Rate Limiting on Agent Endpoints
`/v1/organizations/:orgId/clusters/:clusterId/deployments` has no specific rate limiting for agent polling.

### 🟡 D4. No Agent Identity Verification
Status update endpoint accepts any valid JWT, doesn't verify the caller is the actual agent for that cluster.

---

## E. Failure Scenarios

### E1. Agent Fails to Parse Desired State
```
Agent calls: GET /v1/organizations/{org}/clusters/{cluster}/deployments
Response:    {"items": [{"id": "...", "applicationId": "...", ...}]}
Agent:       json.Unmarshal fails (missing required fields)
Result:      RECONCILIATION LOOP BROKEN - NO KUBERNETES RESOURCES CREATED
```

### E2. Deployment to Disconnected Cluster
```
User creates deployment → Deployment Service accepts
Cluster is disconnected → Agent not running
Result:     Deployment stuck in "pending" forever, no user feedback
```

### E3. Agent Can't Report Status
```
Agent creates K8s Deployment → Success
Agent tries to report status → Missing releaseId from desired state
Result:     K8s resources exist but platform shows "pending" forever
```

### E4. Duplicate Kubernetes Resources
```
Agent restart → Loads state file
Agent queries deployments → Can't match (ID vs DeploymentID mismatch)
Result:     May create duplicate resources or skip updates
```

---

## F. Production Risks

### 🔴 Critical (P0)
1. **Complete deployment failure** - Agent can't parse API response
2. **Status never updated** - Missing releaseId prevents status reporting
3. **K8s resource naming failure** - Missing applicationSlug

### 🟠 High (P1)
4. **Agent authentication** - No long-lived token mechanism
5. **Cluster status ignored** - Deployments to disconnected clusters
6. **Orphan resources** - If state mismatch occurs

### 🟡 Medium (P2)
7. **No retry backoff** - Agent polls every 30s regardless of errors
8. **No circuit breaker** - Agent keeps calling failing API
9. **Resource limits optional** - Pods may consume all node resources

---

## G. Required Fixes

### G1. Critical (Must Fix Before Any Deployment)

**Fix 1: Create Dedicated Agent Desired State Endpoint**

Create a new endpoint specifically for agents:
```
GET /v1/agent/clusters/{clusterId}/desired-state
```

Response schema matching agent expectations:
```json
{
  "items": [
    {
      "deploymentId": "uuid",
      "releaseId": "uuid",
      "applicationName": "My App",
      "applicationSlug": "my-app",
      "image": "nginx:latest",
      "replicas": 3,
      "port": 80,
      "envVars": [{"name": "FOO", "value": "bar"}],
      "resourceRequests": {"cpu": "100m", "memory": "128Mi"},
      "resourceLimits": {"cpu": "500m", "memory": "512Mi"},
      "revision": 1,
      "status": "pending"
    }
  ]
}
```

Implementation:
- Join `deployments` with `applications` (for name/slug)
- Join with `releases` (for latest releaseId and revision)
- Transform resource fields to nested format
- Filter only pending/running deployments

**Fix 2: Add Cluster Status Validation**

In `deployment/service.go` `CreateDeployment`:
```go
// Verify cluster is connected
cluster, err := s.clusters.GetByID(ctx, req.ClusterID)
if err != nil {
    return nil, nil, errClusterNotFound
}
if cluster.Status != "connected" {
    return nil, nil, errClusterNotReady
}
```

**Fix 3: Agent Service Account Token**

Add endpoint for long-lived agent tokens:
```
POST /v1/organizations/:orgId/clusters/:clusterId/agent-token
Response: { "token": "jwt...", "expiresAt": "2027-..." }
```

### G2. High Priority

**Fix 4: Update Gateway Routes**

Add new agent endpoint to gateway:
```go
// Agent desired state (capability-based auth via cluster agentId)
v1.Get("/agent/clusters/:clusterId/desired-state", svc.Handler())
```

**Fix 5: Agent Identity Verification**

Verify status updates come from the actual cluster agent:
```go
// In UpdateDeploymentStatus handler
agentID := c.Get("X-Agent-ID")
cluster, _ := clusterService.GetByID(ctx, orgID, clusterID)
if cluster.AgentID != agentID {
    return errForbidden
}
```

### G3. Medium Priority

**Fix 6: Add Exponential Backoff**

In agent reconciler:
```go
type Config struct {
    // ...
    MaxRetries    int           // Default: 3
    RetryBackoff  time.Duration // Default: 5s, doubles each retry
}
```

**Fix 7: Circuit Breaker**

Add circuit breaker for API failures to prevent thundering herd.

---

## H. Deployment Readiness Score

| Category | Score | Notes |
|----------|-------|-------|
| Application Creation | 10/10 | Fully functional |
| Deployment Creation | 8/10 | Works but no cluster validation |
| Desired State Retrieval | 0/10 | **BROKEN** - Schema mismatch |
| Agent Reconciliation | 2/10 | Code exists but can't run |
| K8s Deployment Creation | 8/10 | Works in isolation tests |
| K8s Service Creation | 8/10 | Works in isolation tests |
| Status Reporting | 0/10 | **BROKEN** - Missing releaseId |
| Success/Failure Lifecycle | 0/10 | **BLOCKED** by above |
| Security | 5/10 | RLS good, agent auth missing |
| Event Contract | 9/10 | Complete with tests |

### Overall Deployment Readiness: 15/100

**Status: NOT READY FOR DEPLOYMENT**

---

## Evidence Summary

### Can a customer deploy nginx:latest today?

**NO.** Here is the concrete evidence:

1. **API Response Mismatch** (file: `deployment/domain.go` lines 171-223):
   - `DeploymentView.ID` maps to `"id"` not `"deploymentId"`
   - No `releaseId`, `applicationName`, `applicationSlug`, `revision` fields

2. **Agent Expects Different Schema** (file: `controlplane/deployment.go` lines 12-26):
   - `DesiredDeployment.DeploymentID` expects `"deploymentId"`
   - Requires `releaseId` for status reporting
   - Requires `applicationSlug` for K8s resource naming

3. **Handler Returns Wrong Data** (file: `deployment/handlers.go` lines 177-192):
   - `ListDeploymentsByCluster` calls `toDeploymentView()` 
   - Returns `DeploymentView` which doesn't match agent expectations

4. **K8s Manager Needs ApplicationSlug** (file: `k8s/manager.go` lines 51-63):
   - `DeploymentSpec.ApplicationSlug` is required
   - Used as K8s Deployment name
   - Without it, `FromDesiredDeployment` will set empty string

5. **Status Reporting Requires ReleaseID** (file: `controlplane/deployment.go` lines 91-97):
   - `ReportDeploymentStatus` URL includes `releaseId`
   - Agent has no way to obtain this from current API

### Test Simulation

```bash
# What agent receives from API
curl /v1/organizations/org-1/clusters/cluster-1/deployments
{"items":[{"id":"dep-1","applicationId":"app-1","image":"nginx:latest",...}]}

# What agent code expects
json.Unmarshal into DesiredDeployment{DeploymentID, ReleaseID, ApplicationSlug, ...}

# Result: Missing required fields, empty ApplicationSlug
# K8s Deployment created with empty name → FAILS
```

---

## Recommended Action Plan

1. **Immediate** (Day 1): Implement Fix 1 (dedicated agent endpoint)
2. **Short-term** (Day 2): Implement Fix 2-3 (cluster validation, agent tokens)
3. **Medium-term** (Week 1): Implement Fix 4-7 (gateway, verification, resilience)
4. **Validation** (Week 1): End-to-end test with real cluster

After fixes, re-run this audit to confirm deployment readiness.

---

*Document generated by Principal Platform Architect review on June 24, 2026*
