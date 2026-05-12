#!/usr/bin/env bash
# Convert a Teleport <sid>.tar recording into an asciinema .cast by
# replaying via `tsh play` and capturing with `asciinema rec`.
#
# Why this exists: `harnesses/terminal-bench-teleport/human-runtask.sh`
# runs `tsh ssh -t` directly (no harness, no tmux, no asciinema), so
# human sessions only produce a Teleport .tar — no .cast. This rebuilds
# one post-hoc so the human run can go through the same cast-to-slides.sh
# path as the agent runs.
#
# Usage:
#   tar-to-cast.sh <sid.tar>             # writes <sid>.cast next to the tar
#   tar-to-cast.sh <sid.tar> human.cast  # explicit output path
#
# Note: `tsh play` replays in real time, so this command takes about as
# long as the original session did. Pipe straight into cast-to-slides.sh
# after — that script's `speed` arg fast-forwards for slides.

set -euo pipefail

TAR="${1:?usage: $0 <sid.tar> [out.cast]}"
OUT="${2:-${TAR%.tar}.cast}"
SID="$(basename "$TAR" .tar)"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IDENTITY="$REPO_ROOT/harnesses/terminal-bench-teleport/cluster/.agent-identity"

if ! command -v asciinema >/dev/null 2>&1; then
  echo "[tar-to-cast] installing asciinema via brew"
  brew install asciinema
fi
if ! command -v tsh >/dev/null 2>&1; then
  echo "[tar-to-cast] tsh missing — brew install teleport@17"
  exit 1
fi

echo "[tar-to-cast] sid=$SID out=$OUT (real-time replay; may take a while)"
exec asciinema rec --overwrite --command \
  "tsh --insecure --skip-version-check --identity=$IDENTITY --proxy=localhost:3080 play $SID" \
  "$OUT"
