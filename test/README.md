# Platform Agent — Production Certification Test Suites

End-to-end, chaos, load, soak, and upgrade suites that certify the
Agent Registration & Recovery mechanism against enterprise production standards.
They prove the agent needs **zero manual intervention** across every failure mode.

| Phase | Directory | What it proves |
|------|-----------|----------------|
| 1 — E2E | `test/e2e/` | Fresh install, registration, heartbeat, pod/deployment/RS/node restarts, Helm upgrade, PVC & `state.json` deletion, leader failover, disconnect→reconnect, rolling updates — identity stays stable throughout. |
| 2 — Chaos | `test/chaos/` | Random kills of agent/leader/follower/node/network (and backend/gateway/db with a compose control plane); asserts automatic recovery and stable ClusterID. Optional Chaos Mesh manifests for true netem partitions. |
| 3 — Load | `test/load/` | Stdlib load generator: 100/500/1000/5000 agents, register + heartbeat latency (p50/p95/p99), throughput, error rate, and control-plane CPU/mem samples. |
| 4 — Soak | `test/soak/` | 48h steady load with periodic goroutine/heap snapshots; fails on goroutine/heap growth, restarts (retry-storm proxy), or leader instability. |
| 5 — Upgrade | `test/upgrade/` | Helm upgrades across versions; asserts no duplicate clusters/AgentIDs, no token regeneration, no manual intervention. |

## Prerequisites

`kind`, `kubectl`, `helm`, `jq`, `docker`, `curl`, and Go 1.25+. A reachable
control plane (`GATEWAY_URL`/`CONTROL_PLANE_URL`) with `ADMIN_JWT` + `ORG_ID`
exported (or supply a pre-minted `INSTALL_TOKEN`).

## Quick start

```bash
# 1. Bring up the control plane (backend + gateway + db)
docker compose up -d --wait

# 2. Provision an org + install token, then run the full E2E suite
export ADMIN_JWT=... ORG_ID=...
./test/e2e/e2e.sh all

# 3. Chaos (uses the deployment left running by e2e setup)
ROUNDS=12 INTERVAL=45 COMPOSE_FILE=docker-compose.yml ./test/chaos/chaos.sh

# 4. Load test across fleet sizes
SYNTHETIC=1 ./test/load/run.sh                 # latency characterization
CREDS=creds.csv ./test/load/run.sh             # realistic, pre-provisioned agents

# 5. Upgrade compatibility
INSTALL_TOKEN=... VERSIONS="0.1.0 0.2.0 0.3.0" ./test/upgrade/upgrade.sh

# 6. Soak (long-running)
SOAK_HOURS=48 AGENTS=500 PPROF_URL=http://localhost:8085/debug/pprof ./test/soak/soak.sh
```

Configuration is via environment variables documented at the top of each script
and in `test/lib.sh`.

## The load generator

`test/load` is a self-contained, **stdlib-only** Go module (no external deps),
so it builds and runs anywhere:

```bash
cd test/load
go build -o load .
./load -url http://localhost:8080 -agents 1000 -duration 5m \
       -heartbeat-interval 30s -ramp 30s -synthetic -json
```

`-creds creds.csv` (header `clusterId,agentId,token`) drives real, pre-registered
agents. `-max-error-rate` makes the tool exit non-zero for CI gating.

## CI

`.github/workflows/agent-certification.yml` runs unit tests + Helm validation on
every push, and the kind-based E2E/chaos/load suites on `workflow_dispatch` or a
`certify` PR label.
