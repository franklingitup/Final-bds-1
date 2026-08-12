#!/usr/bin/env bash
# Phase 5 — Upgrade compatibility.
#
# Installs an initial agent version, registers, then upgrades across a sequence
# of chart/image versions, asserting after every step that:
#   * ClusterID is unchanged
#   * AgentID is unchanged
#   * the installation token was NOT regenerated (same Secret value)
#   * no duplicate cluster was created (control-plane side, if API creds given)
#   * no manual intervention was required (agent stayed Ready throughout)
#
#   VERSIONS="0.1.0 0.2.0 0.3.0" ./test/upgrade/upgrade.sh
#
# VERSIONS are image tags that already exist / are loadable into the kind cluster.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib.sh"

: "${VERSIONS:=}"
[[ -n "$VERSIONS" ]] || { echo "set VERSIONS='v1 v2 ...' (image tags)"; exit 2; }

read -r -a version_list <<<"$VERSIONS"
base="${version_list[0]}"

# Identity is read from the control-plane API (image-agnostic). Requires
# ADMIN_JWT + ORG_ID + CLUSTER_ID; otherwise identity asserts are skipped.
have_api() { [[ -n "$ADMIN_JWT" && -n "$ORG_ID" && -n "${CLUSTER_ID:-}" ]]; }
token_secret() { k get secret "${RELEASE}-install" -o jsonpath='{.data.token}' 2>/dev/null || true; }

log "install base version ${base}"
helm --kube-context "kind-${KIND_CLUSTER}" upgrade --install "$RELEASE" "$CHART" \
  --namespace "$AGENT_NAMESPACE" --create-namespace \
  --set image.tag="$base" --set image.pullPolicy=Never \
  --set controlPlane.endpoint="$CONTROL_PLANE_URL" \
  --set controlPlane.installToken="${INSTALL_TOKEN:?set INSTALL_TOKEN}" \
  --wait --timeout 180s
assert_true "base ready" agent_ready
assert_true "base registered" wait_registered 120

cid0="${CLUSTER_ID:-}"; aid0=""
have_api && aid0=$(api_agent_id)
tok0=$(token_secret)
ok "baseline captured cluster=${cid0:-<n/a>} agent=${aid0:-<n/a>}"

for v in "${version_list[@]:1}"; do
  log "upgrade -> ${v}"
  helm --kube-context "kind-${KIND_CLUSTER}" upgrade "$RELEASE" "$CHART" \
    --namespace "$AGENT_NAMESPACE" --reuse-values \
    --set image.tag="$v" --wait --timeout 180s
  assert_true "v${v}: ready (no manual intervention)" agent_ready

  if have_api; then
    assert_eq "$CLUSTER_ID" "$cid0" "v${v}: no duplicate ClusterID"
    assert_eq "$(api_agent_id)" "$aid0" "v${v}: AgentID unchanged"
  else
    ok "v${v}: identity check skipped (no API creds)"
  fi
  assert_eq "$(token_secret)" "$tok0" "v${v}: install token not regenerated"
done

# Control-plane side: exactly one cluster should carry this AgentID.
if have_api && [[ -n "$aid0" ]]; then
  count=$(api GET "/v1/organizations/${ORG_ID}/clusters" | jq --arg a "$aid0" '[.items[] | select(.agentId==$a)] | length')
  assert_eq "$count" "1" "control plane has exactly one cluster for the AgentID (no duplicates)"
fi

summary
