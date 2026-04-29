#!/usr/bin/env bash
# Tear down the test cluster. Use --wipe to also drop persisted state.
set -euo pipefail

cluster_dir="$(cd "$(dirname "$0")" && pwd)"
cd "$cluster_dir"

docker compose down

if [[ "${1:-}" == "--wipe" ]]; then
  echo "Wiping cluster state..."
  rm -rf data .node-join-token .agent-identity
fi
