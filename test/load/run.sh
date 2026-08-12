#!/usr/bin/env bash
# Phase 3 — Load test driver. Runs the stdlib load generator at the standard
# fleet sizes (100/500/1000/5000 agents) and records latency percentiles,
# throughput, and error rate, alongside control-plane CPU/memory samples.
#
#   GATEWAY_URL=http://localhost:8080 CREDS=creds.csv ./test/load/run.sh
#   # or, without pre-provisioned clusters (characterizes latency only):
#   SYNTHETIC=1 ./test/load/run.sh
#
# Results are written to test/load/results/<size>-agents.json.

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

: "${GATEWAY_URL:=http://localhost:8080}"
: "${DURATION:=5m}"
: "${HEARTBEAT_INTERVAL:=30s}"
: "${MAX_ERROR_RATE:=0.01}"
: "${SIZES:=100 500 1000 5000}"
: "${CREDS:=}"
: "${SYNTHETIC:=}"
: "${COMPOSE_FILE:=}"

mkdir -p results
GOWORK=off go build -o load.bin .

cred_args() {
  if [[ -n "$CREDS" ]]; then echo "-creds $CREDS"; else echo "-synthetic"; fi
}

sample_backend() { # background CPU/mem sampler via docker stats
  [[ -n "$COMPOSE_FILE" ]] || return 0
  local out=$1
  ( while :; do
      docker stats --no-stream --format '{{.Name}} {{.CPUPerc}} {{.MemUsage}}' 2>/dev/null >>"$out"
      echo "---" >>"$out"; sleep 15
    done ) &
  echo $!
}

for size in $SIZES; do
  echo "=== ${size} agents ==="
  stats_file="results/${size}-agents.resources.txt"; : >"$stats_file"
  sampler_pid=$(sample_backend "$stats_file" || true)

  # shellcheck disable=SC2046
  ./load.bin -url "$GATEWAY_URL" -agents "$size" -duration "$DURATION" \
    -heartbeat-interval "$HEARTBEAT_INTERVAL" -ramp 30s \
    -max-error-rate "$MAX_ERROR_RATE" -json $(cred_args) \
    | tee "results/${size}-agents.json" || echo "size ${size} exceeded error budget"

  [[ -n "${sampler_pid:-}" ]] && kill "$sampler_pid" 2>/dev/null || true
  echo
done

echo "Summary (p95/p99 ms, error rate):"
for size in $SIZES; do
  f="results/${size}-agents.json"
  [[ -f "$f" ]] || continue
  jq -r --arg n "$size" '"agents=\($n)  register p95=\(.register.p95Ms) p99=\(.register.p99Ms)  heartbeat p95=\(.heartbeat.p95Ms) p99=\(.heartbeat.p99Ms)  err=\(.overallErrorRate)"' "$f"
done
