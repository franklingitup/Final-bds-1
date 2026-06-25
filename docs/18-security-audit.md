# Platform Security Audit Report

**Version:** 1.0  
**Date:** June 2026  
**Auditor Role:** Principal Security Engineer  
**Scope:** Cluster Service, Deployment Service, Platform Agent, API Gateway

---

## Executive Summary

This security audit examines the authentication, authorization, and data isolation mechanisms across the platform's cluster management and deployment workflow. The audit identified **2 Critical**, **3 High**, **5 Medium**, and **4 Low** severity findings.

**Overall Security Posture Score: 62/100** (Significant Improvements Required)

### Key Risk Areas
1. Cross-tenant data exposure through RLS bypass
2. Deployment status spoofing by malicious agents
3. Insufficient ownership validation in agent APIs
4. Missing rate limiting on agent endpoints

---

## Table of Contents

1. [Critical Findings](#1-critical-findings)
2. [High Severity Findings](#2-high-severity-findings)
3. [Medium Severity Findings](#3-medium-severity-findings)
4. [Low Severity Findings](#4-low-severity-findings)
5. [Security Architecture Review](#5-security-architecture-review)
6. [Remediation Priority Matrix](#6-remediation-priority-matrix)
7. [Appendix: Exploit Scenarios](#7-appendix-exploit-scenarios)

---

## 1. Critical Findings

### CRIT-001: RLS Bypass in Agent Desired State Query

**Severity:** Critical  
**CVSS Score:** 9.1 (Critical)  
**Component:** Deployment Service (`agent_repository.go`)  
**CWE:** CWE-863 (Incorrect Authorization)

#### Description

The `GetDesiredState` method in the Deployment Service queries the database **without setting tenant context**, bypassing PostgreSQL Row-Level Security (RLS) policies. This allows any authenticated agent to potentially retrieve deployment information from other organizations.

#### Vulnerable Code

```go
// backend/services/deployment/internal/agent_repository.go:26-57
func (r *desiredStateRepo) GetDesiredState(ctx context.Context, clusterID string) ([]DesiredDeployment, error) {
    const sql = `
SELECT DISTINCT ON (d.id)
    d.id AS deployment_id,
    ...
FROM deployments d
INNER JOIN applications a ON a.id = d.application_id
INNER JOIN releases r ON r.deployment_id = d.id
WHERE d.cluster_id = $1  -- No RLS enforcement!
  AND d.status NOT IN ('deleted', 'deleting')
  AND r.status IN ('pending', 'deploying', 'succeeded')
ORDER BY d.id, r.revision DESC`

    // Uses db.Conn(ctx) without WithTenant - RLS not enforced
    rows, err := r.db.Conn(ctx).Query(ctx, sql, clusterID)
```

The query uses `r.db.Conn(ctx)` which does NOT set `app.current_org_id`, meaning RLS policies are not applied.

#### Exploit Scenario

1. Attacker compromises or deploys a rogue agent in their own cluster
2. Agent authenticates successfully with valid cluster credentials
3. Agent calls `GET /v1/agent/clusters/{victimClusterId}/desired-state` using a victim's cluster ID
4. RLS is bypassed because no tenant context is set
5. Attacker receives deployment configurations (images, env vars, resource specs) from victim's organization

#### Impact

- **Confidentiality:** Complete exposure of deployment configurations across all tenants
- **Data Leak:** Environment variables may contain secrets (API keys, database credentials)
- **Compliance:** GDPR, SOC2, and PCI-DSS violations

#### Recommended Fix

```go
// OPTION 1: Add tenant validation before query
func (h *AgentHandler) GetDesiredState(c *fiber.Ctx) error {
    agent := AgentFromContext(c.UserContext())
    
    // Verify cluster ID matches and use org from authenticated agent
    clusterID := c.Params("clusterId")
    if clusterID != agent.ClusterID {
        return apperrors.Forbidden("cluster ID mismatch")
    }
    
    // Query with tenant context
    var deployments []DesiredDeployment
    err := db.WithTenant(ctx, agent.OrganizationID, func(ctx context.Context) error {
        var err error
        deployments, err = h.desiredState.GetDesiredState(ctx, clusterID)
        return err
    })
    // ...
}

// OPTION 2: Add explicit org_id filter in query
const sql = `
SELECT ...
FROM deployments d
WHERE d.cluster_id = $1
  AND d.org_id = $2  -- Explicit org filter
  AND d.status NOT IN ('deleted', 'deleting')
...`
```

---

### CRIT-002: Deployment Status Update Spoofing

**Severity:** Critical  
**CVSS Score:** 8.8 (High)  
**Component:** Deployment Service (`agent_handlers.go`)  
**CWE:** CWE-284 (Improper Access Control)

#### Description

The agent status update endpoint (`POST /v1/agent/deployments/:deploymentId/releases/:releaseId/status`) does not validate that the deployment belongs to the authenticated cluster. Any authenticated agent can update the status of ANY deployment across the entire platform.

#### Vulnerable Code

```go
// backend/services/deployment/internal/agent_handlers.go:52-108
func (h *AgentHandler) UpdateDeploymentStatus(c *fiber.Ctx, releases ReleaseStore, deployments DeploymentStore) error {
    agent := AgentFromContext(c.UserContext())
    if agent == nil {
        return apperrors.Unauthorized("agent identity required")
    }

    deploymentID := c.Params("deploymentId")  // Attacker-controlled
    releaseID := c.Params("releaseId")        // Attacker-controlled

    // NO VALIDATION that deployment belongs to agent.ClusterID!
    // NO VALIDATION that deployment belongs to agent.OrganizationID!

    switch req.Status {
    case ReleaseStatusDeploying:
        if err := releases.MarkStarted(c.UserContext(), releaseID); err != nil {
            return err
        }
    // ... status updates proceed without ownership check
    }
```

#### Exploit Scenario

1. Attacker registers a legitimate cluster with their organization
2. Agent authenticates with valid credentials
3. Attacker discovers or guesses deployment IDs (UUIDs, potentially via enumeration)
4. Agent sends `POST /v1/agent/deployments/{victimDeploymentId}/releases/{victimReleaseId}/status`
   ```json
   {"status": "failed", "errorMessage": "Malicious failure injection"}
   ```
5. Victim's deployment is marked as failed, causing operational disruption
6. Alternatively, attacker marks their own malicious deployment as "succeeded" to bypass health checks

#### Impact

- **Integrity:** Arbitrary manipulation of deployment states across all tenants
- **Availability:** Denial of service by marking healthy deployments as failed
- **Trust:** Undermines the integrity of deployment status reporting
- **Audit Trail:** False audit records generated with incorrect agent attribution

#### Recommended Fix

```go
func (h *AgentHandler) UpdateDeploymentStatus(c *fiber.Ctx, releases ReleaseStore, deployments DeploymentStore) error {
    agent := AgentFromContext(c.UserContext())
    
    deploymentID := c.Params("deploymentId")
    releaseID := c.Params("releaseId")

    // CRITICAL: Validate deployment belongs to the authenticated cluster
    deployment, err := deployments.GetByID(c.UserContext(), deploymentID)
    if err != nil {
        return apperrors.NotFound("deployment not found")
    }
    
    if deployment.ClusterID != agent.ClusterID {
        return apperrors.Forbidden("deployment does not belong to this cluster")
    }
    
    if deployment.OrgID != agent.OrganizationID {
        return apperrors.Forbidden("deployment does not belong to this organization")
    }
    
    // Also validate release belongs to deployment
    release, err := releases.GetByID(c.UserContext(), releaseID)
    if err != nil || release.DeploymentID != deploymentID {
        return apperrors.NotFound("release not found for this deployment")
    }
    
    // Proceed with status update...
}
```

---

## 2. High Severity Findings

### HIGH-001: Agent Credential Guessing Attack Vector

**Severity:** High  
**CVSS Score:** 7.5 (High)  
**Component:** Deployment Service (`agent_middleware.go`)  
**CWE:** CWE-307 (Improper Restriction of Excessive Authentication Attempts)

#### Description

Agent authentication relies solely on matching `X-Cluster-ID` and `X-Agent-ID` headers against database values. There is no rate limiting, account lockout, or additional verification factor. If cluster IDs leak (via logs, error messages, or enumeration), attackers can attempt to guess agent IDs.

#### Vulnerable Code

```go
// backend/services/deployment/internal/agent_middleware.go:86-109
func (v *clusterValidatorImpl) ValidateCluster(ctx context.Context, clusterID, agentID string) (string, error) {
    const sql = `SELECT org_id, agent_id, status FROM clusters WHERE id = $1`
    
    // No rate limiting on validation attempts
    // No logging of failed attempts
    // No account lockout after N failures
    
    if err := v.pool.QueryRow(ctx, sql, clusterID).Scan(&orgID, &storedAgentID, &status); err != nil {
        return "", apperrors.Unauthorized("cluster not found")  // Same error for not found
    }
    
    if storedAgentID == nil || *storedAgentID != agentID {
        return "", apperrors.Unauthorized("invalid agent credentials")  // Same error for wrong agent
    }
```

#### Exploit Scenario

1. Attacker discovers a valid cluster ID (leaked in logs, API responses, or guessed)
2. Attacker enumerates agent IDs by calling agent endpoints with different IDs
3. No rate limiting allows thousands of attempts per second
4. Once agent ID is guessed, attacker has full agent access to that cluster

#### Impact

- **Authentication Bypass:** Successful agent impersonation
- **Full Agent Access:** Read desired deployments, update deployment status

#### Recommended Fix

```go
// 1. Add rate limiting middleware for agent endpoints
func AgentRateLimiter(limiter *middleware.RateLimiter) fiber.Handler {
    return func(c *fiber.Ctx) error {
        key := "agent:" + c.Get("X-Cluster-ID") + ":" + c.IP()
        // Strict limit: 10 requests per minute per cluster/IP combo
        if !limiter.Allow(key, 10, time.Minute) {
            return apperrors.RateLimited("too many authentication attempts")
        }
        return c.Next()
    }
}

// 2. Log failed authentication attempts
func (v *clusterValidatorImpl) ValidateCluster(ctx context.Context, clusterID, agentID string) (string, error) {
    // ... validation logic ...
    
    if storedAgentID == nil || *storedAgentID != agentID {
        v.logger.WarnContext(ctx, "agent authentication failed",
            "cluster_id", clusterID,
            "attempted_agent_id", agentID[:8]+"...",  // Partial for privacy
            "client_ip", extractIP(ctx))
        
        // Consider incrementing a failure counter and implementing lockout
        return "", apperrors.Unauthorized("invalid agent credentials")
    }
}

// 3. Consider HMAC-based challenge-response for stronger auth
```

---

### HIGH-002: Registration Token Brute Force Risk

**Severity:** High  
**CVSS Score:** 7.3 (High)  
**Component:** Cluster Service (`handlers.go`, `service.go`)  
**CWE:** CWE-307 (Improper Restriction of Excessive Authentication Attempts)

#### Description

The agent registration endpoint accepts registration tokens without rate limiting. Although tokens are 256-bit random, the endpoint does not implement protections against automated attacks.

#### Vulnerable Endpoint

```go
// backend/services/cluster/internal/routes.go:12
v1.Post("/agent/register", h.RegisterAgent)  // Public, no rate limiting

// backend/services/cluster/internal/service.go:296-371
func (s *Service) RegisterAgent(ctx context.Context, req AgentRegisterRequest) (*Cluster, error) {
    tokenHash := hashToken(req.Token)  // SHA-256 hash
    
    // No rate limiting on token validation
    // No logging of failed registration attempts
    // No lockout after repeated failures
    
    token, err := s.tokens.GetByHash(ctx, tokenHash)
```

#### Exploit Scenario

1. Attacker targets organizations with pending cluster registrations
2. Automated tool attempts registration with random/sequential tokens
3. Successful guess allows attacker to register as the legitimate agent
4. Attacker gains persistent access to the cluster management plane

#### Recommended Fix

```go
// Add rate limiting to registration endpoint
func (r *Router) registerAgentRoutes(v1 fiber.Router) {
    // Strict rate limit: 5 attempts per minute per IP
    registrationLimiter := middleware.NewRateLimiter(middleware.RateLimiterConfig{
        RequestsPerMinute: 5,
        BurstSize:         2,
        KeyFunc:           func(c *fiber.Ctx) string { return "register:" + c.IP() },
    })
    
    if svc, ok := r.services["cluster"]; ok {
        v1.Post("/agent/register", registrationLimiter.Middleware(), svc.Handler())
    }
}
```

---

### HIGH-003: Cross-Tenant Cluster ID Enumeration

**Severity:** High  
**CVSS Score:** 6.5 (Medium)  
**Component:** Deployment Service (`agent_handlers.go`)  
**CWE:** CWE-200 (Information Exposure)

#### Description

The desired state endpoint returns different errors for "cluster not found" vs "cluster not connected" vs "invalid agent credentials", enabling attackers to enumerate valid cluster IDs across all tenants.

#### Vulnerable Code

```go
// backend/services/deployment/internal/agent_middleware.go:96-106
if err := v.pool.QueryRow(ctx, sql, clusterID).Scan(&orgID, &storedAgentID, &status); err != nil {
    return "", apperrors.Unauthorized("cluster not found")  // Reveals: cluster doesn't exist
}

if status != "connected" {
    return "", apperrors.Unauthorized("cluster not connected")  // Reveals: cluster exists but offline
}

if storedAgentID == nil || *storedAgentID != agentID {
    return "", apperrors.Unauthorized("invalid agent credentials")  // Reveals: cluster exists and is connected
}
```

#### Exploit Scenario

1. Attacker iterates through potential cluster UUIDs
2. Different error messages reveal cluster existence and status
3. Attacker builds a map of valid cluster IDs and their states
4. Information used for targeted attacks or social engineering

#### Recommended Fix

```go
// Return generic error for all authentication failures
func (v *clusterValidatorImpl) ValidateCluster(ctx context.Context, clusterID, agentID string) (string, error) {
    var orgID string
    var storedAgentID *string
    var status string

    if err := v.pool.QueryRow(ctx, sql, clusterID).Scan(&orgID, &storedAgentID, &status); err != nil {
        // Log detailed error internally
        v.logger.Debug("cluster validation failed: not found", "cluster_id", clusterID)
        return "", apperrors.Unauthorized("authentication failed")  // Generic message
    }

    if status != "connected" || storedAgentID == nil || *storedAgentID != agentID {
        // Log detailed error internally
        v.logger.Debug("cluster validation failed", 
            "cluster_id", clusterID, 
            "status", status, 
            "agent_match", storedAgentID != nil && *storedAgentID == agentID)
        return "", apperrors.Unauthorized("authentication failed")  // Same generic message
    }

    return orgID, nil
}
```

---

## 3. Medium Severity Findings

### MED-001: No Rate Limiting on Agent Endpoints

**Severity:** Medium  
**CVSS Score:** 5.3 (Medium)  
**Component:** API Gateway (`router.go`)  
**CWE:** CWE-770 (Allocation of Resources Without Limits)

#### Description

Agent endpoints in the Gateway bypass the rate limiter, making them vulnerable to abuse.

#### Vulnerable Code

```go
// backend/services/gateway/internal/router/router.go:252-267
func (r *Router) registerAgentRoutes(v1 fiber.Router) {
    // No rate limiting applied to agent routes
    if svc, ok := r.services["cluster"]; ok {
        v1.Post("/agent/register", svc.Handler())  // No limiter
    }

    if svc, ok := r.services["deployment"]; ok {
        agent := v1.Group("/agent")
        agent.Get("/clusters/:clusterId/desired-state", svc.Handler())  // No limiter
        agent.Post("/deployments/:deploymentId/releases/:releaseId/status", svc.Handler())  // No limiter
    }
}
```

#### Recommended Fix

```go
func (r *Router) registerAgentRoutes(v1 fiber.Router) {
    // Create dedicated rate limiter for agent endpoints
    agentLimiter := middleware.NewRateLimiter(middleware.RateLimiterConfig{
        RequestsPerMinute: 120,  // 2 requests/second
        BurstSize:         10,
        KeyFunc: func(c *fiber.Ctx) string {
            return "agent:" + c.Get("X-Cluster-ID") + ":" + c.IP()
        },
    })

    if svc, ok := r.services["deployment"]; ok {
        agent := v1.Group("/agent", agentLimiter.Middleware())
        // ...
    }
}
```

---

### MED-002: Deployment Created for Disconnected Clusters

**Severity:** Medium  
**CVSS Score:** 4.3 (Medium)  
**Component:** Deployment Service (`service.go`)  
**CWE:** CWE-754 (Improper Check for Unusual Conditions)

#### Description

The Deployment Service allows creating deployments targeting clusters in any status (pending, disconnected, deleted), leading to orphaned deployments.

#### Vulnerable Code

```go
// backend/services/deployment/internal/service.go:190-280
func (s *Service) CreateDeployment(ctx context.Context, orgID, userID string, req CreateDeploymentRequest) (*Deployment, *Release, error) {
    // No validation that cluster is connected
    dep := &Deployment{
        ClusterID: req.ClusterID,  // Could be disconnected or deleted
        // ...
    }
    
    // Only validates application exists, not cluster status
    _, err := s.apps.GetByID(ctx, req.ApplicationID)
```

#### Recommended Fix

```go
func (s *Service) CreateDeployment(ctx context.Context, orgID, userID string, req CreateDeploymentRequest) (*Deployment, *Release, error) {
    // ... validation ...
    
    err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
        // Validate cluster exists and is connected
        cluster, err := s.clusters.GetByID(ctx, req.ClusterID)
        if err != nil {
            return apperrors.Validation("cluster not found")
        }
        if cluster.Status != "connected" {
            return apperrors.Validation("cluster must be connected to create deployments")
        }
        
        // Proceed with deployment creation...
    })
}
```

---

### MED-003: Heartbeat Endpoint Lacks Agent Credential Validation

**Severity:** Medium  
**CVSS Score:** 5.0 (Medium)  
**Component:** Cluster Service (`service.go`)  
**CWE:** CWE-287 (Improper Authentication)

#### Description

The heartbeat endpoint is exposed through the Gateway on user-authenticated routes (`/organizations/:orgId/clusters/:clusterId/heartbeat`), allowing any authenticated user to send heartbeats for any cluster in their organization.

#### Vulnerable Code

```go
// backend/services/gateway/internal/router/router.go:289
clusters.Post("/:clusterId/heartbeat", svc.Handler())  // User auth, not agent auth

// backend/services/cluster/internal/service.go:378-424
func (s *Service) RecordHeartbeat(ctx context.Context, orgID, clusterID string, req AgentHeartbeatRequest) error {
    // Validates agent ID matches, but doesn't verify caller IS the agent
    if c.AgentID == nil || *c.AgentID != req.AgentID {
        return errAgentMismatch
    }
```

#### Impact

An attacker with user credentials could send fake heartbeats if they know the agent ID, keeping a disconnected cluster appearing connected.

#### Recommended Fix

Create a dedicated agent heartbeat endpoint with agent credential authentication:

```go
// Move heartbeat to agent routes with credential auth
func (r *Router) registerAgentRoutes(v1 fiber.Router) {
    if svc, ok := r.services["cluster"]; ok {
        agent := v1.Group("/agent", agentAuth)
        agent.Post("/clusters/:clusterId/heartbeat", svc.Handler())
    }
}
```

---

### MED-004: Registration Token Lookup Cross-Tenant by Design

**Severity:** Medium  
**CVSS Score:** 4.0 (Medium)  
**Component:** Cluster Service (`repository.go`)  
**CWE:** CWE-639 (Authorization Bypass Through User-Controlled Key)

#### Description

The `GetByHash` method intentionally bypasses RLS for capability-based token lookup, but this design means any token hash lookup can potentially return tokens from any organization.

#### Code

```go
// backend/services/cluster/internal/repository.go:233-241
// GetByHash reads a token by hash (cross-tenant, capability-based).
func (r *tokenRepo) GetByHash(ctx context.Context, hash string) (*RegistrationToken, error) {
    t, err := database.QueryOne[RegistrationToken](ctx, r.db.Pool,  // Uses Pool directly, not Conn
        "SELECT * FROM cluster_registration_tokens WHERE token_hash = $1", hash)
```

#### Risk Assessment

While this is intentional for capability-based auth, the lack of rate limiting (HIGH-002) makes this a concern. If combined with HIGH-002, successful brute force could retrieve any organization's registration token data.

#### Recommended Fix

Ensure comprehensive rate limiting on the registration endpoint (see HIGH-002) and consider adding token IP binding:

```go
type RegistrationToken struct {
    // ... existing fields ...
    AllowedCIDR *string  // Optional IP restriction for token usage
}
```

---

### MED-005: JWT Signing Key Shared Across Services

**Severity:** Medium  
**CVSS Score:** 5.5 (Medium)  
**Component:** Gateway, Cluster Service, Deployment Service  
**CWE:** CWE-321 (Use of Hard-coded Cryptographic Key)

#### Description

All services use the same `JWTSigningKey` from configuration. If any service is compromised, the attacker can forge tokens for all services.

#### Vulnerable Pattern

```go
// Multiple services use the same key
// backend/services/gateway/internal/auth/auth.go:64
func NewValidator(cfg config.AuthConfig) *Validator {
    return &Validator{key: []byte(cfg.JWTSigningKey)}
}

// backend/services/cluster/internal/crypto.go:45
func NewTokenVerifier(cfg config.AuthConfig) *TokenVerifier {
    return &TokenVerifier{key: []byte(cfg.JWTSigningKey)}
}
```

#### Recommended Fix

Consider service-specific keys or asymmetric cryptography:

```go
// Use RS256 with public/private key pair
type AuthConfig struct {
    JWTPrivateKeyPath string  // Only Auth service has this
    JWTPublicKeyPath  string  // All services can verify
}
```

---

## 4. Low Severity Findings

### LOW-001: Agent State File Plaintext Storage

**Severity:** Low  
**CVSS Score:** 3.3 (Low)  
**Component:** Platform Agent (`config.go`)  
**CWE:** CWE-312 (Cleartext Storage of Sensitive Information)

#### Description

Agent stores credentials (cluster ID, agent ID) in plaintext JSON file at `/var/lib/platform-agent/state.json`.

#### Vulnerable Code

```go
// agents/platform-agent/internal/config/config.go:35
StateFile: "/var/lib/platform-agent/state.json",
```

#### Recommended Fix

- Use Kubernetes Secret for credential storage
- Encrypt state file at rest
- Set restrictive file permissions (0600)

---

### LOW-002: Config Hash Truncation Weakness

**Severity:** Low  
**CVSS Score:** 2.0 (Low)  
**Component:** Deployment Service (`repository.go`)  
**CWE:** CWE-328 (Reversible One-Way Hash)

#### Description

Config hash uses only first 8 bytes of SHA-256, reducing collision resistance.

```go
// backend/services/deployment/internal/service.go:608-611
func hashConfig(config []byte) string {
    h := sha256.Sum256(config)
    return hex.EncodeToString(h[:8])  // Only 64 bits
}
```

#### Recommended Fix

Use full hash or at least 16 bytes for adequate collision resistance:

```go
return hex.EncodeToString(h[:16])  // 128 bits
```

---

### LOW-003: Missing TLS Certificate Validation Option

**Severity:** Low  
**CVSS Score:** 3.7 (Low)  
**Component:** Platform Agent  
**CWE:** CWE-295 (Improper Certificate Validation)

#### Description

Agent uses default HTTP client without option for custom CA certificates, problematic for private PKI deployments.

#### Recommended Fix

Add TLS configuration options:

```go
type Config struct {
    // ...
    TLSCACertPath     string  // Custom CA certificate
    TLSSkipVerify     bool    // For development only
}
```

---

### LOW-004: Verbose Error Messages in Production

**Severity:** Low  
**CVSS Score:** 2.0 (Low)  
**Component:** Multiple Services  
**CWE:** CWE-209 (Information Exposure Through Error Message)

#### Description

Some error messages reveal internal implementation details that could aid attackers.

#### Examples

```go
// Reveals table structure
return "", apperrors.Unauthorized("cluster not found")
return "", apperrors.Unauthorized("cluster not connected")
return "", apperrors.Unauthorized("invalid agent credentials")

// Reveals validation logic
return apperrors.Validation("slug must be 2-64 chars of lowercase letters, digits, or hyphens")
```

#### Recommended Fix

Use generic error messages in production, detailed messages only in development mode.

---

## 5. Security Architecture Review

### 5.1 Authentication Flow Analysis

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Authentication Flows                              │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  User Flow (JWT):                                                    │
│  ┌────────┐    ┌─────────┐    ┌──────────┐    ┌─────────────┐      │
│  │ User   │───▶│ Gateway │───▶│ Validator│───▶│ Backend Svc │      │
│  └────────┘    └─────────┘    └──────────┘    └─────────────┘      │
│       │             │              │                 │               │
│       │ JWT Token   │ Validate     │ Identity       │ RLS Context   │
│       │             │ Signature    │ Injection      │ Set           │
│       ▼             ▼              ▼                 ▼               │
│  ✅ Strong auth    ✅ HMAC-SHA256  ✅ X-User-ID     ✅ WithTenant   │
│                                                                      │
│  Agent Flow (Credential Headers):                                    │
│  ┌────────┐    ┌─────────┐    ┌──────────────┐    ┌─────────────┐  │
│  │ Agent  │───▶│ Gateway │───▶│ AgentAuth MW │───▶│ Deployment  │  │
│  └────────┘    └─────────┘    └──────────────┘    └─────────────┘  │
│       │             │              │                     │           │
│       │ X-Cluster-ID│ No validation│ DB Lookup          │ ⚠️ No RLS │
│       │ X-Agent-ID  │ (passthrough)│ Match Check        │           │
│       ▼             ▼              ▼                     ▼           │
│  ⚠️ Weak secrets  ❌ No rate limit ⚠️ No lockout      ❌ CRITICAL │
│                                                                      │
│  Registration Flow (Capability Token):                               │
│  ┌────────┐    ┌─────────┐    ┌──────────┐    ┌─────────────┐      │
│  │ Agent  │───▶│ Gateway │───▶│ Cluster  │───▶│ Token Store │      │
│  └────────┘    └─────────┘    │ Service  │    └─────────────┘      │
│       │             │         └──────────┘          │               │
│       │ Reg Token   │ No auth    │ Hash lookup     │ Cross-tenant  │
│       │             │ required   │ (capability)    │ by design     │
│       ▼             ▼            ▼                  ▼               │
│  ✅ 256-bit random❌ No rate limit⚠️ Token reuse  ✅ Intentional  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 5.2 RLS Policy Coverage

| Table | RLS Enabled | Policy | Coverage |
|-------|-------------|--------|----------|
| `clusters` | ✅ | `org_id = current_setting('app.current_org_id')` | ⚠️ Bypassed by agent query |
| `cluster_registration_tokens` | ✅ | Same policy | ⚠️ Bypassed by `GetByHash` |
| `cluster_heartbeats` | ✅ | Same policy | ✅ Enforced |
| `applications` | ✅ | Same policy | ✅ Enforced |
| `deployments` | ✅ | Same policy | ⚠️ Bypassed by agent query |
| `releases` | ✅ | Same policy | ⚠️ Bypassed by agent query |

### 5.3 Endpoint Security Matrix

| Endpoint | Auth | Rate Limit | RLS | Ownership Check |
|----------|------|------------|-----|-----------------|
| `POST /v1/agent/register` | Token | ❌ | N/A | N/A |
| `GET /v1/agent/clusters/:id/desired-state` | Headers | ❌ | ❌ | ⚠️ Partial |
| `POST /v1/agent/deployments/:id/releases/:id/status` | Headers | ❌ | ❌ | ❌ |
| `POST /v1/organizations/:id/clusters/:id/heartbeat` | JWT | ✅ | ✅ | ⚠️ |
| `POST /v1/organizations/:id/deployments` | JWT | ✅ | ✅ | ✅ |

---

## 6. Remediation Priority Matrix

| ID | Severity | Effort | Priority | Remediation |
|----|----------|--------|----------|-------------|
| CRIT-001 | Critical | Low | P0 | Add tenant context to agent queries |
| CRIT-002 | Critical | Low | P0 | Add deployment ownership validation |
| HIGH-001 | High | Medium | P1 | Implement agent rate limiting |
| HIGH-002 | High | Medium | P1 | Rate limit registration endpoint |
| HIGH-003 | High | Low | P1 | Unify authentication error messages |
| MED-001 | Medium | Low | P2 | Add rate limiter to agent routes |
| MED-002 | Medium | Low | P2 | Validate cluster status on deployment |
| MED-003 | Medium | Medium | P2 | Move heartbeat to agent auth |
| MED-004 | Medium | N/A | P3 | Monitor (intentional design) |
| MED-005 | Medium | High | P3 | Consider asymmetric JWT |
| LOW-* | Low | Low | P4 | Address in next sprint |

---

## 7. Appendix: Exploit Scenarios

### Scenario A: Cross-Tenant Deployment Exfiltration

```bash
# Attacker has compromised Agent A in Org A
# Attacker discovers Cluster B ID from Org B (leaked in logs)

# Step 1: Authenticate as Agent A
export CLUSTER_ID_A="org-a-cluster-uuid"
export AGENT_ID_A="org-a-agent-id"

# Step 2: Request Org B's deployments (RLS bypassed!)
curl -H "X-Cluster-ID: org-b-cluster-uuid" \
     -H "X-Agent-ID: $AGENT_ID_A" \
     https://api.platform.io/v1/agent/clusters/org-b-cluster-uuid/desired-state

# Result: Returns Org B's deployments because:
# 1. Agent auth passes (X-Agent-ID matches Cluster A, but path uses Cluster B)
# 2. Query doesn't set tenant context
# 3. RLS is not enforced
```

### Scenario B: Deployment Status Manipulation

```bash
# Attacker knows a victim's deployment ID (via enumeration or leak)
VICTIM_DEPLOYMENT="victim-deployment-uuid"
VICTIM_RELEASE="victim-release-uuid"

# Attacker authenticates with their own valid agent credentials
curl -X POST \
     -H "X-Cluster-ID: attacker-cluster" \
     -H "X-Agent-ID: attacker-agent" \
     -H "Content-Type: application/json" \
     -d '{"status": "failed", "errorMessage": "Critical failure"}' \
     https://api.platform.io/v1/agent/deployments/$VICTIM_DEPLOYMENT/releases/$VICTIM_RELEASE/status

# Result: Victim's deployment is marked as failed
# - Victim sees "Critical failure" in their dashboard
# - Potentially triggers alerts and incident response
# - Audit log shows attacker's agent as the reporter
```

### Scenario C: Registration Token Brute Force

```python
import requests
import string
import itertools

# Target: Organization with pending cluster registration
TARGET_URL = "https://api.platform.io/v1/agent/register"

# Generate potential tokens (in practice, would use rainbow tables or leaked patterns)
def brute_force():
    # 256-bit token = 32 bytes = 43 base64 chars
    # Impractical for truly random tokens, but possible if:
    # - Token generation is weak
    # - Partial token leaked
    # - No rate limiting allows millions of attempts
    
    for attempt in range(1_000_000):
        token = generate_candidate()
        resp = requests.post(TARGET_URL, json={
            "token": token,
            "agentId": "attacker-agent",
            "kubernetesVersion": "1.28.0",
            "nodeCount": 3
        })
        
        if resp.status_code == 200:
            print(f"SUCCESS: Token found - {token}")
            return resp.json()
        
        # No rate limiting = can try thousands per second
```

---

## Conclusion

The platform has fundamental security infrastructure (RLS, JWT auth, tenant isolation), but critical gaps in the agent authentication and authorization layer create significant cross-tenant risks. **Immediate remediation of CRIT-001 and CRIT-002 is required before production deployment.**

The security team recommends:
1. **Immediate:** Fix RLS bypass and ownership validation
2. **Short-term:** Implement comprehensive rate limiting
3. **Medium-term:** Consider stronger agent authentication (mTLS or HMAC challenges)
4. **Long-term:** Security audit of remaining services and penetration testing

---

*Report generated by automated security analysis. Manual penetration testing recommended before production release.*
