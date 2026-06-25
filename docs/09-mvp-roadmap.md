# 09 — MVP Definition & Development Roadmap

## 1. MVP v1 Scope

The MVP proves the core loop: **register a cluster → deploy a container → manage secrets, logs, and domains.** New cluster creation and source builds follow MVP.

### Must Have
- Email/password auth, sessions, refresh tokens.
- Organizations, projects, membership, RBAC (org + project roles).
- Audit logging.
- Existing cluster registration (one-time token → agent credential).
- Cluster Agent: register, heartbeat, reconcile, status report, log shipping.
- Docker image deployment with config (replicas, resources, ports, env).
- Deployment status + rollback.
- Secrets (envelope-encrypted) + scoped sync to clusters.
- Live + recent logs.
- Custom domain bind + automatic TLS (cert-manager).
- Deployment success/failure notifications (email).
- Web console for all of the above.

### Should Have
- New cluster creation for one cloud (e.g., EKS) via generated command.
- Git repository deployment + Build Service.
- Metrics dashboards (CPU/memory/requests).
- Autoscaling (HPA) config.
- Webhook notifications.

### Nice To Have
- Source code upload builds.
- All three clouds for cluster creation (GKE, AKS).
- SSO/SAML, customer-managed KMS.
- SBOM / image scanning / signed images.
- Advanced RBAC, billing/usage metering, multi-region control plane.

## 2. MVP Exit Criteria

- A new org can register an existing cluster and reach `ready` in under 15 minutes.
- A developer can deploy a Docker image and reach `healthy`, then roll back.
- Secrets sync to the correct namespace without exposing plaintext.
- Live logs visible within seconds; custom domain serves over TLS.
- All state changes appear in the audit log.
- Security review and load test of heartbeat/ingest paths passed.

## 3. Implementation Order (to MVP)

| Week | Focus | Deliverables |
|---|---|---|
| 1 | Foundation | Monorepo + CI, shared libs (`db`, `authz`, `events`, `telemetry`, `errors`), schema/migrations for users/orgs/projects, Auth Service (email/password, tokens), API Gateway routing + auth |
| 2 | Tenancy & RBAC | Tenant Service (org/project CRUD, invitations, roles), RBAC middleware + RLS backstop, Audit Service ingest+query, frontend auth + dashboard shell |
| 3 | Cluster Registration | Cluster Service (records, install sessions, agent register, heartbeat), Auth agent-credential exchange, Cluster Agent v0, Helm agent chart + installer CLI skeleton |
| 4 | Deployment Core | Deployment Service (apps, envs, deployments, immutable revisions), desired-state pull API, agent reconcile loop, Docker image deploy end-to-end |
| 5 | Status, Logs, Secrets | Agent status reporting + log shipping, Observability ingest+query (Loki), Secrets Service envelope encryption + scoped sync, frontend deploy/logs UI |
| 6 | Domains, TLS, Launch | Domain Service (add/verify/bind + cert-manager), Notification Service (deploy + invite + webhook), e2e tests, security review, load test, **MVP launch** |

## 4. Post-MVP Roadmap

### Phase 7 — New Cluster Creation
- Provisioning Service + EKS/GKE/AKS Terraform modules.
- Install bundle generation, install session tracking, failure diagnostics.
- Module versioning + reproducibility.

### Phase 8 — Source-Based Deployments
- Git repository integration + webhooks.
- Source upload flow.
- Build Service (Kaniko/BuildKit) + registry + build logs + cache.
- Deployment provenance + SBOM.

### Phase 9 — Observability Depth
- Metrics dashboards, alerting rules, autoscaling.
- Deployment health summaries, SLO tracking.

### Phase 10 — Enterprise Readiness
- SSO/SAML/OIDC, advanced RBAC.
- Customer-managed KMS.
- Private networking options, admission policy integration.
- Image scanning, signed images.
- Billing, quotas, usage metering.
- Multi-region control plane.

## 5. Dependencies & Risks

| Risk | Mitigation |
|---|---|
| Agent connectivity behind strict customer firewalls | Outbound-only design; document required egress |
| Cross-tenant data leakage | RLS + repository guard + audit from day one |
| Terraform drift / partial provisioning | Idempotent commands; install session diagnostics |
| Secret exposure | Envelope encryption; no plaintext in logs/events/APIs |
| Heartbeat/log ingest scale | Separate ingest path; Redis buffering; load test before launch |
| Cloud provider API differences | Uniform module variable contract over per-cloud modules |
