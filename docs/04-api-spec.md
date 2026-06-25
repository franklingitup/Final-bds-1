# 04 — API Specification

## Conventions

- **Protocol:** REST over HTTPS; JSON request/response. WebSocket for log streaming.
- **Auth:** `Authorization: Bearer <accessToken>`; agent endpoints use agent credentials; install endpoints use one-time tokens.
- **Tenant scoping:** `org_id` is derived from the token. Path `orgId`/`projectId` must fall within the caller's accessible scope.
- **Versioning:** prefix `/v1`.
- **Pagination:** cursor-based, `?limit=&cursor=`; responses include `nextCursor`.
- **Idempotency:** mutating endpoints accept `Idempotency-Key` header.

### Standard Error Envelope
```json
{
  "error": {
    "code": "STRING_CODE",
    "message": "human readable",
    "details": [],
    "requestId": "uuid"
  }
}
```

### Common Error Codes
| Code | HTTP | Meaning |
|---|---|---|
| `UNAUTHENTICATED` | 401 | Missing/invalid credentials |
| `FORBIDDEN` | 403 | Authenticated but not authorized |
| `NOT_FOUND` | 404 | Resource not found / out of scope |
| `VALIDATION_FAILED` | 422 | Request schema/business validation failed |
| `CONFLICT` | 409 | State conflict (duplicate, wrong state) |
| `RATE_LIMITED` | 429 | Throttled |
| `INTERNAL` | 500 | Unexpected server error |

---

## 1. Auth

### POST /v1/auth/login  *(public)*
- **Request:** `{ email: string, password: string, mfaCode?: string }`
- **Response 200:** `{ accessToken, refreshToken, expiresIn, user: { id, email, name } }`
- **Validation:** valid email; non-empty password.
- **Authorization:** none.
- **Errors:** `INVALID_CREDENTIALS` (401), `MFA_REQUIRED` (401), `ACCOUNT_LOCKED` (423), `RATE_LIMITED`.

### POST /v1/auth/refresh  *(refresh token)*
- **Request:** `{ refreshToken: string }`
- **Response 200:** `{ accessToken, refreshToken, expiresIn }`
- **Errors:** `INVALID_TOKEN` (401), `TOKEN_REVOKED` (401).

### POST /v1/auth/logout
- **Request:** `{ refreshToken: string }`
- **Response 204**
- **Authorization:** authenticated.

### GET /v1/auth/me
- **Response 200:** `{ id, email, name, organizations: [{ orgId, role }] }`
- **Authorization:** any authenticated user.

---

## 2. Organizations & Projects

### POST /v1/organizations
- **Request:** `{ name: string, slug: string }`
- **Response 201:** `{ id, name, slug, plan, createdAt }`
- **Validation:** `name` 2–64 chars; `slug` lowercase `[a-z0-9-]`, globally unique.
- **Authorization:** any authenticated user (becomes owner).
- **Errors:** `SLUG_TAKEN` (409), `VALIDATION_FAILED`.

### GET /v1/organizations/{orgId}
- **Response 200:** `{ id, name, slug, plan, status, createdAt }`
- **Authorization:** member of org.

### POST /v1/organizations/{orgId}/members
- **Request:** `{ email: string, role: "admin"|"member"|"auditor" }`
- **Response 201:** `{ invitationId, status: "pending" }`
- **Validation:** valid email; role in enum.
- **Authorization:** org `owner`/`admin`.
- **Errors:** `FORBIDDEN`, `MEMBER_EXISTS` (409).

### POST /v1/organizations/{orgId}/projects
- **Request:** `{ name: string, slug: string, description?: string }`
- **Response 201:** `{ id, orgId, name, slug, createdBy }`
- **Validation:** `slug` unique within org.
- **Authorization:** org `admin`.
- **Errors:** `SLUG_TAKEN` (409), `FORBIDDEN`.

### GET /v1/projects/{projectId}
- **Response 200:** `{ id, orgId, name, slug, description }`
- **Authorization:** project member.

---

## 3. Clusters

### POST /v1/organizations/{orgId}/clusters/register
- **Request:** `{ name: string, provider: "aws"|"gcp"|"azure", region?: string }`
- **Response 201:** `{ clusterId, installSessionId, installCommand, token, expiresAt }` *(token shown once)*
- **Validation:** `name` unique within org; provider in enum.
- **Authorization:** org `admin`.
- **Errors:** `NAME_TAKEN` (409), `FORBIDDEN`.

### POST /v1/organizations/{orgId}/clusters/provision
- **Request:**
```json
{
  "name": "prod-eks",
  "provider": "aws",
  "region": "us-east-1",
  "kubernetesVersion": "1.30",
  "nodePools": [{ "name": "default", "instanceType": "m6i.large", "min": 2, "max": 6 }],
  "networking": { "vpcCidr": "10.0.0.0/16" }
}
```
- **Response 201:** `{ clusterId, installSessionId, installCommand, bundleUrl, expiresAt }`
- **Validation:** node pool `min ≤ max`; valid region/version for provider; CIDR well-formed.
- **Authorization:** org `admin`.
- **Errors:** `INVALID_PROVIDER_CONFIG` (422), `QUOTA_EXCEEDED` (403).

### GET /v1/clusters/{clusterId}
- **Response 200:** `{ id, provider, region, status, version, agentStatus, environments: [] }`
- **Authorization:** org member.

### POST /v1/clusters/{clusterId}/agents/register  *(one-time install token)*
- **Request:** `{ token: string, agentVersion: string, capabilities: [string] }`
- **Response 200:** `{ agentId, agentCredential, controlPlaneEndpoints }`
- **Validation:** token matches active session, not expired/used.
- **Authorization:** valid one-time install token.
- **Errors:** `INVALID_TOKEN` (401), `TOKEN_EXPIRED` (401), `TOKEN_ALREADY_USED` (409).

### POST /v1/clusters/{clusterId}/heartbeat  *(agent credential)*
- **Request:** `{ agentId, status, nodeCount, version, timestamp }`
- **Response 200:** `{ acknowledged: true, desiredAgentVersion }`
- **Authorization:** valid agent credential bound to cluster.
- **Errors:** `AGENT_NOT_REGISTERED` (404), `UNAUTHENTICATED`.

### POST /v1/clusters/{clusterId}/assignments
- **Request:** `{ projectId: string, environment: string, namespace?: string }`
- **Response 201:** `{ clusterEnvId, namespace }`
- **Authorization:** org `admin`.
- **Errors:** `CLUSTER_NOT_READY` (409).

---

## 4. Applications & Deployments

### POST /v1/projects/{projectId}/applications
- **Request:** `{ name: string, description?: string }`
- **Response 201:** `{ id, projectId, name }`
- **Authorization:** project `admin`/`developer`.

### POST /v1/applications/{appId}/environments
- **Request:** `{ environment: string, clusterEnvId: string }`
- **Response 201:** `{ id, appId, environment, namespace }`
- **Authorization:** project `admin`.
- **Errors:** `CLUSTER_ENV_NOT_FOUND` (404).

### POST /v1/applications/{appId}/deployments
- **Request:**
```json
{
  "environment": "prod",
  "source": { "type": "image", "image": "repo/app:tag" },
  "config": {
    "replicas": 2,
    "resources": { "cpu": "500m", "memory": "512Mi" },
    "ports": [{ "containerPort": 8080 }],
    "env": [{ "name": "K", "value": "V" }],
    "secretBindings": ["secretId1"],
    "autoscaling": { "min": 2, "max": 10, "cpuTarget": 70 }
  }
}
```
- **Response 202:** `{ deploymentId, revisionId, status: "pending" }`
- **Validation:** `source.type` in `{git,image,upload}`; if `image`, `image` required; if `git`, `repo`+`ref` required; `replicas ≥ 0`; valid resource quantities; referenced secrets exist and bound to env.
- **Authorization:** project `developer`+ on target environment.
- **Errors:** `ENVIRONMENT_NOT_FOUND` (404), `CLUSTER_NOT_READY` (409), `INVALID_SOURCE` (422), `SECRET_NOT_FOUND` (422).

### GET /v1/deployments/{deploymentId}
- **Response 200:** `{ id, status, revisionId, rollout: { ready, total }, startedAt, finishedAt }`
- **Authorization:** project read access.

### POST /v1/deployments/{deploymentId}/rollback
- **Request:** `{ targetRevisionId?: string }` *(defaults to previous healthy)*
- **Response 202:** `{ deploymentId, newRevisionId, status: "rolling_back" }`
- **Authorization:** project `developer`+.
- **Errors:** `NO_PREVIOUS_REVISION` (409).

### GET /v1/deployments/{deploymentId}/events
- **Response 200:** `{ events: [{ ts, type, message }], nextCursor }`
- **Authorization:** project read access.

---

## 5. Builds

### POST /v1/projects/{projectId}/builds
- **Request:** `{ source: { type: "git"|"upload", repo?, ref?, uploadId? }, dockerfilePath?: string }`
- **Response 202:** `{ buildId, status: "queued" }`
- **Validation:** valid source per type.
- **Authorization:** project `developer`+.
- **Errors:** `INVALID_SOURCE` (422).

### GET /v1/builds/{buildId}/logs
- **Query:** `?cursor=&limit=`
- **Response 200:** `{ entries: [{ ts, message }], nextCursor }`
- **Authorization:** project read access.

---

## 6. Secrets

### POST /v1/projects/{projectId}/secrets
- **Request:** `{ scope: "project"|"app", name: string, value: string, appEnvId?: string }`
- **Response 201:** `{ id, name, scope, version: 1 }` *(value never returned)*
- **Validation:** `name` `[A-Z0-9_]`, unique within scope; `appEnvId` required if `scope=app`.
- **Authorization:** project `admin`.
- **Errors:** `SECRET_EXISTS` (409).

### POST /v1/applications/{appId}/secret-bindings
- **Request:** `{ secretId: string, envVarName: string, appEnvId: string }`
- **Response 201:** `{ id }`
- **Validation:** `envVarName` unique within app env.
- **Authorization:** project `admin`.
- **Errors:** `SECRET_NOT_FOUND` (404), `BINDING_EXISTS` (409).

---

## 7. Domains & TLS

### POST /v1/projects/{projectId}/domains
- **Request:** `{ hostname: string }`
- **Response 201:** `{ id, hostname, verificationStatus: "pending", record: { type: "TXT", name, value } }`
- **Validation:** valid FQDN; globally unique.
- **Authorization:** project `admin`.
- **Errors:** `DOMAIN_EXISTS` (409).

### POST /v1/domains/{domainId}/verify
- **Request:** `{}`
- **Response 200:** `{ status: "verified"|"pending", method: "TXT", record: {} }`
- **Authorization:** project `admin`.
- **Errors:** `VERIFICATION_FAILED` (422), `DNS_RECORD_MISSING` (422).

### POST /v1/domains/{domainId}/bindings
- **Request:** `{ appEnvId: string, port: number, tlsEnabled: boolean }`
- **Response 201:** `{ id, status: "configuring" }`
- **Authorization:** project `admin`.
- **Errors:** `DOMAIN_NOT_VERIFIED` (409).

---

## 8. Observability

### GET /v1/applications/{appId}/logs
- **Query:** `?env=prod&since=&until=&limit=500&cursor=`
- **Response 200:** `{ entries: [{ ts, level, message, pod }], nextCursor }`
- **Authorization:** project read access.
- **Errors:** `TIME_RANGE_TOO_LARGE` (422).

### GET /v1/applications/{appId}/metrics
- **Query:** `?env=prod&metric=cpu|memory|requests|errors|latency&since=&until=&step=`
- **Response 200:** `{ series: [{ ts, value }], unit }`
- **Authorization:** project read access.

---

## 9. Audit

### GET /v1/organizations/{orgId}/audit-logs
- **Query:** `?actorId=&resourceType=&resourceId=&since=&until=&cursor=`
- **Response 200:** `{ entries: [{ id, actorId, action, resourceType, resourceId, metadata, createdAt }], nextCursor }`
- **Authorization:** org `auditor`/`admin`.

---

## Authorization Matrix (summary)

| Endpoint group | Owner | Org Admin | Project Admin | Developer | Auditor |
|---|---|---|---|---|---|
| Org CRUD | ✓ | partial | – | – | – |
| Members/Roles | ✓ | ✓ | – | – | – |
| Clusters | ✓ | ✓ | – | – | read |
| Projects | ✓ | ✓ | manage own | read | read |
| Applications/Deploy | ✓ | ✓ | ✓ | ✓ | read |
| Secrets | ✓ | ✓ | ✓ | – | metadata |
| Domains/TLS | ✓ | ✓ | ✓ | read | read |
| Logs/Metrics | ✓ | ✓ | ✓ | ✓ | read |
| Audit logs | ✓ | ✓ | – | – | ✓ |
