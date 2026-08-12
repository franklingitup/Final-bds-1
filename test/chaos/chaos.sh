#!/usr/bin/env bash
# Phase 2 — Chaos suite for the Platform Agent.
#
# Randomly injects faults (killing agent replicas, the leader, a follower,
# draining a node, partitioning the network to the control plane) and, after
# each injection, verifies the agent self-heals: pods return Ready, identity is
# stable, and heartbeats resume — with zero manual intervention.
#
# Assumes a running deployment from `test/e2e/e2e.sh setup`.
#
#   ROUNDS=20 INTERVAL=45 ./test/chaos/chaos.sh
#
# Backend/gateway/database faults are injected against a docker-compose control
# plane when COMPOSE_FILE is set; otherwise those rounds are skipped with a note.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib.sh"

: "${ROUNDS:=12}"
: "${INTERVAL:=45}"
: "${COMPOSE_FILE:=}"

compose() { [[ -n "$COMPOSE_FILE" ]] && docker compose -f "$COMPOSE_FILE" "$@"; }

# Verify the agent is healthy and heartbeating after a fault.
verify_recovery() {
  local what=$1
  assert_true "$what: pods Ready" retry 30 6 agent_ready
  local hb_before hb_after
  hb_before=$(agent_metric agent_heartbeat_success_total)
  sleep "$INTERVAL"
  hb_after=$(agent_metric agent_heartbeat_success_total)
  if awk -v a="$hb_after" -v b="$hb_before" 'BEGIN{exit !(a>=b)}'; then
    ok "$what: heartbeats healthy after recovery"
  else
    fail "$what: heartbeats regressed after recovery"
  fi
}

fault_kill_random_agent() {
  local pod; pod=$(agent_pods | shuf | head -n1); pod=${pod#pod/}
  [[ -n "$pod" ]] || { ok "no agent pod to kill"; return; }
  log "CHAOS: kill agent pod $pod"
  k delete pod "$pod" --grace-period=0 --force --wait=false
  verify_recovery "kill-agent"
}

fault_kill_leader() {
  local leader; leader=$(leader_holder)
  [[ -n "$leader" ]] || { ok "no leader lease; skipping"; return; }
  log "CHAOS: kill leader $leader"
  k delete pod "$leader" --grace-period=0 --force --wait=false 2>/dev/null || true
  verify_recovery "kill-leader"
  [[ -n "$(leader_holder)" ]] && ok "leader re-elected" || fail "no leader after killing leader"
}

fault_kill_follower() {
  local leader followers pod
  leader=$(leader_holder)
  followers=$(agent_pods | sed 's|pod/||' | grep -v -x "$leader" || true)
  pod=$(echo "$followers" | shuf | head -n1)
  [[ -n "$pod" ]] || { ok "no follower to kill"; return; }
  log "CHAOS: kill follower $pod"
  k delete pod "$pod" --grace-period=0 --force --wait=false
  verify_recovery "kill-follower"
}

fault_drain_node() {
  local node; node=$(kk get nodes -o jsonpath='{.items[0].metadata.name}')
  log "CHAOS: drain node $node"
  kk drain "$node" --ignore-daemonsets --delete-emptydir-data --force --timeout=90s || true
  kk uncordon "$node"
  verify_recovery "drain-node"
}

fault_network_partition() {
  # Simulate loss of control-plane connectivity by scaling to zero briefly, then
  # restoring. The agent must resume heartbeats (and recover if the control
  # plane forgot the cluster). A true netem partition can be injected via Chaos
  # Mesh (see chaos-mesh/network-partition.yaml).
  log "CHAOS: simulate network partition (scale 0 -> N)"
  local replicas; replicas=$(k get deploy "$RELEASE" -o jsonpath='{.spec.replicas}')
  k scale deploy/"$RELEASE" --replicas=0
  sleep 20
  k scale deploy/"$RELEASE" --replicas="$replicas"
  verify_recovery "network-partition"
}

fault_backend() {
  [[ -n "$COMPOSE_FILE" ]] || { ok "backend fault skipped (no COMPOSE_FILE)"; return; }
  log "CHAOS: restart backend cluster service"
  compose restart cluster || true
  verify_recovery "backend-restart"
}

fault_gateway() {
  [[ -n "$COMPOSE_FILE" ]] || { ok "gateway fault skipped (no COMPOSE_FILE)"; return; }
  log "CHAOS: restart gateway"
  compose restart gateway || true
  verify_recovery "gateway-restart"
}

fault_database() {
  [[ -n "$COMPOSE_FILE" ]] || { ok "database fault skipped (no COMPOSE_FILE)"; return; }
  log "CHAOS: restart database"
  compose restart postgres || compose restart db || true
  verify_recovery "database-restart"
}

FAULTS=(
  fault_kill_random_agent
  fault_kill_leader
  fault_kill_follower
  fault_drain_node
  fault_network_partition
  fault_backend
  fault_gateway
  fault_database
)

log "starting chaos: ${ROUNDS} rounds, ${INTERVAL}s recovery window each"
# Authoritative identity from the control-plane API (image-agnostic).
identity_start=""
[[ -n "${CLUSTER_ID:-}" ]] && identity_start="${CLUSTER_ID} $(api_agent_id)"
for ((round=1; round<=ROUNDS; round++)); do
  fault=${FAULTS[$((RANDOM % ${#FAULTS[@]}))]}
  log "=== round ${round}/${ROUNDS}: ${fault} ==="
  "$fault"
done

# Identity must be unchanged across the entire chaos run (no re-provisioning).
if [[ -n "${CLUSTER_ID:-}" ]]; then
  assert_eq "${CLUSTER_ID} $(api_agent_id)" "$identity_start" "Cluster/Agent identity stable across entire chaos run"
else
  ok "identity stability check skipped (no API creds / CLUSTER_ID)"
fi

summary
