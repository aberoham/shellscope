#!/usr/bin/env bash
# Run a single Terminal-Bench task end-to-end through the teleport fixture.
# Usage:
#   harnesses/terminal-bench-teleport/runtask.sh <task-id> <model>
# Defaults to hello-world + moonshot/moonshot-v1-8k.
set -euo pipefail

TASK_ID="${1:-hello-world}"
MODEL="${2:-moonshot/moonshot-v1-8k}"

cd "$(dirname "$0")/../.."  # whodrove/

# Pull MOONSHOT_API_KEY from terminal-bench's .env (the user keeps keys
# there). Export only what's actually present so we don't blow up on a
# missing variable.
ENV_FILE="harnesses/terminal-bench/.env"
if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

# LiteLLM defaults moonshot/* to the .cn endpoint; pin to .ai so the
# user's MOONSHOT_API_KEY works. Matches harnesses/terminal-bench/tmp/run_tb.sh.
if [[ "$MODEL" == moonshot/* ]] && [[ -z "${MOONSHOT_API_BASE:-}" ]]; then
  export MOONSHOT_API_BASE="https://api.moonshot.ai/v1"
fi

# vertex_ai/* uses Google Cloud ADC; require a project + region so
# LiteLLM can resolve the publisher model. us-east5 is the primary
# Claude-on-Vertex region; override with TBENCH_VERTEX_LOCATION if you
# have the model deployed elsewhere.
if [[ "$MODEL" == vertex_ai/* ]]; then
  export VERTEXAI_PROJECT="${VERTEXAI_PROJECT:-thg-dev-vertexai-execs}"
  export VERTEXAI_LOCATION="${VERTEXAI_LOCATION:-${TBENCH_VERTEX_LOCATION:-us-east5}}"
fi

# `--with-editable` over `--with terminal-bench` so we pick up the local
# submodule patches (notably the moonshot/* temperature=1 override that
# the published PyPI build is missing).
#
# google-cloud-aiplatform is only loaded by LiteLLM when a vertex_ai/*
# model is requested, but is needed at import time for Vertex calls and
# isn't a transitive dep of terminal-bench. Adding it unconditionally
# avoids a "No module named 'vertexai'" surprise for Vertex runs.
uv run \
  --with-editable ./harnesses/terminal-bench \
  --with pyyaml --with pexpect \
  --with 'google-cloud-aiplatform>=1.38' \
  python harnesses/terminal-bench-teleport/runner/run.py \
    --task-id "$TASK_ID" \
    --agent terminus \
    --model "$MODEL"
