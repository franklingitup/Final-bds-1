#!/usr/bin/env bash
# Start local development dependencies.
set -euo pipefail
cd "$(dirname "$0")/../.."

if [ ! -f .env ]; then
  echo "==> creating .env from .env.example"
  cp .env.example .env
fi

echo "==> starting local stack"
docker compose up -d

echo "==> waiting for postgres"
until docker compose exec -T postgres pg_isready -U platform >/dev/null 2>&1; do
  sleep 1
done

echo "==> local stack ready"
echo "    grafana:    http://localhost:3001"
echo "    prometheus: http://localhost:9090"
echo "    minio:      http://localhost:9001"
