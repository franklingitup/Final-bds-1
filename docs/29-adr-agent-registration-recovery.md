# 29 — ADR: Production-Grade Agent Registration & Recovery

- **Status:** Accepted
- **Date:** 2026-08
- **Supersedes:** the single-use registration behavior described in
  [08 — Agent Design](08-agent-design.md) and
  [14 — E2E Cluster Registration](14-e2e-cluster-registration.md)

---

## 1. Context

The Platform Agent runs inside customer Kubernetes clusters and connects
outbound to the control plane. Its original registration lifecycle was:

```
start → load state.json → if not registered → POST /register → save state
```

This was **not fault tolerant**. The registration token was treated as a
strictly single-use credential: once consumed, `POST /v1/agent/register`
returned `409 Conflict` ("registration token has already been used"). Any event
that erased local state — pod rescheduling with an ephemeral root filesystem, a
deleted `state.json`, node loss, or a fresh replica — caused the agent to
generate a new AgentID, re-`POST /register`, receive `409`, and exit. Kubernetes
then restarted it, reproducing the failure: **CrashLoopBackOff requiring manual
operator intervention** (mint a new token, reinstall).

Root problems:

1. Registration was **not idempotent** (second call fails instead of returning
   the existing cluster).
2. There was **no recovery path** to rebuild local state from the control
   plane.
3. **AgentID was unstable** — regenerated on every state loss.
4. The deployment set `readOnlyRootFilesystem: true` with **no writable
   volume**, so state was effectively ephemeral and often unwritable.
5. Registration failures were **fatal** (the process exited) rather than
   retried.

## 2. Decision

Redesign the lifecycle around **idempotency + control-plane-authoritative
recovery**, so the agent can always rebuild itself with **zero manual
intervention**.

### 2.1 Idempotent registration API (backend)

`POST /v1/agent/register` becomes idempotent (`cluster` service —
`internal/service.go: RegisterAgent`):

| Case | Condition | Result |
|------|-----------|--------|
| 1 | Unknown / invalid token | `401 Unauthorized` |
| 2 | Valid, unused token | Register cluster, persist, mark token used → `200` with cluster + `agentId` |
| 3 | Token already used **and** cluster exists | `recoverRegisteredCluster` returns the existing cluster → `200`, **never `409`** |
| — | Revoked token | Rejected (revocation still wins over idempotency) |

Ownership is enforced via the token→cluster mapping; a token can only ever
resolve to the cluster it originally created. Every attempt is audited: fresh
registrations emit `cluster.registered`; idempotent/recovered ones emit
`cluster.recovered` (`internal/events.go: EventClusterRecovered`,
`clusterRecoveredPayload`) recording both the authoritative `AgentID` and the
`requestedAgentId` the agent presented (they differ after state loss).

### 2.2 Dedicated recovery API (backend)

`GET /v1/agent/recover` (`internal/handlers.go: RecoverAgent`,
`internal/service.go: RecoverCluster`, wired in `internal/routes.go` and the
gateway `router.go`). Authenticated **solely by possession of the installation
token** (`X-Registration-Token` header, or `Authorization: Bearer`), with the
presented identity in `X-Agent-ID`. It accepts both **active and used** tokens
(a used token can still recover its own cluster) and returns the cluster
metadata the agent needs to rebuild state: `ClusterID`, `OrganizationID`,
`Namespace`, `Name`, certificates/agent credentials, and current `Status`. It
returns only the cluster bound to that token — a token never grants access to
another cluster.

### 2.3 Stable agent identity (agent)

`internal/agent/agent.go: resolveAgentID` establishes the AgentID with this
precedence, so it stays constant across restarts:

1. **Persisted AgentID** (`state.json`) — highest priority.
2. **Configured AgentID** (`AGENT_ID` env).
3. **Pod UID** (downward API `metadata.uid` → `POD_UID`) — stable per pod.
4. **Generated UUID** — first boot only, last resort.

On recovery the agent **adopts the control plane's authoritative AgentID**
(`adoptRegistration`), which supersedes any locally-derived value, so the
cluster's identity is single-sourced.

### 2.4 Fault-tolerant startup (agent)

`ensureRegistered` replaces the old `register()`:

- If persisted state says registered → short-circuit (the heartbeat loop
  validates it continuously).
- Otherwise loop: `tryRegister` →
  - success → adopt + persist;
  - `409 Conflict` → `recoverRegistration` (recover the existing cluster);
  - `401 Unauthorized` → try recovery once; if that also fails the token is
    genuinely unusable → return `*fatalConfigError` (the **only** error allowed
    to terminate the process);
  - any other/transient error → **exponential backoff** (`RegistrationRetryInterval`
    → capped at `RegistrationMaxRetryInterval`) and retry, honoring context
    cancellation.

The agent **never crash-loops** on registration; it stays alive and keeps
retrying.

### 2.5 Heartbeat-driven recovery (agent)

`sendHeartbeat` treats `401`/`404` from the control plane (cluster record lost)
as a recovery trigger: `recoverAfterHeartbeatLoss` recovers or idempotently
re-registers the existing cluster and heartbeats resume. It never terminates the
process; the next tick simply retries on failure.

### 2.6 Writable persistent state (Kubernetes)

The Helm deployment now mounts a writable volume at
`/var/lib/platform-agent` (`helm/agent`), keeping `readOnlyRootFilesystem: true`
on the container. `state.json`, `reconciler-state.json`, and
`secret-sync-state.json` all live under it via `STATE_FILE`,
`RECONCILER_STATE_FILE`, `SECRETS_SYNCER_STATE_FILE`.

- `persistence.type: emptyDir` (default) — ephemeral, rebuilt from the control
  plane on restart; correct for HA (leader-elected, `replicaCount: 2`)
  Deployments because a `ReadWriteOnce` PVC cannot be shared by multiple pods.
- `persistence.type: pvc` — survives pod recreation; pair with `replicaCount: 1`
  (StatefulSet-style single writer).

`POD_UID` is injected via the downward API for the stable-identity fallback.

### 2.7 Observability

Agent counters (`internal/metrics/metrics.go`): `agent_registration_attempt_total`,
`agent_registration_success_total`, `agent_registration_recovered_total`,
`agent_registration_failure_total`, `agent_state_load_total`,
`agent_state_save_total`, `agent_heartbeat_success_total`,
`agent_heartbeat_failure_total`. Structured logs annotate every transition
(establishing registration, recovered vs fresh, backing off, fatal token).

## 3. Consequences

**Positive**

- Deleting `state.json`, recreating the pod, rolling the deployment, or losing a
  node no longer requires a new token or any manual action.
- ClusterID and AgentID remain stable across restarts.
- No CrashLoopBackOff caused by registration.
- Security preserved: bootstrap token is one-time-for-registration, ownership is
  enforced, recovery is token-scoped, revocation still wins, and the token can be
  rotated without disrupting a registered cluster.
- Works with pod/node restart, deployment rollout, PVC recovery, leader
  election, and horizontal scaling (followers do not re-register).

**Negative / trade-offs**

- The installation token remains valid for **recovery** after first use (it is a
  recovery credential, not strictly single-use). Mitigation: token rotation and
  revocation are supported; permanent per-cluster credentials are what
  authorize normal operation (heartbeat/reconcile), not the bootstrap token.
- `emptyDir` default means state is intentionally disposable; correctness relies
  on the recovery path (covered by tests). Operators needing on-disk durability
  opt into `pvc`.

## 4. Alternatives considered

- **Keep single-use tokens, require re-issue on state loss.** Rejected: this is
  the status quo that causes CrashLoopBackOff and manual toil.
- **Always use a PVC.** Rejected as a default: incompatible with multi-replica
  HA (`ReadWriteOnce`), and unnecessary once recovery exists. Offered as an opt-in.
- **Persist identity only in a Kubernetes Secret/ConfigMap written by the
  agent.** Rejected for first cut: broadens RBAC (write access to Secrets for
  self-identity) without removing the need for control-plane recovery. Pod UID +
  control-plane authority achieves stability with less privilege.

## 5. References

- Backend: `backend/services/cluster/internal/{service,handlers,routes,events}.go`
- Agent: `agents/platform-agent/internal/agent/agent.go`,
  `internal/controlplane/client.go`, `internal/config/config.go`,
  `internal/metrics/metrics.go`
- Deploy: `helm/agent/templates/{deployment,pvc,secret}.yaml`, `helm/agent/values.yaml`
- Migration: [30 — Agent Recovery Migration Guide](30-agent-recovery-migration-guide.md)
