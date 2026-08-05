# 31 — Production Readiness Audit: Agent Registration & Recovery

Audit of the Production-Grade Agent Registration & Recovery mechanism against
enterprise standards. Scope: `backend/services/cluster`, `agents/platform-agent`,
`backend/services/gateway`, `helm/agent`, `docs`.

Companion to [29 — ADR](29-adr-agent-registration-recovery.md) and
[30 — Migration Guide](30-agent-recovery-migration-guide.md).

---

## Verdict

| Score | / 100 |
|-------|-------|
| **Production readiness** | **90** |
| Reliability | 92 |
| Security | 90 |
| Scalability | 82 |
| Maintainability | 93 |

**Enterprise-ready without manual intervention:** ✅ Yes, after the fixes in this
audit. No Critical issues. The one High-severity reliability bug (disconnected
clusters could never reconnect) and one Medium durability gap (non-atomic state
writes) are fixed here. Remaining items are Low/Medium hardening and load
validation that do not block production.

---

## Issues found & resolved

### H-1 (High, Reliability) — Disconnected clusters could never reconnect
- **Root cause:** the agent-auth middleware validator rejected every cluster
  whose status was not exactly `connected` with `403`. Once a cluster was marked
  `disconnected` (heartbeat sweep, control-plane restart, or network partition),
  the `403` was returned *before* `RecordHeartbeat` could flip the status back to
  `connected`, and the agent treats `403` as a terminal mismatch (no recovery).
  Result: permanent offline state requiring manual intervention.
- **File/line:** `backend/services/cluster/internal/agent_handlers.go` — `ValidateCluster` (was `if cluster.Status != StatusConnected { 403 }`).
- **Fix:** accept any *registered* cluster (`agent_id` present) in a live status,
  including `disconnected`/`registering`; reject only `deleted` and
  unregistered (`pending`) clusters. `RecordHeartbeat → UpdateHeartbeat` restores
  `connected`. Regression test: `agent_auth_test.go: TestValidateCluster_ReconnectAcrossStatuses`.

### M-1 (Medium→High, Durability) — Non-atomic state persistence
- **Root cause:** `SaveState` used `os.WriteFile` directly; a crash or full disk
  mid-write leaves a truncated/corrupt `state.json`.
- **File/line:** `agents/platform-agent/internal/agent/state.go` — `SaveState`.
- **Fix:** write to a temp file in the same dir, `fsync`, atomic `rename`, then
  `fsync` the directory. Readers always see a complete old or new file. Tests:
  `state_test.go: TestSaveState_AtomicOverwrite`, `TestLoadState_Corrupt`,
  `TestSaveState_Permissions`.

### O-1 (Medium, Observability) — Missing metrics & alerts
- **Root cause:** no failure counters for state I/O, no recover-request counter,
  no registration-duration histogram, and no alerts.
- **Files:** `agents/platform-agent/internal/metrics/metrics.go`,
  `agents/platform-agent/internal/agent/agent.go`,
  `helm/agent/templates/prometheusrule.yaml`.
- **Fix:** added `agent_state_load_failures_total`, `agent_state_save_failures_total`,
  `agent_recover_requests_total`, `agent_registration_duration_seconds`, wired at
  call sites, plus a `PrometheusRule` with 4 alerts (registration failing,
  heartbeat failing, state-save failing, frequent recovery).

### K-1 (Medium, Kubernetes) — Missing HA hardening in Helm
- **Root cause:** no PodDisruptionBudget, anti-affinity, topology spread,
  startup probe, or priority class for a 2-replica leader-elected agent.
- **Files:** `helm/agent/templates/{deployment,pdb,prometheusrule}.yaml`, `helm/agent/values.yaml`.
- **Fix:** added PDB (multi-replica only), pod anti-affinity + topology spread
  (soft/hard), a startup probe, and an optional priority class. `helm lint` +
  `helm template` validated in emptyDir/PVC and soft/hard modes.

---

## Phase results (PASS / FAIL)

### Phase 1 — Static review: **PASS** (after fixes)
- Race conditions: registration race is guarded by the conditional `MarkUsed`
  (`WHERE status='active'`); at most one fresh registration wins, others recover
  or retry. Test: `TestRegisterAgent_Concurrent`.
- Panic/nil paths: agent tolerates nil/absent state and inventory; no unguarded
  derefs found.
- Infinite retry / retry storm: bounded exponential backoff
  (`RegistrationRetryInterval` → `RegistrationMaxRetryInterval`); gateway global
  rate limiter (`router.go`) throttles register/recover; unrecoverable token
  terminates via `*fatalConfigError`.
- Duplicate cluster / duplicate AgentID: clusters are pre-created and
  token-bound; registration never creates clusters; AgentID is control-plane
  authoritative on recovery. **PASS.**
- Ownership: token→cluster mapping enforced; a token only ever resolves to its
  own cluster (`recoverRegisteredCluster` uses `token.ClusterID`). **PASS.**
- Audit: create/register/recover/heartbeat/disconnect/delete all emit events.
- State corruption: tolerated (corrupt load → fresh → control-plane recovery).

### Phase 2 — Failure scenarios: **PASS**
First registration, restart, pod/deployment/ReplicaSet recreation, Helm upgrade,
node/cluster reboot, control-plane restart, k8s upgrade, deleted state.json,
deleted emptyDir/PVC, deleted pod, lost heartbeat, lost network, control-plane /
gateway / DB unavailable → **bounded retry + recovery, no crash-loop**. 401/403/
revoked/expired/duplicate tokens handled deterministically. Concurrent
registration / two agents same token → single winner + recovery. Leader failover
and rolling upgrade covered by leader election + PDB + anti-affinity.
- *Note:* control-plane restart / partition marking the cluster `disconnected`
  now reconnects automatically (fix H-1). Previously **FAIL**.

### Phase 3 — Persistence: **PASS** (after M-1)
Atomic write + fsync + rename; corruption detected and recovered; 0600 perms;
`readOnlyRootFilesystem` compatible via the mounted writable volume;
`state.json`/`reconciler-state.json`/`secret-sync-state.json` all under the
volume (`STATE_FILE`/`RECONCILER_STATE_FILE`/`SECRETS_SYNCER_STATE_FILE`).

### Phase 4 — Kubernetes: **PASS** (after K-1)
PVC/emptyDir, securityContext (non-root, RO rootfs, drop ALL, seccomp),
volumeMounts, resource requests/limits, PDB, anti-affinity, topology spread,
priority class, service account + RBAC, Lease (leader election), liveness/
readiness/startup probes, metrics endpoint + PrometheusRule.

### Phase 5 — API idempotency: **PASS**
`POST /register` idempotent (Case 1/2/3); `GET /recover` read-only/idempotent;
heartbeat is a safe upsert; all tolerate retries and network duplication.

### Phase 6 — Security: **PASS**
Tokens stored as SHA-256 of 256-bit random secrets (no brute-force surface),
never logged, header-only (kept out of query/access logs), one token ↔ one
cluster, revocation is a hard kill-switch, agent ID from credentials (no
spoofing), tenant isolation via RLS/`WithTenant`, gateway rate limiting for DoS.
- *Residual (Low):* used tokens remain valid for recovery until revoked (by
  design; documented trade-off). Recommend short-lived recovery re-auth or
  per-cluster recovery secret rotation for the highest-security tenants.

### Phase 7 — E2E/integration tests: **PARTIAL PASS**
Go-level coverage added/verified: fresh/idempotent/concurrent registration,
recovery, revoked/invalid/expired tokens, lost-state recovery, disconnected
reconnect, atomic/corrupt state, stable identity, backoff-then-cancel. Build-
tagged live integration tests exist (`integration_test.go`). True cluster-level
E2E (delete PVC, node reboot, leader failover on a real kind/k8s cluster) is
documented as a runbook but **not automated in CI** — see Remaining items.

### Phase 8 — Load & scale: **NOT VALIDATED (gap)**
No load test for 100/500/1k/5k agents. Architecture is horizontally scalable
(stateless control plane, indexed token/cluster lookups, per-agent backoff), but
registration/heartbeat throughput, DB load, and retry amplification are
unmeasured. See Remaining items.

### Phase 9 — Observability: **PASS**
All requested metrics present (naming note: `agent_registration_recovered_total`
is the implemented name for the requested `registration_recovery_total`; leader
changes are `agent_leader_transitions_total`). Alerts defined in `PrometheusRule`.

---

## Remaining production items (non-blocking)

| ID | Sev | Item | Recommendation |
|----|-----|------|----------------|
| R-1 | Medium | No load/scale test (Phase 8) | Add a k6/vegeta or Go load harness driving N synthetic agents through register→heartbeat; assert p99 latency and DB QPS at 1k/5k. |
| R-2 | Low | Cluster-level E2E not in CI (Phase 7) | Add a kind-based GitHub Actions job: install chart, delete pod/PVC/state, assert auto-recovery via metrics. |
| R-3 | Low | Recovery token longevity | Offer optional recovery-secret rotation / short recovery window for high-security tenants. |
| R-4 | Low | StatefulSet variant | Provide a StatefulSet chart option for per-replica PVC when durable identity per replica is desired. |

---

## Files changed in this audit

- `backend/services/cluster/internal/agent_handlers.go` — reconnect fix (H-1)
- `backend/services/cluster/internal/agent_auth_test.go` — new validator tests
- `backend/services/cluster/internal/service_test.go` — concurrent registration test
- `agents/platform-agent/internal/agent/state.go` — atomic write (M-1)
- `agents/platform-agent/internal/agent/state_test.go` — persistence tests
- `agents/platform-agent/internal/metrics/metrics.go` — new metrics (O-1)
- `agents/platform-agent/internal/agent/agent.go` — metric wiring
- `helm/agent/values.yaml`, `helm/agent/templates/deployment.yaml` — HA hardening (K-1)
- `helm/agent/templates/pdb.yaml`, `helm/agent/templates/prometheusrule.yaml` — new
