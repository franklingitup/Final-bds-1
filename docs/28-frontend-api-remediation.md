# Frontend API Remediation Guide

**Date:** 2026-06-24  
**Auditor:** Principal API Architect  
**Scope:** Frontend ↔ Backend API alignment

---

## Executive Summary

| Severity | Count |
|----------|-------|
| **Critical** | 12 |
| **High** | 15 |
| **Medium** | 11 |

The frontend API layer has **38 distinct misalignments** with the backend. The most severe issues are:
1. Error envelope parsing fails on every API error
2. Pagination field names differ (`cursor` vs `nextCursor`)
3. 6 endpoints called by frontend don't exist
4. 18+ DTO field mismatches cause runtime parse failures

---

## Critical Findings

### CRIT-01: Error Envelope Structure Mismatch

**Impact:** All API errors are unparseable; error messages never display correctly

**Frontend expects:**
```typescript
// frontend/src/types/api.ts:3-7
export interface ApiError {
  error: string;         // Flat string
  code?: string;         // Top-level code
  details?: Record<string, string[]>;
}
```

**Backend returns:**
```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "invalid request body",
    "details": ["name is required"],
    "requestId": "abc123"
  }
}
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Update `ApiError` interface |
| `frontend/src/lib/api/client.ts` | Update error parsing at lines 177-189 |

**Fix:**

```typescript
// frontend/src/types/api.ts (REPLACE lines 3-7)
export interface ApiErrorDetail {
  code: string;
  message: string;
  details?: string[];
  requestId?: string;
}

export interface ApiError {
  error: ApiErrorDetail;
}
```

```typescript
// frontend/src/lib/api/client.ts (REPLACE lines 177-189)
let errorData: ApiError;
try {
  errorData = await response.json();
} catch {
  errorData = { error: { code: "UNKNOWN", message: response.statusText } };
}

throw new ApiClientError(
  response.status,
  errorData.error.code,
  errorData.error.message,
  errorData.error.details ? { general: errorData.error.details } : undefined
);
```

---

### CRIT-02: Pagination Response Field Name Mismatch

**Impact:** Pagination breaks on every list endpoint; `cursor` is always undefined

**Frontend expects:**
```typescript
// frontend/src/types/api.ts:9-13
export interface PaginatedResponse<T> {
  items: T[];
  cursor?: string;    // WRONG
  hasMore?: boolean;  // WRONG
}
```

**Backend returns:**
```json
{
  "items": [...],
  "nextCursor": "eyJjIjoi..."
}
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Fix `PaginatedResponse` interface |

**Fix:**

```typescript
// frontend/src/types/api.ts (REPLACE lines 9-13)
export interface PaginatedResponse<T> {
  items: T[];
  nextCursor?: string;
}
```

Also update all usages of `.cursor` to `.nextCursor` in hooks.

---

### CRIT-03: Missing List Organizations Endpoint

**Impact:** Organization list page shows empty/error; users can't see their orgs

**Frontend calls:**
```typescript
// frontend/src/lib/api/organizations.ts:13-14
async list(params?: { limit?: number; cursor?: string }): Promise<PaginatedResponse<Organization>> {
  return apiClient.get<PaginatedResponse<Organization>>("/v1/organizations", params);
}
```

**Backend:** Endpoint does not exist in `tenant/internal/routes.go`

**Files to modify:**

| File | Change |
|------|--------|
| `backend/services/tenant/internal/routes.go` | Add `GET /organizations` route |
| `backend/services/tenant/internal/handlers.go` | Add `ListOrganizations` handler |
| `backend/services/tenant/internal/service.go` | Add `ListUserOrganizations` method |
| `backend/services/tenant/internal/repository.go` | Add `ListByUser` query |
| `backend/services/gateway/internal/router/router.go` | Add gateway route |

---

### CRIT-04: Missing Get Organization by Slug Endpoint

**Impact:** Dashboard navigation fails; URLs like `/acme/projects` don't resolve

**Frontend calls:**
```typescript
// frontend/src/lib/api/organizations.ts:21-23
async getBySlug(slug: string): Promise<Organization> {
  return apiClient.get<Organization>(`/v1/organizations/by-slug/${slug}`);
}
```

**Backend:** Endpoint does not exist

**Files to modify:**

| File | Change |
|------|--------|
| `backend/services/tenant/internal/routes.go` | Add route |
| `backend/services/tenant/internal/handlers.go` | Add `GetOrganizationBySlug` handler |
| `backend/services/tenant/internal/service.go` | Add service method |
| `backend/services/tenant/internal/repository.go` | Add `GetBySlug` query |
| `backend/services/gateway/internal/router/router.go` | Add gateway route |

---

### CRIT-05: Missing List Deployments at Org Level

**Impact:** Deployments page shows empty/error

**Frontend calls:**
```typescript
// frontend/src/lib/api/deployments.ts:12-17
async list(orgId: string, params?: {...}): Promise<PaginatedResponse<Deployment>> {
  return apiClient.get<PaginatedResponse<Deployment>>(`/v1/organizations/${orgId}/deployments`, params);
}
```

**Backend:** Only `POST /organizations/:orgId/deployments` exists, not GET

**Files to modify:**

| File | Change |
|------|--------|
| `backend/services/deployment/internal/routes.go` | Add `GET /organizations/:orgId/deployments` |
| `backend/services/deployment/internal/handlers.go` | Add `ListOrgDeployments` handler |
| `backend/services/deployment/internal/service.go` | Add service method |
| `backend/services/gateway/internal/router/router.go` | Add gateway route |

---

### CRIT-06: Missing Delete Deployment Endpoint

**Impact:** Users cannot delete deployments

**Frontend calls:**
```typescript
// frontend/src/lib/api/deployments.ts:31-32
async delete(orgId: string, deploymentId: string): Promise<void> {
  return apiClient.delete(`/v1/organizations/${orgId}/deployments/${deploymentId}`);
}
```

**Backend:** DELETE route does not exist

**Files to modify:**

| File | Change |
|------|--------|
| `backend/services/deployment/internal/routes.go` | Add DELETE route |
| `backend/services/deployment/internal/handlers.go` | Add `DeleteDeployment` handler |
| `backend/services/deployment/internal/service.go` | Add `DeleteDeployment` method |
| `backend/services/gateway/internal/router/router.go` | Add gateway route |

---

### CRIT-07: Audit API Path Mismatch

**Impact:** Audit page returns 404

**Frontend calls:**
```typescript
// frontend/src/lib/api/audit.ts:9,13
`/v1/organizations/${orgId}/audit`
`/v1/organizations/${orgId}/audit/${auditId}`
```

**Backend routes:**
```go
// backend/services/audit/internal/routes.go:13-15
logs := v1.Group("/organizations/:orgId/audit-logs")  // Uses "audit-logs"
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/lib/api/audit.ts` | Change `/audit` to `/audit-logs` |

**Fix:**

```typescript
// frontend/src/lib/api/audit.ts (REPLACE)
async list(...): Promise<PaginatedResponse<AuditLog>> {
  return apiClient.get<PaginatedResponse<AuditLog>>(`/v1/organizations/${orgId}/audit-logs`, params);
}

async get(orgId: string, eventId: string): Promise<AuditLog> {
  return apiClient.get<AuditLog>(`/v1/organizations/${orgId}/audit-logs/${eventId}`);
}
```

---

### CRIT-08: Application API Path Mismatch (Get/Update/Delete)

**Impact:** Application detail, edit, and delete fail with 404

**Frontend calls:**
```typescript
// frontend/src/lib/api/applications.ts:21-49
`/v1/organizations/${orgId}/projects/${projectId}/applications/${applicationId}`
```

**Backend routes:**
```go
// backend/services/deployment/internal/routes.go:19-22
singleApp := authenticated.Group("/organizations/:orgId/applications/:appId")
singleApp.Get("", h.GetApplication)
singleApp.Patch("", h.UpdateApplication)
singleApp.Delete("", h.DeleteApplication)
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/lib/api/applications.ts` | Update paths for get/update/delete |
| `frontend/src/components/applications/*` | Update function signatures |

**Fix:**

```typescript
// frontend/src/lib/api/applications.ts (REPLACE get/update/delete)
async get(orgId: string, applicationId: string): Promise<Application> {
  return apiClient.get<Application>(
    `/v1/organizations/${orgId}/applications/${applicationId}`
  );
}

async update(orgId: string, applicationId: string, data: UpdateApplicationRequest): Promise<Application> {
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

---

### CRIT-09: Logout Requires Request Body

**Impact:** Logout fails silently or with 422

**Frontend:**
```typescript
// frontend/src/lib/api/auth.ts:25-30
async logout(): Promise<void> {
  try {
    await apiClient.post("/v1/auth/logout");  // No body
  } finally {
    apiClient.clearTokens();
  }
}
```

**Backend expects:**
```go
// backend/services/auth/internal/domain.go:122-124
type LogoutRequest struct {
    RefreshToken string `json:"refreshToken"`
}
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/lib/api/auth.ts` | Send refreshToken in logout body |

**Fix:**

```typescript
// frontend/src/lib/api/auth.ts (REPLACE logout)
async logout(): Promise<void> {
  try {
    const refreshToken = localStorage.getItem("refreshToken");
    if (refreshToken) {
      await apiClient.post("/v1/auth/logout", { refreshToken });
    }
  } finally {
    apiClient.clearTokens();
  }
}
```

---

### CRIT-10: Missing Change Password Endpoint

**Impact:** Change password feature doesn't work

**Frontend calls:**
```typescript
// frontend/src/lib/api/auth.ts:41-43
async changePassword(data: { currentPassword: string; newPassword: string }): Promise<void> {
  return apiClient.post("/v1/auth/change-password", data);
}
```

**Backend:** Endpoint does not exist

**Files to modify:**

| File | Change |
|------|--------|
| `backend/services/auth/internal/routes.go` | Add route (or) |
| `frontend/src/lib/api/auth.ts` | Remove method if not implementing |

**Recommendation:** Either implement backend endpoint or remove frontend method and related UI.

---

### CRIT-11: Deployment Mutation Response Shape Mismatch

**Impact:** Create/update/rollback deployment fails to parse response

**Frontend expects:** `Deployment` or `Release` directly

**Backend returns:**
```json
{
  "deployment": { ... },
  "release": { ... }
}
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/lib/api/deployments.ts` | Update response handling |
| `frontend/src/types/api.ts` | Add wrapped response type |

**Fix:**

```typescript
// frontend/src/types/api.ts (ADD)
export interface DeploymentCreateResponse {
  deployment: Deployment;
  release: Release;
}

// frontend/src/lib/api/deployments.ts (MODIFY)
async create(orgId: string, data: CreateDeploymentRequest): Promise<DeploymentCreateResponse> {
  return apiClient.post<DeploymentCreateResponse>(`/v1/organizations/${orgId}/deployments`, data);
}

async update(orgId: string, deploymentId: string, data: UpdateDeploymentRequest): Promise<DeploymentCreateResponse> {
  return apiClient.patch<DeploymentCreateResponse>(`/v1/organizations/${orgId}/deployments/${deploymentId}`, data);
}

async rollback(orgId: string, deploymentId: string, data?: RollbackRequest): Promise<DeploymentCreateResponse> {
  return apiClient.post<DeploymentCreateResponse>(
    `/v1/organizations/${orgId}/deployments/${deploymentId}/rollback`,
    data
  );
}
```

---

### CRIT-12: Member Path Parameter Mismatch

**Impact:** Update/remove member fails with 404

**Frontend uses:**
```typescript
// frontend/src/lib/api/organizations.ts:42-47
`/v1/organizations/${orgId}/members/${memberId}`
```

**Backend expects:**
```go
// backend/services/tenant/internal/routes.go:21-22
orgs.Patch("/:orgId/members/:userId", h.ChangeRole)
orgs.Delete("/:orgId/members/:userId", h.RemoveMember)
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/lib/api/organizations.ts` | Rename `memberId` param to `userId` |
| `frontend/src/lib/api/projects.ts` | Same for project members |

**Fix:**

```typescript
// frontend/src/lib/api/organizations.ts (RENAME parameter)
async updateMemberRole(orgId: string, userId: string, role: string): Promise<OrganizationMember> {
  return apiClient.patch<OrganizationMember>(`/v1/organizations/${orgId}/members/${userId}`, { role });
}

async removeMember(orgId: string, userId: string): Promise<void> {
  return apiClient.delete(`/v1/organizations/${orgId}/members/${userId}`);
}
```

---

## High Findings

### HIGH-01: User DTO Field Mismatch

**Frontend expects:**
```typescript
// frontend/src/types/api.ts:16-22
export interface User {
  id: string;
  email: string;
  name: string;
  avatarUrl?: string;   // NOT IN BACKEND
  createdAt: string;    // NOT IN BACKEND
}
```

**Backend returns:**
```go
// backend/services/auth/internal/domain.go:163-169
type UserProfile struct {
    ID            string `json:"id"`
    Email         string `json:"email"`
    Name          string `json:"name"`
    EmailVerified bool   `json:"emailVerified"`  // NOT IN FRONTEND
    MFAEnabled    bool   `json:"mfaEnabled"`     // NOT IN FRONTEND
}
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Update `User` interface |

**Fix:**

```typescript
// frontend/src/types/api.ts (REPLACE User)
export interface User {
  id: string;
  email: string;
  name: string;
  emailVerified: boolean;
  mfaEnabled: boolean;
}
```

---

### HIGH-02: Organization DTO Field Mismatch

**Frontend expects:**
```typescript
// frontend/src/types/api.ts:42-50
export interface Organization {
  id: string;
  name: string;
  slug: string;
  description?: string;  // NOT IN BACKEND
  logoUrl?: string;      // NOT IN BACKEND
  createdAt: string;
  updatedAt: string;     // NOT IN BACKEND
}
```

**Backend returns:**
```go
// backend/services/tenant/internal/domain.go:140-147
type OrganizationView struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Slug      string    `json:"slug"`
    Plan      string    `json:"plan"`      // NOT IN FRONTEND
    Status    string    `json:"status"`    // NOT IN FRONTEND
    CreatedAt time.Time `json:"createdAt"`
}
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Update `Organization` interface |

**Fix:**

```typescript
// frontend/src/types/api.ts (REPLACE Organization)
export interface Organization {
  id: string;
  name: string;
  slug: string;
  plan: string;
  status: string;
  createdAt: string;
}
```

---

### HIGH-03: OrganizationMember DTO Mismatch

**Frontend expects:**
```typescript
// frontend/src/types/api.ts:52-59
export interface OrganizationMember {
  id: string;              // NOT IN BACKEND
  userId: string;
  organizationId: string;  // NOT IN BACKEND
  role: "owner" | "admin" | "member" | "viewer";  // "member" WRONG
  user: User;              // NOT IN BACKEND
  createdAt: string;
}
```

**Backend returns:**
```go
// backend/services/tenant/internal/domain.go:156-161
type MemberView struct {
    UserID    string    `json:"userId"`
    Role      Role      `json:"role"`    // "developer" not "member"
    Status    string    `json:"status"`  // NOT IN FRONTEND
    CreatedAt time.Time `json:"createdAt"`
}
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Update `OrganizationMember` interface |

**Fix:**

```typescript
// frontend/src/types/api.ts (REPLACE OrganizationMember)
export interface OrganizationMember {
  userId: string;
  role: "owner" | "admin" | "developer" | "viewer";
  status: string;
  createdAt: string;
}
```

---

### HIGH-04: InviteMemberRequest Role Mismatch

**Frontend:**
```typescript
// frontend/src/types/api.ts:72-75
export interface InviteMemberRequest {
  email: string;
  role: "admin" | "member" | "viewer";  // "member" WRONG
}
```

**Backend:**
```go
// backend/services/tenant/internal/domain.go:15-19
const (
    RoleOwner     Role = "owner"
    RoleAdmin     Role = "admin"
    RoleDeveloper Role = "developer"  // NOT "member"
    RoleViewer    Role = "viewer"
)
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Change `"member"` to `"developer"` |

---

### HIGH-05: Invitation Response Mismatch

**Frontend expects:** Full `Invitation` object

**Backend returns:**
```go
// backend/services/tenant/internal/handlers.go:124-127
return c.Status(fiber.StatusCreated).JSON(fiber.Map{
    "invitationId": inv.ID,
    "status":       inv.Status,
})
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Add `InvitationCreateResponse` type |
| `frontend/src/lib/api/organizations.ts` | Update return type |

---

### HIGH-06: Accept Invitation Response Mismatch

**Frontend expects:** `OrganizationMember`

**Backend returns:**
```go
// backend/services/tenant/internal/handlers.go:158-161
return c.JSON(fiber.Map{
    "orgId": member.OrgID,
    "role":  member.Role,
})
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Add `AcceptInvitationResponse` type |
| `frontend/src/lib/api/organizations.ts` | Update return type |

---

### HIGH-07: ProjectMember DTO Mismatch

Same issue as OrganizationMember - no nested `user`, no `id`, no `projectId`.

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Update `ProjectMember` interface |

---

### HIGH-08: Secret DTO Missing organizationId

**Frontend expects:**
```typescript
// frontend/src/types/api.ts:162-171
export interface Secret {
  id: string;
  organizationId: string;  // NOT IN BACKEND
  projectId: string;
  // ...
}
```

**Backend returns:** Only `projectId`, no `organizationId`

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Remove `organizationId` from `Secret` |

---

### HIGH-09: Secrets Pagination Format Differs

**Backend:**
```go
// backend/services/secrets/internal/domain.go:94-99
type SecretListView struct {
    Items      []SecretView `json:"items"`
    Total      int          `json:"total"`      // NOT IN FRONTEND
    HasMore    bool         `json:"hasMore"`    // FRONTEND HAS THIS
    NextCursor string       `json:"nextCursor,omitempty"`
}
```

**Fix:** Update `PaginatedResponse` or create `SecretListResponse` type.

---

### HIGH-10: AuditLog DTO Complete Mismatch

**Frontend expects:**
```typescript
// frontend/src/types/api.ts:278-292
export interface AuditLog {
  id: string;
  organizationId: string;
  actorType: string;       // NOT IN BACKEND
  actorId: string;
  actorName?: string;      // NOT IN BACKEND
  action: string;          // NOT IN BACKEND (uses eventType)
  resourceType: string;
  resourceId: string;
  resourceName?: string;   // NOT IN BACKEND
  metadata?: Record<string, unknown>;  // DIFFERENT (payload)
  ipAddress?: string;      // NOT IN BACKEND
  userAgent?: string;      // NOT IN BACKEND
  createdAt: string;
}
```

**Backend returns:**
```go
// backend/services/audit/internal/domain.go:83-94
type AuditLogView struct {
    ID           string          `json:"id"`
    EventID      string          `json:"eventId"`       // NOT IN FRONTEND
    EventType    string          `json:"eventType"`     // NOT IN FRONTEND
    OrgID        string          `json:"organizationId"`
    ActorID      string          `json:"actorId,omitempty"`
    ResourceType string          `json:"resourceType,omitempty"`
    ResourceID   string          `json:"resourceId,omitempty"`
    Timestamp    time.Time       `json:"timestamp"`     // NOT IN FRONTEND
    Payload      json.RawMessage `json:"payload,omitempty"`
    RecordedAt   time.Time       `json:"recordedAt"`    // NOT IN FRONTEND
}
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Completely rewrite `AuditLog` interface |

**Fix:**

```typescript
// frontend/src/types/api.ts (REPLACE AuditLog)
export interface AuditLog {
  id: string;
  eventId: string;
  eventType: string;
  organizationId: string;
  actorId?: string;
  resourceType?: string;
  resourceId?: string;
  timestamp: string;
  payload?: Record<string, unknown>;
  recordedAt: string;
}
```

---

### HIGH-11: AuditLogFilter Query Parameters Mismatch

**Frontend:**
```typescript
// frontend/src/types/api.ts:294-301
export interface AuditLogFilter {
  actorId?: string;
  action?: string;       // NOT IN BACKEND
  resourceType?: string;
  resourceId?: string;
  startDate?: string;    // BACKEND uses "from"
  endDate?: string;      // BACKEND uses "to"
}
```

**Backend:**
```go
// backend/services/audit/internal/handlers.go:37-44
From: parseTime(c.Query("from")),
To:   parseTime(c.Query("to")),
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Rename `startDate`/`endDate` to `from`/`to` |

---

### HIGH-12: Heartbeat DTO Mismatch

**Frontend expects:**
```typescript
// frontend/src/types/api.ts:152-159
export interface Heartbeat {
  id: string;              // NOT IN BACKEND
  agentId: string;
  kubernetesVersion: string;
  nodeCount: number;
  apiServerHealthy: boolean;
  receivedAt: string;
}
```

**Backend returns:**
```go
// backend/services/cluster/internal/domain.go:206-213
type HeartbeatView struct {
    ClusterID          string    `json:"clusterId"`  // NOT IN FRONTEND
    AgentID            string    `json:"agentId"`
    // ...
    ReceivedAt         time.Time `json:"receivedAt"`
}
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Update `Heartbeat` interface |

---

### HIGH-13: Missing updatedAt on Multiple Types

The following types have `updatedAt` in frontend but backend doesn't return it:
- `Cluster`
- `Application`
- `Deployment`
- `Project`

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Remove `updatedAt` from these types |

---

### HIGH-14: Missing Project by Slug Endpoint

**Frontend calls:**
```typescript
// frontend/src/lib/api/projects.ts:19-21
async getBySlug(orgId: string, slug: string): Promise<Project> {
  return apiClient.get<Project>(`/v1/organizations/${orgId}/projects/by-slug/${slug}`);
}
```

**Backend:** Endpoint does not exist

**Files to modify:**

| File | Change |
|------|--------|
| `backend/services/project/internal/routes.go` | Add route (or) |
| `frontend/src/lib/api/projects.ts` | Remove method if not needed |

---

### HIGH-15: CreateOrganizationRequest Has description

**Frontend:**
```typescript
// frontend/src/types/api.ts:61-65
export interface CreateOrganizationRequest {
  name: string;
  slug: string;
  description?: string;  // NOT IN BACKEND
}
```

**Backend:**
```go
// backend/services/tenant/internal/domain.go:116-119
type CreateOrganizationRequest struct {
    Name string `json:"name"`
    Slug string `json:"slug"`
    // NO description
}
```

**Files to modify:**

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Remove `description` from request |

---

## Medium Findings

### MED-01: Cluster Missing Labels in Frontend Type

Backend returns `labels: map[string]string`, frontend type doesn't include it.

**Files to modify:** `frontend/src/types/api.ts`

---

### MED-02: RegistrationToken Response Has Extra Fields on Create

Backend `TokenWithSecret` includes `token` only on create; frontend type always expects it.

**Files to modify:** `frontend/src/types/api.ts`

---

### MED-03: Release Status Enum Difference

**Frontend:** `"pending" | "deploying" | "succeeded" | "failed" | "rolled_back"`

**Backend:** Same, but user handler accepts `"started"` instead of `"deploying"`

**Files to modify:** Document this difference or align

---

### MED-04: UpdateOrganizationRequest Field Mismatch

**Frontend has:** `name?, description?`  
**Backend has:** `name?, plan?`

**Files to modify:** `frontend/src/types/api.ts`

---

### MED-05: Application RuntimeType Optional in Request

**Frontend:** Optional `runtimeType?: string`  
**Backend:** Defaults to `"container"` if not provided

**Status:** Acceptable but document default

---

### MED-06: EnvVar Type Duplication

EnvVar is defined in both frontend and matches backend. No change needed but note for reference.

---

### MED-07: Project Status Field Missing from Frontend

Backend returns `status: "active" | "archived"`, frontend doesn't include it.

**Files to modify:** `frontend/src/types/api.ts`

---

### MED-08: Deployment Status Values Incomplete

**Frontend:** `"pending" | "running" | "succeeded" | "failed" | "rolled_back"`

Backend also has these but maps from release status. Document relationship.

---

### MED-09: Secret CreateRequest description Optional Handling

Both have `description?: string` - aligned.

---

### MED-10: Cluster Provider Enum Not Enforced

Frontend accepts any string, backend has specific constants. Consider adding enum.

---

### MED-11: Auth Refresh Request Body Mismatch

Public `authApi.refreshToken()` doesn't send body, but internal refresh does.

**Files to modify:** `frontend/src/lib/api/auth.ts`

---

## Summary Table: Files Requiring Modification

### Frontend Files

| File | Changes Required |
|------|------------------|
| `frontend/src/types/api.ts` | 15 interface fixes |
| `frontend/src/lib/api/client.ts` | Error parsing fix |
| `frontend/src/lib/api/auth.ts` | Logout body, change-password |
| `frontend/src/lib/api/organizations.ts` | Member path param, response types |
| `frontend/src/lib/api/projects.ts` | Member path param, remove by-slug |
| `frontend/src/lib/api/applications.ts` | Path fix for get/update/delete |
| `frontend/src/lib/api/deployments.ts` | Response type wrapping |
| `frontend/src/lib/api/audit.ts` | Path fix, filter params |
| `frontend/src/hooks/use-*.ts` | Pagination field updates |

### Backend Files (if implementing missing endpoints)

| File | Changes Required |
|------|------------------|
| `backend/services/tenant/internal/routes.go` | List orgs, get by slug |
| `backend/services/tenant/internal/handlers.go` | New handlers |
| `backend/services/tenant/internal/service.go` | New service methods |
| `backend/services/tenant/internal/repository.go` | New queries |
| `backend/services/deployment/internal/routes.go` | List at org level, delete |
| `backend/services/deployment/internal/handlers.go` | New handlers |
| `backend/services/deployment/internal/service.go` | New service methods |
| `backend/services/gateway/internal/router/router.go` | New routes |

---

## Remediation Priority

### Phase 1: Critical Path (Day 1)

1. CRIT-01: Error envelope parsing
2. CRIT-02: Pagination field names
3. CRIT-07: Audit path fix
4. CRIT-08: Application path fix
5. CRIT-09: Logout body
6. CRIT-11: Deployment response wrapping
7. CRIT-12: Member path parameter

### Phase 2: Missing Endpoints (Day 2)

1. CRIT-03: List organizations
2. CRIT-04: Get org by slug
3. CRIT-05: List deployments at org level
4. CRIT-06: Delete deployment

### Phase 3: DTO Alignment (Day 3)

All HIGH findings related to DTO mismatches

### Phase 4: Cleanup (Day 4)

- Remove unused frontend methods
- Add missing fields to types
- Document intentional differences
