#!/usr/bin/env bash
# Bootstraps an admin JWT + organization ID for the E2E/chaos suites and exports
# them (to $GITHUB_ENV in CI, or prints `export` lines locally).
#
# If ADMIN_JWT and ORG_ID are already set, it just re-exports them. Otherwise it
# signs up + logs in against the gateway and resolves the caller's org.
#
#   source ./test/scripts/bootstrap-control-plane.sh
#   # or in CI:  ./test/scripts/bootstrap-control-plane.sh   (writes $GITHUB_ENV)

set -euo pipefail

: "${GATEWAY_URL:=http://localhost:8080}"
: "${ADMIN_EMAIL:=e2e-admin+$(date +%s)@example.com}"
: "${ADMIN_PASSWORD:=E2eP@ssw0rd!$(date +%s)}"

emit() {
  local k=$1 v=$2
  if [[ -n "${GITHUB_ENV:-}" ]]; then echo "${k}=${v}" >>"$GITHUB_ENV"; fi
  echo "export ${k}=${v}"
  export "${k}=${v}"
}

if [[ -n "${ADMIN_JWT:-}" && -n "${ORG_ID:-}" ]]; then
  emit ADMIN_JWT "$ADMIN_JWT"; emit ORG_ID "$ORG_ID"
  exit 0
fi

jqf() { jq -r "$1 // empty"; }

echo "signing up ${ADMIN_EMAIL}"
signup=$(curl -sS -X POST "${GATEWAY_URL}/v1/auth/signup" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"${ADMIN_EMAIL}\",\"password\":\"${ADMIN_PASSWORD}\",\"name\":\"E2E Admin\"}" || true)

login=$(curl -sS -X POST "${GATEWAY_URL}/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"${ADMIN_EMAIL}\",\"password\":\"${ADMIN_PASSWORD}\"}")

# Token field name varies by build; try the common shapes.
token=$(echo "$login" | jqf '.accessToken')
[[ -z "$token" ]] && token=$(echo "$login" | jqf '.token')
[[ -z "$token" ]] && token=$(echo "$login" | jqf '.access_token')
[[ -z "$token" ]] && token=$(echo "$signup" | jqf '.accessToken')
[[ -n "$token" ]] || { echo "could not extract access token from login response: $login" >&2; exit 1; }

# Resolve the caller's organization from /v1/auth/me.
me=$(curl -sS "${GATEWAY_URL}/v1/auth/me" -H "Authorization: Bearer ${token}")
org=$(echo "$me" | jqf '.organizations[0].id')
[[ -z "$org" ]] && org=$(echo "$me" | jqf '.orgId')
[[ -z "$org" ]] && org=$(echo "$me" | jqf '.defaultOrgId')
[[ -n "$org" ]] || { echo "could not resolve ORG_ID from /v1/auth/me: $me" >&2; exit 1; }

emit ADMIN_JWT "$token"
emit ORG_ID "$org"
