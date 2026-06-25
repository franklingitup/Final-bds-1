# 10 — Implementation Backlog

This backlog is derived from `01-prd.md` through `09-mvp-roadmap.md`. It is organized as:

```text
epics/
  stories/
    tasks/
```

Complexity scale:

- **S** — small, localized change.
- **M** — moderate feature with service/data/API work.
- **L** — large feature touching multiple components.
- **XL** — cross-system feature or high-risk infrastructure work.

---

## Epic 1 — Platform Foundation

### Story 1.1 — Establish Production Monorepo

#### Task 1.1.1 — Create Monorepo Structure

- **Description:** Create the production repository layout: `frontend/`, `backend/`, `infra/`, `helm/`, `terraform/`, `agents/`, `docs/`, `scripts/`, and `proto/`.
- **Dependencies:** None.
- **Acceptance Criteria:**
  - Repository contains all top-level folders defined in the engineering design.
  - Each folder has a README explaining ownership and purpose.
  - `docs/` contains the architecture documents and backlog.
- **Estimated Complexity:** S

#### Task 1.1.2 — Configure Shared Build and Test Tooling

- **Description:** Add workspace-level build, lint, test, and formatting commands for backend services, frontend apps, agents, and shared libraries.
- **Dependencies:** Task 1.1.1.
- **Acceptance Criteria:**
  - A single command runs lint/test across all packages.
  - CI can run package-level checks independently.
  - Failed lint/test blocks merge.
- **Estimated Complexity:** M

#### Task 1.1.3 — Add Local Development Bootstrap

- **Description:** Provide `scripts/dev` tooling to start local dependencies: PostgreSQL, Redis, message broker, object storage emulator, and observability backends.
- **Dependencies:** Task 1.1.1.
- **Acceptance Criteria:**
  - Developers can start the local platform stack with one command.
  - Health checks verify PostgreSQL, Redis, broker, and object storage are reachable.
  - Local env configuration is documented.
- **Estimated Complexity:** M

### Story 1.2 — Build Shared Backend Libraries

#### Task 1.2.1 — Implement Standard Error Envelope Library

- **Description:** Create shared error handling for API responses using the documented `{ error: { code, message, details, requestId } }` shape.
- **Dependencies:** Task 1.1.2.
- **Acceptance Criteria:**
  - All services can return consistent error responses.
  - Error codes map to the documented HTTP statuses.
  - Request ID is included in every error response.
- **Estimated Complexity:** M

#### Task 1.2.2 — Implement Database Utility Library

- **Description:** Provide connection management, migrations runner integration, transaction helpers, and tenant session-variable support for RLS.
- **Dependencies:** Task 1.1.3.
- **Acceptance Criteria:**
  - Services can run migrations deterministically.
  - DB sessions can set `app.current_org_id`.
  - Transaction helper supports outbox writes.
- **Estimated Complexity:** L

#### Task 1.2.3 — Implement Event Library and Outbox Relay

- **Description:** Build shared event envelope types, producer/consumer clients, idempotency helpers, and transactional outbox relay support.
- **Dependencies:** Task 1.2.2.
- **Acceptance Criteria:**
  - Events use the standard envelope with `eventId`, `type`, `version`, `orgId`, `actor`, `resource`, and `payload`.
  - Outbox relay publishes only unpublished rows.
  - Consumers can deduplicate by `eventId`.
- **Estimated Complexity:** L

#### Task 1.2.4 — Implement Telemetry Library

- **Description:** Add structured logging, metrics, tracing, correlation IDs, and service health conventions.
- **Dependencies:** Task 1.1.2.
- **Acceptance Criteria:**
  - Every service can emit structured logs with request ID and tenant context.
  - Health endpoints expose readiness and liveness status.
  - OpenTelemetry traces propagate across service calls.
- **Estimated Complexity:** M

### Story 1.3 — Platform Infrastructure Baseline

#### Task 1.3.1 — Provision Control Plane Runtime Manifests

- **Description:** Create baseline Helm/GitOps manifests for `platform-system`, `platform-data`, `platform-observability`, and `platform-ingress`.
- **Dependencies:** Task 1.1.1.
- **Acceptance Criteria:**
  - Namespaces match the architecture design.
  - Service accounts are created per service.
  - No service runs with cluster-admin privileges.
- **Estimated Complexity:** L

#### Task 1.3.2 — Configure API Gateway Baseline

- **Description:** Implement API Gateway routing, request ID injection, basic rate limiting, `/healthz`, and `/version`.
- **Dependencies:** Task 1.2.1, Task 1.2.4.
- **Acceptance Criteria:**
  - Gateway routes requests to backend services.
  - Rate-limited requests return documented error format.
  - Health/version endpoints are public and stable.
- **Estimated Complexity:** M

#### Task 1.3.3 — Configure CI/CD Pipeline

- **Description:** Add CI stages for lint, tests, migrations validation, image builds, and Helm chart validation.
- **Dependencies:** Task 1.1.2, Task 1.3.1.
- **Acceptance Criteria:**
  - Pull requests run all relevant checks.
  - Container images are built for changed services.
  - Helm templates are rendered and validated in CI.
- **Estimated Complexity:** M

---

## Epic 2 — Authentication

### Story 2.1 — User Authentication

#### Task 2.1.1 — Create Auth Database Schema

- **Description:** Add tables for `users`, `credentials`, `sessions`, `api_tokens`, `service_accounts`, `mfa_factors`, and `oauth_identities`.
- **Dependencies:** Task 1.2.2.
- **Acceptance Criteria:**
  - Migrations create all auth tables and indexes.
  - Password credential data is separated from user profile data.
  - No plaintext password field exists.
- **Estimated Complexity:** M

#### Task 2.1.2 — Implement Email and Password Registration

- **Description:** Implement user registration with email normalization, argon2id password hashing, and duplicate email detection.
- **Dependencies:** Task 2.1.1.
- **Acceptance Criteria:**
  - Valid registration creates a user and credential record.
  - Duplicate email returns conflict.
  - Password hashes use argon2id and are never logged.
- **Estimated Complexity:** M

#### Task 2.1.3 — Implement Login and Token Issuance

- **Description:** Implement `/v1/auth/login` with access tokens, refresh tokens, failed-login handling, and account lockout support.
- **Dependencies:** Task 2.1.2, Task 1.2.1.
- **Acceptance Criteria:**
  - Valid credentials return access and refresh tokens.
  - Invalid credentials return `INVALID_CREDENTIALS`.
  - Repeated failures trigger rate limiting or lockout.
  - `UserLoggedIn` and `LoginFailed` events are emitted.
- **Estimated Complexity:** L

#### Task 2.1.4 — Implement Refresh and Logout

- **Description:** Implement `/v1/auth/refresh` and `/v1/auth/logout` with refresh-token rotation and revocation.
- **Dependencies:** Task 2.1.3, Task 1.2.3.
- **Acceptance Criteria:**
  - Refresh rotates refresh tokens.
  - Reused/revoked refresh tokens are rejected.
  - Logout revokes the active refresh token.
  - `TokenRevoked` events are emitted.
- **Estimated Complexity:** M

### Story 2.2 — Gateway Authentication Enforcement

#### Task 2.2.1 — Validate Access Tokens at API Gateway

- **Description:** Add JWT validation, tenant/user context extraction, and rejection behavior for invalid tokens.
- **Dependencies:** Task 2.1.3, Task 1.3.2.
- **Acceptance Criteria:**
  - Authenticated requests forward user and org context to services.
  - Missing/invalid token returns `UNAUTHENTICATED`.
  - Public routes bypass auth only when explicitly configured.
- **Estimated Complexity:** M

#### Task 2.2.2 — Consume Token Revocation Events

- **Description:** Subscribe the gateway to `TokenRevoked` and cache revoked token IDs in Redis.
- **Dependencies:** Task 2.1.4, Task 1.2.3.
- **Acceptance Criteria:**
  - Revoked tokens are rejected without waiting for expiry.
  - Redis denylist TTL matches token expiry.
  - Gateway remains functional if Redis temporarily fails, with degraded-mode alerting.
- **Estimated Complexity:** M

### Story 2.3 — Machine and Agent Authentication

#### Task 2.3.1 — Implement Service Accounts and API Tokens

- **Description:** Add scoped service accounts and API tokens for automation.
- **Dependencies:** Task 2.1.1, Task 3.2.2.
- **Acceptance Criteria:**
  - API tokens can be created, listed by metadata, and revoked.
  - Token scopes map to RBAC permissions.
  - Raw token value is shown only once.
- **Estimated Complexity:** L

#### Task 2.3.2 — Implement Agent Credential Issuance

- **Description:** Exchange a validated one-time cluster registration token for a per-cluster agent credential.
- **Dependencies:** Task 6.2.2, Task 2.1.4.
- **Acceptance Criteria:**
  - One-time token is accepted once and then marked used.
  - Agent credential is scoped to a single cluster.
  - `AgentCredentialIssued` event is emitted.
- **Estimated Complexity:** L

---

## Epic 3 — Organizations

### Story 3.1 — Organization Lifecycle

#### Task 3.1.1 — Create Organization Schema

- **Description:** Add `organizations`, `organization_members`, `invitations`, `roles`, and `quotas` tables with indexes and constraints.
- **Dependencies:** Task 1.2.2, Task 2.1.1.
- **Acceptance Criteria:**
  - `organizations.slug` is unique.
  - `organization_members` enforces unique `(org_id, user_id)`.
  - Tenant-owned rows include `org_id`.
- **Estimated Complexity:** M

#### Task 3.1.2 — Implement Create Organization API

- **Description:** Implement `POST /v1/organizations` and bootstrap the creator as `owner`.
- **Dependencies:** Task 3.1.1, Task 2.2.1.
- **Acceptance Criteria:**
  - Valid request creates org and owner membership in one transaction.
  - Duplicate slug returns `SLUG_TAKEN`.
  - `OrganizationCreated` event is emitted.
- **Estimated Complexity:** M

#### Task 3.1.3 — Implement Organization Read API

- **Description:** Implement `GET /v1/organizations/{orgId}` with membership authorization.
- **Dependencies:** Task 3.1.2.
- **Acceptance Criteria:**
  - Members can read their org.
  - Non-members receive `NOT_FOUND` or `FORBIDDEN` without data leakage.
  - Response matches API spec.
- **Estimated Complexity:** S

### Story 3.2 — Membership and RBAC

#### Task 3.2.1 — Implement Organization Invitations

- **Description:** Implement invite creation, invite token hashing, expiry, and `MemberInvited` event emission.
- **Dependencies:** Task 3.1.2, Task 13.4.1.
- **Acceptance Criteria:**
  - Only owner/admin can invite.
  - Duplicate active membership returns `MEMBER_EXISTS`.
  - Invite tokens are stored hashed and expire.
- **Estimated Complexity:** M

#### Task 3.2.2 — Implement Permission Resolution

- **Description:** Build Tenant Service permission resolver for org roles and project roles, with Redis caching.
- **Dependencies:** Task 3.1.1, Task 4.1.1.
- **Acceptance Criteria:**
  - Resolver returns effective permissions for user/org/project.
  - Cache invalidates on `RoleChanged` and `MemberRemoved`.
  - Deny-by-default behavior is covered by tests.
- **Estimated Complexity:** L

#### Task 3.2.3 — Implement Role Changes and Member Removal

- **Description:** Add APIs for org admins to change roles and remove members safely.
- **Dependencies:** Task 3.2.2.
- **Acceptance Criteria:**
  - Owner cannot remove the last owner.
  - Role changes emit `RoleChanged`.
  - Removal emits `MemberRemoved`.
- **Estimated Complexity:** M

### Story 3.3 — Tenant Isolation Controls

#### Task 3.3.1 — Enable RLS on Tenant Tables

- **Description:** Add PostgreSQL RLS policies for tenant-owned tables keyed on `app.current_org_id`.
- **Dependencies:** Task 1.2.2, Task 3.1.1.
- **Acceptance Criteria:**
  - Queries without tenant session variable fail or return no rows.
  - Cross-tenant reads are blocked at DB level.
  - RLS tests cover representative tables.
- **Estimated Complexity:** L

#### Task 3.3.2 — Add Repository Tenant Scope Guard

- **Description:** Add runtime/lint guard preventing repository queries without an org scope.
- **Dependencies:** Task 1.2.2.
- **Acceptance Criteria:**
  - Unscoped tenant queries fail in tests.
  - Repository APIs require org context for tenant resources.
  - Cross-tenant access attempts are audited.
- **Estimated Complexity:** M

---

## Epic 4 — Projects

### Story 4.1 — Project Lifecycle

#### Task 4.1.1 — Create Project Schema

- **Description:** Add `projects` and `project_members` tables with constraints, indexes, and RLS policies.
- **Dependencies:** Task 3.1.1, Task 3.3.1.
- **Acceptance Criteria:**
  - Project slug is unique within an org.
  - `project_members` enforces unique `(project_id, user_id)`.
  - Project rows are tenant-scoped.
- **Estimated Complexity:** M

#### Task 4.1.2 — Implement Project Create API

- **Description:** Implement `POST /v1/organizations/{orgId}/projects`.
- **Dependencies:** Task 4.1.1, Task 3.2.2.
- **Acceptance Criteria:**
  - Org admin can create a project.
  - Duplicate slug returns `SLUG_TAKEN`.
  - `ProjectCreated` event is emitted.
- **Estimated Complexity:** M

#### Task 4.1.3 — Implement Project Read API

- **Description:** Implement `GET /v1/projects/{projectId}` with project membership authorization.
- **Dependencies:** Task 4.1.2.
- **Acceptance Criteria:**
  - Project members can read project details.
  - Non-members cannot infer project existence.
  - Response matches API spec.
- **Estimated Complexity:** S

### Story 4.2 — Project Roles

#### Task 4.2.1 — Implement Project Member Management

- **Description:** Add project member add/remove/update role APIs for project admins and org admins.
- **Dependencies:** Task 4.1.2, Task 3.2.2.
- **Acceptance Criteria:**
  - Project admin can grant `admin`, `developer`, or `viewer`.
  - Developer cannot manage membership.
  - Role updates invalidate permission cache.
- **Estimated Complexity:** M

#### Task 4.2.2 — Implement Project Quota Checks

- **Description:** Enforce per-org/project quotas for project count, apps, clusters, deployments, and builds.
- **Dependencies:** Task 3.1.1, Task 4.1.2.
- **Acceptance Criteria:**
  - Exceeding quota returns `QUOTA_EXCEEDED`.
  - Quota usage updates atomically.
  - Quotas are included in audit metadata.
- **Estimated Complexity:** M

---

## Epic 5 — Cluster Management

### Story 5.1 — Cluster Inventory

#### Task 5.1.1 — Create Cluster Schema

- **Description:** Add `clusters`, `cluster_environments`, `cluster_agents`, `cluster_capabilities`, and related indexes.
- **Dependencies:** Task 3.3.1, Task 4.1.1.
- **Acceptance Criteria:**
  - Cluster provider is constrained to `aws`, `gcp`, or `azure`.
  - Cluster status uses constrained lifecycle values.
  - Indexes support listing by org/status/provider.
- **Estimated Complexity:** M

#### Task 5.1.2 — Implement Cluster Read/List APIs

- **Description:** Implement cluster details and org-scoped cluster listing.
- **Dependencies:** Task 5.1.1, Task 3.2.2.
- **Acceptance Criteria:**
  - Org members can list clusters in their org.
  - Cluster details include agent status and environments.
  - Cross-org access is blocked.
- **Estimated Complexity:** M

#### Task 5.1.3 — Implement Cluster Lifecycle State Machine

- **Description:** Implement legal status transitions for `pending`, `provisioning`, `registered`, `ready`, `unhealthy`, `failed`, and `deleted`.
- **Dependencies:** Task 5.1.1.
- **Acceptance Criteria:**
  - Invalid transitions are rejected.
  - State transitions emit corresponding cluster events.
  - Lifecycle behavior matches the cluster engine state diagram.
- **Estimated Complexity:** M

### Story 5.2 — Cluster Environment Assignment

#### Task 5.2.1 — Implement Cluster Assignment API

- **Description:** Implement `POST /v1/clusters/{clusterId}/assignments` to map project/environment to namespace.
- **Dependencies:** Task 5.1.3, Task 4.1.2.
- **Acceptance Criteria:**
  - Only org admins can assign clusters.
  - Assignment requires cluster status `ready`.
  - Response includes `clusterEnvId` and namespace.
- **Estimated Complexity:** M

#### Task 5.2.2 — Enforce Namespace Naming Rules

- **Description:** Generate and validate namespaces using `proj-{slug}-{env}` unless an allowed namespace override is provided.
- **Dependencies:** Task 5.2.1.
- **Acceptance Criteria:**
  - Generated names are Kubernetes-valid.
  - Namespace conflicts return `CONFLICT`.
  - Names are deterministic and auditable.
- **Estimated Complexity:** S

---

## Epic 6 — Existing Cluster Registration

### Story 6.1 — Registration Session Creation

#### Task 6.1.1 — Create Cluster Install Session Schema

- **Description:** Add `cluster_install_sessions` with token hash, expiry, status, and uniqueness constraints.
- **Dependencies:** Task 5.1.1.
- **Acceptance Criteria:**
  - Token hash is unique.
  - Expired sessions can be queried efficiently.
  - Raw token is never persisted.
- **Estimated Complexity:** S

#### Task 6.1.2 — Implement Existing Cluster Registration Request API

- **Description:** Implement `POST /v1/organizations/{orgId}/clusters/register`.
- **Dependencies:** Task 6.1.1, Task 3.2.2.
- **Acceptance Criteria:**
  - Creates cluster in `pending`.
  - Creates one-time registration token and install session.
  - Returns install command and shows token once.
  - Emits `ClusterRegistrationRequested`.
- **Estimated Complexity:** L

#### Task 6.1.3 — Generate Existing Cluster Install Command

- **Description:** Generate command that installs the agent Helm chart into an existing cluster using the one-time token.
- **Dependencies:** Task 6.1.2, Task 8.1.3.
- **Acceptance Criteria:**
  - Command includes control-plane endpoint and token.
  - Command is shell-safe and documented.
  - Command is reproducible for an active session.
- **Estimated Complexity:** M

### Story 6.2 — Agent Registration Callback

#### Task 6.2.1 — Implement One-Time Token Validation

- **Description:** Validate install token hash, expiry, cluster binding, and session status.
- **Dependencies:** Task 6.1.1.
- **Acceptance Criteria:**
  - Invalid token returns `INVALID_TOKEN`.
  - Expired token returns `TOKEN_EXPIRED`.
  - Used token returns `TOKEN_ALREADY_USED`.
- **Estimated Complexity:** M

#### Task 6.2.2 — Implement Agent Register API

- **Description:** Implement `POST /v1/clusters/{clusterId}/agents/register`.
- **Dependencies:** Task 6.2.1, Task 2.3.2.
- **Acceptance Criteria:**
  - Valid token registers an agent.
  - Token is marked used atomically.
  - Cluster transitions to `registered`.
  - Response returns agent credential and control-plane endpoints.
- **Estimated Complexity:** L

#### Task 6.2.3 — Mark Cluster Ready on Healthy Heartbeat

- **Description:** Transition registered clusters to `ready` after the first valid heartbeat and capability report.
- **Dependencies:** Task 6.2.2, Task 8.2.1.
- **Acceptance Criteria:**
  - First healthy heartbeat marks cluster `ready`.
  - `ClusterReady` event is emitted once.
  - Capabilities are persisted.
- **Estimated Complexity:** M

---

## Epic 7 — Cluster Provisioning Engine

### Story 7.1 — Provisioning Service Baseline

#### Task 7.1.1 — Create Provisioning Schema

- **Description:** Add `provisioning_templates`, `install_bundles`, and `provisioning_runs`.
- **Dependencies:** Task 5.1.1.
- **Acceptance Criteria:**
  - Provisioning runs store status, provider, module version, bundle ref, and diagnostics.
  - Install bundles are tenant-scoped.
  - Indexes support lookup by org/status.
- **Estimated Complexity:** M

#### Task 7.1.2 — Implement Cluster Provision API

- **Description:** Implement `POST /v1/organizations/{orgId}/clusters/provision`.
- **Dependencies:** Task 7.1.1, Task 6.1.1.
- **Acceptance Criteria:**
  - Valid provider config creates cluster in `provisioning`.
  - Invalid region/version/node pool returns `INVALID_PROVIDER_CONFIG`.
  - Response includes install command, bundle URL, and expiry.
  - Emits `ClusterProvisioningRequested`.
- **Estimated Complexity:** L

#### Task 7.1.3 — Implement Provider Config Validation

- **Description:** Validate cloud-specific regions, Kubernetes versions, node pool min/max, instance types, and networking CIDRs.
- **Dependencies:** Task 7.1.2.
- **Acceptance Criteria:**
  - Validation covers AWS, GCP, and Azure.
  - Provider errors include actionable details.
  - Quota violations return `QUOTA_EXCEEDED`.
- **Estimated Complexity:** L

### Story 7.2 — Terraform/OpenTofu Modules

#### Task 7.2.1 — Create Uniform Terraform Module Contract

- **Description:** Define common module inputs/outputs for cluster name, region, version, node pools, networking, and agent bootstrap.
- **Dependencies:** Task 7.1.2.
- **Acceptance Criteria:**
  - EKS/GKE/AKS modules share a consistent variable interface.
  - Outputs include kubeconfig/access data needed by installer.
  - Contract is documented in `terraform/`.
- **Estimated Complexity:** L

#### Task 7.2.2 — Implement EKS Module

- **Description:** Build Terraform/OpenTofu module for VPC, subnets, IAM/OIDC, EKS control plane, node groups, and addons.
- **Dependencies:** Task 7.2.1.
- **Acceptance Criteria:**
  - Module provisions a working EKS cluster.
  - Module is idempotent on re-run.
  - Least-privilege IAM is documented.
- **Estimated Complexity:** XL

#### Task 7.2.3 — Implement GKE Module

- **Description:** Build Terraform/OpenTofu module for VPC, subnets, service accounts, GKE cluster, node pools, and workload identity.
- **Dependencies:** Task 7.2.1.
- **Acceptance Criteria:**
  - Module provisions a working GKE cluster.
  - Module is idempotent on re-run.
  - Workload identity is configured according to least privilege.
- **Estimated Complexity:** XL

#### Task 7.2.4 — Implement AKS Module

- **Description:** Build Terraform/OpenTofu module for VNet, subnets, managed identity, AKS cluster, node pools, and RBAC.
- **Dependencies:** Task 7.2.1.
- **Acceptance Criteria:**
  - Module provisions a working AKS cluster.
  - Module is idempotent on re-run.
  - Managed identity permissions are least-privilege.
- **Estimated Complexity:** XL

### Story 7.3 — Install Bundle and Installer CLI

#### Task 7.3.1 — Generate Install Bundles

- **Description:** Render provider tfvars, backend config, module version metadata, and agent bootstrap manifest into a downloadable bundle.
- **Dependencies:** Task 7.1.2, Task 7.2.1.
- **Acceptance Criteria:**
  - Bundle is stored in object storage.
  - Bundle URL is short-lived or access-controlled.
  - Bundle records exact module versions for reproducibility.
- **Estimated Complexity:** L

#### Task 7.3.2 — Implement Installer CLI Provisioning Flow

- **Description:** Build installer CLI flow: prerequisite checks, `tofu init/plan/apply`, kubeconfig retrieval, agent Helm install, registration callback.
- **Dependencies:** Task 7.3.1, Task 8.1.3.
- **Acceptance Criteria:**
  - CLI produces clear errors for missing cloud/tofu/kubectl prerequisites.
  - CLI can resume safely after partial failure.
  - CLI installs the agent and triggers registration.
- **Estimated Complexity:** XL

#### Task 7.3.3 — Ingest Provisioning Telemetry

- **Description:** Add APIs/events for installer to report provisioning started/succeeded/failed with diagnostics.
- **Dependencies:** Task 7.3.2.
- **Acceptance Criteria:**
  - Provisioning run status updates in control plane.
  - Failures preserve diagnostics without secrets.
  - `ClusterProvisioningSucceeded` and `ClusterProvisioningFailed` are emitted.
- **Estimated Complexity:** M

---

## Epic 8 — Cluster Agent

### Story 8.1 — Agent Packaging and Installation

#### Task 8.1.1 — Create Agent Project Skeleton

- **Description:** Create `agents/cluster-agent` with controller runtime structure, config loading, logging, and health endpoints.
- **Dependencies:** Task 1.1.1.
- **Acceptance Criteria:**
  - Agent starts locally with config.
  - Health/readiness endpoints exist.
  - Structured logs include cluster and agent identity when available.
- **Estimated Complexity:** M

#### Task 8.1.2 — Define Agent CRDs

- **Description:** Define `PlatformApplication`, `PlatformDeployment`, `PlatformSecretSync`, and `PlatformDomain` with status subresources.
- **Dependencies:** Task 8.1.1.
- **Acceptance Criteria:**
  - CRDs are Kubernetes-valid and versioned.
  - Status subresources are enabled.
  - CRD schemas validate required fields.
- **Estimated Complexity:** L

#### Task 8.1.3 — Create Agent Helm Chart

- **Description:** Package agent Deployment, ServiceAccount, RBAC, CRDs, leader election permissions, and config into `helm/agent`.
- **Dependencies:** Task 8.1.2.
- **Acceptance Criteria:**
  - Chart installs into `platform-agent`.
  - Agent runs with two replicas and leader election enabled.
  - RBAC grants only required permissions.
- **Estimated Complexity:** L

### Story 8.2 — Agent Registration and Heartbeat

#### Task 8.2.1 — Implement Agent Registration Controller

- **Description:** Agent uses one-time token to register, stores returned credential in an in-cluster Secret, and transitions to active mode.
- **Dependencies:** Task 6.2.2, Task 8.1.3.
- **Acceptance Criteria:**
  - Registration succeeds with a valid token.
  - Credential is stored securely in `platform-agent`.
  - Invalid token leaves agent in degraded state with useful logs.
- **Estimated Complexity:** L

#### Task 8.2.2 — Implement Heartbeat Reporter

- **Description:** Agent periodically sends heartbeat with version, status, node count, capabilities, and timestamp.
- **Dependencies:** Task 8.2.1.
- **Acceptance Criteria:**
  - Heartbeats authenticate using agent credential.
  - Control-plane response can include desired agent version.
  - Heartbeat failures retry with backoff.
- **Estimated Complexity:** M

#### Task 8.2.3 — Implement Cluster Staleness Detection

- **Description:** Cluster Service detects missed heartbeats and emits `ClusterUnhealthy`.
- **Dependencies:** Task 8.2.2.
- **Acceptance Criteria:**
  - Missed heartbeat threshold is configurable.
  - Status returns to `ready` when heartbeat resumes.
  - Notifications and audit receive the event.
- **Estimated Complexity:** M

### Story 8.3 — Agent Reconciliation

#### Task 8.3.1 — Implement Desired State Pull Client

- **Description:** Agent pulls desired state for assigned cluster environments over authenticated outbound connection.
- **Dependencies:** Task 8.2.1, Task 9.2.1.
- **Acceptance Criteria:**
  - Agent only receives desired state for its cluster.
  - Pull uses backoff and supports long-poll/streaming mode.
  - Desired state is versioned by revision ID.
- **Estimated Complexity:** L

#### Task 8.3.2 — Reconcile Application Workloads

- **Description:** Reconcile desired app specs into Kubernetes Deployments, Services, and optional HPA.
- **Dependencies:** Task 8.3.1, Task 9.2.2.
- **Acceptance Criteria:**
  - Docker image deployment creates a working workload.
  - Replicas, resources, ports, env vars, and autoscaling are applied.
  - Reconcile is idempotent.
- **Estimated Complexity:** XL

#### Task 8.3.3 — Report Rollout Status

- **Description:** Watch rollout status and report `AgentStatusReported`, `ReconcileSucceeded`, and `ReconcileFailed`.
- **Dependencies:** Task 8.3.2.
- **Acceptance Criteria:**
  - Deployment Service receives ready/total status.
  - Failures include actionable reason without secrets.
  - CRD status mirrors actual state.
- **Estimated Complexity:** L

### Story 8.4 — Agent Operational Capabilities

#### Task 8.4.1 — Implement Agent Log Shipper

- **Description:** Stream pod logs from managed namespaces to Observability ingest with batching and backpressure.
- **Dependencies:** Task 13.1.1, Task 8.3.2.
- **Acceptance Criteria:**
  - Logs arrive within seconds under normal load.
  - Backpressure prevents agent memory runaway.
  - Agent only reads logs from managed namespaces.
- **Estimated Complexity:** L

#### Task 8.4.2 — Implement Agent Self-Update Hook

- **Description:** Support control-plane requested agent version upgrades using signed/version-pinned artifacts.
- **Dependencies:** Task 8.2.2.
- **Acceptance Criteria:**
  - Heartbeat response can trigger upgrade flow.
  - Failed upgrade rolls back or reports degraded state.
  - Upgrade request is authenticated and version-pinned.
- **Estimated Complexity:** L

---

## Epic 9 — Deployments

### Story 9.1 — Application and Environment Model

#### Task 9.1.1 — Create Deployment Schema

- **Description:** Add `applications`, `application_environments`, `deployments`, `deployment_revisions`, and `rollout_status` tables.
- **Dependencies:** Task 4.1.1, Task 5.2.1.
- **Acceptance Criteria:**
  - Deployment revisions are insert-only.
  - Active deployment uniqueness is enforced per app environment.
  - Indexes support deployment history listing.
- **Estimated Complexity:** L

#### Task 9.1.2 — Implement Application CRUD APIs

- **Description:** Implement `POST /v1/projects/{projectId}/applications` and supporting read/list APIs.
- **Dependencies:** Task 9.1.1, Task 3.2.2.
- **Acceptance Criteria:**
  - Project admin/developer can create apps.
  - Project viewers can read apps.
  - App names are unique within project.
- **Estimated Complexity:** M

#### Task 9.1.3 — Implement Application Environment API

- **Description:** Implement `POST /v1/applications/{appId}/environments` bound to `clusterEnvId`.
- **Dependencies:** Task 9.1.2, Task 5.2.1.
- **Acceptance Criteria:**
  - Project admin can create environments.
  - Environment maps to exactly one cluster namespace.
  - Invalid cluster environment returns `CLUSTER_ENV_NOT_FOUND`.
- **Estimated Complexity:** M

### Story 9.2 — Docker Image Deployment

#### Task 9.2.1 — Implement Deployment Create API

- **Description:** Implement `POST /v1/applications/{appId}/deployments` with source/config validation.
- **Dependencies:** Task 9.1.3, Task 11.2.1.
- **Acceptance Criteria:**
  - Docker image source is supported for MVP.
  - Resource quantities, replicas, ports, env, and secret bindings validate.
  - Invalid source returns `INVALID_SOURCE`.
- **Estimated Complexity:** L

#### Task 9.2.2 — Generate Immutable Deployment Revision

- **Description:** Snapshot deployment config into immutable `deployment_revisions` and emit `DeploymentCreated`/`DeploymentStarted`.
- **Dependencies:** Task 9.2.1, Task 1.2.3.
- **Acceptance Criteria:**
  - Revision cannot be updated after creation.
  - Config snapshot includes source, resources, env, secrets, ports, domains, and autoscaling settings.
  - Events include deployment and revision IDs.
- **Estimated Complexity:** L

#### Task 9.2.3 — Implement Desired State API for Agents

- **Description:** Expose agent-facing desired state pull endpoint scoped by cluster identity.
- **Dependencies:** Task 9.2.2, Task 2.3.2.
- **Acceptance Criteria:**
  - Agent can pull only its cluster's desired state.
  - Response includes revision ID and workload spec.
  - Endpoint supports no-op response when state is unchanged.
- **Estimated Complexity:** L

#### Task 9.2.4 — Process Agent Rollout Status

- **Description:** Consume agent status reports and update deployment/rollout status.
- **Dependencies:** Task 8.3.3.
- **Acceptance Criteria:**
  - Healthy rollout emits `DeploymentSucceeded`.
  - Failed rollout emits `DeploymentFailed`.
  - Deployment detail API shows ready/total and timestamps.
- **Estimated Complexity:** L

### Story 9.3 — Rollback

#### Task 9.3.1 — Implement Rollback API

- **Description:** Implement `POST /v1/deployments/{deploymentId}/rollback`.
- **Dependencies:** Task 9.2.4.
- **Acceptance Criteria:**
  - Default rollback targets previous healthy revision.
  - Explicit `targetRevisionId` is validated.
  - Missing previous healthy revision returns `NO_PREVIOUS_REVISION`.
- **Estimated Complexity:** M

#### Task 9.3.2 — Emit Rollback Desired State

- **Description:** Create a new revision from the rollback target and notify the agent with updated desired state.
- **Dependencies:** Task 9.3.1, Task 8.3.1.
- **Acceptance Criteria:**
  - Rollback creates a new immutable revision.
  - `DeploymentRolledBack` emits after successful rollout.
  - Audit records source and target revision IDs.
- **Estimated Complexity:** M

---

## Epic 10 — Build System

### Story 10.1 — Build Metadata and API

#### Task 10.1.1 — Create Build Schema

- **Description:** Add `builds`, `build_steps`, `build_artifacts`, and `source_uploads` tables.
- **Dependencies:** Task 4.1.1.
- **Acceptance Criteria:**
  - Builds are tenant/project scoped.
  - Indexes support querying by org/status/date.
  - Source uploads reference object storage, not DB blobs.
- **Estimated Complexity:** M

#### Task 10.1.2 — Implement Build Create API

- **Description:** Implement `POST /v1/projects/{projectId}/builds` for Git and uploaded source inputs.
- **Dependencies:** Task 10.1.1, Task 11.1.1.
- **Acceptance Criteria:**
  - Valid source creates build in `queued`.
  - Invalid source returns `INVALID_SOURCE`.
  - `BuildQueued` event is emitted.
- **Estimated Complexity:** M

#### Task 10.1.3 — Implement Source Upload API

- **Description:** Add source archive upload flow backed by object storage, checksum validation, and upload metadata.
- **Dependencies:** Task 10.1.1, Task 1.1.3.
- **Acceptance Criteria:**
  - Source upload stores object ref and checksum.
  - Upload size limits are enforced.
  - Project authorization is enforced.
- **Estimated Complexity:** L

### Story 10.2 — Build Execution

#### Task 10.2.1 — Implement Build Worker

- **Description:** Consume `BuildRequested`/`BuildQueued`, fetch source, run Kaniko/BuildKit, and push image to registry.
- **Dependencies:** Task 10.1.2, Task 11.1.2.
- **Acceptance Criteria:**
  - Git source and uploaded source can be built.
  - Built image is pushed to registry.
  - `BuildStarted`, `BuildSucceeded`, and `BuildFailed` are emitted.
- **Estimated Complexity:** XL

#### Task 10.2.2 — Implement Build Logs

- **Description:** Capture build step logs and expose `GET /v1/builds/{buildId}/logs`.
- **Dependencies:** Task 10.2.1, Task 13.1.1.
- **Acceptance Criteria:**
  - Logs are cursor-paginated.
  - Project read access is required.
  - Failed builds include log references.
- **Estimated Complexity:** M

#### Task 10.2.3 — Add Tenant Build Isolation and Limits

- **Description:** Enforce per-tenant build concurrency, resource limits, and isolated credentials.
- **Dependencies:** Task 10.2.1, Task 4.2.2.
- **Acceptance Criteria:**
  - Tenant cannot exceed concurrency quota.
  - Build jobs do not share writable workspace across tenants.
  - Registry/Git credentials are scoped to build job.
- **Estimated Complexity:** L

### Story 10.3 — Supply Chain Metadata

#### Task 10.3.1 — Generate Build Provenance and SBOM

- **Description:** Capture source ref, image digest, build parameters, and optional SBOM artifact for built images.
- **Dependencies:** Task 10.2.1.
- **Acceptance Criteria:**
  - Build artifacts store image digest and SBOM ref when available.
  - Deployment revision can link to build artifact.
  - Provenance contains no secret values.
- **Estimated Complexity:** L

---

## Epic 11 — Secrets

### Story 11.1 — Encrypted Secret Storage

#### Task 11.1.1 — Create Secrets Schema

- **Description:** Add `secrets`, `secret_versions`, `secret_bindings`, and `kms_keys`.
- **Dependencies:** Task 9.1.1.
- **Acceptance Criteria:**
  - `secrets` has no plaintext value column.
  - `(project_id, scope, name)` uniqueness is enforced.
  - Secret versions are immutable.
- **Estimated Complexity:** M

#### Task 11.1.2 — Implement Envelope Encryption

- **Description:** Encrypt secret values using envelope encryption with platform KMS key support and future customer-managed key references.
- **Dependencies:** Task 11.1.1.
- **Acceptance Criteria:**
  - Plaintext secret is never stored.
  - Logs/events/API responses never include secret values.
  - Encryption/decryption errors are handled safely.
- **Estimated Complexity:** L

#### Task 11.1.3 — Implement Create Secret API

- **Description:** Implement `POST /v1/projects/{projectId}/secrets`.
- **Dependencies:** Task 11.1.2, Task 3.2.2.
- **Acceptance Criteria:**
  - Project admin can create project/app secrets.
  - Name validation enforces `[A-Z0-9_]`.
  - Response never includes secret value.
  - `SecretCreated` event is emitted.
- **Estimated Complexity:** M

### Story 11.2 — Secret Bindings and Sync

#### Task 11.2.1 — Implement Secret Binding API

- **Description:** Implement `POST /v1/applications/{appId}/secret-bindings`.
- **Dependencies:** Task 11.1.3, Task 9.1.3.
- **Acceptance Criteria:**
  - `envVarName` is unique within app environment.
  - Secret and app environment must belong to the same project/org.
  - `SecretBound` event is emitted.
- **Estimated Complexity:** M

#### Task 11.2.2 — Emit Secret Sync Requests

- **Description:** Generate `SecretSyncRequested` for affected cluster environments when secrets are created, updated, rotated, or bound.
- **Dependencies:** Task 11.2.1, Task 1.2.3.
- **Acceptance Criteria:**
  - Only impacted cluster environments receive sync events.
  - Payload contains secret refs, not plaintext.
  - Audit metadata is recorded.
- **Estimated Complexity:** L

#### Task 11.2.3 — Agent Secret Reconciliation

- **Description:** Agent fetches scoped secret material and writes Kubernetes Secrets into target namespaces.
- **Dependencies:** Task 11.2.2, Task 8.3.1.
- **Acceptance Criteria:**
  - Only bound secrets sync to the correct namespace.
  - Agent never logs secret values.
  - Sync status is reported to control plane.
- **Estimated Complexity:** L

### Story 11.3 — Rotation and Versioning

#### Task 11.3.1 — Implement Secret Update/Rotation

- **Description:** Add secret update API that creates a new immutable version and triggers scoped sync.
- **Dependencies:** Task 11.2.2.
- **Acceptance Criteria:**
  - Rotation increments version.
  - Old versions remain available for audit/rollback policy.
  - `SecretUpdated` or `SecretRotated` event is emitted.
- **Estimated Complexity:** M

---

## Epic 12 — Domains

### Story 12.1 — Domain Ownership Verification

#### Task 12.1.1 — Create Domain Schema

- **Description:** Add `domains`, `dns_verifications`, `domain_bindings`, and `certificates`.
- **Dependencies:** Task 9.1.1.
- **Acceptance Criteria:**
  - `domains.hostname` is globally unique.
  - Verification records store TXT record name/value.
  - Domain rows are tenant/project scoped.
- **Estimated Complexity:** M

#### Task 12.1.2 — Implement Add Domain API

- **Description:** Implement `POST /v1/projects/{projectId}/domains`.
- **Dependencies:** Task 12.1.1, Task 3.2.2.
- **Acceptance Criteria:**
  - Valid FQDN creates domain in pending verification.
  - Duplicate hostname returns `DOMAIN_EXISTS`.
  - Response includes DNS TXT record instructions.
- **Estimated Complexity:** M

#### Task 12.1.3 — Implement DNS Verification

- **Description:** Implement `POST /v1/domains/{domainId}/verify` and background polling support.
- **Dependencies:** Task 12.1.2.
- **Acceptance Criteria:**
  - Correct TXT record marks domain verified.
  - Missing record returns `DNS_RECORD_MISSING`.
  - `DomainVerified` event is emitted once.
- **Estimated Complexity:** M

### Story 12.2 — Domain Binding and Ingress

#### Task 12.2.1 — Implement Domain Binding API

- **Description:** Implement `POST /v1/domains/{domainId}/bindings`.
- **Dependencies:** Task 12.1.3, Task 9.1.3.
- **Acceptance Criteria:**
  - Verified domains can be bound to app environments.
  - Unverified domains return `DOMAIN_NOT_VERIFIED`.
  - `DomainBound` event is emitted.
- **Estimated Complexity:** M

#### Task 12.2.2 — Agent Ingress Reconciliation

- **Description:** Agent reconciles `PlatformDomain` into Kubernetes Ingress resources for the target app environment.
- **Dependencies:** Task 12.2.1, Task 8.3.2.
- **Acceptance Criteria:**
  - Ingress routes hostname to the correct service/port.
  - Reconcile is idempotent.
  - Agent reports ingress configuration status.
- **Estimated Complexity:** L

### Story 12.3 — TLS Lifecycle

#### Task 12.3.1 — Integrate cert-manager Issuance

- **Description:** Configure agent/domain flow to create cert-manager Certificate resources using ACME ClusterIssuer.
- **Dependencies:** Task 12.2.2, Task 8.1.3.
- **Acceptance Criteria:**
  - TLS certificate is issued for verified domain.
  - Certificate status is reported to Domain Service.
  - `CertificateIssued` event is emitted.
- **Estimated Complexity:** L

#### Task 12.3.2 — Implement Certificate Renewal Monitoring

- **Description:** Monitor certificate expiry and emit `CertificateExpiring`/`CertificateFailed`.
- **Dependencies:** Task 12.3.1, Task 13.4.1.
- **Acceptance Criteria:**
  - Expiring certificates trigger notification before expiry.
  - Failed certificate issuance is visible in UI and audit.
  - Renewal state is updated without manual intervention.
- **Estimated Complexity:** M

---

## Epic 13 — Observability

### Story 13.1 — Log Ingestion and Query

#### Task 13.1.1 — Provision Log Backend

- **Description:** Add Loki or equivalent log backend to local/dev/prod infrastructure and configure retention.
- **Dependencies:** Task 1.3.1.
- **Acceptance Criteria:**
  - Log backend is deployable via infra/Helm.
  - Retention is configurable.
  - Health checks and dashboards exist.
- **Estimated Complexity:** L

#### Task 13.1.2 — Implement Agent Log Ingest Endpoint

- **Description:** Implement authenticated ingest endpoint for agent log batches.
- **Dependencies:** Task 13.1.1, Task 2.3.2.
- **Acceptance Criteria:**
  - Only valid agent credentials can ingest logs.
  - Logs are tenant/project/cluster/app tagged.
  - Backpressure/rate limits are enforced.
- **Estimated Complexity:** L

#### Task 13.1.3 — Implement Application Logs API

- **Description:** Implement `GET /v1/applications/{appId}/logs` with time range, cursor pagination, and tenant auth.
- **Dependencies:** Task 13.1.2, Task 9.1.2.
- **Acceptance Criteria:**
  - Project members can query logs.
  - Time range cap returns `TIME_RANGE_TOO_LARGE`.
  - Response matches API spec.
- **Estimated Complexity:** M

#### Task 13.1.4 — Implement Live Log Streaming

- **Description:** Add WebSocket support through API Gateway for live pod log tailing.
- **Dependencies:** Task 13.1.3, Task 1.3.2.
- **Acceptance Criteria:**
  - Authorized users can live-tail app logs.
  - Gateway enforces auth and rate limits on WebSockets.
  - Stream closes safely when access is revoked.
- **Estimated Complexity:** L

### Story 13.2 — Metrics and Health

#### Task 13.2.1 — Provision Metrics Backend

- **Description:** Add Prometheus/Mimir-compatible metrics backend and scrape/remote-write configuration.
- **Dependencies:** Task 1.3.1.
- **Acceptance Criteria:**
  - Control-plane metrics are collected.
  - Agent metrics path is defined.
  - Dashboards include service health and resource usage.
- **Estimated Complexity:** L

#### Task 13.2.2 — Implement Metrics Query API

- **Description:** Implement `GET /v1/applications/{appId}/metrics`.
- **Dependencies:** Task 13.2.1, Task 9.1.2.
- **Acceptance Criteria:**
  - Supports CPU, memory, requests, errors, and latency.
  - Project read authorization is enforced.
  - Query range and step validation prevent expensive queries.
- **Estimated Complexity:** M

#### Task 13.2.3 — Implement Health Summaries

- **Description:** Generate app/deployment/cluster health summaries from events and telemetry.
- **Dependencies:** Task 9.2.4, Task 8.2.3.
- **Acceptance Criteria:**
  - `HealthSummaryUpdated` emitted when summary changes.
  - UI can consume current health summary.
  - Summaries degrade when telemetry is stale.
- **Estimated Complexity:** L

### Story 13.3 — Audit Service

#### Task 13.3.1 — Create Audit Schema

- **Description:** Add partitioned `audit_logs` table with append-only triggers and indexes.
- **Dependencies:** Task 1.2.2.
- **Acceptance Criteria:**
  - `audit_logs` cannot be updated or deleted.
  - Monthly partitioning is configured.
  - Indexes support actor/resource/time queries.
- **Estimated Complexity:** M

#### Task 13.3.2 — Implement Audit Event Sink

- **Description:** Subscribe Audit Service to all domain events and direct internal ingest calls.
- **Dependencies:** Task 13.3.1, Task 1.2.3.
- **Acceptance Criteria:**
  - Every state-changing event produces an audit row.
  - Secret values are redacted or absent.
  - Duplicate events are idempotently ignored.
- **Estimated Complexity:** L

#### Task 13.3.3 — Implement Audit Query API

- **Description:** Implement `GET /v1/organizations/{orgId}/audit-logs`.
- **Dependencies:** Task 13.3.2, Task 3.2.2.
- **Acceptance Criteria:**
  - Org auditor/admin can query audit logs.
  - Cursor pagination works on partitioned tables.
  - Cross-org access is blocked.
- **Estimated Complexity:** M

### Story 13.4 — Notifications

#### Task 13.4.1 — Implement Email Notification Channel

- **Description:** Build Notification Service email channel for invitations and deployment/cluster/domain events.
- **Dependencies:** Task 1.2.3.
- **Acceptance Criteria:**
  - `MemberInvited`, `DeploymentFailed`, and `ClusterUnhealthy` send emails.
  - Email templates are configurable.
  - Delivery failures are retried.
- **Estimated Complexity:** M

#### Task 13.4.2 — Implement Webhook Notification Channel

- **Description:** Add webhook endpoint management, signed deliveries, retry policy, and delivery logs.
- **Dependencies:** Task 13.4.1.
- **Acceptance Criteria:**
  - Webhook payloads are signed.
  - Failed deliveries retry with backoff.
  - Delivery status is queryable.
- **Estimated Complexity:** L

---

## Epic 14 — Frontend

### Story 14.1 — Console Foundation

#### Task 14.1.1 — Scaffold Web Console

- **Description:** Create frontend app shell with routing, authenticated layout, unauthenticated layout, design system integration, and API client setup.
- **Dependencies:** Task 1.1.2, Task 2.2.1.
- **Acceptance Criteria:**
  - App has login, authenticated dashboard shell, and route guards.
  - API client attaches access token.
  - Session expiration redirects to login.
- **Estimated Complexity:** M

#### Task 14.1.2 — Implement Auth Screens

- **Description:** Build login, logout, session refresh handling, and current user display.
- **Dependencies:** Task 14.1.1, Task 2.1.4.
- **Acceptance Criteria:**
  - User can log in and log out.
  - Refresh token flow keeps session active.
  - Auth errors are shown in user-friendly form.
- **Estimated Complexity:** M

### Story 14.2 — Organization and Project UI

#### Task 14.2.1 — Build Organization Dashboard

- **Description:** UI for org overview, member list, invite member, role changes, and audit entry link.
- **Dependencies:** Task 3.2.3, Task 13.3.3.
- **Acceptance Criteria:**
  - Org admins can invite members and change roles.
  - Auditors can access audit logs read-only.
  - Unauthorized actions are hidden and blocked by API.
- **Estimated Complexity:** L

#### Task 14.2.2 — Build Project Dashboard

- **Description:** UI for project list, create project, project detail, members, apps, secrets, domains, and environments.
- **Dependencies:** Task 4.2.1.
- **Acceptance Criteria:**
  - Org admins can create projects.
  - Project members see only accessible projects.
  - Empty states guide users to next setup step.
- **Estimated Complexity:** L

### Story 14.3 — Cluster UI

#### Task 14.3.1 — Build Cluster List and Detail Views

- **Description:** UI for clusters, status, provider, region, agent status, environments, and capabilities.
- **Dependencies:** Task 5.1.2.
- **Acceptance Criteria:**
  - Cluster list updates status accurately.
  - Detail page shows heartbeat freshness and capabilities.
  - Unhealthy clusters are visually marked.
- **Estimated Complexity:** M

#### Task 14.3.2 — Build Existing Cluster Registration Flow

- **Description:** UI to register an existing cluster and display generated install command/token.
- **Dependencies:** Task 6.1.3.
- **Acceptance Criteria:**
  - Org admin can create registration session.
  - Token is shown once with warning.
  - UI tracks pending/registered/ready status.
- **Estimated Complexity:** L

#### Task 14.3.3 — Build Cluster Provisioning Flow

- **Description:** UI for new cluster config input across AWS/GCP/Azure and generated provisioning command.
- **Dependencies:** Task 7.1.2.
- **Acceptance Criteria:**
  - Provider-specific validation errors display clearly.
  - Generated command and bundle URL are shown.
  - Provisioning run status is visible.
- **Estimated Complexity:** L

### Story 14.4 — Deployment UI

#### Task 14.4.1 — Build Application and Environment Screens

- **Description:** UI to create apps, bind environments to cluster environments, and view app environment state.
- **Dependencies:** Task 9.1.3, Task 5.2.1.
- **Acceptance Criteria:**
  - User can create an application and environment.
  - Environment selector shows only accessible cluster environments.
  - Errors map to documented API codes.
- **Estimated Complexity:** L

#### Task 14.4.2 — Build Docker Image Deployment Form

- **Description:** UI for Docker image deploy with replicas, resources, ports, env vars, secret bindings, and autoscaling.
- **Dependencies:** Task 9.2.1, Task 11.2.1.
- **Acceptance Criteria:**
  - Form validates required image and config fields.
  - Secret bindings can be selected but values are never displayed.
  - Successful submit navigates to deployment status.
- **Estimated Complexity:** L

#### Task 14.4.3 — Build Deployment Detail and Rollback UI

- **Description:** UI for rollout status, revision history, events, and rollback.
- **Dependencies:** Task 9.3.2, Task 13.1.3.
- **Acceptance Criteria:**
  - Status shows ready/total and phase.
  - Revision history identifies healthy revisions.
  - Rollback action is role-gated.
- **Estimated Complexity:** L

### Story 14.5 — Secrets, Domains, and Observability UI

#### Task 14.5.1 — Build Secrets UI

- **Description:** UI to create, rotate, and bind project/app secrets without revealing secret values.
- **Dependencies:** Task 11.3.1.
- **Acceptance Criteria:**
  - Secret value is write-only.
  - Rotation creates a new version.
  - Secret admin permissions are enforced visually and via API.
- **Estimated Complexity:** M

#### Task 14.5.2 — Build Domains and TLS UI

- **Description:** UI for adding domains, DNS verification, binding to app environments, and certificate status.
- **Dependencies:** Task 12.3.2.
- **Acceptance Criteria:**
  - DNS TXT instructions are displayed.
  - Verification and certificate status update in UI.
  - Unverified domains cannot be bound.
- **Estimated Complexity:** L

#### Task 14.5.3 — Build Logs and Metrics UI

- **Description:** UI for recent logs, live logs, and basic metrics dashboards.
- **Dependencies:** Task 13.1.4, Task 13.2.2.
- **Acceptance Criteria:**
  - Logs support time range and live tail.
  - Metrics show CPU, memory, requests, errors, and latency.
  - Query limits and errors are surfaced clearly.
- **Estimated Complexity:** L

### Story 14.6 — Frontend Release Quality

#### Task 14.6.1 — Add End-to-End MVP Flows

- **Description:** Add Playwright tests for login, org/project setup, existing cluster registration, Docker deployment, logs, secrets, domains, and rollback.
- **Dependencies:** Task 14.5.3.
- **Acceptance Criteria:**
  - E2E tests cover the full MVP happy path.
  - Key negative cases are covered: forbidden action, invalid token, failed deployment.
  - E2E suite runs in CI.
- **Estimated Complexity:** XL

#### Task 14.6.2 — Add Frontend Error and Empty State Coverage

- **Description:** Standardize loading, error, empty, forbidden, and not-found states across the console.
- **Dependencies:** Task 14.2.2, Task 14.4.3.
- **Acceptance Criteria:**
  - All major pages have loading/error/empty states.
  - API error codes map to helpful messages.
  - Forbidden actions do not leak hidden resource details.
- **Estimated Complexity:** M
