# scripts/recordings — Teleport recording utilities

Tools that work on Teleport session recordings (`<sid>.tar`) and
the asciinema `.cast` files that some agent harnesses produce
alongside them.

| Script | Purpose |
|---|---|
| `plot_cadence.py` | Render a 3-panel cadence PNG (rug, bytes/sec, log-scale inter-event gap) from a `<sid>.tar` and print summary stats. Anchors the cadence-fingerprint work documented in `whodrove/notes/10-cadence-fingerprints.md`. |
| `tar-to-cast.sh`  | Synthesize an asciinema `.cast` from a `<sid>.tar` by wrapping `tsh play` in `asciinema rec`. Real-time replay — takes as long as the session did. |
| `cast-to-slides.sh` | Convert an asciinema `.cast` into a GIF + an H.264 MP4 (Slides-friendly), via `agg` + `ffmpeg`. Default 2× speed; pass an integer 3rd arg to change. |

Typical pipelines:

```
# Agent run (already produces a .cast):
scripts/recordings/cast-to-slides.sh runs/<run>/.../sessions/agent.cast my-stem 4

# Human run (only produces a Teleport .tar — re-synthesize the cast first):
scripts/recordings/tar-to-cast.sh   harnesses/terminal-bench-teleport/cluster/data/log/records/<sid>.tar
scripts/recordings/cast-to-slides.sh <sid>.cast human-stem 4

# Cadence analysis (works directly off the .tar):
uv run --with matplotlib python scripts/recordings/plot_cadence.py <sid>.tar
```
