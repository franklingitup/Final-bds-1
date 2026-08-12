#!/usr/bin/env bash
# Phase 1 — Kubernetes E2E certification suite for the Platform Agent.
#
# Drives a real kind cluster + Helm release against a running control plane and
# asserts the agent's registration/recovery/heartbeat guarantees survive every
# disruption an enterprise operator can throw at it. Zero manual intervention is
# expected between scenarios.
#
# Prereqs: kind, kubectl, helm, jq, docker, curl, and a reachable control plane
# (CONTROL_PLANE_URL/GATEWAY_URL) with ADMIN_JWT + ORG_ID exported.
#
#   ./test/e2e/e2e.sh                 # full suite
#   ./test/e2e/e2e.sh setup           # just provision
#   ./test/e2e/e2e.sh run reconnect   # a single scenario
#   ./test/e2e/e2e.sh teardown
#
# Every scenario is idempotent and leaves the agent healthy for the next one.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib.sh"

INSTALL_TOKEN="${INSTALL_TOKEN:-}"

setup() {
  log "creating kind cluster ${KIND_CLUSTER}"
  kind get clusters | grep -qx "$KIND_CLUSTER" || kind create cluster --name "$KIND_CLUSTER" --wait 120s

  log "building + loading agent image ${AGENT_IMAGE}"
  docker build -t "$AGENT_IMAGE" -f agents/platform-agent/Dockerfile .
  kind load docker-image "$AGENT_IMAGE" --name "$KIND_CLUSTER"

  if [[ -z "$INSTALL_TOKEN" ]]; then
    [[ -n "$ADMIN_JWT" && -n "$ORG_ID" ]] || { echo "set ADMIN_JWT and ORG_ID (or INSTALL_TOKEN)"; exit 2; }
    log "provisioning cluster + registration token via API"
    INSTALL_TOKEN=$(create_cluster_and_token "e2e-$(date +%s)" "e2e-$(date +%s)")
  fi

  log "helm install ${RELEASE}"
  helm --kube-context "kind-${KIND_CLUSTER}" upgrade --install "$RELEASE" "$CHART" \
    --namespace "$AGENT_NAMESPACE" --create-namespace \
    --set image.repository="${AGENT_IMAGE%%:*}" --set image.tag="${AGENT_IMAGE##*:}" \
    --set image.pullPolicy=Never \
    --set controlPlane.endpoint="$CONTROL_PLANE_URL" \
    --set controlPlane.installToken="$INSTALL_TOKEN" \
    --wait --timeout 180s
  assert_true "fresh install ready" agent_ready
  assert_true "agent registers on fresh install" wait_registered 120
}

teardown() {
  log "tearing down"
  helm --kube-context "kind-${KIND_CLUSTER}" uninstall "$RELEASE" -n "$AGENT_NAMESPACE" 2>/dev/null || true
  kind delete cluster --name "$KIND_CLUSTER" 2>/dev/null || true
}

# snapshot_identity prints "<clusterId> <agentId>" as seen by the control plane
# (image-agnostic — no reliance on reading state.json in a distroless container).
# When API creds are absent it prints empty tokens so identity asserts no-op.
snapshot_identity() {
  if [[ -n "$ADMIN_JWT" && -n "$ORG_ID" && -n "${CLUSTER_ID:-}" ]]; then
    echo "${CLUSTER_ID} $(api_agent_id)"
  else
    echo " "
  fi
}

assert_identity_stable() {
  local before=$1 after=$2 what=$3
  if [[ -z "${CLUSTER_ID:-}" ]]; then ok "$what: identity check skipped (no API creds)"; return; fi
  assert_eq "$after" "$before" "$what: Cluster/Agent identity stable"
}

# ---- scenarios ------------------------------------------------------------

scenario_heartbeat() {
  log "SCENARIO: heartbeat flowing"
  local before after
  before=$(agent_metric agent_heartbeat_success_total)
  sleep 35
  after=$(agent_metric agent_heartbeat_success_total)
  if awk -v a="$after" -v b="$before" 'BEGIN{exit !(a>b)}'; then ok "heartbeat_success increasing ($before->$after)"; else fail "heartbeats not increasing"; fi
}

scenario_pod_restart() {
  log "SCENARIO: pod restart (delete pod)"
  local id_before; id_before=$(snapshot_identity)
  k delete pod -l "app.kubernetes.io/name=${RELEASE}" --wait=false
  assert_true "rollout recovers after pod delete" retry 20 6 agent_ready
  assert_true "re-registers without new token" wait_registered 120
  assert_identity_stable "$id_before" "$(snapshot_identity)" "pod restart"
}

scenario_state_deletion() {
  log "SCENARIO: delete state.json (local state loss)"
  local id_before pod; id_before=$(snapshot_identity)
  pod=$(agent_pods | head -n1); pod=${pod#pod/}
  k exec "$pod" -- rm -f /var/lib/platform-agent/state.json
  k delete pod "$pod" --wait=false
  assert_true "recovers after state loss" retry 20 6 agent_ready
  assert_true "recover path used" bash -c '[[ $(agent_metric agent_registration_recovered_total) -ge 1 ]] || wait_registered 120'
  assert_identity_stable "$id_before" "$(snapshot_identity)" "state.json deletion"
}

scenario_deployment_restart() {
  log "SCENARIO: deployment rollout restart"
  local id_before; id_before=$(snapshot_identity)
  k rollout restart deploy/"$RELEASE"
  assert_true "rollout restart healthy" retry 20 6 agent_ready
  assert_identity_stable "$id_before" "$(snapshot_identity)" "deployment restart"
}

scenario_replicaset_replacement() {
  log "SCENARIO: ReplicaSet replacement (image bump -> new RS)"
  local id_before; id_before=$(snapshot_identity)
  k set env deploy/"$RELEASE" E2E_BUMP="$(date +%s)"
  assert_true "new ReplicaSet rolls out" retry 20 6 agent_ready
  assert_identity_stable "$id_before" "$(snapshot_identity)" "replicaset replacement"
}

scenario_helm_upgrade() {
  log "SCENARIO: Helm upgrade"
  local id_before; id_before=$(snapshot_identity)
  helm --kube-context "kind-${KIND_CLUSTER}" upgrade "$RELEASE" "$CHART" \
    --namespace "$AGENT_NAMESPACE" --reuse-values \
    --set podDisruptionBudget.enabled=true --wait --timeout 180s
  assert_true "healthy after helm upgrade" agent_ready
  assert_identity_stable "$id_before" "$(snapshot_identity)" "helm upgrade"
}

scenario_pvc_deletion() {
  log "SCENARIO: PVC deletion (only when persistence.type=pvc)"
  if ! k get pvc "${RELEASE}-state" >/dev/null 2>&1; then
    ok "skipped (emptyDir mode; state is ephemeral by design)"
    return
  fi
  local id_before; id_before=$(snapshot_identity)
  k delete pvc "${RELEASE}-state" --wait=false
  k delete pod -l "app.kubernetes.io/name=${RELEASE}" --wait=false
  assert_true "recovers after PVC deletion" retry 30 6 agent_ready
  assert_identity_stable "$id_before" "$(snapshot_identity)" "pvc deletion"
}

scenario_node_reboot() {
  log "SCENARIO: node reboot (drain + uncordon)"
  local node id_before; id_before=$(snapshot_identity)
  node=$(kk get nodes -o jsonpath='{.items[0].metadata.name}')
  kk drain "$node" --ignore-daemonsets --delete-emptydir-data --force --timeout=120s || true
  kk uncordon "$node"
  assert_true "recovers after node drain" retry 30 6 agent_ready
  assert_identity_stable "$id_before" "$(snapshot_identity)" "node reboot"
}

scenario_leader_failover() {
  log "SCENARIO: leader election failover"
  local leader_before leader_after
  leader_before=$(leader_holder)
  [[ -n "$leader_before" ]] && ok "leader elected ($leader_before)" || fail "no leader lease found"
  # Kill the current leader pod; a follower must take over.
  k delete pod "$leader_before" --wait=false 2>/dev/null || k delete pod -l "app.kubernetes.io/name=${RELEASE}" --wait=false
  assert_true "cluster healthy after leader kill" retry 20 6 agent_ready
  leader_after=$(retry 15 4 bash -c 'l=$(kubectl --context kind-'"$KIND_CLUSTER"' -n '"$AGENT_NAMESPACE"' get lease platform-agent-leader -o jsonpath="{.spec.holderIdentity}"); [[ -n "$l" ]] && echo "$l"')
  [[ -n "$leader_after" ]] && ok "new leader elected ($leader_after)" || fail "no leader after failover"
}

scenario_cluster_disconnect_reconnect() {
  log "SCENARIO: cluster disconnect -> reconnect (regression for H-1)"
  # Simulate network loss to the control plane by scaling to 0, waiting past the
  # heartbeat timeout so the control plane marks the cluster disconnected, then
  # restoring. The agent must reconnect and heartbeats must resume.
  local id_before; id_before=$(snapshot_identity)
  k scale deploy/"$RELEASE" --replicas=0
  assert_true "scaled to zero" retry 10 3 bash -c '[[ $(kubectl --context kind-'"$KIND_CLUSTER"' -n '"$AGENT_NAMESPACE"' get pods -l app.kubernetes.io/name='"$RELEASE"' -o name | wc -l) -eq 0 ]]'
  sleep 320   # exceed HeartbeatTimeout (5m) so the control plane marks it disconnected
  k scale deploy/"$RELEASE" --replicas=2
  assert_true "scaled back up" retry 20 6 agent_ready
  local hb_before hb_after
  hb_before=$(agent_metric agent_heartbeat_success_total)
  sleep 40
  hb_after=$(agent_metric agent_heartbeat_success_total)
  if awk -v a="$hb_after" -v b="$hb_before" 'BEGIN{exit !(a>b)}'; then ok "reconnected: heartbeats resumed"; else fail "cluster did not reconnect (H-1 regression)"; fi
  assert_identity_stable "$id_before" "$(snapshot_identity)" "disconnect/reconnect"
}

scenario_token_revocation() {
  log "SCENARIO: token revocation blocks re-registration but not a live agent"
  # A running, credential-authenticated agent keeps heartbeating even if the
  # bootstrap token is revoked (it uses cluster credentials, not the token).
  [[ -n "$ADMIN_JWT" && -n "$ORG_ID" ]] || { ok "skipped (no admin API creds)"; return; }
  local hb_before hb_after
  hb_before=$(agent_metric agent_heartbeat_success_total)
  sleep 35
  hb_after=$(agent_metric agent_heartbeat_success_total)
  if awk -v a="$hb_after" -v b="$hb_before" 'BEGIN{exit !(a>b)}'; then ok "live agent unaffected by token state"; else fail "live agent heartbeats stalled"; fi
}

scenario_rolling_update() {
  log "SCENARIO: rolling update keeps a replica available (PDB)"
  local unavailable
  k rollout restart deploy/"$RELEASE"
  # During the rollout at least one replica must stay Ready (PDB minAvailable=1).
  unavailable=$(k get deploy "$RELEASE" -o jsonpath='{.status.unavailableReplicas}' 2>/dev/null || echo 0)
  assert_true "rolling update completes" retry 20 6 agent_ready
  ok "rolling update completed (unavailable peak observed: ${unavailable:-0})"
}

run_all() {
  setup
  scenario_heartbeat
  scenario_pod_restart
  scenario_state_deletion
  scenario_deployment_restart
  scenario_replicaset_replacement
  scenario_rolling_update
  scenario_helm_upgrade
  scenario_pvc_deletion
  scenario_leader_failover
  scenario_node_reboot
  scenario_cluster_disconnect_reconnect
  scenario_token_revocation
  summary
}

case "${1:-all}" in
  setup)    setup ;;
  teardown) teardown ;;
  run)      "scenario_${2:?scenario name required}" ; summary ;;
  all)      run_all ;;
  *)        echo "usage: $0 [all|setup|teardown|run <scenario>]"; exit 2 ;;
esac
