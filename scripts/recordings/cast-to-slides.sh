#!/usr/bin/env bash
# Convert an asciinema .cast file into an animation suitable for embedding
# in Google Slides. GIF for short/small clips (auto-plays when inserted as
# an image), MP4 for anything over ~10–15s (upload to Drive, then Insert >
# Video > Google Drive).
#
# Usage:
#   cast-to-slides.sh <path/to/agent.cast> [out-stem] [speed]
#     speed: playback speedup factor (default 2; "1" = realtime, "3" = 3x faster)
#
# Examples:
#   cast-to-slides.sh whodrove/runs/.../sessions/agent.cast
#   cast-to-slides.sh whodrove/runs/.../sessions/agent.cast kimi-vulnsecret 3
#
# Output (next to the cast by default):
#   <stem>.gif
#   <stem>.mp4

set -euo pipefail

CAST="${1:?usage: $0 <agent.cast> [out-stem] [speed]}"
STEM="${2:-$(basename "$CAST" .cast)}"
SPEED="${3:-2}"

OUTDIR="$(dirname "$CAST")"
GIF="$OUTDIR/${STEM}.gif"
MP4="$OUTDIR/${STEM}.mp4"

if ! command -v agg >/dev/null 2>&1; then
  echo "[cast-to-slides] installing agg via brew (asciinema's gif renderer)"
  brew install agg
fi
if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "[cast-to-slides] installing ffmpeg via brew"
  brew install ffmpeg
fi

echo "[cast-to-slides] rendering GIF at ${SPEED}x speed: $GIF"
agg --speed "$SPEED" --theme monokai --font-size 14 "$CAST" "$GIF"
ls -lh "$GIF"

echo "[cast-to-slides] transcoding GIF -> MP4 (Slides-friendly, H.264 yuv420p):"
ffmpeg -y -i "$GIF" \
  -movflags +faststart \
  -pix_fmt yuv420p \
  -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" \
  -c:v libx264 -preset slow -crf 20 \
  "$MP4" 2>&1 | tail -5
ls -lh "$MP4"

echo
echo "[cast-to-slides] done."
echo "  GIF (drag-drop into Slides; auto-plays): $GIF"
echo "  MP4 (upload to Drive, Insert > Video):  $MP4"
