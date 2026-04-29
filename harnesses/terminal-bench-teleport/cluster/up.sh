#!/usr/bin/env bash
# Bring up the OSS Teleport test cluster on the local Docker engine
# (Colima on macOS). Idempotent.
set -euo pipefail

cluster_dir="$(cd "$(dirname "$0")" && pwd)"
cd "$cluster_dir"

mkdir -p data

if ! docker info >/dev/null 2>&1; then
  echo "ERROR: Docker engine not reachable. Start Colima first: colima start" >&2
  exit 1
fi

docker compose up -d

echo "Waiting for auth service to come up..."
for i in {1..60}; do
  if docker compose exec -T teleport-auth tctl status >/dev/null 2>&1; then
    echo "Cluster up."
    docker compose exec -T teleport-auth tctl status
    exit 0
  fi
  sleep 2
done

echo "ERROR: auth service did not come up within 120s." >&2
docker compose logs --tail=50 teleport-auth >&2
exit 1
