"""Plot session cadence from a Teleport <sid>.tar recording.

The recording captures every byte the PTY emitted — both echoed keystrokes
and program output — as a stream of SessionPrint events with absolute
timestamps. `tsh play --format=json` decodes the tar to a JSON event list
without us having to touch protobuf.

Usage:
    uv run --with matplotlib python whodrove/scripts/recordings/plot_cadence.py \
        whodrove/harnesses/terminal-bench-teleport/cluster/data/log/records/<sid>.tar \
        --out cadence.png

The plot has three panels sharing an x-axis (seconds since session start):
  1. Rug — one tick per print event. Pure "when did things happen?".
  2. Bytes-per-second — intensity histogram (1s bins).
  3. Inter-event gap (log scale) — exposes "thinking pauses" between bursts.

Cadence is a proxy: SessionPrint chunks combine input echo + program
output, so a fast typist and a chatty command look similar. For finer
grain (separate keystrokes from output) you'd need BPF session.command
events, which the OSS file backend can capture but our fixture cluster
isn't running enhanced recording.
"""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys
from datetime import datetime
from pathlib import Path

import matplotlib.pyplot as plt
import numpy as np


HERE = Path(__file__).resolve().parent
REPO = HERE.parent  # whodrove/
FIXTURE = REPO / "harnesses" / "terminal-bench-teleport"
IDENTITY = FIXTURE / "cluster" / ".agent-identity"


def _load_events(path: Path) -> list[dict]:
    """If `path` is a .tar, shell out to `tsh play --format=json`. If it's
    a .json file (already dumped), load it directly.
    """
    if path.suffix == ".json":
        return json.loads(path.read_text())
    if path.suffix != ".tar":
        raise SystemExit(f"expected .tar or .json, got {path.suffix}")

    if not shutil.which("tsh"):
        raise SystemExit("tsh not on PATH; brew install teleport@17")
    if not IDENTITY.exists():
        raise SystemExit(f"missing identity: {IDENTITY}")

    sid = path.stem
    proc = subprocess.run(
        [
            "tsh", "--insecure", "--skip-version-check",
            f"--identity={IDENTITY}", "--proxy=localhost:3080",
            "play", "--format=json", sid,
        ],
        capture_output=True, text=True, check=False,
    )
    if proc.returncode != 0:
        raise SystemExit(f"tsh play failed: {proc.stderr.strip()}")
    return json.loads(proc.stdout)


def _parse_time(s: str) -> datetime:
    # tsh emits RFC3339 with a trailing Z; fromisoformat handles Z only on 3.11+.
    return datetime.fromisoformat(s.replace("Z", "+00:00"))


def plot_cadence(events: list[dict], title: str, out: Path) -> None:
    prints = [e for e in events if e.get("event") == "print"]
    if not prints:
        raise SystemExit("no print events in recording — was PTY enabled?")

    t0 = _parse_time(prints[0]["time"])
    rel_s = np.array([
        (_parse_time(e["time"]) - t0).total_seconds() for e in prints
    ])
    sizes = np.array([int(e.get("bytes", 0)) for e in prints])

    duration = rel_s[-1]
    bins = max(int(np.ceil(duration)), 1)
    counts, edges = np.histogram(rel_s, bins=bins, range=(0, duration), weights=sizes)
    centers = 0.5 * (edges[:-1] + edges[1:])

    gaps = np.diff(rel_s)
    gap_t = rel_s[1:]

    fig, axes = plt.subplots(
        3, 1, figsize=(14, 7), sharex=True,
        gridspec_kw={"height_ratios": [0.6, 2, 1.6]},
    )

    ax0, ax1, ax2 = axes
    ax0.vlines(rel_s, 0, 1, colors="#222", linewidth=0.5, alpha=0.7)
    ax0.set_yticks([])
    ax0.set_ylabel("events", rotation=0, ha="right", va="center")
    ax0.set_title(f"{title}  —  {len(prints)} print events over {duration:.1f}s")

    ax1.bar(centers, counts, width=1.0, align="center", color="#3b82f6", edgecolor="none")
    ax1.set_ylabel("bytes / sec")
    ax1.grid(axis="y", alpha=0.2)

    if len(gaps):
        positive = gaps[gaps > 0]
        floor = positive.min() / 2 if positive.size else 1e-3
        gaps_plot = np.where(gaps > 0, gaps, floor)
        ax2.scatter(gap_t, gaps_plot, s=6, color="#dc2626", alpha=0.6)
        ax2.set_yscale("log")
        ax2.set_ylabel("gap to next event (s)")
        for thresh, label in [(1, "1s"), (5, "5s"), (30, "30s")]:
            ax2.axhline(thresh, color="#888", linestyle="--", linewidth=0.5)
            ax2.text(duration, thresh, f" {label}", va="center", fontsize=7, color="#666")
        ax2.grid(axis="y", which="both", alpha=0.2)

    ax2.set_xlabel("seconds since session start")
    ax2.set_xlim(0, duration)

    fig.tight_layout()
    fig.savefig(out, dpi=130)
    print(f"wrote {out}")

    # Print quick summary so the user gets cadence stats in chat without opening the image.
    print()
    print(f"duration:        {duration:.1f}s")
    print(f"print events:    {len(prints)}")
    print(f"total bytes:     {sizes.sum():,}")
    print(f"events/sec mean: {len(prints) / duration:.2f}")
    if len(gaps):
        print(f"gap p50:         {np.median(gaps):.3f}s")
        print(f"gap p90:         {np.percentile(gaps, 90):.3f}s")
        print(f"gap p99:         {np.percentile(gaps, 99):.3f}s")
        print(f"gap max:         {gaps.max():.3f}s")
        for thresh in (1, 5, 30):
            n = int((gaps > thresh).sum())
            print(f"pauses >{thresh:>3}s:    {n}")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("recording", help="path to <sid>.tar (or a pre-dumped .json)")
    ap.add_argument("--out", default=None, help="output PNG path (default: alongside recording)")
    ap.add_argument("--title", default=None, help="plot title (default: file stem)")
    args = ap.parse_args()

    path = Path(args.recording).resolve()
    if not path.exists():
        sys.exit(f"no such file: {path}")
    out = Path(args.out) if args.out else path.with_suffix(".cadence.png")
    title = args.title or path.stem

    events = _load_events(path)
    plot_cadence(events, title, out)


if __name__ == "__main__":
    main()
