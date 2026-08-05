# 30 — Migration Guide: Agent Registration & Recovery

Audience: operators running an existing Platform Agent, and platform engineers
deploying the updated `cluster` service. See
[29 — ADR](29-adr-agent-registration-recovery.md) for the design rationale.

This change is **backward compatible**. Already-registered clusters keep working
without re-registration. Follow the steps below to adopt the new fault-tolerant
behavior.

---

## 1. What changed (at a glance)

| Area | Before | After |
|------|--------|-------|
| `POST /v1/agent/register` (2nd call) | `409 Conflict`, agent exits | `200` with existing cluster + `agentId` (idempotent) |
| Recovery | none | `GET /v1/agent/recover` (token-authenticated) |
| AgentID | regenerated on state loss | stable: persisted → `AGENT_ID` → `POD_UID` → generated |
| Registration failure | process exits (CrashLoopBackOff) | exponential backoff, never exits (except unrecoverable token) |
| State volume | `readOnlyRootFilesystem`, no writable mount | writable volume at `/var/lib/platform-agent` |
| Heartbeat after CP loses cluster | fails / crash | auto-recovers and continues |

No database schema changes are required for the core behavior. If you introduced
the `cluster.recovered` audit event / token→cluster mapping columns in your
environment, apply those migrations first; otherwise the service degrades
gracefully (recovery still works, auditing is best-effort).

## 2. Deploy order

1. **Control plane first.** Roll out the updated `cluster` service (and the API
   gateway, which now routes `GET /v1/agent/recover`). Idempotent registration
   and recovery must exist before agents rely on them.
2. **Then agents.** Upgrade the agent Helm release. Old agents keep working
   against the new backend; new agents gain recovery.

## 3. Upgrading the agent (Helm)

The chart now provisions a writable state volume and injects `POD_UID`.

```bash
# Ephemeral state (recommended for HA / multi-replica). Default.
helm upgrade --install bds-agent ./helm/agent \
  --namespace platform-agent \
  --set controlPlane.endpoint="https://control-plane.example.com" \
  --set controlPlane.installToken="<INSTALL_TOKEN>"

# Durable state across pod recreation (single replica, StatefulSet-style).
helm upgrade --install bds-agent ./helm/agent \
  --namespace platform-agent \
  --set replicaCount=1 \
  --set persistence.type=pvc \
  --set persistence.size=64Mi \
  --set persistence.storageClass="<STORAGE_CLASS>" \
  --set controlPlane.installToken="<INSTALL_TOKEN>"
```

Key values:

| Value | Default | Meaning |
|-------|---------|---------|
| `persistence.type` | `emptyDir` | `emptyDir` (ephemeral, rebuilt on restart) or `pvc` (durable). |
| `persistence.mountPath` | `/var/lib/platform-agent` | Writable state directory. |
| `persistence.size` | `64Mi` | emptyDir size limit / PVC request. |
| `persistence.storageClass` | `""` | PVC storage class (`""` = cluster default). |
| `controlPlane.installToken` | `""` | If set, the chart renders the install Secret; if empty, manage it out-of-band. |

If you already manage the `bds-agent-install` Secret externally (sealed secrets,
external-secrets, etc.), leave `controlPlane.installToken` empty so the chart
does not overwrite it.

## 4. Migrating existing agents

Existing agents with a valid `state.json` are unaffected — startup
short-circuits registration.

For agents that had been failing (CrashLoopBackOff from `409`) or that lose
state during the upgrade:

1. Ensure the **original installation token** is still present in the
   `bds-agent-install` Secret (do **not** mint a new one — the old one now
   recovers the existing cluster).
2. Upgrade. On boot the agent will:
   - derive a stable AgentID (from `POD_UID` if no persisted value),
   - `POST /register`, receive the existing cluster (`200`) or `409` → recover,
   - adopt the control plane's authoritative AgentID and persist state,
   - begin heartbeating.
   No manual action required.

## 5. Token lifecycle & security notes

- The installation token is a **bootstrap + recovery** credential, not the
  operational credential. Normal operation (heartbeat, reconcile) uses the
  permanent per-cluster credentials issued at registration.
- **Rotation:** issue a new registration token for the cluster and update the
  `bds-agent-install` Secret; the old token can then be revoked.
- **Revocation:** revoking a token blocks further registration/recovery with it.
  Revocation takes precedence over idempotency (a revoked token yields an auth
  error, not a `200`). Revoking the cluster's permanent credentials
  disconnects the agent.
- A token only ever resolves to the cluster it created — it cannot register or
  recover a different cluster.

## 6. Verification

After upgrade, confirm zero-touch recovery:

```bash
# 1. Delete the agent's state and the pod; it must recover on its own.
kubectl -n platform-agent exec deploy/bds-agent -- rm -f /var/lib/platform-agent/state.json
kubectl -n platform-agent rollout restart deploy/bds-agent

# 2. Watch logs: expect "registration established ... recovered=true" then heartbeats.
kubectl -n platform-agent logs deploy/bds-agent -f

# 3. Confirm identity stability: ClusterID/AgentID unchanged in the control plane.
```

Metrics to watch (agent `/metrics`): `agent_registration_recovered_total` should
increment on recovery, `agent_heartbeat_success_total` should keep rising, and
`agent_registration_failure_total` should stay flat once recovered.

## 7. Rollback

Roll back the agent Helm release and, if necessary, the `cluster` service.
Because registered clusters authenticate with permanent credentials (not the
bootstrap token), rolling back the agent does not require re-registration. The
new backend is a superset of the old contract, so a newer backend with older
agents is a safe intermediate state.
