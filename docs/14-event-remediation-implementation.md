# 14 — Event Remediation Implementation Notes & Compatibility Report

This document records what was implemented for the event contract remediation
(plan: `docs/13-event-remediation-plan.md`), the operational migration steps,
and the backward-compatibility impact.

---

## 1. Summary of Changes

| Area | Change |
|---|---|
| `libs/events` | Added `CanonicalName`/`CatalogName` helpers. Subscribers (`Client.Subscribe` and `MemoryBroker`) now accept an upcaster `Registry` via `SubscriptionOptions.Upcasters` and apply it before the handler runs; an unrecoverable upcast dead-letters the message. |
| Auth Service | All event names standardized to canonical `auth.*` dot-form (no underscores). All state-changing operations now enqueue events through the transactional outbox inside the same DB transaction. Sensitive tokens removed from events. Added the `auth.user.email.verification.requested.v1` delivery-reference event. |
| Tenant Service | Payloads no longer duplicate envelope `orgId`. `tenant.member.invited.v1` no longer carries the invitation token; it carries `deliveryRef` + `expiresAt`. |
| Secure delivery | Both services gained a `Notifier` seam that hands plaintext one-time tokens to a secure out-of-band channel instead of the event bus. |
| Migrations | Added `outbox/0002_org_id_text` to widen `outbox.org_id` from `UUID` to `TEXT` so non-tenant identity events (org `platform`) can be enqueued. Auth `main.go` now applies the `outbox` migration and runs a relay. |
| Catalog | `docs/12-event-catalog.md` updated: `eventName` is derived (not an envelope field), Auth/Tenant entries match the implementation, single producer ownership enforced, `deployment.rollback.v1` replaced by `deployment.rollback.requested.v1` + `deployment.rolled.back.v1`, agents marked as non-producers. |
| Tests | Added producer contract tests (`auth`, `tenant`) verifying names, versions, payload schema (no metadata duplication, no secrets), and ownership; added a framework test proving subscribers auto-apply upcasters. |

---

## 2. Canonical Event Names (implemented)

### Auth Service (owns `auth.*`)

| Legacy name | Canonical name (v1) |
|---|---|
| `auth.user.signed_up` | `auth.user.created` |
| `auth.email.verified` | `auth.user.email.verified` |
| (new) | `auth.user.email.verification.requested` |
| `auth.password.reset` | `auth.user.password.reset` (payload `phase`: `requested`/`completed`) |
| `auth.login.succeeded` | `auth.login.succeeded` (unchanged) |
| `auth.login.failed` | `auth.login.failed` (unchanged) |
| `auth.token.revoked` | `auth.token.revoked` (unchanged) |
| `auth.service_account.created` | `auth.service.account.created` |
| `auth.api_token.created` | `auth.api.token.created` |
| `auth.api_token.revoked` | `auth.api.token.revoked` |

### Tenant Service (owns `tenant.*`)

Names unchanged (already canonical):
`tenant.organization.created`, `tenant.organization.updated`,
`tenant.member.invited`, `tenant.member.removed`, `tenant.role.changed`.

---

## 3. Sensitive Data Removed from Events

The following values are **no longer published** on the event bus:

- Email verification tokens (`auth.user.created` previously carried `verificationToken`).
- Password reset tokens (`auth.user.password.reset` previously carried `resetToken`).
- Invitation tokens (`tenant.member.invited` previously carried `token`).

Replacement: events carry a non-secret `deliveryRef` (the one-time-token row id
for Auth, the invitation id for Tenant) plus a `purpose`. The plaintext token is
delivered out-of-band via the service `Notifier` seam, modeling a restricted
internal delivery API that the Notification Service will call. With no notifier
wired (current default), the token is simply not delivered anywhere — see
Operational Notes.

---

## 4. Migration Notes (operational)

1. **Schema:** Both services apply migrations on startup. The new
   `outbox/0002_org_id_text` migration runs automatically and is safe/idempotent
   (`ALTER COLUMN ... TYPE TEXT USING org_id::text`). It is forward-only in
   practice; the down migration only succeeds if every `org_id` is a valid UUID.
2. **Auth outbox + relay:** Auth `main.go` now creates a `PostgresOutbox` and a
   background `Relay`. Ensure `NATS_URL` is configured in production; without it,
   Auth falls back to the in-memory broker (events are not durably delivered
   across processes — dev only).
3. **Apply order:** Auth and Tenant each own their schema-migrations table and
   share the `outbox` table (tracked by `schema_migrations_outbox`). Running both
   services against the same database is safe; the `outbox/0001` and `0002`
   migrations are guarded by `CREATE TABLE IF NOT EXISTS` / idempotent `ALTER`.
4. **Token rotation:** Any one-time tokens that may have transited event streams
   in pre-production environments should be considered exposed and left to expire
   or be invalidated; production never published them under this change.
5. **Notifier wiring (follow-up):** Implement `auth.Notifier` and
   `tenant.Notifier` (or an internal delivery API) when the Notification Service
   is built, then inject them in each service's `main.go`.

---

## 5. Compatibility Report

### Breaking changes

| Change | Impact | Mitigation |
|---|---|---|
| Auth event renames (`auth.*`) | Any consumer bound to the legacy subjects would stop receiving events. | No external consumers exist yet (Auth/Tenant are the only implemented services). Renamed in place; no dual-publish needed. If consumers existed, the plan's dual-publish window (§H) would apply. |
| Tokens removed from events | A consumer relying on tokens in `auth`/`tenant` payloads would break. | No such consumer exists. Notification Service must use `deliveryRef` + internal delivery API from day one. |
| Tenant payloads drop `orgId` and timestamps | Consumers reading `payload.orgId`/`payload.createdAt` would break. | No consumers yet. `orgId`/`occurredAt` are available in the envelope. |
| `deployment.rollback.v1` removed | None (Deployment Service not implemented). | Replaced in catalog before implementation. |

### Non-breaking changes

- Adding `auth.user.email.verification.requested.v1` (new event, no consumers yet).
- Adding `expiresAt`/`deliveryRef` to `tenant.member.invited.v1`.
- Subscriber upcaster support (opt-in via `SubscriptionOptions.Upcasters`;
  existing subscribers that pass no registry are unaffected).
- `CanonicalName`/`CatalogName` helpers (additive).

### Versioning

All implemented events remain at **v1**. Because there are no external consumers,
the payload reshaping was applied directly to v1 rather than introducing v2 +
upcasters. The upcaster path is implemented and tested in the framework so future
breaking payload changes can use v2 with a registered upcaster, applied
automatically by subscribers before handler execution.

---

## 6. Verification

- `go test ./backend/libs/events/...` — envelope, memory broker, retry, upcaster
  subscribe, and `CatalogName` tests.
- `go test ./backend/services/auth/...` — service flow tests (now asserting
  outbox enqueues and out-of-band token delivery) and `contract_test.go`.
- `go test ./backend/services/tenant/...` — service tests and `contract_test.go`.
- `go test -tags=integration ./backend/services/tenant/...` — verifies events
  land in the outbox transactionally and that the invited event carries no token.

The contract tests fail the build if any produced event: uses a non-canonical
name, is owned by the wrong domain, duplicates envelope metadata in its payload,
or exposes a secret field — giving CI a mechanical drift check.
