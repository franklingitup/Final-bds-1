# 15 — MVP Gap Analysis

This document provides a comprehensive analysis of the current implementation state against the MVP requirements defined in `09-mvp-roadmap.md`.

---

## Executive Summary

| Metric | Value |
|--------|-------|
| **Overall MVP Completion** | **38%** |
| **MVP Readiness Score** | **3/10** (Not Ready for Launch) |
| **Critical Blockers** | 4 |
| **Estimated Effort to MVP** | 4-6 weeks |

### Key Findings

1. **Foundation is solid:** All shared libraries, event framework, and database infrastructure are production-ready.
2. **Identity & Tenancy complete:** Auth, Tenant, Project, and Audit services are fully implemented.
3. **Cluster registration works:** End-to-end cluster registration workflow is functional.
4. **Core deployment loop missing:** Cannot deploy applications, manage secrets, or view logs.
5. **No UI exists:** Web console has not been started.

---

## 1. User Journey Analysis

### 1.1 Fully Functional User Journeys ✓

| Journey | Services Involved | Status |
|---------|-------------------|--------|
| **User signup and login** | Auth Service | ✓ Complete |
| **Create organization** | Tenant Service | ✓ Complete |
| **Invite members to organization** | Tenant Service | ✓ Complete |
| **Accept invitation** | Tenant Service | ✓ Complete |
| **Create project** | Project Service | ✓ Complete |
| **Add members to project** | Project Service | ✓ Complete |
| **Create cluster record** | Cluster Service | ✓ Complete |
| **Generate registration token** | Cluster Service | ✓ Complete |
| **Agent registration** | Cluster Service + Agent | ✓ Complete |
| **Agent heartbeat** | Cluster Service + Agent | ✓ Complete |
| **View audit logs** | Audit Service | ✓ Complete |

**Completion: 100%** (11/11 journeys)

### 1.2 Partially Functional User Journeys ⚠️

| Journey | What Works | What's Missing |
|---------|------------|----------------|
| **Cluster health monitoring** | Heartbeat tracking, status transitions | Health dashboard, alerting, disconnection notifications |
| **Role-based access control** | Org roles, project roles, RLS | Fine-grained permission checks on all endpoints |
| **Token refresh flow** | Refresh token issuance | Token revocation list (Redis), session management |
| **MFA setup** | TOTP generation, verification | Full enrollment flow, backup codes |

**Completion: ~50%** (partial functionality)

### 1.3 Impossible User Journeys ✗

| Journey | Missing Services | Priority |
|---------|-----------------|----------|
| **Deploy Docker image** | Deployment Service | **Critical** |
| **View deployment status** | Deployment Service | **Critical** |
| **Rollback deployment** | Deployment Service | **Critical** |
| **Create/manage secrets** | Secrets Service | **Critical** |
| **Bind secrets to apps** | Secrets Service | **Critical** |
| **View application logs** | Observability Service | **Critical** |
| **Add custom domain** | Domain Service | High |
| **Get TLS certificate** | Domain Service | High |
| **Receive deploy notifications** | Notification Service | Medium |
| **Build from Git** | Build Service | Post-MVP |
| **Create new cluster (EKS/GKE/AKS)** | Provisioning Service | Post-MVP |
| **View metrics dashboard** | Observability Service | Post-MVP |

**Completion: 0%** (0/12 journeys)

---

## 2. Service Implementation Status

### 2.1 Fully Implemented Services ✓

| Service | Features | Tests | OpenAPI | Events |
|---------|----------|-------|---------|--------|
| **Auth Service** | Signup, Login, Refresh, Logout, MFA-ready, Service Accounts, API Tokens | Unit + Integration | ✓ | ✓ |
| **Tenant Service** | Organizations, Memberships, Invitations, Roles | Unit + Integration + Contract | ✓ | ✓ |
| **Project Service** | Projects, Project Members, Project Roles | Unit + Integration + Contract | ✓ | ✓ |
| **Audit Service** | Event Consumer, Audit Repository, Query APIs | Unit + Integration | ✓ | Consumer |
| **Cluster Service** | Clusters, Registration Tokens, Heartbeats, Inventory | Unit + Integration + Contract | ✓ | ✓ |
| **API Gateway** | Auth, Routing, Rate Limiting, Middleware | Unit + Integration | ✓ | N/A |

**Count: 6/13 services (46%)**

### 2.2 Stub Services (Not Implemented) ✗

| Service | Files Present | Implementation | Priority |
|---------|---------------|----------------|----------|
| **Deployment Service** | `routes.go` only | 0% | **Critical** |
| **Secrets Service** | `routes.go` only | 0% | **Critical** |
| **Observability Service** | `routes.go` only | 0% | **Critical** |
| **Domain Service** | `routes.go` only | 0% | High |
| **Notification Service** | `routes.go` only | 0% | Medium |
| **Build Service** | `routes.go` only | 0% | Post-MVP |
| **Provisioning Service** | `routes.go` only | 0% | Post-MVP |

**Count: 0/7 services (0%)**

### 2.3 Platform Agent Status

| Component | Status | Notes |
|-----------|--------|-------|
| Configuration | ✓ Complete | Env vars, state file |
| Registration Client | ✓ Complete | Token-based registration |
| Heartbeat Client | ✓ Complete | 30s interval |
| Inventory Collector | ✓ Complete | K8s version, nodes, provider, region |
| Helm Chart | ✓ Complete | RBAC, PVC, tolerations |
| Dockerfile | ✓ Complete | Multi-stage, scratch base |
| **Reconcile Loop** | ✗ Missing | Cannot apply deployments |
| **Status Reporter** | ✗ Missing | Cannot report rollout status |
| **Log Shipper** | ✗ Missing | Cannot stream logs |
| **Secret Sync** | ✗ Missing | Cannot sync secrets |

**Completion: 40%** (4/10 components)

---

## 3. Data Model Status

### 3.1 Implemented Tables ✓

```
✓ users
✓ user_refresh_tokens
✓ user_mfa_secrets
✓ service_accounts
✓ api_tokens
✓ organizations
✓ organization_members
✓ organization_invitations
✓ projects
✓ project_members
✓ clusters
✓ cluster_registration_tokens
✓ cluster_heartbeats
✓ audit_logs
✓ outbox
```

**Count: 15 tables**

### 3.2 Missing Tables ✗

```
✗ cloud_accounts
✗ cluster_environments
✗ cluster_agents (separate from clusters)
✗ cluster_capabilities
✗ cluster_install_sessions (for provisioning)
✗ applications
✗ application_environments
✗ deployments
✗ deployment_revisions
✗ rollout_status
✗ builds
✗ build_steps
✗ build_artifacts
✗ source_uploads
✗ secrets
✗ secret_versions
✗ secret_bindings
✗ kms_keys
✗ domains
✗ dns_verifications
✗ domain_bindings
✗ certificates
✗ events (domain events table)
✗ quotas
```

**Count: 24 missing tables**

### 3.3 Migration Status

| Service | Migration Files | Tables Created |
|---------|-----------------|----------------|
| Auth | ✓ 0001_init | 5 |
| Tenant | ✓ 0001_init | 3 |
| Project | ✓ 0001_init | 2 |
| Cluster | ✓ 0001_init | 3 |
| Audit | ✓ 0001_init | 1 |
| Outbox | ✓ 0001_init, 0002_org_id_text | 1 |
| Deployment | ✗ None | 0 |
| Secrets | ✗ None | 0 |
| Domain | ✗ None | 0 |
| Build | ✗ None | 0 |

---

## 4. API Coverage

### 4.1 Implemented APIs ✓

| Domain | Endpoint | Method | Status |
|--------|----------|--------|--------|
| **Auth** | `/v1/auth/signup` | POST | ✓ |
| **Auth** | `/v1/auth/login` | POST | ✓ |
| **Auth** | `/v1/auth/refresh` | POST | ✓ |
| **Auth** | `/v1/auth/logout` | POST | ✓ |
| **Auth** | `/v1/auth/verify-email` | POST | ✓ |
| **Auth** | `/v1/auth/resend-verification` | POST | ✓ |
| **Auth** | `/v1/auth/request-password-reset` | POST | ✓ |
| **Auth** | `/v1/auth/reset-password` | POST | ✓ |
| **Auth** | `/v1/auth/me` | GET | ✓ |
| **Tenant** | `/v1/organizations` | POST | ✓ |
| **Tenant** | `/v1/organizations/{orgId}` | GET/PATCH | ✓ |
| **Tenant** | `/v1/organizations/{orgId}/members` | GET/POST/PATCH/DELETE | ✓ |
| **Tenant** | `/v1/organizations/{orgId}/invitations` | GET/POST/DELETE | ✓ |
| **Project** | `/v1/organizations/{orgId}/projects` | GET/POST | ✓ |
| **Project** | `/v1/organizations/{orgId}/projects/{projectId}` | GET/PATCH/DELETE | ✓ |
| **Project** | `/v1/organizations/{orgId}/projects/{projectId}/members` | GET/POST/PATCH/DELETE | ✓ |
| **Cluster** | `/v1/organizations/{orgId}/clusters` | GET/POST | ✓ |
| **Cluster** | `/v1/organizations/{orgId}/clusters/{clusterId}` | GET/PATCH/DELETE | ✓ |
| **Cluster** | `/v1/organizations/{orgId}/clusters/{clusterId}/tokens` | POST | ✓ |
| **Cluster** | `/v1/organizations/{orgId}/clusters/{clusterId}/tokens/{tokenId}` | DELETE | ✓ |
| **Cluster** | `/v1/organizations/{orgId}/clusters/{clusterId}/heartbeat` | POST | ✓ |
| **Cluster** | `/v1/organizations/{orgId}/clusters/{clusterId}/heartbeats` | GET | ✓ |
| **Cluster** | `/v1/agent/register` | POST | ✓ |
| **Audit** | `/v1/organizations/{orgId}/audit-logs` | GET | ✓ |

**Count: 24 endpoints implemented**

### 4.2 Missing APIs ✗

| Domain | Endpoint | Method | Priority |
|--------|----------|--------|----------|
| **Cluster** | `/v1/clusters/{clusterId}/environments` | POST/GET | High |
| **Cluster** | `/v1/clusters/{clusterId}/assignments` | POST | High |
| **App** | `/v1/projects/{projectId}/applications` | POST/GET | **Critical** |
| **App** | `/v1/applications/{appId}` | GET/PATCH/DELETE | **Critical** |
| **App** | `/v1/applications/{appId}/environments` | POST/GET | **Critical** |
| **Deploy** | `/v1/applications/{appId}/deployments` | POST/GET | **Critical** |
| **Deploy** | `/v1/deployments/{deploymentId}` | GET | **Critical** |
| **Deploy** | `/v1/deployments/{deploymentId}/rollback` | POST | **Critical** |
| **Deploy** | `/v1/deployments/{deploymentId}/events` | GET | **Critical** |
| **Secrets** | `/v1/projects/{projectId}/secrets` | POST/GET | **Critical** |
| **Secrets** | `/v1/secrets/{secretId}` | GET/PATCH/DELETE | **Critical** |
| **Secrets** | `/v1/applications/{appId}/secret-bindings` | POST/GET/DELETE | **Critical** |
| **Logs** | `/v1/applications/{appId}/logs` | GET | **Critical** |
| **Metrics** | `/v1/applications/{appId}/metrics` | GET | Post-MVP |
| **Domain** | `/v1/projects/{projectId}/domains` | POST/GET | High |
| **Domain** | `/v1/domains/{domainId}` | GET/DELETE | High |
| **Domain** | `/v1/domains/{domainId}/verify` | POST | High |
| **Domain** | `/v1/domains/{domainId}/bindings` | POST/GET/DELETE | High |
| **Build** | `/v1/projects/{projectId}/builds` | POST/GET | Post-MVP |
| **Build** | `/v1/builds/{buildId}` | GET | Post-MVP |
| **Build** | `/v1/builds/{buildId}/logs` | GET | Post-MVP |
| **Provision** | `/v1/organizations/{orgId}/clusters/provision` | POST | Post-MVP |

**Count: ~30 endpoints missing**

---

## 5. Event Coverage

### 5.1 Implemented Events ✓

| Domain | Event | Producer | Consumers |
|--------|-------|----------|-----------|
| `auth.user.created.v1` | Auth | Audit |
| `auth.user.email.verified.v1` | Auth | Audit |
| `auth.user.password.reset.v1` | Auth | Audit |
| `auth.login.succeeded.v1` | Auth | Audit |
| `auth.login.failed.v1` | Auth | Audit |
| `auth.token.revoked.v1` | Auth | Audit |
| `auth.token.rotated.v1` | Auth | Audit |
| `auth.mfa.setup.started.v1` | Auth | Audit |
| `auth.mfa.enabled.v1` | Auth | Audit |
| `auth.mfa.disabled.v1` | Auth | Audit |
| `auth.service.account.created.v1` | Auth | Audit |
| `auth.service.account.deleted.v1` | Auth | Audit |
| `auth.api.token.created.v1` | Auth | Audit |
| `auth.api.token.revoked.v1` | Auth | Audit |
| `tenant.organization.created.v1` | Tenant | Audit |
| `tenant.organization.updated.v1` | Tenant | Audit |
| `tenant.organization.deleted.v1` | Tenant | Audit |
| `tenant.member.invited.v1` | Tenant | Audit, Notification |
| `tenant.member.removed.v1` | Tenant | Audit |
| `tenant.role.changed.v1` | Tenant | Audit |
| `tenant.invitation.accepted.v1` | Tenant | Audit |
| `tenant.invitation.revoked.v1` | Tenant | Audit |
| `project.created.v1` | Project | Audit |
| `project.updated.v1` | Project | Audit |
| `project.deleted.v1` | Project | Audit |
| `project.member.added.v1` | Project | Audit |
| `project.member.removed.v1` | Project | Audit |
| `project.role.changed.v1` | Project | Audit |
| `cluster.created.v1` | Cluster | Audit |
| `cluster.registration.token.created.v1` | Cluster | Audit |
| `cluster.registered.v1` | Cluster | Audit |
| `cluster.heartbeat.received.v1` | Cluster | Audit |
| `cluster.disconnected.v1` | Cluster | Audit |
| `cluster.deleted.v1` | Cluster | Audit |

**Count: 34 events implemented**

### 5.2 Missing Events ✗

| Domain | Event | Priority |
|--------|-------|----------|
| `deployment.created.v1` | **Critical** |
| `deployment.started.v1` | **Critical** |
| `deployment.succeeded.v1` | **Critical** |
| `deployment.failed.v1` | **Critical** |
| `deployment.rollback.requested.v1` | **Critical** |
| `deployment.rolled.back.v1` | **Critical** |
| `secret.created.v1` | **Critical** |
| `secret.updated.v1` | **Critical** |
| `secret.deleted.v1` | **Critical** |
| `domain.added.v1` | High |
| `domain.verified.v1` | High |
| `domain.bound.v1` | High |
| `domain.certificate.issued.v1` | High |
| `build.started.v1` | Post-MVP |
| `build.succeeded.v1` | Post-MVP |
| `build.failed.v1` | Post-MVP |
| `provisioning.started.v1` | Post-MVP |
| `provisioning.succeeded.v1` | Post-MVP |
| `provisioning.failed.v1` | Post-MVP |

**Count: ~19 events missing**

---

## 6. UI Status

### 6.1 Required Pages

| Page | Priority | Status |
|------|----------|--------|
| Login / Signup | **Critical** | ✗ |
| Dashboard | **Critical** | ✗ |
| Organization Settings | **Critical** | ✗ |
| Member Management | **Critical** | ✗ |
| Project List | **Critical** | ✗ |
| Project Settings | **Critical** | ✗ |
| Cluster List | **Critical** | ✗ |
| Cluster Details | **Critical** | ✗ |
| Cluster Registration Wizard | **Critical** | ✗ |
| Application List | **Critical** | ✗ |
| Application Details | **Critical** | ✗ |
| Deployment History | **Critical** | ✗ |
| Deploy Wizard | **Critical** | ✗ |
| Live Logs Viewer | **Critical** | ✗ |
| Secrets Management | **Critical** | ✗ |
| Domain Management | High | ✗ |
| Audit Log Viewer | High | ✗ |
| User Profile | Medium | ✗ |
| Metrics Dashboard | Post-MVP | ✗ |
| Build History | Post-MVP | ✗ |

**Count: 0/20 pages (0%)**

### 6.2 UI Framework Recommendation

For rapid development to MVP:

- **Framework:** React 18+ with TypeScript
- **UI Library:** Shadcn/ui (Radix + Tailwind)
- **State:** TanStack Query for server state
- **Routing:** React Router v6 or Next.js App Router
- **Forms:** React Hook Form + Zod validation
- **Auth:** JWT storage in httpOnly cookies
- **Realtime:** WebSocket for logs, SSE for events

---

## 7. Security Gaps

### 7.1 Implemented Security ✓

| Control | Status | Notes |
|---------|--------|-------|
| Password hashing (Argon2id) | ✓ | Strong algorithm |
| JWT with HS256 | ✓ | Short expiry, refresh tokens |
| Row-Level Security | ✓ | All tenant tables |
| Rate limiting | ✓ | API Gateway |
| HTTPS enforcement | ✓ | Gateway config |
| Input validation | ✓ | Request DTOs |
| Correlation IDs | ✓ | Request tracing |
| Audit logging | ✓ | All state changes |
| Secure token delivery | ✓ | Notifier pattern for sensitive tokens |

### 7.2 Security Gaps ✗

| Gap | Risk | Priority |
|-----|------|----------|
| **No secret encryption** | High | **Critical** |
| **No KMS integration** | High | **Critical** |
| Token revocation list not in Redis | Medium | High |
| No CORS configuration | Medium | High |
| No CSP headers | Medium | High |
| No session management UI | Low | Medium |
| No IP allowlisting | Low | Post-MVP |
| No SBOM/image scanning | Low | Post-MVP |

---

## 8. Operational Gaps

### 8.1 Implemented Operations ✓

| Capability | Status |
|------------|--------|
| Structured logging | ✓ |
| OpenTelemetry tracing | ✓ |
| Health check endpoints | ✓ |
| Graceful shutdown | ✓ |
| Database migrations | ✓ |
| Event relay (outbox) | ✓ |
| Heartbeat monitoring | ✓ |

### 8.2 Operational Gaps ✗

| Gap | Priority |
|-----|----------|
| **No Kubernetes manifests for services** | **Critical** |
| **No Helm charts for services** | **Critical** |
| **No CI/CD pipeline** | **Critical** |
| No Prometheus metrics | High |
| No Grafana dashboards | High |
| No alerting rules | High |
| No log aggregation (Loki) | High |
| No backup/restore procedures | Medium |
| No runbooks | Medium |
| No load testing results | Medium |
| No disaster recovery plan | Post-MVP |

---

## 9. Minimum Components for Application Deployment

To allow a customer to deploy their first application, the following components are required:

### 9.1 Must Build (Critical Path)

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        CRITICAL PATH TO DEPLOYMENT                       │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  1. Deployment Service (NEW)                                             │
│     ├── Applications CRUD                                                │
│     ├── Application Environments                                         │
│     ├── Deployments (Docker image only for MVP)                         │
│     ├── Deployment Revisions (immutable)                                │
│     ├── Rollout Status                                                   │
│     └── Rollback                                                         │
│                                                                          │
│  2. Secrets Service (NEW)                                                │
│     ├── Secrets CRUD (envelope encryption)                              │
│     ├── Secret Versions                                                  │
│     ├── Secret Bindings to Apps                                         │
│     └── KMS Key Management                                               │
│                                                                          │
│  3. Agent Enhancements (MODIFY)                                          │
│     ├── Desired State Pull API                                          │
│     ├── Reconcile Loop (apply K8s manifests)                            │
│     ├── Status Reporter (rollout progress)                              │
│     └── Secret Sync (K8s secrets)                                       │
│                                                                          │
│  4. Observability Service (NEW - Partial)                                │
│     ├── Log Ingest (from agent)                                         │
│     └── Log Query API                                                    │
│                                                                          │
│  5. Agent Log Shipper (MODIFY)                                           │
│     └── Stream pod logs to control plane                                │
│                                                                          │
│  6. Web Console (NEW)                                                    │
│     ├── Login / Auth flows                                              │
│     ├── Dashboard                                                        │
│     ├── Cluster list + registration                                     │
│     ├── Application list + deploy wizard                                │
│     ├── Deployment history + rollback                                   │
│     ├── Secrets management                                               │
│     └── Live logs viewer                                                 │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 9.2 Dependency Order

```
Week 1: Deployment Service + Migrations
        └── Applications, Environments, Deployments, Revisions
        
Week 2: Agent Reconcile + Status
        └── Pull desired state, apply manifests, report status
        
Week 3: Secrets Service + Agent Sync
        └── Envelope encryption, bindings, K8s secret sync
        
Week 4: Observability + Log Shipper
        └── Log ingest, storage, query API, agent log streaming
        
Week 5-6: Web Console
        └── All critical pages, deploy wizard, logs viewer
```

---

## 10. Completion Metrics by Domain

| Domain | Implemented | Required | Completion |
|--------|-------------|----------|------------|
| **Identity & Auth** | 5 tables, 15 APIs, 14 events | 5 tables, 15 APIs, 14 events | **100%** |
| **Tenancy (Orgs/Projects)** | 5 tables, 15 APIs, 14 events | 5 tables, 15 APIs, 14 events | **100%** |
| **Cluster Management** | 3 tables, 10 APIs, 6 events | 5 tables, 15 APIs, 8 events | **70%** |
| **Deployments** | 0 tables, 0 APIs, 0 events | 5 tables, 10 APIs, 6 events | **0%** |
| **Secrets** | 0 tables, 0 APIs, 0 events | 4 tables, 8 APIs, 3 events | **0%** |
| **Observability** | 0 tables, 0 APIs, 0 events | 2 tables, 3 APIs, 0 events | **0%** |
| **Domains/TLS** | 0 tables, 0 APIs, 0 events | 4 tables, 6 APIs, 4 events | **0%** |
| **Notifications** | 0 tables, 0 APIs, 0 events | 2 tables, 3 APIs, 2 events | **0%** |
| **Platform Agent** | 4 components | 10 components | **40%** |
| **Web Console** | 0 pages | 15 pages (MVP) | **0%** |

### Overall Calculation

```
Backend Services:     6/13 implemented    = 46%
Database Tables:      15/39 implemented   = 38%
API Endpoints:        24/54 implemented   = 44%
Domain Events:        34/53 implemented   = 64%
Agent Components:     4/10 implemented    = 40%
Web Console:          0/15 pages          = 0%

Weighted Average (Backend 40%, Agent 20%, UI 40%):
= (0.4 × 44%) + (0.2 × 40%) + (0.4 × 0%)
= 17.6% + 8% + 0%
= 25.6% → ~26%

MVP Core Features Checklist (from 09-mvp-roadmap.md):
✓ Email/password auth                    = 1
✓ Organizations, projects, RBAC          = 1
✓ Audit logging                          = 1
✓ Existing cluster registration          = 1
⚠ Cluster Agent (partial)                = 0.5
✗ Docker image deployment                = 0
✗ Deployment status + rollback           = 0
✗ Secrets + sync                         = 0
✗ Live logs                              = 0
✗ Domains + TLS                          = 0
✗ Notifications                          = 0
✗ Web console                            = 0

= 4.5 / 12 = 37.5% → ~38%
```

**Final MVP Completion: 38%**

---

## 11. Recommended Build Order

### Phase 1: Core Deployment Loop (Weeks 1-2)

| Priority | Component | Effort | Dependencies |
|----------|-----------|--------|--------------|
| 1 | Deployment Service | 5 days | None |
| 2 | Agent Reconcile Loop | 3 days | Deployment Service |
| 3 | Agent Status Reporter | 2 days | Reconcile Loop |
| 4 | Rollback Functionality | 2 days | Status Reporter |

**Milestone:** Can deploy Docker image and see rollout status.

### Phase 2: Secrets & Security (Week 3)

| Priority | Component | Effort | Dependencies |
|----------|-----------|--------|--------------|
| 1 | Secrets Service | 4 days | None |
| 2 | Agent Secret Sync | 2 days | Secrets Service |
| 3 | KMS Integration | 2 days | Secrets Service |

**Milestone:** Can create secrets and bind to deployments.

### Phase 3: Observability (Week 4)

| Priority | Component | Effort | Dependencies |
|----------|-----------|--------|--------------|
| 1 | Observability Service (logs) | 3 days | None |
| 2 | Agent Log Shipper | 2 days | Observability Service |
| 3 | Log Query API | 2 days | Observability Service |

**Milestone:** Can view live and historical logs.

### Phase 4: Web Console (Weeks 5-6)

| Priority | Component | Effort | Dependencies |
|----------|-----------|--------|--------------|
| 1 | Auth pages + shell | 2 days | Auth Service |
| 2 | Dashboard + nav | 1 day | All services |
| 3 | Cluster pages | 2 days | Cluster Service |
| 4 | Application pages | 3 days | Deployment Service |
| 5 | Secrets pages | 1 day | Secrets Service |
| 6 | Logs viewer | 2 days | Observability Service |
| 7 | Polish + testing | 2 days | All pages |

**Milestone:** MVP Launch Ready.

### Phase 5: Post-MVP Enhancements

| Component | Effort | Priority |
|-----------|--------|----------|
| Domain Service | 4 days | High |
| Notification Service | 3 days | Medium |
| Metrics dashboards | 3 days | Medium |
| Build Service | 5 days | Low |
| Provisioning Service | 5 days | Low |

---

## 12. Critical Blockers

### Blocker 1: No Deployment Service
- **Impact:** Cannot deploy any application
- **Resolution:** Build Deployment Service with applications, environments, deployments, revisions
- **Effort:** 5 days

### Blocker 2: No Secrets Service
- **Impact:** Cannot securely manage application configuration
- **Resolution:** Build Secrets Service with envelope encryption
- **Effort:** 4 days

### Blocker 3: No Agent Reconcile Loop
- **Impact:** Agent cannot apply deployments to cluster
- **Resolution:** Implement desired-state pull and K8s manifest reconciliation
- **Effort:** 5 days

### Blocker 4: No Web Console
- **Impact:** No user interface for the platform
- **Resolution:** Build React web console with critical pages
- **Effort:** 10 days

---

## 13. Nice-to-Have Features (Post-MVP)

| Feature | Value | Effort | Priority |
|---------|-------|--------|----------|
| Metrics dashboards | High visibility | 3 days | High |
| Domain + TLS automation | Custom domains | 4 days | High |
| Email notifications | User engagement | 2 days | Medium |
| Webhook notifications | Integrations | 2 days | Medium |
| Git repository deployment | Developer convenience | 5 days | Medium |
| Source upload builds | Alternative to Docker | 4 days | Low |
| New cluster creation (EKS) | Cloud provisioning | 5 days | Low |
| SSO/SAML | Enterprise auth | 4 days | Low |
| Multi-region control plane | High availability | 10 days | Future |

---

## 14. Summary & Recommendations

### Current State
The platform has a solid foundation with identity, tenancy, and cluster registration fully implemented. The event-driven architecture, transactional outbox, and multi-tenant isolation are production-ready. However, the core deployment functionality that would allow customers to actually use the platform is entirely missing.

### MVP Gap
To reach MVP, the team needs to:
1. Build 3 new services (Deployment, Secrets, Observability)
2. Enhance the Platform Agent with reconcile/status/logs capabilities
3. Build a complete web console

### Recommended Next Steps
1. **Immediately:** Start Deployment Service implementation
2. **Week 1:** Complete Deployment Service + Agent reconcile loop
3. **Week 2:** Complete Agent status reporting + rollback
4. **Week 3:** Build Secrets Service + Agent secret sync
5. **Week 4:** Build Observability Service + Agent log shipper
6. **Weeks 5-6:** Build Web Console

### MVP Launch Criteria
- [ ] Deploy Docker image from UI
- [ ] View deployment status and rollback
- [ ] Create and bind secrets
- [ ] View live application logs
- [ ] All actions in audit log
- [ ] Security review passed
- [ ] Load test passed

**Estimated Time to MVP: 6 weeks**

---

*Document Version: 1.0*
*Analysis Date: 2026-06-24*
*Author: Principal Platform Architect*
