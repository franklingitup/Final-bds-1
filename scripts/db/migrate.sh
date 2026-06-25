#!/usr/bin/env bash
# Apply per-service migrations using golang-migrate.
# Usage: scripts/db/migrate.sh <service> [up|down]
set -euo pipefail
cd "$(dirname "$0")/../.."

SERVICE="${1:?service name required}"
DIRECTION="${2:-up}"
DB_URL="${DATABASE_URL:-postgres://platform:platform@localhost:5432/platform?sslmode=disable}"
MIGRATIONS_DIR="backend/migrations/${SERVICE}"

if [ ! -d "$MIGRATIONS_DIR" ]; then
  echo "no migrations directory: $MIGRATIONS_DIR" >&2
  exit 1
fi

# Requires the `migrate` CLI: https://github.com/golang-migrate/migrate
migrate -path "$MIGRATIONS_DIR" -database "$DB_URL" "$DIRECTION"
