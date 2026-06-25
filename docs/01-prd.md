# 01 — Product Requirements Document

## 1. Overview

Build a multi-tenant cloud application deployment platform (similar to TrueFoundry) that lets customers register existing Kubernetes clusters or create new managed clusters on AWS EKS, GCP GKE, and Azure AKS. The platform manages organizations, projects, deployments, secrets, domains, TLS, logs, and monitoring while keeping infrastructure ownership with the customer.

**Core principle:** The platform generates cluster configuration and an installation command; the customer executes it locally. That command provisions infrastructure with Terraform/OpenTofu and registers the cluster back to the control plane. The platform never needs to store customer cloud root credentials.

## 2. Goals

- Strong tenant isolation across organizations and projects.
- Cloud-agnostic abstractions over EKS, GKE, and AKS.
- Secure-by-default cluster registration with one-time tokens and outbound-only agents.
- Deploy applications from Git repositories, Docker images, or uploaded source.
- Centralized secrets, domains/TLS, logs, and monitoring.
- Full auditability of every state-changing action.

## 3. Non-Goals (v1)

- Acting as a general-purpose CI system beyond build-for-deploy.
- Managing non-Kubernetes compute (VMs, serverless) directly.
- Storing long-lived cloud root credentials on the platform.

## 4. Primary Users / Personas

| Persona | Description | Key needs |
|---|---|---|
| Platform Admin | Operates the platform itself | Global config, plans, integrations, support |
| Organization Admin | Owns an organization | Users, cloud accounts, clusters, billing, security |
| Project Admin | Owns a project | Environments, secrets, domains, deployments |
| Developer | Ships applications | Deploy, logs, metrics, rollout status, builds |
| Auditor / Security | Oversight | Access logs, deployment history, secret-access metadata, policy violations |

## 5. Product Modules

1. Identity and access management
2. Organizations and projects
3. Cloud provider onboarding
4. Existing cluster registration
5. New cluster creation
6. Application build and deployment
7. Secrets and environment management
8. Domains, ingress, and TLS
9. Logs and metrics
10. Audit, events, and notifications

## 6. Supported Matrix

- **Clouds:** AWS (EKS), GCP (GKE), Azure (AKS)
- **Deployment sources:** Git repository, Docker image, source code upload
- **Cluster onboarding:** register existing cluster, create new cluster via generated command

## 7. Functional Requirements (User Stories)

### Authentication & Organizations
- As a user, I can sign up using email/password, SSO, or an enterprise IdP.
- As an org admin, I can invite users and assign roles.
- As an org admin, I can create projects and assign users to them.
- As an auditor, I can view all actions performed in my organization.

### Clusters
- As an org admin, I can register an existing Kubernetes cluster.
- As an org admin, I can generate a cloud-specific cluster creation config.
- As an org admin, I can execute a generated command on my machine to provision EKS, GKE, or AKS.
- As the platform, I can verify cluster agent health before marking a cluster ready.
- As a project admin, I can assign clusters to projects and environments.

### Deployments
- As a developer, I can deploy from a Git repository.
- As a developer, I can deploy from an existing Docker image.
- As a developer, I can upload source code and trigger a build.
- As a developer, I can configure CPU, memory, replicas, env vars, secrets, ports, domains, and autoscaling.
- As a developer, I can view rollout status, logs, events, and metrics.

### Secrets, Domains & Observability
- As a project admin, I can create project-level and app-level secrets.
- As a project admin, I can attach custom domains to apps.
- As the platform, I can provision TLS certificates automatically.
- As a developer, I can view live and historical logs.
- As a developer, I can inspect metrics for CPU, memory, requests, errors, and latency.

## 8. Non-Functional Requirements

- **Isolation:** strict tenant boundary at API, service, and DB layers.
- **Security:** envelope-encrypted secrets, one-time registration tokens, least-privilege IAM/RBAC, zero platform access to customer cloud creds unless explicitly delegated.
- **Scalability:** horizontally scalable control plane; heartbeat and log ingest paths scale independently.
- **Reliability:** idempotent provisioning commands; immutable deployment revisions for safe rollback.
- **Auditability:** append-only audit log for all state-changing actions.
- **Extensibility:** cloud-agnostic abstractions allow adding providers without data-model changes.

## 9. Success Metrics

- Time-to-first-deploy after cluster registration < 15 minutes.
- Deployment success rate > 98%.
- Cluster registration success rate > 95% on first command run.
- p99 control-plane API latency < 300 ms.
- Zero cross-tenant data access incidents.

## 10. Release Strategy

MVP focuses on existing-cluster registration + Docker image deployment + secrets + logs + domains/TLS. New cluster creation and source builds follow MVP. See `09-mvp-roadmap.md`.
