#!/usr/bin/env bash
# Build the base image used by patched Terminal-Bench task images.
#
# Usage:  ./image/build.sh
# Idempotent. Rebuild after editing entrypoint.sh.
set -euo pipefail

image_dir="$(cd "$(dirname "$0")" && pwd)"
cd "$image_dir"

docker build -t tbench-teleport-base:v17.7.20 -f Dockerfile.base .
docker tag tbench-teleport-base:v17.7.20 tbench-teleport-base:latest
echo "Built tbench-teleport-base:v17.7.20 (also tagged :latest)"
