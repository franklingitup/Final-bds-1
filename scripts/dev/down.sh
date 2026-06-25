#!/usr/bin/env bash
# Stop local development dependencies and remove volumes.
set -euo pipefail
cd "$(dirname "$0")/../.."
docker compose down -v
echo "==> local stack stopped"
