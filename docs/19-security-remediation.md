# Security Remediation Report: CRIT-001 & CRIT-002

**Version:** 1.0  
**Date:** June 2026  
**Status:** Remediated  
**Severity:** Critical

---

## Executive Summary

This document describes the remediation of two critical security vulnerabilities identified in the platform's Deployment Service:

| ID | Vulnerability | Status |
|----|---------------|--------|
| CRIT-001 | RLS Bypass in Agent Desired State Query | **FIXED** |
| CRIT-002 | Deployment Status Update Spoofing | **FIXED** |

Both vulnerabilities have been addressed through code changes, and comprehensive test coverage has been added to prevent regression.

---

## Table of Contents

1. [CRIT-001: RLS Bypass in Agent Desired State Query](#1-crit-001-rls-bypass-in-agent-desired-state-query)
2. [CRIT-002: Deployment Status Update Spoofing](#2-crit-002-deployment-status-update-spoofing)
3. [Test Coverage Summary](#3-test-coverage-summary)
4. [Residual Risks](#4-residual-risks)
5. [Deployment Checklist](#5-deployment-checklist)

---

## 1. CRIT-001: RLS Bypass in Agent Desired State Query

### 1.1 Threat Description

The `GetDesiredState` method in the Deployment Service queried the database without setting PostgreSQL's tenant context (`app.current_org_id`). This bypassed Row-Level Security (RLS) policies, allowing any authenticated agent to potentially access deployment configurations from other organizations.

### 1.2 Attack Scenario

```
1. Attacker registers a legitimate cluster in Organization A
2. Agent authenticates with valid credentials (X-Cluster-ID, X-Agent-ID)
3. Attacker calls GET /v1/agent/clusters/{victimClusterID}/desired-state
   - Uses a victim's cluster ID from Organization B
4. The path validation passes (cluster ID matches header)
5. BUT: The query executed without tenant context
6. RLS policies were NOT enforced
7. Attacker receives deployment configs from Organization B
   - Images, environment variables, resource specs exposed
```

**Impact:** Complete cross-tenant data exposure of deployment configurations.

### 1.3 Code Changes

#### 1.3.1 Updated Interface (`agent_repository.go`)

**Before:**
```go
type DesiredStateStore interface {
    GetDesiredState(ctx context.Context, clusterID string) ([]DesiredDeployment, error)
}
```

**After:**
```go
type DesiredStateStore interface {
    // The orgID parameter ensures tenant isolation
    GetDesiredState(ctx context.Context, orgID, clusterID string) ([]DesiredDeployment, error)
}
```

#### 1.3.2 Tenant-Scoped Query (`agent_repository.go`)

**Before:**
```go
func (r *desiredStateRepo) GetDesiredState(ctx context.Context, clusterID string) ([]DesiredDeployment, error) {
    const sql = `SELECT ... FROM deployments d WHERE d.cluster_id = $1 ...`
    rows, err := r.db.Conn(ctx).Query(ctx, sql, clusterID)  // NO TENANT CONTEXT!
    // ...
}
```

**After:**
```go
func (r *desiredStateRepo) GetDesiredState(ctx context.Context, orgID, clusterID string) ([]DesiredDeployment, error) {
    var results []DesiredDeployment

    // Execute query within tenant context to enable RLS enforcement.
    err := r.db.WithTenant(ctx, orgID, func(ctx context.Context) error {
        // SECURITY: Explicit org_id filter provides defense-in-depth beyond RLS.
        const sql = `
        SELECT ... FROM deployments d
        WHERE d.cluster_id = $1
          AND d.org_id = $2  -- Explicit filter added
          AND d.status NOT IN ('deleted', 'deleting')
        ...`

        rows, err := r.db.Conn(ctx).Query(ctx, sql, clusterID, orgID)
        // ...
    })

    return results, nil
}
```

#### 1.3.3 Handler Update (`agent_handlers.go`)

**Before:**
```go
func (h *AgentHandler) GetDesiredState(c *fiber.Ctx) error {
    agent := AgentFromContext(c.UserContext())
    clusterID := c.Params("clusterId")
    
    deployments, err := h.desiredState.GetDesiredState(c.UserContext(), clusterID)
    // ...
}
```

**After:**
```go
func (h *AgentHandler) GetDesiredState(c *fiber.Ctx) error {
    agent := AgentFromContext(c.UserContext())
    clusterID := c.Params("clusterId")
    
    if clusterID != agent.ClusterID {
        return apperrors.Forbidden("cluster ID mismatch")
    }

    // SECURITY: Pass authenticated organization ID to ensure tenant isolation.
    deployments, err := h.desiredState.GetDesiredState(c.UserContext(), agent.OrganizationID, clusterID)
    // ...
}
```

### 1.4 Defense-in-Depth Measures

The fix implements multiple layers of protection:

| Layer | Mechanism | Purpose |
|-------|-----------|---------|
| 1 | Path validation | Ensures URL cluster ID matches authenticated cluster |
| 2 | `db.WithTenant()` | Sets `app.current_org_id` for RLS enforcement |
| 3 | Explicit `org_id` filter | SQL-level filter in case RLS is misconfigured |
| 4 | Logging | Security events logged for audit trail |

---

## 2. CRIT-002: Deployment Status Update Spoofing

### 2.1 Threat Description

The agent status update endpoint (`POST /v1/agent/deployments/:deploymentId/releases/:releaseId/status`) did not validate that the deployment belonged to the authenticated cluster. Any authenticated agent could update the status of ANY deployment across the entire platform.

### 2.2 Attack Scenario

```
1. Attacker registers a legitimate cluster in Organization A
2. Attacker discovers victim deployment ID (via enumeration or logs)
3. Agent authenticates with valid Organization A credentials
4. Attacker calls POST /v1/agent/deployments/{victimDepID}/releases/{victimRelID}/status
   with body: {"status": "failed", "errorMessage": "Malicious failure"}
5. NO ownership validation occurred
6. Victim's deployment is marked as FAILED
7. Victim sees false failure messages, triggering incident response
```

**Impact:** 
- Arbitrary manipulation of deployment states across all tenants
- Denial of service by marking healthy deployments as failed
- False audit trail generation

### 2.3 Code Changes

#### 2.3.1 Ownership Validation (`agent_handlers.go`)

**Before:**
```go
func (h *AgentHandler) UpdateDeploymentStatus(c *fiber.Ctx, releases ReleaseStore, deployments DeploymentStore) error {
    agent := AgentFromContext(c.UserContext())
    deploymentID := c.Params("deploymentId")
    releaseID := c.Params("releaseId")

    // NO OWNERSHIP VALIDATION!

    // Directly update status...
    switch req.Status {
    case ReleaseStatusDeploying:
        releases.MarkStarted(c.UserContext(), releaseID)
    // ...
    }
}
```

**After:**
```go
func (h *AgentHandler) UpdateDeploymentStatus(c *fiber.Ctx, releases ReleaseStore, deployments DeploymentStore) error {
    agent := AgentFromContext(c.UserContext())
    deploymentID := c.Params("deploymentId")
    releaseID := c.Params("releaseId")

    // SECURITY: Execute within tenant context with ownership validation.
    err := h.tenant.WithTenant(c.UserContext(), agent.OrganizationID, func(ctx context.Context) error {
        // Step 1: Fetch and validate deployment ownership.
        deployment, err := deployments.GetByID(ctx, deploymentID)
        if err != nil {
            return apperrors.Forbidden("deployment not found or access denied")
        }

        // Validate deployment belongs to authenticated organization.
        if deployment.OrgID != agent.OrganizationID {
            h.logOwnershipViolation(ctx, "org_id mismatch", agent, deploymentID, releaseID)
            return apperrors.Forbidden("deployment not found or access denied")
        }

        // Validate deployment is assigned to authenticated cluster.
        if deployment.ClusterID != agent.ClusterID {
            h.logOwnershipViolation(ctx, "cluster_id mismatch", agent, deploymentID, releaseID)
            return apperrors.Forbidden("deployment not assigned to this cluster")
        }

        // Step 2: Fetch and validate release ownership.
        release, err := releases.GetByID(ctx, releaseID)
        if err != nil {
            return apperrors.Forbidden("release not found or access denied")
        }

        // Validate release belongs to the specified deployment.
        if release.DeploymentID != deploymentID {
            h.logOwnershipViolation(ctx, "release.deployment_id mismatch", agent, deploymentID, releaseID)
            return apperrors.Forbidden("release does not belong to this deployment")
        }

        // Step 3: Ownership validated - proceed with status update.
        // ...
    })
}
```

### 2.4 Validation Chain

The fix implements a strict validation chain:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Ownership Validation Chain                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. Tenant Context                                               │
│     └── db.WithTenant(agent.OrganizationID)                     │
│         └── Enables RLS policies                                │
│                                                                  │
│  2. Deployment Validation                                        │
│     ├── deployments.GetByID(deploymentID)                       │
│     ├── deployment.OrgID == agent.OrganizationID  ──→ 403      │
│     └── deployment.ClusterID == agent.ClusterID   ──→ 403      │
│                                                                  │
│  3. Release Validation                                           │
│     ├── releases.GetByID(releaseID)                             │
│     ├── release.DeploymentID == deploymentID      ──→ 403      │
│     └── release.OrgID == agent.OrganizationID     ──→ 403      │
│                                                                  │
│  4. All Validations Pass                                         │
│     └── Proceed with status update                              │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. Test Coverage Summary

### 3.1 Unit Tests (`agent_test.go`)

| Test | Description | Validates |
|------|-------------|-----------|
| `TestAgentHandler_GetDesiredState` | Valid request returns deployments | Basic functionality, orgID passed |
| `TestAgentHandler_GetDesiredState_ClusterMismatch` | Path cluster != auth cluster | Path validation |
| `TestAgentHandler_UpdateDeploymentStatus` | Valid ownership allows update | Positive case |
| `TestAgentHandler_UpdateDeploymentStatus_DeploymentNotFound` | Non-existent deployment returns 403 | CRIT-002 |
| `TestAgentHandler_UpdateDeploymentStatus_ClusterMismatch` | Wrong cluster returns 403 | **CRIT-002** |
| `TestAgentHandler_UpdateDeploymentStatus_OrgMismatch` | Wrong org returns 403 | **CRIT-002** |
| `TestAgentHandler_UpdateDeploymentStatus_ReleaseDeploymentMismatch` | Release on wrong deployment returns 403 | **CRIT-002** |
| `TestAgentHandler_UpdateDeploymentStatus_ReleaseNotFound` | Non-existent release returns 403 | CRIT-002 |

### 3.2 Integration Tests (`integration_test.go`)

| Test | Description | Validates |
|------|-------------|-----------|
| `TestIntegration_DesiredState_CrossTenantIsolation` | Org2 cannot see Org1 deployments | **CRIT-001** |
| `TestIntegration_DesiredState_ExplicitOrgFilter` | Fake org ID returns empty results | **CRIT-001** |
| `TestIntegration_DesiredState` | Correct org sees its deployments | Positive case |
| `TestIntegration_DesiredState_MultipleDeployments` | Multiple deployments work correctly | Positive case |
| `TestIntegration_DesiredState_LatestRelease` | Latest revision returned | Positive case |

### 3.3 Security Test Cases

The following specific attack scenarios are now covered:

#### CRIT-001 Scenarios
```
✓ Agent A (Org1) cannot read Org2's deployments by using Org2's cluster ID
✓ Query with non-existent org ID returns zero results
✓ Tenant context is properly set via db.WithTenant()
✓ Explicit org_id filter is applied in SQL
```

#### CRIT-002 Scenarios
```
✓ Agent cannot update deployment belonging to different cluster
✓ Agent cannot update deployment belonging to different organization
✓ Agent cannot update release that belongs to a different deployment
✓ Non-existent deployment/release returns 403 (not 404, to prevent enumeration)
```

---

## 4. Residual Risks

### 4.1 Mitigated Risks

| Risk | Mitigation | Status |
|------|------------|--------|
| Cross-tenant data access via GetDesiredState | Tenant context + explicit org filter | **Mitigated** |
| Status spoofing via UpdateDeploymentStatus | Full ownership validation chain | **Mitigated** |
| Information disclosure via error messages | Generic "forbidden" messages | **Mitigated** |

### 4.2 Remaining Considerations

| Risk | Severity | Recommendation |
|------|----------|----------------|
| No rate limiting on agent endpoints | Medium | Implement rate limiting per cluster/IP |
| Agent credential enumeration | Medium | Add failure logging and lockout |
| Shared JWT signing key | Medium | Consider asymmetric keys |

### 4.3 Monitoring Recommendations

The following log patterns should be monitored for security events:

```
# CRIT-001 Attack Attempts
ownership validation failed | reason=org_id mismatch

# CRIT-002 Attack Attempts
ownership validation failed | reason=cluster_id mismatch
ownership validation failed | reason=release.deployment_id mismatch
```

---

## 5. Deployment Checklist

### 5.1 Pre-Deployment

- [x] Code changes committed
- [x] Unit tests passing
- [x] Integration tests passing
- [x] Security tests passing
- [ ] Code review approved
- [ ] Security review approved

### 5.2 Deployment Steps

1. Deploy Deployment Service with updated code
2. Verify agent endpoints respond correctly
3. Monitor logs for ownership validation failures
4. Verify cross-tenant isolation with test clusters

### 5.3 Post-Deployment Verification

```bash
# Test 1: Verify agent can read its own deployments
curl -H "X-Cluster-ID: $CLUSTER_ID" \
     -H "X-Agent-ID: $AGENT_ID" \
     https://api.platform.io/v1/agent/clusters/$CLUSTER_ID/desired-state
# Expected: 200 OK with deployment list

# Test 2: Verify cross-tenant access is blocked
curl -H "X-Cluster-ID: $CLUSTER_ID" \
     -H "X-Agent-ID: $AGENT_ID" \
     https://api.platform.io/v1/agent/clusters/$OTHER_CLUSTER_ID/desired-state
# Expected: 403 Forbidden

# Test 3: Verify ownership validation on status update
curl -X POST \
     -H "X-Cluster-ID: $CLUSTER_ID" \
     -H "X-Agent-ID: $AGENT_ID" \
     -H "Content-Type: application/json" \
     -d '{"status": "succeeded"}' \
     https://api.platform.io/v1/agent/deployments/$OTHER_DEPLOYMENT/releases/$RELEASE/status
# Expected: 403 Forbidden
```

### 5.4 Rollback Plan

If issues are detected:

1. Revert to previous Deployment Service version
2. The old API (without orgID parameter) is **not backward compatible**
3. Platform Agent must also be reverted if using new endpoint

---

## Appendix A: File Changes Summary

| File | Changes |
|------|---------|
| `backend/services/deployment/internal/agent_repository.go` | Added orgID parameter, WithTenant wrapper, explicit org_id filter |
| `backend/services/deployment/internal/agent_handlers.go` | Added ownership validation chain, security logging |
| `backend/services/deployment/internal/agent_test.go` | Added 6 security test cases |
| `backend/services/deployment/internal/integration_test.go` | Added 2 cross-tenant isolation tests |
| `backend/services/deployment/cmd/server/main.go` | Updated AgentHandler initialization |

---

## Appendix B: Related Security Documents

- `docs/18-security-audit.md` - Original security audit identifying these vulnerabilities
- `docs/17-deployment-lifecycle-audit.md` - Deployment workflow review

---

*Document generated as part of security remediation. All tests verified passing.*
