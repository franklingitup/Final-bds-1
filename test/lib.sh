#!/usr/bin/env bash
# Shared helpers for the Platform Agent production-certification test suites
# (e2e, chaos, soak, upgrade). Source this file: `source "$(dirname "$0")/../lib.sh"`.
#
# Configuration is via environment variables (see defaults below) so the same
# scripts run locally (kind + docker compose) and in CI.

set -euo pipefail

# ---- configuration --------------------------------------------------------
: "${KIND_CLUSTER:=agent-e2e}"
: "${AGENT_NAMESPACE:=platform-agent}"
: "${RELEASE:=bds-agent}"
: "${CHART:=helm/agent}"
: "${AGENT_IMAGE:=platform-agent:e2e}"
: "${CONTROL_PLANE_URL:=http://host.docker.internal:8080}"   # reachable from inside kind
: "${GATEWAY_URL:=http://localhost:8080}"                    # reachable from the host
# Admin credentials used to provision a cluster + registration token via the API.
: "${ADMIN_JWT:=}"
: "${ORG_ID:=}"

# ---- logging & assertions -------------------------------------------------
_c_red=$'\e[31m'; _c_grn=$'\e[32m'; _c_ylw=$'\e[33m'; _c_rst=$'\e[0m'
PASS_COUNT=0; FAIL_COUNT=0; FAILURES=()

log()  { printf '%s[e2e]%s %s\n' "$_c_ylw" "$_c_rst" "$*"; }
ok()   { printf '%s[PASS]%s %s\n' "$_c_grn" "$_c_rst" "$*"; PASS_COUNT=$((PASS_COUNT+1)); }
fail() { printf '%s[FAIL]%s %s\n' "$_c_red" "$_c_rst" "$*"; FAIL_COUNT=$((FAIL_COUNT+1)); FAILURES+=("$*"); }

# assert_eq <actual> <expected> <message>
assert_eq() { if [[ "$1" == "$2" ]]; then ok "$3 ($1)"; else fail "$3: got '$1' want '$2'"; fi; }
# assert_contains <haystack> <needle> <message>
assert_contains() { if [[ "$1" == *"$2"* ]]; then ok "$3"; else fail "$3: '$2' not in output"; fi; }
# assert_true <cmd...> — passes if cmd exits 0
assert_true() { local msg="$1"; shift; if "$@" >/dev/null 2>&1; then ok "$msg"; else fail "$msg"; fi; }

# retry <attempts> <sleep_sec> <cmd...>
retry() {
  local attempts=$1 delay=$2; shift 2
  local i
  for ((i=1; i<=attempts; i++)); do
    if "$@"; then return 0; fi
    sleep "$delay"
  done
  return 1
}

# ---- kubectl / helm wrappers ----------------------------------------------
k()  { kubectl --context "kind-${KIND_CLUSTER}" -n "$AGENT_NAMESPACE" "$@"; }
kk() { kubectl --context "kind-${KIND_CLUSTER}" "$@"; }

agent_pods()      { k get pods -l "app.kubernetes.io/name=${RELEASE}" -o name; }
agent_ready()     { k rollout status deploy/"$RELEASE" --timeout=120s; }
leader_holder()   { k get lease platform-agent-leader -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || true; }

# wait_registered <timeout_sec> — waits until at least one agent reports registered.
wait_registered() {
  local timeout=${1:-120} start; start=$(date +%s)
  while true; do
    if k logs -l "app.kubernetes.io/name=${RELEASE}" --tail=200 2>/dev/null | grep -qiE 'registration established|already registered'; then
      return 0
    fi
    (( $(date +%s) - start > timeout )) && return 1
    sleep 3
  done
}

# agent_metric <metric_name> — sums a counter across replicas via /metrics.
# Uses kubectl port-forward + curl from the host so it works against distroless
# agent images (no shell/wget/curl inside the container).
agent_metric() {
  local metric=$1 total=0 pod port=19191
  for pod in $(agent_pods); do
    pod=${pod#pod/}
    k port-forward "pod/${pod}" "${port}:8080" >/dev/null 2>&1 &
    local pf=$!
    sleep 1
    local val
    val=$(curl -sS "http://localhost:${port}/metrics" 2>/dev/null \
      | awk -v m="$metric" '$1==m {sum+=$2} END{print sum+0}')
    kill "$pf" 2>/dev/null || true
    wait "$pf" 2>/dev/null || true
    total=$(awk -v a="$total" -v b="${val:-0}" 'BEGIN{print a+b}')
    port=$((port+1))
  done
  echo "$total"
}

# Authoritative identity/status come from the control-plane API (image-agnostic;
# does not depend on reading state.json inside a distroless container). Requires
# ADMIN_JWT + ORG_ID + a captured CLUSTER_ID.
api_agent_id()       { api GET "/v1/organizations/${ORG_ID}/clusters/${CLUSTER_ID}" | jq -r '.agentId // empty'; }
api_cluster_status() { api GET "/v1/organizations/${ORG_ID}/clusters/${CLUSTER_ID}" | jq -r '.status // empty'; }

# ---- control-plane API helpers --------------------------------------------
api() { # api <method> <path> [json-body]
  local method=$1 path=$2 body=${3:-}
  local args=(-sS -X "$method" -H "Authorization: Bearer ${ADMIN_JWT}" -H "Content-Type: application/json")
  [[ -n "$body" ]] && args+=(-d "$body")
  curl "${args[@]}" "${GATEWAY_URL}${path}"
}

# create_cluster_and_token <name> <slug> -> prints the registration token and
# exports CLUSTER_ID for later identity/status assertions. Requires ADMIN_JWT
# and ORG_ID. Uses the documented cluster + token API.
create_cluster_and_token() {
  local name=$1 slug=$2
  local cluster token
  cluster=$(api POST "/v1/organizations/${ORG_ID}/clusters" "{\"name\":\"${name}\",\"slug\":\"${slug}\"}")
  CLUSTER_ID=$(echo "$cluster" | jq -r '.id')
  export CLUSTER_ID
  token=$(api POST "/v1/organizations/${ORG_ID}/clusters/${CLUSTER_ID}/tokens" '{}')
  echo "$token" | jq -r '.token'
}

# ---- summary --------------------------------------------------------------
summary() {
  echo
  log "Results: ${PASS_COUNT} passed, ${FAIL_COUNT} failed"
  if (( FAIL_COUNT > 0 )); then
    printf '%s\n' "${FAILURES[@]}"
    return 1
  fi
  return 0
}
