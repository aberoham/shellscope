#!/usr/bin/env bash
# Bring up the terminal-bench-teleport fixture cluster + base image.
# Idempotent. Run as `harnesses/terminal-bench-teleport/bringup.sh`
# from anywhere; paths are resolved relative to this script's location.
set -euo pipefail

FIXTURE="$(cd "$(dirname "$0")" && pwd)"

echo "=== Step 1: Cluster up ==="
"$FIXTURE/cluster/up.sh"

echo
echo "=== Step 2: Bootstrap (role/user/token/identity) ==="
"$FIXTURE/cluster/bootstrap.sh"

echo
echo "=== Step 3: Build base image ==="
"$FIXTURE/image/build.sh"

echo
echo "=== Bring-up complete ==="
echo "Cluster: docker ps --filter name=tbench-fixture-auth"
echo "Identity: harnesses/terminal-bench-teleport/cluster/.agent-identity"
echo "Token:    harnesses/terminal-bench-teleport/cluster/.node-join-token"
echo "Image:    docker image inspect tbench-teleport-base:v17.7.20"
