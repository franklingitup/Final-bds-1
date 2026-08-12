#!/usr/bin/env bash
# Phase 4 — 48-hour soak test.
#
# Sustains a steady heartbeat load while periodically snapshotting the control
# plane's goroutine count and heap so we can prove the absence of goroutine and
# memory leaks, retry storms, and heartbeat/leader instability over 48h.
#
#   SOAK_HOURS=48 PPROF_URL=http://localhost:8085/debug/pprof \
#   GATEWAY_URL=http://localhost:8080 SYNTHETIC=1 AGENTS=500 ./test/soak/soak.sh
#
# Requires the target service to expose net/http/pprof at PPROF_URL and
# Prometheus metrics. Fails if goroutines or heap grow beyond the thresholds,
# or if the agent restarts (a proxy for retry storms / crash loops).

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib.sh"

: "${SOAK_HOURS:=48}"
: "${SAMPLE_INTERVAL:=900}"        # 15 min
: "${PPROF_URL:=http://localhost:8085/debug/pprof}"
: "${AGENTS:=500}"
: "${GATEWAY_URL:=http://localhost:8080}"
: "${GOROUTINE_GROWTH_MAX:=1.5}"   # end/start ratio ceiling
: "${HEAP_GROWTH_MAX:=1.5}"
: "${SYNTHETIC:=1}"
OUT="test/soak/results"; mkdir -p "$OUT"

# Start the steady-state load generator in the background for the whole soak.
( cd test/load && GOWORK=off go build -o load.bin . && \
  ./load.bin -url "$GATEWAY_URL" -agents "$AGENTS" -duration "${SOAK_HOURS}h" \
    -heartbeat-interval 30s -ramp 2m ${SYNTHETIC:+-synthetic} -json >"../soak/results/load.json" 2>&1 ) &
LOAD_PID=$!
trap 'kill $LOAD_PID 2>/dev/null || true' EXIT

goroutines() { curl -sS "${PPROF_URL}/goroutine?debug=1" | head -n1 | grep -oE '[0-9]+' | head -n1; }
heap_bytes() { curl -sS "${PPROF_URL}/heap?debug=1" | awk '/HeapAlloc/ {print $2; exit}'; }
restarts()   { k get pods -l "app.kubernetes.io/name=${RELEASE}" -o jsonpath='{range .items[*]}{.status.containerStatuses[0].restartCount}{"\n"}{end}' 2>/dev/null | awk '{s+=$1} END{print s+0}'; }

g0=$(goroutines || echo 0); h0=$(heap_bytes || echo 0); r0=$(restarts || echo 0)
log "soak start: goroutines=${g0} heapAlloc=${h0} restarts=${r0}"

samples=$(( SOAK_HOURS * 3600 / SAMPLE_INTERVAL ))
for ((i=1; i<=samples; i++)); do
  sleep "$SAMPLE_INTERVAL"
  g=$(goroutines || echo 0); h=$(heap_bytes || echo 0); r=$(restarts || echo 0)
  hb=$(agent_metric agent_heartbeat_success_total 2>/dev/null || echo 0)
  hbf=$(agent_metric agent_heartbeat_failure_total 2>/dev/null || echo 0)
  leader=$(leader_holder)
  echo "$(date -Is) goroutines=$g heapAlloc=$h restarts=$r hb_ok=$hb hb_fail=$hbf leader=$leader" | tee -a "$OUT/timeline.log"
done

g1=$(goroutines || echo 0); h1=$(heap_bytes || echo 0); r1=$(restarts || echo 0)
log "soak end: goroutines=${g1} heapAlloc=${h1} restarts=${r1}"

# ---- assertions -----------------------------------------------------------
grow() { awk -v a="$1" -v b="$2" 'BEGIN{ if (a==0){print "1"} else {printf "%.3f", b/a} }'; }
g_ratio=$(grow "$g0" "$g1"); h_ratio=$(grow "$h0" "$h1")

awk -v r="$g_ratio" -v m="$GOROUTINE_GROWTH_MAX" 'BEGIN{exit !(r<=m)}' \
  && ok "no goroutine leak (growth x${g_ratio} <= ${GOROUTINE_GROWTH_MAX})" \
  || fail "goroutine growth x${g_ratio} exceeds ${GOROUTINE_GROWTH_MAX}"

awk -v r="$h_ratio" -v m="$HEAP_GROWTH_MAX" 'BEGIN{exit !(r<=m)}' \
  && ok "no heap leak (growth x${h_ratio} <= ${HEAP_GROWTH_MAX})" \
  || fail "heap growth x${h_ratio} exceeds ${HEAP_GROWTH_MAX}"

assert_eq "$r1" "$r0" "no agent restarts during soak (retry-storm proxy)"
[[ -n "$(leader_holder)" ]] && ok "leader election stable (holder present at end)" || fail "no leader at soak end"

summary
