# 13 — Event Contract Remediation Plan

This document defines the remediation plan for aligning the event catalog, event framework, Auth Service, Tenant Service, and future services around a single production-grade event contract.

Scope reviewed:

- `docs/12-event-catalog.md`
- `backend/libs/events/*`
- Auth Service
- Tenant Service

Current findings:

- Auth event names do not match the catalog.
- Auth uses best-effort publishing instead of transactional outbox.
- Tenant payloads duplicate envelope metadata and also omit some catalog-required fields.
- Event ownership conflicts exist in the catalog.
- Sensitive tokens are published in events.
- Versioning policy exists but is not enforced by consumers.
- Catalog and implementation have drifted.

---

## Executive Summary

The platform has the right architectural foundation: a versioned envelope, subject derivation, JetStream publisher/subscriber, retry/DLQ behavior, and a PostgreSQL transactional outbox. The main gap is governance and enforcement. The catalog currently describes one contract, while implemented services publish a different contract.

Remediation should happen in three phases:

1. **Contract freeze:** declare canonical naming, envelope, payload, ownership, sensitive data, and versioning rules.
2. **Compatibility bridge:** publish or consume legacy and canonical names during a migration window.
3. **Enforcement:** add contract tests, schema validation, upcaster integration, and outbox requirements for state-changing events.

Target outcome:

- All state-changing operations publish durable events via outbox.
- Every event has exactly one owner.
- Payload schemas exclude envelope metadata unless domain-specific.
- Sensitive tokens are not emitted on broadly consumed streams.
- Consumers can safely handle versioned contracts and replays.
- The catalog and implementation are mechanically checked in CI.

---

## A. Canonical Event Naming Convention

### Desired Convention

Canonical event names use:

```text
<domain>.<resource>.<action>.v<version>
```

Examples:

- `auth.user.created.v1`
- `auth.user.email.verified.v1`
- `auth.user.password.reset.requested.v1`
- `tenant.organization.created.v1`
- `tenant.member.invited.v1`
- `project.created.v1`
- `cluster.registered.v1`
- `deployment.started.v1`

The implemented Go event framework stores:

- `type`: canonical name without version, e.g. `tenant.organization.created`
- `version`: integer version, e.g. `1`
- NATS subject: `<subjectPrefix>.<type>.v<version>`

The catalog name is derived as:

```text
<type>.v<version>
```

### Rules

- Use lowercase dot-separated tokens.
- Use past-tense factual actions, not commands.
- Avoid underscores in event type tokens.
- Do not include transport concerns in names.
- Do not include environment, tenant, or project IDs in names.
- Use a stable resource noun.
- Version is always represented as `vN` in catalog names and broker subjects.

### Current State

- Catalog uses `eventName` as a required envelope field.
- Implementation does not have `eventName`; it uses `type` and `version`.
- Auth uses non-catalog names:
  - `auth.user.signed_up`
  - `auth.email.verified`
  - `auth.password.reset`
  - `auth.service_account.created`
  - `auth.api_token.created`

### Desired State

- `eventName` is not stored as a first-class envelope field unless explicitly added to `libs/events.Envelope`.
- Catalog treats `eventName` as derived from `type` and `version`.
- Auth names are canonicalized.

### Impact

Without remediation, consumers cannot reliably subscribe by catalog name, event documentation does not match actual subjects, and replay tooling must carry special-case mappings.

### Breaking or Non-Breaking

- Renaming event subjects is **breaking** for consumers.
- Treating `eventName` as derived documentation is **non-breaking**.

### Migration Strategy

1. Define a legacy-to-canonical mapping.
2. During migration, Auth publishes canonical events and optionally legacy aliases.
3. Consumers subscribe to canonical names.
4. Add metrics for legacy event usage.
5. Remove legacy aliases after the deprecation window.

### Recommended Implementation

Canonical replacements:

| Current Event | Canonical Event |
|---|---|
| `auth.user.signed_up.v1` | `auth.user.created.v1` |
| `auth.email.verified.v1` | `auth.user.email.verified.v1` |
| `auth.password.reset.v1` | split into `auth.user.password.reset.requested.v1` and `auth.user.password.reset.completed.v1` |
| `auth.service_account.created.v1` | `auth.service.account.created.v1` |
| `auth.api_token.created.v1` | `auth.api.token.created.v1` |
| `auth.api_token.revoked.v1` | `auth.api.token.revoked.v1` |

Recommended catalog correction:

- Replace `auth.user.email_verified.v1` with `auth.user.email.verified.v1`.
- Replace `auth.user.password_reset.v1` with explicit requested/completed events.
- Keep `tenant.organization.created.v1`, `tenant.organization.updated.v1`, `tenant.member.invited.v1`, `tenant.member.removed.v1`, `tenant.role.changed.v1`.

---

## B. Canonical Envelope Contract

### Current State

`libs/events.Envelope` contains:

- `eventId`
- `type`
- `version`
- `orgId`
- `correlationId`
- `traceparent`
- `occurredAt`
- `actor`
- `resource`
- `payload`

It does not contain:

- `eventName`

Validation currently requires:

- `eventId`
- `type`
- `version >= 1`
- `orgId`

### Desired State

Canonical envelope:

```json
{
  "eventId": "evt_123",
  "type": "tenant.organization.created",
  "version": 1,
  "orgId": "org_123",
  "correlationId": "req_123",
  "traceparent": "00-...",
  "occurredAt": "2026-06-24T05:10:00Z",
  "actor": {
    "type": "user",
    "id": "user_123"
  },
  "resource": {
    "type": "organization",
    "id": "org_123"
  },
  "payload": {}
}
```

Derived values:

- Catalog name: `type + ".v" + version`
- NATS subject: `<subjectPrefix> + "." + type + ".v" + version`

### Required Envelope Fields

- `eventId`
- `type`
- `version`
- `orgId`
- `occurredAt`
- `payload`

### Recommended Required Envelope Fields for Production

- `actor`
- `resource`
- `correlationId` for request-driven events
- `traceparent` when the event is produced during a traced request or worker span

### Impact

Leaving `eventName` as a required field in docs but absent in implementation causes schema drift and breaks generated validators.

### Breaking or Non-Breaking

- Removing `eventName` from the documented required envelope is **non-breaking** for implementation.
- Adding `eventName` to emitted JSON is **non-breaking** for tolerant consumers, but unnecessary duplication.

### Migration Strategy

1. Update `docs/12-event-catalog.md` to define `eventName` as derived, not required.
2. Update examples to omit `eventName` or label it as derived.
3. Add contract tests that verify examples match `libs/events.Envelope`.

### Recommended Implementation

- Do not add `EventName` to `Envelope`.
- Add helper documentation or helper function:
  - `CanonicalName(e Envelope) string`
- Strengthen `Envelope.Validate()` over time to require `occurredAt` and payload presence for production events.

---

## C. Canonical Payload Rules

### Current State

- Tenant payloads repeat `orgId`, which already exists in the envelope.
- Catalog requires timestamp fields like `createdAt`, `updatedAt`, `removedAt`, `changedAt` even when `occurredAt` exists in the envelope.
- Auth payloads are minimal and do not match catalog schemas.

### Desired State

Payloads contain domain facts not already expressed by the envelope.

Payloads should include:

- Resource-specific IDs not represented by `resource.id`.
- Parent IDs needed for routing or materialization, e.g. `projectId`, `clusterId`.
- Domain state values, e.g. `oldRole`, `newRole`.
- Business-specific timestamps only when they differ from `occurredAt`.

Payloads should not include:

- `eventId`
- `type`
- `version`
- `eventName`
- `occurredAt` duplicates like `createdAt` unless domain-specific
- `orgId` unless the payload is intended for standalone storage without envelope
- Secrets, plaintext credentials, refresh tokens, API tokens, password reset tokens, invitation tokens

### Impact

Duplicated metadata creates disagreement risk. Missing fields create consumer fragility. Sensitive fields create security exposure and replay/audit leakage.

### Breaking or Non-Breaking

- Removing duplicated optional fields is **breaking** if consumers already depend on payload-only data.
- Adding missing optional domain fields is **non-breaking**.
- Removing sensitive tokens is **breaking** for Notification Service if it currently relies on event payloads.

### Migration Strategy

1. Classify each payload field as envelope metadata, domain data, routing data, or sensitive data.
2. Keep duplicate metadata for one version if consumers use it, then remove in `v2`.
3. Add new fields as optional in `v1` where safe.
4. Move sensitive values to restricted fetch APIs before removing them from events.

### Recommended Implementation

For Tenant:

- Keep `orgId` in envelope only.
- Keep `organizationId`, `projectId`, etc. in payload only when not equal to envelope `orgId`.
- For `tenant.member.invited.v1`, replace `token` with `deliveryRef` or `invitationId`.
- Let Notification Service call a restricted internal endpoint to send the invite.

For Auth:

- Include stable user facts in payloads: `userId`, `email`, `name` where needed.
- Do not include password reset or verification tokens on broadly consumed streams.

---

## D. Event Ownership Matrix

### Current State

Catalog has ownership conflicts:

- `cluster.created.v1`: Cluster Service or Provisioning Service.
- `cluster.registered.v1`: Cluster Service or Cluster Agent.
- `cluster.ready.v1`: Cluster Service or Cluster Agent.
- `cluster.agent.disconnected.v1`: Cluster Service or Cluster Agent.
- `deployment.started.v1`: Deployment Service or Cluster Agent.
- `deployment.succeeded.v1`: Deployment Service or Cluster Agent.
- `deployment.failed.v1`: Deployment Service or Cluster Agent.

### Desired State

Every canonical domain event has exactly one owner. Agents and workers can report observations, but the owning service emits the authoritative domain event.

### Impact

Multi-owner events cause incompatible payloads, duplicate publishing, race conditions, and unclear schema ownership.

### Breaking or Non-Breaking

Resolving ownership is **breaking** if multiple producers already publish the same subject. Today most conflicting services are future services, so this is mostly **non-breaking** if fixed before implementation.

### Migration Strategy

1. Assign owner service per event.
2. Rename non-owner emissions as internal observation events if needed.
3. Only the owner emits catalog domain events.
4. Consumers subscribe to owner-owned canonical events.

### Canonical Ownership Matrix

| Domain | Event Family | Owner | Notes |
|---|---|---|---|
| Auth | `auth.user.*` | Auth Service | Identity source of truth. |
| Auth | `auth.session.*`, `auth.token.*`, `auth.mfa.*` | Auth Service | Revocation events feed gateway/cache consumers. |
| Tenant | `tenant.organization.*`, `tenant.member.*`, `tenant.role.*`, `tenant.invitation.*` | Tenant Service | RBAC source of truth. |
| Project | `project.*` | Project Service | Project source of truth. |
| Cluster | `cluster.*` | Cluster Service | Cluster Agent reports observations to Cluster Service; Cluster Service emits canonical events. |
| Provisioning | `provisioning.*` | Provisioning Service | Use provisioning namespace for provisioning run/bundle lifecycle. |
| Deployment | `deployment.*` | Deployment Service | Cluster Agent reports rollout observations; Deployment Service emits canonical status. |
| Build | `build.*` | Build Service | Build execution source of truth. |
| Secrets | `secret.*` | Secrets Service | Secret values never emitted. |
| Domain/TLS | `domain.*` | Domain/TLS Service | Certificate lifecycle source of truth. |
| Audit | `audit.record.*` | Audit Service | Confirms durable audit ingestion. |
| Notification | `notification.*` | Notification Service | Delivery result source of truth. |

### Recommended Implementation

- Change catalog entries with owner conflicts to single-owner entries.
- Introduce optional internal observation events if needed:
  - `cluster.agent.heartbeat.missed.v1`
  - `cluster.agent.status.reported.v1`
  - `deployment.rollout.observed.v1`
- Do not expose observation events as authoritative domain state unless explicitly cataloged.

---

## E. Sensitive Data Policy

### Current State

Sensitive or security-relevant tokens appear in event payloads:

- Auth signup emits `verificationToken`.
- Auth password reset emits `resetToken`.
- Tenant member invited emits `token`.
- Catalog examples also include these tokens.

Audit Service is documented as consuming all platform events, which means tokens may be persisted in audit pipelines, DLQ, replay archives, logs, and local development traces.

### Desired State

No broadly consumed event contains plaintext secrets or credentials.

Forbidden in domain events:

- Passwords.
- Password reset tokens.
- Email verification tokens.
- Invitation tokens.
- Refresh tokens.
- Access tokens.
- API tokens.
- Service account secrets.
- Agent credentials.
- Kubeconfigs.
- Cloud credentials.
- TLS private keys.
- Secret values.

Allowed:

- Stable IDs.
- Hash-safe display prefixes.
- Non-sensitive metadata.
- Short-lived opaque delivery references that cannot authenticate by themselves.

### Impact

Current behavior risks credential leakage through retention, replay, DLQ, audit sinks, and observability tooling.

### Breaking or Non-Breaking

Removing tokens from event payloads is **breaking** for any Notification consumer that depends on them.

### Migration Strategy

1. Introduce restricted delivery APIs:
   - Auth: internal email verification delivery endpoint.
   - Auth: internal password reset delivery endpoint.
   - Tenant: internal invitation delivery endpoint.
2. Publish events with `deliveryRef` or `tokenPurpose`, not token value.
3. Notification Service exchanges `deliveryRef` for a send action through internal authenticated API.
4. Stop publishing token fields.
5. Rotate all tokens that may have passed through event streams in non-production or early environments if needed.

### Recommended Implementation

Replace:

```json
{
  "verificationToken": "opaque-token"
}
```

With:

```json
{
  "deliveryRef": "email_delivery_123",
  "purpose": "email_verification"
}
```

Replace:

```json
{
  "token": "opaque-invite-token"
}
```

With:

```json
{
  "invitationId": "inv_123",
  "deliveryRef": "invite_delivery_123"
}
```

---

## F. Event Versioning Policy

### Current State

- Catalog documents versioning.
- `libs/events.Registry` supports upcasters.
- Subscriber flow does not enforce or automatically apply upcasters.
- No schema registry or contract validation exists.

### Desired State

Versioning is enforced by tooling and runtime validation.

Rules:

- `v1` is the first production contract.
- Additive optional fields do not require a new version.
- Removing fields, renaming fields, changing types, changing semantics, or making optional fields required requires a new version.
- Producers may publish multiple versions during migration windows.
- Consumers must either handle the specific version or register an upcaster.

### Impact

Without enforcement, consumers may silently accept incompatible payloads or fail at runtime during replay.

### Breaking or Non-Breaking

Adding enforcement is **non-breaking** if initially warning-only. It becomes **breaking** once unknown versions are rejected.

### Migration Strategy

1. Add schema definitions for all cataloged payloads.
2. Add producer contract tests.
3. Add consumer compatibility tests.
4. Add warning-only validation in subscribers.
5. Require explicit upcasters before consuming older versions.
6. Fail unknown unsupported versions after one release cycle.

### Recommended Implementation

- Generate schemas from typed payload structs or maintain JSON Schema under `docs/events/schemas`.
- Add a `CatalogName(type, version)` helper.
- Add subscriber middleware:
  - parse subject
  - validate envelope
  - apply upcaster registry
  - validate payload schema
  - invoke handler
- Add CI check: every service event constant must exist in catalog.
- Add CI check: every catalog event owned by an implemented service must have a producer test.

---

## G. Replay Policy

### Current State

Catalog documents replay policy, but implementation-level replay tooling is not present.

### Desired State

Replay is operationally safe, scoped, auditable, and idempotent.

Replay principles:

- Replay preserves original `eventId`.
- Consumers deduplicate by `eventId`.
- Replays are scoped by event type, version, org, resource, time range, and consumer.
- Replays are audited.
- Replay uses retained broker messages when available, then archived event exports or source-of-truth regeneration.

### Impact

Without replay enforcement, operators may duplicate side effects such as notifications, deployments, or audit records.

### Breaking or Non-Breaking

Replay policy is **non-breaking** as documentation. Replay tooling can be breaking if consumers are not idempotent.

### Migration Strategy

1. Mark consumers as replay-safe or replay-unsafe.
2. Add deduplication storage per consumer group.
3. Add replay dry-run mode.
4. Add audit event or audit record for replay operations.
5. Enable replay for read-model consumers first.
6. Enable replay for side-effect consumers only after idempotency proof.

### Recommended Implementation

Consumer categories:

| Category | Replay Policy |
|---|---|
| Audit ingestion | Replay-safe with eventId dedupe. |
| Observability read models | Replay-safe, preferred first target. |
| Notifications | Replay-guarded; never resend by default. |
| Deployments | Replay-guarded; never trigger rollout side effects by default. |
| Secrets sync | Replay-safe only if version-aware and idempotent. |

---

## H. Backward Compatibility Strategy

### Current State

Auth has already produced legacy names relative to the catalog design. Tenant names match catalog but payloads differ.

### Desired State

Backward compatibility is explicit and time-bound.

### Impact

Immediate hard renames would break consumers as soon as they exist. Leaving aliases forever increases complexity and ambiguity.

### Breaking or Non-Breaking

- Dual-publish is **non-breaking** but increases event volume.
- Hard rename is **breaking**.
- Payload additive changes are **non-breaking**.
- Payload removals require new versions and deprecation.

### Migration Strategy

For Auth:

1. Introduce canonical names.
2. Dual-publish legacy and canonical names for one migration window.
3. Mark legacy names deprecated in catalog.
4. Migrate consumers.
5. Remove legacy names.

For Tenant:

1. Align catalog to payload rules by treating envelope metadata as canonical.
2. Add missing payload fields only if domain-specific.
3. Use `v2` only if removing fields used by consumers.

### Recommended Implementation

Use an event alias layer temporarily:

| Legacy | Canonical | Window |
|---|---|---|
| `auth.user.signed_up.v1` | `auth.user.created.v1` | 30 days after first consumer migration |
| `auth.email.verified.v1` | `auth.user.email.verified.v1` | 30 days |
| `auth.password.reset.v1` | split requested/completed | 60 days |
| `auth.service_account.created.v1` | `auth.service.account.created.v1` | 30 days |
| `auth.api_token.*.v1` | `auth.api.token.*.v1` | 30 days |

---

## I. Service-by-Service Remediation Plan

## I.1 Auth Service

### Issue 1: Event Names Do Not Match Catalog

**Current State**

Auth emits:

- `auth.user.signed_up.v1`
- `auth.email.verified.v1`
- `auth.password.reset.v1`
- `auth.login.succeeded.v1`
- `auth.login.failed.v1`
- `auth.token.revoked.v1`
- `auth.service_account.created.v1`
- `auth.api_token.created.v1`
- `auth.api_token.revoked.v1`

Catalog expects:

- `auth.user.created.v1`
- `auth.user.email_verified.v1`
- `auth.user.password_reset.v1`
- `auth.user.deleted.v1`

**Desired State**

Auth emits canonical events:

- `auth.user.created.v1`
- `auth.user.email.verified.v1`
- `auth.user.password.reset.requested.v1`
- `auth.user.password.reset.completed.v1`
- `auth.user.deleted.v1`
- `auth.login.succeeded.v1`
- `auth.login.failed.v1`
- `auth.token.revoked.v1`
- `auth.service.account.created.v1`
- `auth.service.account.deleted.v1`
- `auth.api.token.created.v1`
- `auth.api.token.revoked.v1`
- `auth.mfa.setup.started.v1`
- `auth.mfa.enabled.v1`
- `auth.mfa.disabled.v1`

**Impact**

Consumers built from the catalog will not receive actual Auth events.

**Breaking or Non-Breaking**

Breaking if renamed in place. Non-breaking if dual-published during migration.

**Migration Strategy**

Dual-publish legacy and canonical names, then retire legacy names after consumer migration.

**Recommended Implementation**

- Add canonical event constants.
- Keep legacy constants temporarily.
- Publish canonical via outbox.
- Optionally publish legacy aliases during the compatibility window.
- Update `docs/12-event-catalog.md` to include all Auth events actually needed.

### Issue 2: Best-Effort Publishing Instead of Transactional Outbox

**Current State**

Auth calls direct publisher after database transaction commits. Publish failures are logged and swallowed.

**Desired State**

Auth writes state changes and events to the same database transaction via `events.Outbox`.

**Impact**

Auth state can commit without event publication. This breaks downstream token denylist, notifications, audit, and read models.

**Breaking or Non-Breaking**

Non-breaking for consumers if event names/payloads are unchanged. Operationally significant.

**Migration Strategy**

1. Add outbox migration to Auth runtime if not already applied.
2. Inject `events.Outbox` into Auth Service.
3. Replace `emit` with `enqueue` for state-changing events.
4. Run outbox relay in Auth `main.go`.
5. Keep direct publish only for non-critical telemetry, if any.

**Recommended Implementation**

- Auth should mirror Tenant's `enqueue` pattern.
- Every operation that mutates Postgres should enqueue inside the same `Tx` or `WithTenant`.

### Issue 3: Sensitive Tokens in Events

**Current State**

Auth emits:

- `verificationToken`
- `resetToken`

**Desired State**

Auth emits delivery metadata, not token values.

**Impact**

Tokens may leak via broker retention, DLQ, audit, replay archives, and logs.

**Breaking or Non-Breaking**

Breaking for Notification Service if it relies on tokens from events.

**Migration Strategy**

1. Add internal delivery endpoint or command API.
2. Emit `deliveryRef`.
3. Notification Service calls Auth to initiate delivery or retrieve a sealed one-time delivery payload.
4. Remove token fields.

**Recommended Implementation**

Use:

```json
{
  "userId": "user_123",
  "email": "davit@example.com",
  "purpose": "email_verification",
  "deliveryRef": "delivery_123"
}
```

Not:

```json
{
  "verificationToken": "opaque-token"
}
```

### Issue 4: Missing Events for State Changes

**Current State**

Several state changes do not emit events:

- Refresh token rotation.
- MFA setup.
- MFA enable.
- MFA disable.
- Service account delete.
- User delete not implemented.

**Desired State**

Every durable state transition emits a domain event or is explicitly documented as internal-only.

**Impact**

Security, audit, and notification workflows cannot reliably observe identity lifecycle changes.

**Breaking or Non-Breaking**

Adding new events is non-breaking.

**Migration Strategy**

Add events incrementally with no consumers first, then onboard consumers.

**Recommended Implementation**

Add:

- `auth.token.rotated.v1`
- `auth.mfa.setup.started.v1`
- `auth.mfa.enabled.v1`
- `auth.mfa.disabled.v1`
- `auth.service.account.deleted.v1`
- `auth.user.deleted.v1`

---

## I.2 Tenant Service

### Issue 1: Payloads Duplicate Envelope Metadata and Omit Catalog Fields

**Current State**

Tenant payloads include `orgId`, which duplicates envelope `orgId`. Some catalog fields are missing:

- `createdAt`
- `updatedAt`
- `expiresAt`
- `removedAt`
- `changedAt`

**Desired State**

Use envelope `occurredAt` for event occurrence time. Payload includes domain fields only. Include domain-specific expiry fields like `expiresAt` when meaningful.

**Impact**

Consumers may disagree on whether to use payload timestamps or envelope timestamps.

**Breaking or Non-Breaking**

Adding `expiresAt` is non-breaking. Removing duplicated `orgId` requires `v2` if consumers exist.

**Migration Strategy**

1. Keep `orgId` in payload during `v1`.
2. Add missing non-sensitive domain fields such as `expiresAt`.
3. Define `v2` payloads that remove duplicated envelope metadata.
4. Upcast `v1` to `v2` by copying envelope `orgId`/`occurredAt` where needed.

**Recommended Implementation**

For `tenant.member.invited.v1`, add `expiresAt`.

For future `v2`, use:

```json
{
  "invitationId": "inv_123",
  "email": "new-user@example.com",
  "role": "developer",
  "invitedBy": "user_123",
  "expiresAt": "2026-07-01T05:40:00Z",
  "deliveryRef": "delivery_123"
}
```

### Issue 2: Sensitive Invitation Token in Event

**Current State**

`tenant.member.invited.v1` payload includes plaintext `token`.

**Desired State**

Event includes `invitationId` and `deliveryRef`, not the invitation token.

**Impact**

Invitation tokens can leak through all event consumers and replay paths.

**Breaking or Non-Breaking**

Breaking for consumers that need the token.

**Migration Strategy**

1. Add `deliveryRef` while keeping `token` temporarily.
2. Update Notification Service to use `deliveryRef`.
3. Remove `token` in `tenant.member.invited.v2`.

**Recommended Implementation**

- Store token hash in Tenant DB.
- Keep plaintext token only in a secure one-time delivery channel.
- Do not publish plaintext token to the domain event stream.

### Issue 3: State-Changing Operations Without Events

**Current State**

Tenant emits events for:

- Organization create.
- Organization update.
- Member invite.
- Member remove.
- Role change.

Tenant does not emit events for:

- Organization delete.
- Invitation accepted.
- Invitation revoked.

**Desired State**

Add:

- `tenant.organization.deleted.v1`
- `tenant.invitation.accepted.v1`
- `tenant.invitation.revoked.v1`
- Optionally `tenant.member.joined.v1` when accepting an invitation.

**Impact**

Downstream services cannot clean up org data, update membership read models, or audit invitation lifecycle reliably.

**Breaking or Non-Breaking**

Adding events is non-breaking.

**Migration Strategy**

Add events to catalog first, then emit from Tenant, then onboard consumers.

**Recommended Implementation**

Emit inside existing `WithTenant` transaction via outbox.

---

## I.3 Future Project Service

### Issue 1: Catalog Exists Before Implementation

**Current State**

Catalog defines:

- `project.created.v1`
- `project.updated.v1`
- `project.deleted.v1`

No Project Service implementation exists yet.

**Desired State**

Project Service is implemented contract-first.

**Impact**

If Project Service is built without contract tests, drift will repeat.

**Breaking or Non-Breaking**

Non-breaking before implementation.

**Migration Strategy**

Implement against the canonical contract from day one.

**Recommended Implementation**

- Own all `project.*` events.
- Use transactional outbox for all project state changes.
- Payloads should avoid envelope metadata duplication.
- Emit `project.deleted.v1` before or during deletion transaction so consumers can clean up.
- Add producer contract tests before service release.

### Issue 2: Authorization and Membership Dependencies

**Current State**

Project Service is expected to consume tenant role/member changes.

**Desired State**

Project Service consumes:

- `tenant.organization.created.v1`
- `tenant.organization.updated.v1`
- `tenant.member.removed.v1`
- `tenant.role.changed.v1`
- `tenant.organization.deleted.v1` once added

**Impact**

Project membership/read models can become stale if Tenant lifecycle events are incomplete.

**Breaking or Non-Breaking**

Adding missing Tenant events is non-breaking.

**Migration Strategy**

Implement Project Service after Tenant emits complete lifecycle events.

**Recommended Implementation**

- Build idempotent consumers keyed by `eventId`.
- Never mutate Tenant-owned membership data.
- Maintain project-specific read models only.

---

## I.4 Future Cluster Service

### Issue 1: Event Ownership Conflicts

**Current State**

Catalog lists Cluster Service and Cluster Agent as possible producers for some `cluster.*` events.

**Desired State**

Cluster Service owns all canonical `cluster.*` events.

**Impact**

Without owner clarity, Cluster Agent and Cluster Service can publish conflicting state.

**Breaking or Non-Breaking**

Non-breaking before implementation.

**Migration Strategy**

Define Cluster Agent messages as internal observations, not canonical events.

**Recommended Implementation**

Cluster Service owns:

- `cluster.created.v1`
- `cluster.registered.v1`
- `cluster.ready.v1`
- `cluster.deleted.v1`
- `cluster.agent.disconnected.v1`

Cluster Agent may send internal messages/API calls:

- `agent.registered`
- `agent.heartbeat`
- `agent.status.reported`

Cluster Service validates and emits canonical domain events.

### Issue 2: Provisioning Ownership Boundary

**Current State**

Catalog allows Provisioning Service to produce `cluster.created.v1`.

**Desired State**

Provisioning Service emits `provisioning.*`; Cluster Service emits `cluster.*`.

**Impact**

Provisioning and Cluster inventory can diverge if both write cluster lifecycle events.

**Breaking or Non-Breaking**

Non-breaking before implementation.

**Migration Strategy**

Add provisioning event family before implementing Provisioning.

**Recommended Implementation**

Provisioning emits:

- `provisioning.requested.v1`
- `provisioning.started.v1`
- `provisioning.succeeded.v1`
- `provisioning.failed.v1`
- `provisioning.bundle.generated.v1`

Cluster Service consumes provisioning success/failure and emits cluster domain events as needed.

---

## I.5 Future Deployment Service

### Issue 1: Deployment Event Ownership Conflict

**Current State**

Catalog allows Deployment Service or Cluster Agent to produce:

- `deployment.started.v1`
- `deployment.succeeded.v1`
- `deployment.failed.v1`

**Desired State**

Deployment Service owns all canonical `deployment.*` events.

**Impact**

If Cluster Agent emits authoritative deployment events, deployment state can fork from the control-plane source of truth.

**Breaking or Non-Breaking**

Non-breaking before implementation.

**Migration Strategy**

Use agent observations as inputs to Deployment Service state machine.

**Recommended Implementation**

Cluster Agent reports:

- rollout observed
- pod status
- health check status
- apply result

Deployment Service emits:

- `deployment.created.v1`
- `deployment.started.v1`
- `deployment.succeeded.v1`
- `deployment.failed.v1`
- `deployment.rollback.requested.v1`
- `deployment.rolled.back.v1`

### Issue 2: Command-Like Event Name

**Current State**

Catalog includes `deployment.rollback.v1`.

**Desired State**

Use factual lifecycle names:

- `deployment.rollback.requested.v1`
- `deployment.rolled.back.v1`

**Impact**

`deployment.rollback.v1` reads like a command, not a fact, and is ambiguous between request, start, and completion.

**Breaking or Non-Breaking**

Breaking if consumers already exist. Non-breaking before implementation.

**Migration Strategy**

Correct catalog before implementation.

**Recommended Implementation**

Do not implement `deployment.rollback.v1`. Replace it in catalog before Deployment Service build begins.

---

## Issue-by-Issue Remediation Checklist

### 1. Auth Event Names Do Not Match Catalog

- **Current State:** Implemented names diverge from catalog.
- **Desired State:** Auth emits canonical names.
- **Impact:** Consumers miss events.
- **Breaking or Non-Breaking:** Breaking without dual-publish.
- **Migration Strategy:** Dual-publish canonical and legacy.
- **Recommended Implementation:** Add canonical constants and deprecate legacy constants.

### 2. Auth Uses Best-Effort Publishing

- **Current State:** Auth direct-publishes and logs failures.
- **Desired State:** Auth uses outbox for state-change events.
- **Impact:** Lost events after committed state changes.
- **Breaking or Non-Breaking:** Non-breaking.
- **Migration Strategy:** Add outbox dependency and relay, then switch event writes into transactions.
- **Recommended Implementation:** Mirror Tenant's outbox pattern.

### 3. Tenant Payloads Duplicate Envelope Metadata

- **Current State:** Tenant payloads include `orgId`; catalog duplicates timestamp metadata.
- **Desired State:** Envelope holds metadata; payload holds domain facts.
- **Impact:** Inconsistent source of truth.
- **Breaking or Non-Breaking:** Potentially breaking if consumers rely on duplicated fields.
- **Migration Strategy:** Keep duplicates in `v1`, remove in `v2`.
- **Recommended Implementation:** Define `v2` payloads and upcasters.

### 4. Event Ownership Conflicts Exist

- **Current State:** Some events have multiple possible producers.
- **Desired State:** One owner per canonical event.
- **Impact:** Schema and state authority ambiguity.
- **Breaking or Non-Breaking:** Non-breaking before future services ship.
- **Migration Strategy:** Fix catalog before implementation.
- **Recommended Implementation:** Cluster Service owns `cluster.*`; Deployment Service owns `deployment.*`.

### 5. Sensitive Tokens Are Published in Events

- **Current State:** Auth and Tenant publish plaintext one-time tokens.
- **Desired State:** No plaintext tokens in domain events.
- **Impact:** Credential leakage through broker, DLQ, replay, audit, logs.
- **Breaking or Non-Breaking:** Breaking for token-consuming Notification flows.
- **Migration Strategy:** Introduce `deliveryRef`, migrate Notification, remove token fields in next version.
- **Recommended Implementation:** Restricted internal delivery APIs.

### 6. Versioning Policy Exists but Is Not Enforced

- **Current State:** Upcaster registry exists but is not integrated into subscription flow.
- **Desired State:** Consumers declare supported versions and upcasters.
- **Impact:** Runtime decode failures and unsafe replay.
- **Breaking or Non-Breaking:** Non-breaking if introduced warning-only first.
- **Migration Strategy:** Add validation and warnings, then enforce.
- **Recommended Implementation:** Subscriber middleware for version and schema validation.

### 7. Catalog and Implementation Have Drifted

- **Current State:** No automated catalog-to-code validation.
- **Desired State:** CI blocks drift.
- **Impact:** Documentation cannot be trusted by implementers.
- **Breaking or Non-Breaking:** Non-breaking.
- **Migration Strategy:** Start with report-only checks, then fail CI.
- **Recommended Implementation:** Contract tests comparing service event constants and payload structs to catalog schemas.

---

## Target Remediation Sequence

### Phase 0 — Policy Freeze

- Approve this remediation plan.
- Update `docs/12-event-catalog.md` with corrected envelope and ownership.
- Mark legacy Auth names as deprecated aliases.

### Phase 1 — Framework Enforcement

- Add canonical name helper.
- Add schema validation support.
- Add subscriber upcaster middleware.
- Add CI drift checks.

### Phase 2 — Auth Remediation

- Add Auth outbox.
- Add canonical Auth events.
- Remove sensitive token fields from broadly consumed events.
- Add missing Auth lifecycle events.

### Phase 3 — Tenant Remediation

- Add missing Tenant lifecycle events.
- Add `expiresAt`/domain-specific fields where needed.
- Move invitation token to delivery reference.
- Plan `v2` payloads that remove metadata duplication.

### Phase 4 — Future Service Guardrails

- Require event contract review before Project, Cluster, and Deployment implementation.
- Require producer contract tests before merge.
- Require replay-safety classification for every consumer.

---

## Success Criteria

The remediation is complete when:

- `docs/12-event-catalog.md` and emitted event constants match.
- Every implemented producer-owned event has a payload schema.
- Every state-changing operation either emits an event or documents why it does not.
- Auth and Tenant both use transactional outbox for durable domain events.
- No plaintext secrets or tokens appear in domain events.
- Every event has exactly one owner.
- Consumers declare supported versions.
- Upcasters are applied before handler decode.
- CI detects catalog/implementation drift.
- Replay procedures are safe for each consumer category.

