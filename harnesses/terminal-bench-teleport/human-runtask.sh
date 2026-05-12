#!/usr/bin/env bash
# Bring up a Terminal-Bench task container, joined to the local Teleport
# fixture cluster, and drop the user into an interactive `tsh ssh -t`
# session for a HUMAN run. Everything that happens inside the shell is
# captured as a Teleport <sid>.tar recording, just like an agent run.
#
# Usage:
#   harnesses/terminal-bench-teleport/human-runtask.sh <task-id>
#   harnesses/terminal-bench-teleport/human-runtask.sh vulnerable-secret
#
# When you exit the shell (Ctrl-D), the script tears the task container
# down. The recording lives in
#   harnesses/terminal-bench-teleport/cluster/data/log/records/<sid>.tar
# and the patched task dir stays at
#   harnesses/terminal-bench-teleport/runs/human-<ts>/<task-id>/
# for harvesting / labelling.

set -euo pipefail

TASK_ID="${1:?usage: $0 <task-id> [login-user]}"
LOGIN_USER="${2:-root}"  # `agent` to mimic the AI runs; `root` to actually solve security tasks
FIXTURE="$(cd "$(dirname "$0")" && pwd)"  # whodrove/harnesses/terminal-bench-teleport
REPO_ROOT="$(cd "$FIXTURE/../.." && pwd)"  # whodrove/
ORIG_TASK="$REPO_ROOT/harnesses/terminal-bench/original-tasks/$TASK_ID"

if [[ ! -d "$ORIG_TASK" ]]; then
  echo "no such task: $ORIG_TASK" >&2
  exit 1
fi

if [[ ! -f "$FIXTURE/cluster/.agent-identity" || ! -f "$FIXTURE/cluster/.node-join-token" ]]; then
  echo "cluster not bootstrapped — run cluster/up.sh + cluster/bootstrap.sh first" >&2
  exit 1
fi

TS="$(date +%Y%m%d-%H%M%S)"
RUN_ID="human-$TS"
CNAME="human-${TASK_ID}-${TS}"

# Sanitize CNAME the same way image/entrypoint.sh does — this is the
# nodename `tsh ssh` will resolve.
NODENAME="$(echo "$CNAME" | tr '[:upper:]_' '[:lower:]-' | tr -cs 'a-z0-9-' '-' | sed 's/-\+/-/g; s/^-//; s/-$//')"

# Patch the task into runs/<run-id>/<task-id>/ — rewrites Dockerfile FROM
# and injects Teleport env / shared network into the compose.
PATCHED="$(cd "$REPO_ROOT" && uv run --with pyyaml python \
  "$FIXTURE/runner/patch_task.py" "$ORIG_TASK" \
  --out-root "$FIXTURE/runs" --run-id "$RUN_ID")"

# Logs dirs the original compose mount-binds (harmless empty dirs are fine).
LOGS_DIR="$PATCHED/logs"
AGENT_LOGS_DIR="$PATCHED/agent-logs"
mkdir -p "$LOGS_DIR" "$AGENT_LOGS_DIR"

JOIN_TOKEN="$(cat "$FIXTURE/cluster/.node-join-token")"

# All env vars the patched compose references.
export T_BENCH_TASK_DOCKER_CLIENT_IMAGE_NAME="tbench-task-${TASK_ID}:${RUN_ID}"
export T_BENCH_TASK_DOCKER_CLIENT_CONTAINER_NAME="$CNAME"
export T_BENCH_TEST_DIR="/tests"
export T_BENCH_TASK_LOGS_PATH="$LOGS_DIR"
export T_BENCH_CONTAINER_LOGS_PATH="/var/log/tbench"
export T_BENCH_TASK_AGENT_LOGS_PATH="$AGENT_LOGS_DIR"
export T_BENCH_CONTAINER_AGENT_LOGS_PATH="/var/log/tbench-agent"
export TELEPORT_PROXY_ADDR="tbench-fixture-auth:3025"
export TELEPORT_JOIN_TOKEN="$JOIN_TOKEN"
export TELEPORT_NODE_LABELS="operator.type=human,fixture.source=terminal-bench,fixture.task_id=${TASK_ID},fixture.harness_run_id=${RUN_ID}"

cleanup() {
  echo
  echo "[human-runtask] tearing down $CNAME"
  (cd "$PATCHED" && docker compose down --remove-orphans) || true
}
trap cleanup EXIT INT TERM

echo "[human-runtask] task=$TASK_ID run=$RUN_ID container=$CNAME node=$NODENAME login=$LOGIN_USER"
echo "[human-runtask] bringing up patched container (this builds the image on first run)"
(cd "$PATCHED" && docker compose up -d --build)

echo "[human-runtask] waiting for Teleport node '$NODENAME' to register..."
IDENTITY="$FIXTURE/cluster/.agent-identity"
for i in {1..30}; do
  if tsh --insecure --skip-version-check \
       --identity="$IDENTITY" --proxy=localhost:3080 \
       ls 2>/dev/null | grep -q "^$NODENAME "; then
    echo "[human-runtask] node up — connecting"
    break
  fi
  sleep 1
done

echo "[human-runtask] === human session begins — your keystrokes are being recorded by Teleport ==="
echo "[human-runtask] task instructions:"
sed -n '/^instruction:/,/^[a-z_]\+:/p' "$ORIG_TASK/task.yaml" | sed '$d' | sed 's/^/    /'
echo
echo "[human-runtask] when you're done, exit the shell (Ctrl-D) to finalize the recording"
echo

exec tsh --insecure --skip-version-check \
  --identity="$IDENTITY" --proxy=localhost:3080 \
  ssh -t "$LOGIN_USER@$NODENAME"
