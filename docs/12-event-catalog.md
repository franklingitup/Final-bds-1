# 12 — Event Catalog

This document defines the canonical event catalog for the multi-tenant cloud application deployment platform. It is the source of truth for event names, ownership, schema evolution, retention, replay, and dead-letter handling across platform services.

---

## 1. Event Naming Conventions

### 1.1 Canonical Format

All platform events use the following name format:

```text
<domain>.<resource>.<action>.v<version>
```

Examples:

- `auth.user.created.v1`
- `tenant.organization.created.v1`
- `project.created.v1`
- `cluster.registered.v1`
- `deployment.started.v1`

### 1.2 Naming Rules

- Event names are lowercase.
- Tokens are separated by dots.
- Events describe facts that already happened, not commands.
- The final token is always the schema version: `v1`, `v2`, `v3`, etc.
- The producer service owns the event name and payload schema.
- Consumers must treat unknown fields as forward-compatible additions.
- Consumers must not depend on event ordering across unrelated aggregate IDs.
- Consumers must deduplicate by `eventId`.

### 1.3 Common Event Envelope

All events are carried in the platform event envelope (`backend/libs/events.Envelope`). The envelope stores `type` without the version suffix and `version` as an integer. The catalog name and broker subject are **derived**, never stored as first-class fields:

- Catalog name = `<type>.v<version>` (e.g. `auth.user.created.v1`), produced by `events.CanonicalName`.
- Broker subject = `<subjectPrefix>.<type>.v<version>`, produced by `events.Subject`.

```json
{
  "eventId": "01JZ2N2B8TZY7PXE1Z6SPY8V7Y",
  "type": "deployment.started",
  "version": 1,
  "orgId": "org_123",
  "correlationId": "req_123",
  "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
  "occurredAt": "2026-06-24T05:10:00Z",
  "actor": {
    "type": "user",
    "id": "user_123"
  },
  "resource": {
    "type": "deployment",
    "id": "dep_123"
  },
  "payload": {}
}
```

> **Note:** `eventName` is NOT an envelope field. It is the derived catalog name `<type>.v<version>`. Example payloads in this catalog omit it. Implemented services (Auth, Tenant) are the source of truth; their payloads carry domain facts only and never duplicate envelope metadata (`eventId`, `occurredAt`, `correlationId`, `traceparent`, `actor`, `orgId`) or any secret value.

### 1.4 Envelope Required Fields

- `eventId`
- `type`
- `version`
- `orgId`
- `occurredAt`
- `payload`

### 1.5 Envelope Optional Fields

- `correlationId`
- `traceparent`
- `actor`
- `resource`

`eventName` is derived (`<type>.v<version>`) and is never stored in the envelope.

---

## 2. Service Event Matrix

### Auth Service

**Events Produced** (canonical names; all owned solely by Auth)

- `auth.user.created.v1`
- `auth.user.email.verification.requested.v1`
- `auth.user.email.verified.v1`
- `auth.user.password.reset.v1`
- `auth.login.succeeded.v1`
- `auth.login.failed.v1`
- `auth.token.revoked.v1`
- `auth.token.rotated.v1`
- `auth.mfa.setup.started.v1`
- `auth.mfa.enabled.v1`
- `auth.mfa.disabled.v1`
- `auth.service.account.created.v1`
- `auth.service.account.deleted.v1`
- `auth.api.token.created.v1`
- `auth.api.token.revoked.v1`

**Events Consumed**

- `tenant.organization.created.v1`
- `tenant.member.invited.v1`
- `tenant.member.removed.v1`
- `tenant.role.changed.v1`

### Tenant Service

**Events Produced**

- `tenant.organization.created.v1`
- `tenant.organization.updated.v1`
- `tenant.organization.deleted.v1`
- `tenant.member.invited.v1`
- `tenant.member.removed.v1`
- `tenant.role.changed.v1`
- `tenant.invitation.accepted.v1`
- `tenant.invitation.revoked.v1`

**Events Consumed**

- `auth.user.created.v1`

### Project Service

**Events Produced** (canonical names; all owned solely by Project Service)

- `project.created.v1`
- `project.updated.v1`
- `project.deleted.v1`
- `project.member.added.v1`
- `project.member.removed.v1`
- `project.role.changed.v1`

**Events Consumed**

- `tenant.organization.created.v1`
- `tenant.organization.updated.v1`
- `tenant.organization.deleted.v1`
- `tenant.member.removed.v1`
- `tenant.role.changed.v1`

### Cluster Service

> **Note:** The Cluster Service is fully implemented and adheres to all event contract rules. Payloads carry domain facts only, never duplicate envelope metadata, and never include sensitive tokens (registration tokens are delivered out-of-band via a Notifier).

**Events Produced** (canonical names; all owned solely by Cluster Service)

- `cluster.created.v1`
- `cluster.registration.token.created.v1`
- `cluster.registered.v1`
- `cluster.heartbeat.received.v1`
- `cluster.disconnected.v1`
- `cluster.deleted.v1`

**Events Consumed**

- `tenant.organization.created.v1`
- `tenant.organization.deleted.v1`
- `project.deleted.v1`

### Cluster Agent

**Events Produced**

- None. Agents are not domain-event producers. The Cluster Agent reports
  internal observations (registration, heartbeat, rollout/pod status, apply
  result) to the Cluster Service and Deployment Service over the agent API/stream.
  The owning service validates those observations and emits the authoritative
  `cluster.*` / `deployment.*` domain events.

**Events Consumed**

- `cluster.registered.v1` (to confirm successful registration)
- `cluster.deleted.v1` (to gracefully shut down)
- `deployment.created.v1`
- `deployment.rollback.requested.v1`
- `secret.created.v1`
- `secret.updated.v1`
- `secret.deleted.v1`
- `domain.certificate.issued.v1`

### Provisioning Service

**Events Produced**

- `provisioning.requested.v1`
- `provisioning.started.v1`
- `provisioning.succeeded.v1`
- `provisioning.failed.v1`

(Provisioning owns the `provisioning.*` namespace. It does NOT emit `cluster.*`;
the Cluster Service consumes provisioning success/failure and emits cluster
domain events.)

**Events Consumed**

- `tenant.organization.created.v1`
- `cluster.created.v1`
- `cluster.registered.v1`
- `cluster.deleted.v1`

### Deployment Service

> **Status:** Fully implemented as of June 2026. Contract tests verify all events.

**Events Produced**

- `application.created.v1`
- `application.updated.v1`
- `application.deleted.v1`
- `deployment.created.v1`
- `deployment.started.v1`
- `deployment.succeeded.v1`
- `deployment.failed.v1`
- `deployment.rollback.requested.v1`

**Events Consumed**

- `project.created.v1`
- `project.deleted.v1`
- `cluster.registered.v1`
- `cluster.disconnected.v1`
- `cluster.deleted.v1`

### Build Service

**Events Produced**

- `build.started.v1`
- `build.completed.v1`
- `build.failed.v1`

**Events Consumed**

- `deployment.created.v1`
- `deployment.rollback.requested.v1`
- `secret.updated.v1`

### Secrets Service

**Events Produced**

- `secret.created.v1`
- `secret.updated.v1`
- `secret.deleted.v1`

**Events Consumed**

- `project.deleted.v1`
- `tenant.member.removed.v1`
- `tenant.role.changed.v1`

### Domain/TLS Service

**Events Produced**

- `domain.created.v1`
- `domain.verified.v1`
- `domain.certificate.issued.v1`

**Events Consumed**

- `project.deleted.v1`
- `cluster.ready.v1`
- `deployment.succeeded.v1`

### Observability Service

**Events Produced**

- None in MVP. It stores telemetry and derives read models from platform events.

**Events Consumed**

- `cluster.ready.v1`
- `cluster.agent.disconnected.v1`
- `deployment.started.v1`
- `deployment.succeeded.v1`
- `deployment.failed.v1`
- `build.started.v1`
- `build.completed.v1`
- `build.failed.v1`
- `domain.certificate.issued.v1`

### Audit Service

**Events Produced**

- `audit.record.created.v1`

**Events Consumed**

- All platform domain events.

### Notification Service

**Events Produced**

- `notification.sent.v1`
- `notification.failed.v1`

**Events Consumed**

- `auth.user.created.v1`
- `auth.user.email.verification.requested.v1`
- `auth.user.password.reset.v1`
- `tenant.member.invited.v1`
- `deployment.succeeded.v1`
- `deployment.failed.v1`
- `build.failed.v1`
- `cluster.agent.disconnected.v1`
- `domain.certificate.issued.v1`

Notification consumers receive a `deliveryRef` on identity/invitation events and exchange it for the sealed one-time token via the producer's restricted internal delivery API. Tokens never appear on the event stream.

---

## 3. Event Catalog

Each event entry lists the payload schema. The common envelope from section 1.3 wraps every payload.

> **Implemented vs. planned:** Sections 3.1 (Auth) and 3.2 (Tenant) describe **implemented** services and are the contract source of truth — their examples omit `eventName`, never duplicate envelope metadata (`orgId`, timestamps) in the payload, and never carry secrets. Sections 3.3+ describe **planned** services; their example payloads are illustrative and may still show `eventName`/`orgId` for readability. Those services MUST be implemented contract-first to the same rules (derived `eventName`, no metadata duplication, no secrets, single producer ownership) and have producer contract tests before release.

---

## 3.1 Auth Events

Auth is the sole owner of `auth.*`. Identity events that are not tied to a tenant use the logical org `platform`. All Auth events are published through the transactional outbox. No Auth event carries a verification token, reset token, or any secret; verification/reset deliveries reference a `deliveryRef` only.

### auth.user.created.v1

- **Event Name:** `auth.user.created.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Tenant Service, Notification Service, Audit Service
- **Description:** Published when a user identity is created.
- **Payload Schema:** `userId:string`, `email:string`, `name:string`
- **Required Fields:** `userId`, `email`, `name`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_auth_user_created_001",
  "type": "auth.user.created",
  "version": 1,
  "orgId": "platform",
  "occurredAt": "2026-06-24T05:00:00Z",
  "actor": { "type": "user", "id": "user_123" },
  "resource": { "type": "user", "id": "user_123" },
  "payload": {
    "userId": "user_123",
    "email": "davit@example.com",
    "name": "Davit"
  }
}
```

### auth.user.email.verification.requested.v1

- **Event Name:** `auth.user.email.verification.requested.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Notification Service, Audit Service
- **Description:** Published when an email-verification token is issued (signup or resend). Carries a delivery reference, never the token.
- **Payload Schema:** `userId:string`, `email:string`, `purpose:string`, `deliveryRef:string`
- **Required Fields:** `userId`, `email`, `purpose`, `deliveryRef`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_auth_email_verify_req_001",
  "type": "auth.user.email.verification.requested",
  "version": 1,
  "orgId": "platform",
  "occurredAt": "2026-06-24T05:01:00Z",
  "resource": { "type": "user", "id": "user_123" },
  "payload": {
    "userId": "user_123",
    "email": "davit@example.com",
    "purpose": "email_verification",
    "deliveryRef": "ott_123"
  }
}
```

### auth.user.email.verified.v1

- **Event Name:** `auth.user.email.verified.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Tenant Service, Notification Service, Audit Service
- **Description:** Published when a user's email address is verified.
- **Payload Schema:** `userId:string`, `email:string`
- **Required Fields:** `userId`, `email`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_auth_email_verified_001",
  "type": "auth.user.email.verified",
  "version": 1,
  "orgId": "platform",
  "occurredAt": "2026-06-24T05:05:00Z",
  "actor": { "type": "user", "id": "user_123" },
  "resource": { "type": "user", "id": "user_123" },
  "payload": {
    "userId": "user_123",
    "email": "davit@example.com"
  }
}
```

### auth.user.password.reset.v1

- **Event Name:** `auth.user.password.reset.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Notification Service, Audit Service
- **Description:** Covers the password-reset lifecycle. `phase` is `requested` (a token was issued; payload references a delivery) or `completed` (the password was changed). Never carries the reset token.
- **Payload Schema:** `userId:string`, `phase:string`, `email:string`, `purpose:string`, `deliveryRef:string`
- **Required Fields:** `userId`, `phase`
- **Optional Fields:** `email`, `purpose`, `deliveryRef` (present only on `requested`)
- **Example JSON Payload (requested):**

```json
{
  "eventId": "evt_auth_password_reset_001",
  "type": "auth.user.password.reset",
  "version": 1,
  "orgId": "platform",
  "occurredAt": "2026-06-24T05:10:00Z",
  "resource": { "type": "user", "id": "user_123" },
  "payload": {
    "userId": "user_123",
    "phase": "requested",
    "email": "davit@example.com",
    "purpose": "password_reset",
    "deliveryRef": "ott_456"
  }
}
```

### auth.login.succeeded.v1

- **Event Name:** `auth.login.succeeded.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Audit Service, Observability Service
- **Description:** Published when a login succeeds.
- **Payload Schema:** `userId:string`, `email:string`
- **Required Fields:** `userId`, `email`

### auth.login.failed.v1

- **Event Name:** `auth.login.failed.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Audit Service, Observability Service, Notification Service
- **Description:** Published when a login attempt fails. Carries the running failure count for lockout observability.
- **Payload Schema:** `userId:string`, `email:string`, `attempts:number`
- **Required Fields:** `userId`, `email`, `attempts`

### auth.token.revoked.v1

- **Event Name:** `auth.token.revoked.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Gateway/session denylist, Audit Service
- **Description:** Published when a refresh session is revoked (logout). Feeds the gateway denylist.
- **Payload Schema:** `userId:string`, `sessionId:string`
- **Required Fields:** `userId`
- **Optional Fields:** `sessionId`

### auth.token.rotated.v1

- **Event Name:** `auth.token.rotated.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Gateway/session denylist, Audit Service
- **Description:** Published when a refresh session is rotated (refresh flow). The replaced session is revoked and a new session issued in the same transaction. Only opaque session ids are carried; token values are never published.
- **Payload Schema:** `userId:string`, `sessionId:string` (new session), `replacedSessionId:string` (rotated-out session)
- **Required Fields:** `userId`
- **Optional Fields:** `sessionId`, `replacedSessionId`

### auth.mfa.setup.started.v1

- **Event Name:** `auth.mfa.setup.started.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Audit Service
- **Description:** Published when a user begins MFA enrolment (a pending TOTP secret is generated). The secret is never included.
- **Payload Schema:** `userId:string`
- **Required Fields:** `userId`

### auth.mfa.enabled.v1

- **Event Name:** `auth.mfa.enabled.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Notification Service, Audit Service
- **Description:** Published when a user confirms a TOTP code and MFA becomes active.
- **Payload Schema:** `userId:string`
- **Required Fields:** `userId`

### auth.mfa.disabled.v1

- **Event Name:** `auth.mfa.disabled.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Notification Service, Audit Service
- **Description:** Published when a user disables MFA; the stored secret is cleared in the same transaction.
- **Payload Schema:** `userId:string`
- **Required Fields:** `userId`

### auth.service.account.created.v1

- **Event Name:** `auth.service.account.created.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Audit Service
- **Description:** Published when an org-scoped machine identity is created. `orgId` is the owning org (envelope only).
- **Payload Schema:** `serviceAccountId:string`, `name:string`, `createdBy:string`
- **Required Fields:** `serviceAccountId`, `name`
- **Optional Fields:** `createdBy`

### auth.service.account.deleted.v1

- **Event Name:** `auth.service.account.deleted.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Gateway/token cache, Audit Service
- **Description:** Published when a service account is deleted (its API tokens cascade). `orgId` is the owning org (envelope only).
- **Payload Schema:** `serviceAccountId:string`
- **Required Fields:** `serviceAccountId`

### auth.api.token.created.v1

- **Event Name:** `auth.api.token.created.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Audit Service
- **Description:** Published when an API token is issued for a service account. The secret is never included.
- **Payload Schema:** `apiTokenId:string`, `serviceAccountId:string`, `name:string`
- **Required Fields:** `apiTokenId`, `serviceAccountId`, `name`

### auth.api.token.revoked.v1

- **Event Name:** `auth.api.token.revoked.v1`
- **Producer Service:** Auth Service
- **Consumer Services:** Gateway/token cache, Audit Service
- **Description:** Published when an API token is revoked.
- **Payload Schema:** `apiTokenId:string`
- **Required Fields:** `apiTokenId`

---

## 3.2 Tenant Events

Tenant is the sole owner of `tenant.*`. All events are published through the transactional outbox inside the same RLS-scoped transaction as the state change. `orgId` is carried in the envelope only; payloads never duplicate it, and the invitation token is never published.

### tenant.organization.created.v1

- **Event Name:** `tenant.organization.created.v1`
- **Producer Service:** Tenant Service
- **Consumer Services:** Auth Service, Project Service, Provisioning Service, Audit Service
- **Description:** Published when an organization is created and the creator is assigned owner.
- **Payload Schema:** `name:string`, `slug:string`, `ownerId:string`
- **Required Fields:** `name`, `slug`, `ownerId`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_tenant_org_created_001",
  "type": "tenant.organization.created",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T05:30:00Z",
  "actor": { "type": "user", "id": "user_123" },
  "resource": { "type": "organization", "id": "org_123" },
  "payload": {
    "name": "Acme",
    "slug": "acme",
    "ownerId": "user_123"
  }
}
```

### tenant.organization.updated.v1

- **Event Name:** `tenant.organization.updated.v1`
- **Producer Service:** Tenant Service
- **Consumer Services:** Project Service, Cluster Service, Audit Service
- **Description:** Published when organization metadata changes.
- **Payload Schema:** `name:string`, `plan:string`
- **Required Fields:** `name`, `plan`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_tenant_org_updated_001",
  "type": "tenant.organization.updated",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T05:35:00Z",
  "actor": { "type": "user", "id": "user_123" },
  "resource": { "type": "organization", "id": "org_123" },
  "payload": {
    "name": "Acme Inc",
    "plan": "team"
  }
}
```

### tenant.organization.deleted.v1

- **Event Name:** `tenant.organization.deleted.v1`
- **Producer Service:** Tenant Service
- **Consumer Services:** Project Service, Provisioning Service, Secrets Service, Audit Service
- **Description:** Published when an organization is deleted. Downstream services use it to tear down or archive org-scoped resources.
- **Payload Schema:** `deletedBy:string`
- **Required Fields:** `deletedBy`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_tenant_org_deleted_001",
  "type": "tenant.organization.deleted",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T05:36:00Z",
  "actor": { "type": "user", "id": "user_123" },
  "resource": { "type": "organization", "id": "org_123" },
  "payload": {
    "deletedBy": "user_123"
  }
}
```

### tenant.member.invited.v1

- **Event Name:** `tenant.member.invited.v1`
- **Producer Service:** Tenant Service
- **Consumer Services:** Auth Service, Notification Service, Audit Service
- **Description:** Published when a user is invited to an organization. Carries a `deliveryRef` (the invitation id), never the invitation token. The Notification Service exchanges `deliveryRef` for the sealed token via the restricted internal Tenant delivery API.
- **Payload Schema:** `invitationId:string`, `email:string`, `role:string`, `invitedBy:string`, `expiresAt:datetime`, `deliveryRef:string`
- **Required Fields:** `invitationId`, `email`, `role`, `invitedBy`, `expiresAt`, `deliveryRef`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_tenant_member_invited_001",
  "type": "tenant.member.invited",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T05:40:00Z",
  "actor": { "type": "user", "id": "user_123" },
  "resource": { "type": "invitation", "id": "inv_123" },
  "payload": {
    "invitationId": "inv_123",
    "email": "new-user@example.com",
    "role": "developer",
    "invitedBy": "user_123",
    "expiresAt": "2026-07-01T05:40:00Z",
    "deliveryRef": "inv_123"
  }
}
```

### tenant.member.removed.v1

- **Event Name:** `tenant.member.removed.v1`
- **Producer Service:** Tenant Service
- **Consumer Services:** Auth Service, Project Service, Secrets Service, Audit Service, Notification Service
- **Description:** Published when a member is removed from an organization.
- **Payload Schema:** `userId:string`, `removedBy:string`
- **Required Fields:** `userId`, `removedBy`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_tenant_member_removed_001",
  "type": "tenant.member.removed",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T05:45:00Z",
  "actor": { "type": "user", "id": "user_owner" },
  "resource": { "type": "member", "id": "user_456" },
  "payload": {
    "userId": "user_456",
    "removedBy": "user_owner"
  }
}
```

### tenant.role.changed.v1

- **Event Name:** `tenant.role.changed.v1`
- **Producer Service:** Tenant Service
- **Consumer Services:** Auth Service, Project Service, Secrets Service, Audit Service, Notification Service
- **Description:** Published when a member's organization role changes.
- **Payload Schema:** `userId:string`, `oldRole:string`, `newRole:string`, `changedBy:string`
- **Required Fields:** `userId`, `oldRole`, `newRole`, `changedBy`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_tenant_role_changed_001",
  "type": "tenant.role.changed",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T05:50:00Z",
  "actor": { "type": "user", "id": "user_owner" },
  "resource": { "type": "member", "id": "user_456" },
  "payload": {
    "userId": "user_456",
    "oldRole": "viewer",
    "newRole": "developer",
    "changedBy": "user_owner"
  }
}
```

### tenant.invitation.accepted.v1

- **Event Name:** `tenant.invitation.accepted.v1`
- **Producer Service:** Tenant Service
- **Consumer Services:** Auth Service, Notification Service, Audit Service
- **Description:** Published when an invited user accepts an invitation. The membership is created and the invitation marked accepted in the same transaction.
- **Payload Schema:** `invitationId:string`, `userId:string`, `role:string`
- **Required Fields:** `invitationId`, `userId`, `role`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_tenant_invitation_accepted_001",
  "type": "tenant.invitation.accepted",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T05:55:00Z",
  "actor": { "type": "user", "id": "user_456" },
  "resource": { "type": "invitation", "id": "inv_123" },
  "payload": {
    "invitationId": "inv_123",
    "userId": "user_456",
    "role": "developer"
  }
}
```

### tenant.invitation.revoked.v1

- **Event Name:** `tenant.invitation.revoked.v1`
- **Producer Service:** Tenant Service
- **Consumer Services:** Notification Service, Audit Service
- **Description:** Published when a pending invitation is revoked before being accepted. Revocation is idempotent; the event fires only on the transition from pending to revoked.
- **Payload Schema:** `invitationId:string`, `revokedBy:string`
- **Required Fields:** `invitationId`, `revokedBy`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "evt_tenant_invitation_revoked_001",
  "type": "tenant.invitation.revoked",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T05:56:00Z",
  "actor": { "type": "user", "id": "user_owner" },
  "resource": { "type": "invitation", "id": "inv_123" },
  "payload": {
    "invitationId": "inv_123",
    "revokedBy": "user_owner"
  }
}
```

---

## 3.3 Project Events

> **Implementation Status:** Project Service is now fully implemented. All events below follow the event contract remediation rules (no duplicate metadata, no secrets in payloads).

### project.created.v1

- **Event Name:** `project.created.v1`
- **Version:** `v1`
- **Producer Service:** Project Service (sole owner)
- **Consumer Services:** Deployment Service, Cluster Service, Secrets Service, Domain/TLS Service, Audit Service
- **Description:** Published when a project is created within an organization. The creator automatically becomes the project admin.
- **Payload Schema:**
  - `projectId`: string (required) — the new project's ID
  - `name`: string (required) — display name
  - `slug`: string (required) — URL-safe identifier
  - `description`: string (optional) — project description
  - `createdBy`: string (optional) — user ID of the creator
- **Example JSON Payload:**

```json
{
  "projectId": "proj_123",
  "name": "Payments",
  "slug": "payments",
  "description": "Payments workloads",
  "createdBy": "user_123"
}
```

### project.updated.v1

- **Event Name:** `project.updated.v1`
- **Version:** `v1`
- **Producer Service:** Project Service (sole owner)
- **Consumer Services:** Deployment Service, Cluster Service, Audit Service
- **Description:** Published when project metadata changes.
- **Payload Schema:**
  - `projectId`: string (required) — the project's ID
  - `name`: string (required) — updated name
  - `description`: string (optional) — updated description
- **Example JSON Payload:**

```json
{
  "projectId": "proj_123",
  "name": "Payments Core",
  "description": "Core payment services"
}
```

### project.deleted.v1

- **Event Name:** `project.deleted.v1`
- **Version:** `v1`
- **Producer Service:** Project Service (sole owner)
- **Consumer Services:** Deployment Service, Cluster Service, Build Service, Secrets Service, Domain/TLS Service, Observability Service, Audit Service
- **Description:** Published when a project is deleted. Cascades to all related resources.
- **Payload Schema:**
  - `projectId`: string (required) — the deleted project's ID
  - `deletedBy`: string (required) — user ID who performed the deletion
- **Example JSON Payload:**

```json
{
  "projectId": "proj_123",
  "deletedBy": "user_123"
}
```

### project.member.added.v1

- **Event Name:** `project.member.added.v1`
- **Version:** `v1`
- **Producer Service:** Project Service (sole owner)
- **Consumer Services:** Notification Service, Audit Service
- **Description:** Published when a user is added to a project with a specific role.
- **Payload Schema:**
  - `projectId`: string (required) — the project's ID
  - `userId`: string (required) — the added user's ID
  - `role`: string (required) — one of `admin`, `developer`, `viewer`
  - `addedBy`: string (required) — user ID who added the member
- **Example JSON Payload:**

```json
{
  "projectId": "proj_123",
  "userId": "user_456",
  "role": "developer",
  "addedBy": "user_123"
}
```

### project.member.removed.v1

- **Event Name:** `project.member.removed.v1`
- **Version:** `v1`
- **Producer Service:** Project Service (sole owner)
- **Consumer Services:** Notification Service, Audit Service
- **Description:** Published when a user is removed from a project.
- **Payload Schema:**
  - `projectId`: string (required) — the project's ID
  - `userId`: string (required) — the removed user's ID
  - `removedBy`: string (required) — user ID who performed the removal
- **Example JSON Payload:**

```json
{
  "projectId": "proj_123",
  "userId": "user_456",
  "removedBy": "user_123"
}
```

### project.role.changed.v1

- **Event Name:** `project.role.changed.v1`
- **Version:** `v1`
- **Producer Service:** Project Service (sole owner)
- **Consumer Services:** Notification Service, Audit Service
- **Description:** Published when a member's role within a project is changed.
- **Payload Schema:**
  - `projectId`: string (required) — the project's ID
  - `userId`: string (required) — the affected user's ID
  - `oldRole`: string (required) — previous role
  - `newRole`: string (required) — new role
  - `changedBy`: string (required) — user ID who made the change
- **Example JSON Payload:**

```json
{
  "projectId": "proj_123",
  "userId": "user_456",
  "oldRole": "viewer",
  "newRole": "developer",
  "changedBy": "user_123"
}
```

---

## 3.4 Cluster Events

> **Note:** The Cluster Service is fully implemented and adheres to all event contract rules. Payloads carry domain facts only, never duplicate envelope metadata (`eventId`, `occurredAt`, `correlationId`, `traceparent`, `actor`, `orgId`), and never include sensitive values. Registration tokens are delivered out-of-band via a Notifier; only `deliveryRef` appears in event payloads.

### cluster.created.v1

- **Event Name:** `cluster.created.v1`
- **Version:** `v1`
- **Producer Service:** Cluster Service (sole owner)
- **Consumer Services:** Audit Service, Notification Service
- **Description:** Published when a cluster record is created (Phase 1: existing cluster registration only).
- **Payload Schema:**
  - `clusterId:string` — UUID of the new cluster
  - `name:string` — Human-readable cluster name
  - `slug:string` — URL-friendly identifier
  - `cloudProvider:string` — aws, gcp, azure, on-prem, other (optional)
  - `region:string` — Cloud region (optional)
  - `createdBy:string` — User ID who created the cluster
- **Required Fields:** `clusterId`, `name`, `slug`
- **Optional Fields:** `cloudProvider`, `region`, `createdBy`
- **Example JSON Payload:**

```json
{
  "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "name": "Production Cluster",
  "slug": "production",
  "cloudProvider": "aws",
  "region": "us-west-2",
  "createdBy": "01JZ3K4M5N6P7Q8R9S0T1U2V3X"
}
```

### cluster.registration.token.created.v1

- **Event Name:** `cluster.registration.token.created.v1`
- **Version:** `v1`
- **Producer Service:** Cluster Service (sole owner)
- **Consumer Services:** Audit Service
- **Description:** Published when a registration token is generated for agent registration. The plaintext token is delivered out-of-band via Notifier.
- **Payload Schema:**
  - `clusterId:string` — Cluster the token is for
  - `tokenId:string` — UUID of the registration token
  - `expiresAt:datetime` — When the token expires
  - `deliveryRef:string` — Reference for out-of-band token delivery
- **Required Fields:** `clusterId`, `tokenId`, `expiresAt`, `deliveryRef`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "tokenId": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y",
  "expiresAt": "2026-06-25T12:00:00Z",
  "deliveryRef": "01JZ3K4M5N6P7Q8R9S0T1U2V3Z"
}
```

### cluster.registered.v1

- **Event Name:** `cluster.registered.v1`
- **Version:** `v1`
- **Producer Service:** Cluster Service (sole owner; agent reports registration as observation)
- **Consumer Services:** Deployment Service, Audit Service, Notification Service
- **Description:** Published when a cluster agent successfully registers with the control plane.
- **Payload Schema:**
  - `clusterId:string` — UUID of the cluster
  - `agentId:string` — Unique identifier of the registering agent
  - `kubernetesVersion:string` — Reported K8s version
  - `nodeCount:int` — Number of nodes in the cluster
  - `cloudProvider:string` — Cloud provider (optional, may override initial)
  - `region:string` — Region (optional, may override initial)
- **Required Fields:** `clusterId`, `agentId`, `kubernetesVersion`, `nodeCount`
- **Optional Fields:** `cloudProvider`, `region`
- **Example JSON Payload:**

```json
{
  "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "agentId": "agent-prod-001",
  "kubernetesVersion": "1.28.5",
  "nodeCount": 5,
  "cloudProvider": "aws",
  "region": "us-west-2"
}
```

### cluster.heartbeat.received.v1

- **Event Name:** `cluster.heartbeat.received.v1`
- **Version:** `v1`
- **Producer Service:** Cluster Service (sole owner)
- **Consumer Services:** Audit Service (low-volume sampling), Observability Service
- **Description:** Published when the cluster agent sends a heartbeat with health/inventory updates.
- **Payload Schema:**
  - `clusterId:string` — UUID of the cluster
  - `agentId:string` — Agent identifier
  - `kubernetesVersion:string` — Current K8s version (optional)
  - `nodeCount:int` — Current node count (optional)
  - `apiServerHealthy:bool` — Whether the K8s API server is reachable
- **Required Fields:** `clusterId`, `agentId`, `apiServerHealthy`
- **Optional Fields:** `kubernetesVersion`, `nodeCount`
- **Example JSON Payload:**

```json
{
  "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "agentId": "agent-prod-001",
  "kubernetesVersion": "1.28.6",
  "nodeCount": 6,
  "apiServerHealthy": true
}
```

### cluster.disconnected.v1

- **Event Name:** `cluster.disconnected.v1`
- **Version:** `v1`
- **Producer Service:** Cluster Service (sole owner; emitted when heartbeat timeout is exceeded)
- **Consumer Services:** Notification Service, Deployment Service, Audit Service
- **Description:** Published when a cluster agent misses heartbeats and is marked disconnected.
- **Payload Schema:**
  - `clusterId:string` — UUID of the cluster
  - `lastHeartbeatAt:datetime` — Timestamp of the last successful heartbeat
  - `disconnectedAfter:string` — Timeout duration (e.g., "5m")
- **Required Fields:** `clusterId`, `lastHeartbeatAt`, `disconnectedAfter`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "lastHeartbeatAt": "2026-06-24T12:00:00Z",
  "disconnectedAfter": "5m"
}
```

### cluster.deleted.v1

- **Event Name:** `cluster.deleted.v1`
- **Version:** `v1`
- **Producer Service:** Cluster Service (sole owner)
- **Consumer Services:** Deployment Service, Notification Service, Audit Service
- **Description:** Published when a cluster is soft-deleted.
- **Payload Schema:**
  - `clusterId:string` — UUID of the deleted cluster
  - `deletedBy:string` — User ID who deleted the cluster
- **Required Fields:** `clusterId`, `deletedBy`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "deletedBy": "01JZ3K4M5N6P7Q8R9S0T1U2V3X"
}
```

---

## 3.5 Application Events

> **Note:** Application events are produced by the Deployment Service. Applications are containers for deployments within a project.

### application.created.v1

- **Event Name:** `application.created.v1`
- **Version:** `v1`
- **Producer Service:** Deployment Service
- **Consumer Services:** Audit Service
- **Description:** Published when an application is created.
- **Payload Schema:**

| Field          | Type   | Required | Description                        |
|----------------|--------|----------|------------------------------------|
| applicationId  | string | Yes      | UUID of the created application    |
| projectId      | string | Yes      | UUID of the parent project         |
| name           | string | Yes      | Human-readable application name    |
| slug           | string | Yes      | URL-safe identifier                |
| runtimeType    | string | Yes      | container, function, or job        |
| createdBy      | string | No       | UUID of the creator                |

- **Example JSON Payload:**

```json
{
  "applicationId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "projectId": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
  "name": "My App",
  "slug": "my-app",
  "runtimeType": "container",
  "createdBy": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y"
}
```

### application.updated.v1

- **Event Name:** `application.updated.v1`
- **Version:** `v1`
- **Producer Service:** Deployment Service
- **Consumer Services:** Audit Service
- **Description:** Published when an application is updated.
- **Payload Schema:**

| Field          | Type   | Required | Description                        |
|----------------|--------|----------|------------------------------------|
| applicationId  | string | Yes      | UUID of the updated application    |
| name           | string | Yes      | Updated application name           |
| updatedBy      | string | No       | UUID of the user who updated       |

- **Example JSON Payload:**

```json
{
  "applicationId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "name": "My Updated App",
  "updatedBy": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y"
}
```

### application.deleted.v1

- **Event Name:** `application.deleted.v1`
- **Version:** `v1`
- **Producer Service:** Deployment Service
- **Consumer Services:** Audit Service
- **Description:** Published when an application is deleted.
- **Payload Schema:**

| Field          | Type   | Required | Description                        |
|----------------|--------|----------|------------------------------------|
| applicationId  | string | Yes      | UUID of the deleted application    |
| deletedBy      | string | No       | UUID of the user who deleted       |

- **Example JSON Payload:**

```json
{
  "applicationId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "deletedBy": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y"
}
```

---

## 3.6 Deployment Events

> **Note:** Deployment events are fully implemented. Payloads carry domain facts only and do not duplicate envelope metadata.

### deployment.created.v1

- **Event Name:** `deployment.created.v1`
- **Version:** `v1`
- **Producer Service:** Deployment Service
- **Consumer Services:** Cluster Agent, Audit Service
- **Description:** Published when a deployment is created and a new release is scheduled.
- **Payload Schema:**

| Field          | Type   | Required | Description                        |
|----------------|--------|----------|------------------------------------|
| deploymentId   | string | Yes      | UUID of the deployment             |
| applicationId  | string | Yes      | UUID of the application            |
| clusterId      | string | Yes      | UUID of the target cluster         |
| image          | string | Yes      | Container image reference          |
| replicas       | int    | Yes      | Desired replica count              |
| revision       | int    | Yes      | Release revision number            |
| createdBy      | string | No       | UUID of the creator                |

- **Example JSON Payload:**

```json
{
  "deploymentId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "applicationId": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
  "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y",
  "image": "nginx:1.25",
  "replicas": 3,
  "revision": 1,
  "createdBy": "01JZ3K4M5N6P7Q8R9S0T1U2V3Z"
}
```

### deployment.started.v1

- **Event Name:** `deployment.started.v1`
- **Version:** `v1`
- **Producer Service:** Deployment Service
- **Consumer Services:** Observability Service, Notification Service, Audit Service
- **Description:** Published when the agent begins rolling out a release.
- **Payload Schema:**

| Field        | Type   | Required | Description                        |
|--------------|--------|----------|------------------------------------|
| deploymentId | string | Yes      | UUID of the deployment             |
| releaseId    | string | Yes      | UUID of the release being deployed |
| revision     | int    | Yes      | Release revision number            |
| image        | string | Yes      | Container image being deployed     |

- **Example JSON Payload:**

```json
{
  "deploymentId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "releaseId": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
  "revision": 2,
  "image": "nginx:1.26"
}
```

### deployment.succeeded.v1

- **Event Name:** `deployment.succeeded.v1`
- **Version:** `v1`
- **Producer Service:** Deployment Service
- **Consumer Services:** Domain/TLS Service, Observability Service, Notification Service, Audit Service
- **Description:** Published when all replicas are ready and the deployment is healthy.
- **Payload Schema:**

| Field         | Type   | Required | Description                        |
|---------------|--------|----------|------------------------------------|
| deploymentId  | string | Yes      | UUID of the deployment             |
| releaseId     | string | Yes      | UUID of the successful release     |
| revision      | int    | Yes      | Release revision number            |
| readyReplicas | int    | Yes      | Number of ready replicas           |

- **Example JSON Payload:**

```json
{
  "deploymentId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "releaseId": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
  "revision": 2,
  "readyReplicas": 3
}
```

### deployment.failed.v1

- **Event Name:** `deployment.failed.v1`
- **Version:** `v1`
- **Producer Service:** Deployment Service
- **Consumer Services:** Observability Service, Notification Service, Audit Service
- **Description:** Published when a deployment fails to reach healthy state.
- **Payload Schema:**

| Field        | Type   | Required | Description                        |
|--------------|--------|----------|------------------------------------|
| deploymentId | string | Yes      | UUID of the deployment             |
| releaseId    | string | Yes      | UUID of the failed release         |
| revision     | int    | Yes      | Release revision number            |
| errorMessage | string | Yes      | Human-readable error description   |

- **Example JSON Payload:**

```json
{
  "deploymentId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "releaseId": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
  "revision": 2,
  "errorMessage": "ImagePullBackOff: unable to pull image nginx:invalid"
}
```

### deployment.rollback.requested.v1

- **Event Name:** `deployment.rollback.requested.v1`
- **Version:** `v1`
- **Producer Service:** Deployment Service
- **Consumer Services:** Cluster Agent, Audit Service
- **Description:** Published when a user requests rollback to a previous revision.
- **Payload Schema:**

| Field          | Type   | Required | Description                        |
|----------------|--------|----------|------------------------------------|
| deploymentId   | string | Yes      | UUID of the deployment             |
| fromRevision   | int    | Yes      | Current revision being replaced    |
| targetRevision | int    | Yes      | Revision to restore                |
| requestedBy    | string | No       | UUID of the user who requested     |

- **Example JSON Payload:**

```json
{
  "deploymentId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "fromRevision": 3,
  "targetRevision": 1,
  "requestedBy": "01JZ3K4M5N6P7Q8R9S0T1U2V3Z"
}
```

---

## 3.7 Build Events

### build.started.v1

- **Event Name:** `build.started.v1`
- **Version:** `v1`
- **Producer Service:** Build Service
- **Consumer Services:** Deployment Service, Observability Service, Audit Service
- **Description:** Published when a build begins execution.
- **Payload Schema:** `orgId:string`, `projectId:string`, `buildId:string`, `sourceRef:string`, `startedAt:datetime`
- **Required Fields:** `orgId`, `projectId`, `buildId`, `startedAt`
- **Optional Fields:** `sourceRef`, `builder`
- **Example JSON Payload:**

```json
{
  "eventId": "evt_build_started_001",
  "eventName": "build.started.v1",
  "type": "build.started",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T07:20:00Z",
  "resource": { "type": "build", "id": "build_123" },
  "payload": {
    "orgId": "org_123",
    "projectId": "proj_123",
    "buildId": "build_123",
    "sourceRef": "main@abc123",
    "builder": "buildkit",
    "startedAt": "2026-06-24T07:20:00Z"
  }
}
```

### build.completed.v1

- **Event Name:** `build.completed.v1`
- **Version:** `v1`
- **Producer Service:** Build Service
- **Consumer Services:** Deployment Service, Observability Service, Audit Service
- **Description:** Published when a build completes successfully and produces an image.
- **Payload Schema:** `orgId:string`, `projectId:string`, `buildId:string`, `image:string`, `digest:string`, `completedAt:datetime`
- **Required Fields:** `orgId`, `projectId`, `buildId`, `image`, `completedAt`
- **Optional Fields:** `digest`, `sbomRef`, `durationSeconds`
- **Example JSON Payload:**

```json
{
  "eventId": "evt_build_completed_001",
  "eventName": "build.completed.v1",
  "type": "build.completed",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T07:30:00Z",
  "resource": { "type": "build", "id": "build_123" },
  "payload": {
    "orgId": "org_123",
    "projectId": "proj_123",
    "buildId": "build_123",
    "image": "registry.example.com/payments:abc123",
    "digest": "sha256:abc123",
    "sbomRef": "s3://sboms/build_123.json",
    "durationSeconds": 600,
    "completedAt": "2026-06-24T07:30:00Z"
  }
}
```

### build.failed.v1

- **Event Name:** `build.failed.v1`
- **Version:** `v1`
- **Producer Service:** Build Service
- **Consumer Services:** Deployment Service, Observability Service, Notification Service, Audit Service
- **Description:** Published when a build fails.
- **Payload Schema:** `orgId:string`, `projectId:string`, `buildId:string`, `failureCode:string`, `message:string`, `failedAt:datetime`
- **Required Fields:** `orgId`, `projectId`, `buildId`, `failureCode`, `failedAt`
- **Optional Fields:** `message`, `logRef`
- **Example JSON Payload:**

```json
{
  "eventId": "evt_build_failed_001",
  "eventName": "build.failed.v1",
  "type": "build.failed",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T07:25:00Z",
  "resource": { "type": "build", "id": "build_123" },
  "payload": {
    "orgId": "org_123",
    "projectId": "proj_123",
    "buildId": "build_123",
    "failureCode": "dockerfile_not_found",
    "message": "Dockerfile was not found in repository root",
    "logRef": "s3://build-logs/build_123.log",
    "failedAt": "2026-06-24T07:25:00Z"
  }
}
```

---

## 3.7 Secrets Events

> **Status:** Fully implemented as of June 2026. The Secrets Service adheres to all event contract rules. Payloads carry domain facts only, never duplicate envelope metadata (`orgId`, `occurredAt`, timestamps), and NEVER include plaintext secret values.

### secret.created.v1

- **Event Name:** `secret.created.v1`
- **Version:** `v1`
- **Producer Service:** Secrets Service
- **Consumer Services:** Cluster Agent, Audit Service
- **Description:** Published when a secret is created. **Secret plaintext values are NEVER included.**
- **Payload Schema:**
  - `secretId: string` (required) - UUID of the created secret
  - `projectId: string` (required) - UUID of the project
  - `name: string` (required) - Secret name (e.g., DATABASE_URL)
  - `version: int64` (required) - Initial version (always 1)
- **Required Fields:** `secretId`, `projectId`, `name`, `version`
- **Optional Fields:** None
- **Security:** Plaintext value is NEVER included in this event.
- **Example JSON Payload:**

```json
{
  "eventId": "01JZ2N2B8TZY7PXE1Z6SPY8V7Y",
  "type": "secret.created",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T07:40:00Z",
  "actor": { "type": "user", "id": "user_123" },
  "resource": { "type": "secret", "id": "secret_123" },
  "payload": {
    "secretId": "secret_123",
    "projectId": "proj_123",
    "name": "DATABASE_URL",
    "version": 1
  }
}
```

### secret.updated.v1

- **Event Name:** `secret.updated.v1`
- **Version:** `v1`
- **Producer Service:** Secrets Service
- **Consumer Services:** Cluster Agent, Audit Service
- **Description:** Published when a secret's value or metadata is updated. **Secret plaintext values are NEVER included.**
- **Payload Schema:**
  - `secretId: string` (required) - UUID of the updated secret
  - `projectId: string` (required) - UUID of the project
  - `name: string` (required) - Secret name
  - `version: int64` (required) - New version after update
- **Required Fields:** `secretId`, `projectId`, `name`, `version`
- **Optional Fields:** None
- **Security:** Neither old nor new plaintext values are EVER included in this event.
- **Example JSON Payload:**

```json
{
  "eventId": "01JZ2N2B8TZY7PXE1Z6SPY8V7Y",
  "type": "secret.updated",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T07:45:00Z",
  "actor": { "type": "user", "id": "user_123" },
  "resource": { "type": "secret", "id": "secret_123" },
  "payload": {
    "secretId": "secret_123",
    "projectId": "proj_123",
    "name": "DATABASE_URL",
    "version": 3
  }
}
```

### secret.deleted.v1

- **Event Name:** `secret.deleted.v1`
- **Version:** `v1`
- **Producer Service:** Secrets Service
- **Consumer Services:** Cluster Agent, Audit Service
- **Description:** Published when a secret is soft-deleted.
- **Payload Schema:**
  - `secretId: string` (required) - UUID of the deleted secret
  - `projectId: string` (required) - UUID of the project
  - `name: string` (required) - Secret name
  - `deletedBy: string` (required) - UUID of the user who deleted
- **Required Fields:** `secretId`, `projectId`, `name`, `deletedBy`
- **Optional Fields:** None
- **Example JSON Payload:**

```json
{
  "eventId": "01JZ2N2B8TZY7PXE1Z6SPY8V7Y",
  "type": "secret.deleted",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T07:50:00Z",
  "actor": { "type": "user", "id": "user_123" },
  "resource": { "type": "secret", "id": "secret_123" },
  "payload": {
    "secretId": "secret_123",
    "projectId": "proj_123",
    "name": "DATABASE_URL",
    "deletedBy": "user_123"
  }
}
```

---

## 3.8 Domain/TLS Events

### domain.created.v1

- **Event Name:** `domain.created.v1`
- **Version:** `v1`
- **Producer Service:** Domain/TLS Service
- **Consumer Services:** Cluster Agent, Audit Service
- **Description:** Published when a custom domain record is created.
- **Payload Schema:** `orgId:string`, `projectId:string`, `domainId:string`, `hostname:string`, `createdBy:string`, `createdAt:datetime`
- **Required Fields:** `orgId`, `projectId`, `domainId`, `hostname`, `createdAt`
- **Optional Fields:** `createdBy`
- **Example JSON Payload:**

```json
{
  "eventId": "evt_domain_created_001",
  "eventName": "domain.created.v1",
  "type": "domain.created",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T08:00:00Z",
  "resource": { "type": "domain", "id": "domain_123" },
  "payload": {
    "orgId": "org_123",
    "projectId": "proj_123",
    "domainId": "domain_123",
    "hostname": "api.example.com",
    "createdBy": "user_123",
    "createdAt": "2026-06-24T08:00:00Z"
  }
}
```

### domain.verified.v1

- **Event Name:** `domain.verified.v1`
- **Version:** `v1`
- **Producer Service:** Domain/TLS Service
- **Consumer Services:** Deployment Service, Audit Service
- **Description:** Published when domain ownership verification succeeds.
- **Payload Schema:** `orgId:string`, `projectId:string`, `domainId:string`, `hostname:string`, `verifiedAt:datetime`
- **Required Fields:** `orgId`, `projectId`, `domainId`, `hostname`, `verifiedAt`
- **Optional Fields:** `verificationMethod`
- **Example JSON Payload:**

```json
{
  "eventId": "evt_domain_verified_001",
  "eventName": "domain.verified.v1",
  "type": "domain.verified",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T08:05:00Z",
  "resource": { "type": "domain", "id": "domain_123" },
  "payload": {
    "orgId": "org_123",
    "projectId": "proj_123",
    "domainId": "domain_123",
    "hostname": "api.example.com",
    "verificationMethod": "dns_txt",
    "verifiedAt": "2026-06-24T08:05:00Z"
  }
}
```

### domain.certificate.issued.v1

- **Event Name:** `domain.certificate.issued.v1`
- **Version:** `v1`
- **Producer Service:** Domain/TLS Service
- **Consumer Services:** Cluster Agent, Deployment Service, Observability Service, Notification Service, Audit Service
- **Description:** Published when a TLS certificate is issued for a domain.
- **Payload Schema:** `orgId:string`, `projectId:string`, `domainId:string`, `certificateId:string`, `hostname:string`, `issuedAt:datetime`, `expiresAt:datetime`
- **Required Fields:** `orgId`, `projectId`, `domainId`, `certificateId`, `hostname`, `issuedAt`, `expiresAt`
- **Optional Fields:** `issuer`
- **Example JSON Payload:**

```json
{
  "eventId": "evt_domain_cert_issued_001",
  "eventName": "domain.certificate.issued.v1",
  "type": "domain.certificate.issued",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T08:10:00Z",
  "resource": { "type": "certificate", "id": "cert_123" },
  "payload": {
    "orgId": "org_123",
    "projectId": "proj_123",
    "domainId": "domain_123",
    "certificateId": "cert_123",
    "hostname": "api.example.com",
    "issuer": "letsencrypt",
    "issuedAt": "2026-06-24T08:10:00Z",
    "expiresAt": "2026-09-22T08:10:00Z"
  }
}
```

---

## 3.9 Audit Events

### audit.record.created.v1

- **Event Name:** `audit.record.created.v1`
- **Version:** `v1`
- **Producer Service:** Audit Service
- **Consumer Services:** Observability Service, Notification Service
- **Description:** Published when an immutable audit record is created. This event is not used to create the audit record itself; it confirms durable audit ingestion.
- **Payload Schema:** `orgId:string`, `auditRecordId:string`, `action:string`, `resourceType:string`, `resourceId:string`, `actorId:string`, `createdAt:datetime`
- **Required Fields:** `orgId`, `auditRecordId`, `action`, `resourceType`, `createdAt`
- **Optional Fields:** `resourceId`, `actorId`, `severity`
- **Example JSON Payload:**

```json
{
  "eventId": "evt_audit_record_created_001",
  "eventName": "audit.record.created.v1",
  "type": "audit.record.created",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T08:20:00Z",
  "resource": { "type": "audit_record", "id": "audit_123" },
  "payload": {
    "orgId": "org_123",
    "auditRecordId": "audit_123",
    "action": "deployment.created",
    "resourceType": "deployment",
    "resourceId": "dep_123",
    "actorId": "user_123",
    "severity": "info",
    "createdAt": "2026-06-24T08:20:00Z"
  }
}
```

---

## 3.10 Notification Events

### notification.sent.v1

- **Event Name:** `notification.sent.v1`
- **Version:** `v1`
- **Producer Service:** Notification Service
- **Consumer Services:** Observability Service, Audit Service
- **Description:** Published when a notification is successfully delivered to a provider.
- **Payload Schema:** `orgId:string`, `notificationId:string`, `channel:string`, `recipient:string`, `template:string`, `sentAt:datetime`
- **Required Fields:** `orgId`, `notificationId`, `channel`, `recipient`, `sentAt`
- **Optional Fields:** `template`, `provider`, `providerMessageId`
- **Example JSON Payload:**

```json
{
  "eventId": "evt_notification_sent_001",
  "eventName": "notification.sent.v1",
  "type": "notification.sent",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T08:30:00Z",
  "resource": { "type": "notification", "id": "notif_123" },
  "payload": {
    "orgId": "org_123",
    "notificationId": "notif_123",
    "channel": "email",
    "recipient": "davit@example.com",
    "template": "deployment_succeeded",
    "provider": "ses",
    "providerMessageId": "provider_msg_123",
    "sentAt": "2026-06-24T08:30:00Z"
  }
}
```

### notification.failed.v1

- **Event Name:** `notification.failed.v1`
- **Version:** `v1`
- **Producer Service:** Notification Service
- **Consumer Services:** Observability Service, Audit Service
- **Description:** Published when notification delivery fails after retry policy is exhausted or a non-retryable provider error occurs.
- **Payload Schema:** `orgId:string`, `notificationId:string`, `channel:string`, `recipient:string`, `failureCode:string`, `failedAt:datetime`
- **Required Fields:** `orgId`, `notificationId`, `channel`, `recipient`, `failureCode`, `failedAt`
- **Optional Fields:** `template`, `provider`, `message`
- **Example JSON Payload:**

```json
{
  "eventId": "evt_notification_failed_001",
  "eventName": "notification.failed.v1",
  "type": "notification.failed",
  "version": 1,
  "orgId": "org_123",
  "occurredAt": "2026-06-24T08:35:00Z",
  "resource": { "type": "notification", "id": "notif_123" },
  "payload": {
    "orgId": "org_123",
    "notificationId": "notif_123",
    "channel": "email",
    "recipient": "davit@example.com",
    "template": "deployment_failed",
    "provider": "ses",
    "failureCode": "provider_throttled",
    "message": "provider rate limit exceeded",
    "failedAt": "2026-06-24T08:35:00Z"
  }
}
```

---

## 4. Event Ownership Rules

- Each event has exactly one owning producer service.
- Only the producer service may create, rename, or change the payload schema of an event it owns.
- Consumers may request new optional fields, but they may not add fields directly to a producer's schema.
- Event schemas must be reviewed with the owning service before implementation.
- Producers must publish through the transactional outbox when the event corresponds to a database state change.
- Producers must not emit events containing secrets, plaintext credentials, private keys, tokens, or PII beyond the minimum required contract.
- Consumers must treat event delivery as at-least-once and must be idempotent.
- Consumers must persist processing checkpoints or deduplication records keyed by `eventId`.
- Consumers must not mutate producer-owned tables in response to events.
- Audit Service may consume all events but does not own their schemas.

---

## 5. Versioning Rules

### 5.1 Version Semantics

- `v1` is the initial production contract.
- `v2` is introduced when the event contract changes in a way that requires a new consumer schema.
- `v3` and later follow the same rules as `v2`.

### 5.2 Compatible Changes

Compatible changes do not require a new version:

- Adding an optional payload field.
- Adding an optional envelope field.
- Expanding enum values only when consumers are documented to ignore unknown enum values.
- Clarifying field descriptions without changing semantics.

### 5.3 Breaking Changes

Breaking changes require a new version:

- Removing a field.
- Renaming a field.
- Changing a field type.
- Changing field meaning or units.
- Making an optional field required.
- Changing idempotency, ordering, or deduplication assumptions.

### 5.4 Migration Strategy

1. Producer defines the new schema, e.g. `deployment.succeeded.v2`.
2. Producer publishes both `v1` and `v2` during the migration window when feasible.
3. Consumers migrate one at a time to `v2`.
4. Monitoring verifies no active consumers remain on `v1`.
5. `v1` is marked deprecated.
6. After the deprecation window, `v1` publishing stops.
7. Historical replay remains available for retained `v1` events until the retention period expires.

### 5.5 Upcasting

- Consumers may register upcasters to transform older versions to the consumer's current internal model.
- Upcasters must be deterministic and side-effect free.
- Upcasters must not infer missing critical data unless a safe default is documented.
- Upcasting is allowed for replay and backfills, but it does not remove the producer's responsibility to publish the correct active version.

---

## 6. Deprecation Strategy

- A deprecated event version must be documented in this catalog.
- Deprecation requires an owner, target removal date, and migration notes.
- Minimum deprecation window is 90 days for externally visible or cross-team events.
- Minimum deprecation window is 30 days for internal-only events with known consumers.
- Producers must publish consumer impact metrics before removal.
- Consumers must receive a migration issue or engineering task before removal.
- Deprecated events may still be replayed while retained, but no new consumers may be added to deprecated versions.

---

## 7. Event Retention Policy

### 7.1 Broker Retention

- Primary event stream retention: 7 days.
- Dead-letter stream retention: 30 days.
- High-volume telemetry-like events are not part of this catalog and must use observability pipelines, not the domain event stream.

### 7.2 Outbox Retention

- Published outbox rows are retained for 7 days for operational debugging.
- Unpublished outbox rows are retained until successfully published or manually remediated.
- Outbox cleanup must not delete unpublished rows.

### 7.3 Audit Retention

- Audit records are retained according to compliance policy, minimum 1 year.
- Audit events are not a substitute for durable audit storage.
- Audit Service is responsible for immutable audit storage and export.

### 7.4 Replay Storage

- Replays use retained broker messages when available.
- Older replays use service-owned source-of-truth tables or archived event exports.
- Replay exports must preserve `eventId`, `occurredAt`, `orgId`, `type`, and `version`.

---

## 8. DLQ Policy

### 8.1 When Events Enter DLQ

An event is moved to the dead-letter queue when:

- Payload cannot be decoded.
- Required fields are missing.
- Schema version is unsupported and no upcaster exists.
- Consumer processing fails after the configured retry attempts.
- The consumer detects a non-retryable business invariant violation.

### 8.2 DLQ Event Shape

DLQ records include:

- Original envelope.
- Original subject.
- Consumer name.
- Attempt count.
- Failure reason.
- Dead-letter timestamp.

### 8.3 DLQ Ownership

- The owning consumer team is responsible for investigating its own DLQ entries.
- The producer team must assist when the DLQ cause is malformed or semantically invalid producer output.
- Audit Service must not drop malformed events silently; it must DLQ and alert.

### 8.4 DLQ Operational Policy

- DLQ alerts fire when any production consumer has DLQ entries for more than 5 minutes.
- DLQ entries are triaged by severity:
  - **P0:** audit, secrets, auth, or deployment events blocked.
  - **P1:** customer-visible workflow impact.
  - **P2:** delayed notification, read model, or analytics impact.
- DLQ entries may be replayed only after the root cause is fixed or an explicit skip decision is recorded.

---

## 9. Replay Policy

### 9.1 Replay Principles

- Replays are explicit operational actions, not automatic recovery loops.
- Consumers must be idempotent before they are approved for replay.
- Replays must preserve original `eventId` unless intentionally producing a synthetic repair event.
- Synthetic repair events must use a new event name or include `payload.replay.synthetic = true`.

### 9.2 Replay Scope

Replays may be scoped by:

- Event name.
- Event version.
- `orgId`.
- Resource ID.
- Time range.
- Consumer group.

### 9.3 Replay Approval

- Single-tenant replay requires service owner approval.
- Multi-tenant replay requires platform owner approval.
- Audit-related replay requires audit owner approval.
- Secret-related replay requires security owner approval.

### 9.4 Replay Procedure

1. Identify event range and affected consumer.
2. Confirm consumer idempotency and deduplication behavior.
3. Dry-run the replay plan in staging when feasible.
4. Pause or isolate the affected consumer if ordering matters.
5. Replay from broker retention or archived event export.
6. Monitor processing lag, error rate, DLQ, and side effects.
7. Record the replay in the audit log.

### 9.5 Replay Ordering

- Ordering is guaranteed only within the same aggregate partition when the producer preserves that ordering.
- Consumers must not assume global ordering across domains.
- When rebuilding read models, replay by aggregate ID and `occurredAt` where possible.

---

## 10. Security and Privacy Requirements

- Events must not include plaintext secrets, API tokens, private keys, refresh tokens, passwords, kubeconfigs, cloud credentials, or certificate private keys.
- Secret events may include metadata only: IDs, names, environment, and version.
- Auth events may include email only where required for notification and identity correlation.
- Tokens included for out-of-band delivery, such as invitation or email verification tokens, must be short-lived, one-time use, and stored only as hashes in producer databases.
- Events crossing tenant boundaries must include `orgId`.
- Consumers must enforce tenant isolation when materializing read models.
- Events must be logged by ID and type, not by full payload, unless logs are explicitly classified and protected.

---

## 11. Consumer Implementation Requirements

- Use durable consumer groups.
- Acknowledge only after successful, durable processing.
- Retry transient failures with exponential backoff.
- DLQ non-retryable failures with a clear reason.
- Deduplicate by `eventId`.
- Validate required fields before processing.
- Ignore unknown optional fields.
- Reject unknown major schema versions unless an upcaster exists.
- Persist offsets/checkpoints per consumer group where the broker does not manage them.
- Emit metrics for processing latency, success count, retry count, DLQ count, and lag.

---

## 12. Producer Implementation Requirements

- Publish domain events only after validating the state transition.
- Use the transactional outbox for state-change events.
- Set `correlationId` from the inbound request when available.
- Propagate `traceparent` for distributed tracing.
- Set `actor` when a user, service account, agent, or system actor initiated the change.
- Set `resource` to the primary aggregate affected by the event.
- Include stable IDs, not display names, for cross-service joins.
- Keep payloads small and immutable.
- Never republish a mutated event with the same `eventId`.

