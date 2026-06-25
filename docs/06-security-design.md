# 06 — Security Design

## 1. Authentication

- **User auth:** email/password with argon2id hashing; short-lived access tokens (JWT, ~15 min) + rotating refresh tokens (httpOnly, revocable).
- **Enterprise:** SSO via OIDC/SAML; MFA (TOTP) required for privileged roles (owner/admin).
- **Machine identities:** API tokens and service accounts, scoped and revocable, for automation.
- **Token validation:** performed at the API Gateway; revocation denylist cached in Redis, populated by the `TokenRevoked` event.
- **Brute-force protection:** rate limiting + account lockout on repeated `LoginFailed`.

## 2. Authorization

- **Two RBAC levels:**
  - **Org roles:** `owner`, `admin`, `member`, `auditor`.
  - **Project roles:** `admin`, `developer`, `viewer`.
- **Centralized resolution:** Tenant Service is the source of truth; permission decisions cached per `(user, org/project)`.
- **Deny by default:** every service re-checks authorization (gateway checks are not trusted alone).
- **Least privilege:** developers cannot manage secrets or members; auditors are read-only.
- See the authorization matrix in `04-api-spec.md`.

## 3. Tenant Isolation

- `org_id` on every tenant row; mandatory query scoping in the repository layer.
- PostgreSQL RLS policies keyed on a session variable as a database backstop.
- Per-tenant quotas and rate limits; build jobs isolated per tenant.
- Any cross-tenant access attempt is denied and logged as a security event.

## 4. Secrets Encryption

- **Envelope encryption:** a data key encrypts each secret value; the data key is wrapped by a KMS master key (or customer-managed key via `kms_key_ref`).
- **Never persisted in plaintext**, and never returned via API, logs, events, or error messages.
- **Versioning:** secret versions are immutable; rotation creates a new version.
- **Scoped sync:** only secrets bound to a given cluster environment are synced, and only to the target namespace.
- **Audit:** all secret reads, updates, and syncs record access metadata (never values).

## 5. Cluster Registration Security

- **One-time tokens:** short-lived registration token, stored as a **hash**, bound to org + cluster + install session.
- **Single use:** token exchanged at registration for a long-lived, rotatable agent credential, then marked used.
- **Outbound-only connectivity:** customer clusters open only outbound connections; the control plane never connects inbound, so no ports are exposed on customer infrastructure.
- **Least-privilege IaC:** generated Terraform/OpenTofu uses least-privilege IAM; the customer holds cloud credentials; the platform never stores cloud root credentials.
- **Reproducibility:** generated config versions are recorded for audit and reproducibility.

## 6. Agent Authentication

- **Per-cluster credential:** mTLS or signed bearer credential scoped to that cluster's resources only.
- **Rotation & revocation:** credentials are rotatable and revocable from the control plane; revocation takes effect immediately.
- **Scoped authorization:** agent requests carry cluster identity; the control plane authorizes only that cluster's desired state and secrets.
- **Self-update integrity:** agent upgrades are signed and version-pinned by the control plane.

## 7. Audit Logging

- **Append-only:** `audit_logs` is insert-only and partitioned; DB triggers block updates/deletes.
- **Coverage:** captures actor, action, resource, before/after metadata (no secret values), request ID, and source IP.
- **Event sink:** all `*` domain events sink to the Audit Service in addition to direct internal ingest calls.
- **Retention & export:** configurable retention, legal hold, and export to object storage for long-term archival.

## 8. Supply Chain Security

- Image scanning, SBOM generation, provenance metadata, and signed images (post-MVP).
- Git source ownership validation and webhook signature verification.
- Per-tenant isolated build jobs; private registries and Git credentials provided via scoped secrets.

## 9. Runtime Security

- Agent uses least-privilege Kubernetes RBAC scoped to managed namespaces.
- Separate namespaces per project/environment; network policies where supported.
- Admission policy integration available for enterprise customers.
- All deployment, rollback, secret, and domain operations are audited.

## 10. Threat Model Summary

| Threat | Mitigation |
|---|---|
| Cross-tenant data access | org scoping + RLS + repository guard + audit |
| Stolen registration token | one-time, hashed, short-lived, bound to session |
| Compromised agent credential | per-cluster scope, rotation, immediate revocation |
| Secret leakage | envelope encryption, no plaintext anywhere, scoped sync |
| Token theft / replay | short-lived access tokens, refresh rotation, denylist |
| Cloud credential exposure | platform never stores cloud root creds; customer-executed IaC |
| Privilege escalation | deny-by-default RBAC, per-service re-checks, least privilege |
| Tampered audit trail | append-only, trigger-protected, exported to immutable storage |
