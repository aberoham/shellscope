"""Pull the Teleport session recording produced by a task run, then emit a
sidecar metadata JSON suitable for stamping into shellscope's sessions.sqlite
via `teleport-analyze parse` + `teleport-analyze label set`.

OSS Teleport's local file backend writes finalised recordings under the
auth server's data dir. We mount that volume to cluster/data/, so anything
ending in .tar that appears there during the run is a candidate.

This script is conservative: it does not auto-upsert into sessions.sqlite.
It writes a JSON sidecar per recording with the labels we want stamped, and
prints the suggested `teleport-analyze` invocations. The user runs those
once they've decided which sessions.sqlite to write into.

Why not call `tctl recordings download <sid>` directly: in OSS the
recordings already live on the local filesystem we mounted, so a copy is
cheaper and works without minting another Teleport cert.
"""

from __future__ import annotations

import datetime as _dt
import json
import shutil
import subprocess
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
RUNS_DIR = HERE.parent / "runs"
CLUSTER_DATA = HERE.parent / "cluster" / "data"


def _find_recordings_since(t0: float) -> list[Path]:
    """Walk the cluster's data dir for .tar files newer than t0."""
    if not CLUSTER_DATA.exists():
        return []
    out: list[Path] = []
    for p in CLUSTER_DATA.rglob("*.tar"):
        try:
            if p.stat().st_mtime >= t0 and p.stat().st_size > 0:
                out.append(p)
        except FileNotFoundError:
            continue
    return out


def _session_id_from_path(p: Path) -> str:
    """Recording filenames are <sid>.tar. Validate it looks like a UUID-ish."""
    return p.stem


def harvest_run(
    run_id: str,
    task_id: str,
    agent: str,
    model: str,
    tb_exit_code: int,
    t0: float | None = None,
) -> list[Path]:
    """Copy recordings produced by the run into runs/<task>-<run_id>/recordings/
    and emit a sidecar metadata JSON for each.

    Returns the list of harvested recording paths.
    """
    # If t0 unknown, fall back to "recordings touched in the last 30 min".
    t0 = t0 or (time.time() - 30 * 60)

    # patch_task layout: runs/<run_id>/<task_id>/. Recordings get a
    # sibling /recordings dir so they're discoverable from the run id alone.
    run_dir = RUNS_DIR / run_id / task_id
    run_dir.mkdir(parents=True, exist_ok=True)
    rec_dir = run_dir / "recordings"
    rec_dir.mkdir(exist_ok=True)

    found = _find_recordings_since(t0)
    harvested: list[Path] = []

    for src in found:
        sid = _session_id_from_path(src)
        dst = rec_dir / f"{sid}.tar"
        shutil.copy2(src, dst)

        labels = {
            "operator.type": "agent",
            "agent.tool": agent,
            "agent.model": model,
            "fixture.source": "terminal-bench",
            "fixture.task_id": task_id,
            "fixture.task_passed": "1" if tb_exit_code == 0 else "0",
            "fixture.harness_run_id": run_id,
            "fixture.harvested_at": _dt.datetime.utcnow().isoformat() + "Z",
        }
        sidecar = {
            "session_id": sid,
            "recording_path": str(dst),
            "labels": labels,
        }
        (rec_dir / f"{sid}.labels.json").write_text(
            json.dumps(sidecar, indent=2)
        )
        harvested.append(dst)
        print(f"Harvested: {dst}")

    if not harvested:
        print("WARN: no recordings found under cluster/data after t0. "
              "Recording mode mis-set, or session never finalised.")
        return harvested

    print()
    print("To ingest into shellscope's sessions.sqlite:")
    for rec in harvested:
        sid = rec.stem
        sidecar = json.loads((rec.parent / f"{sid}.labels.json").read_text())
        print(f"  teleport-analyze parse {rec}")
        for k, v in sidecar["labels"].items():
            print(
                f"  teleport-analyze label set --session {sid} "
                f"--key {k} --value {v} --set-by tbench-fixture@v0"
            )
        print()

    return harvested


def _which_teleport_analyze() -> str | None:
    return shutil.which("teleport-analyze")


if __name__ == "__main__":
    import argparse

    p = argparse.ArgumentParser()
    p.add_argument("--run-id", required=True)
    p.add_argument("--task-id", required=True)
    p.add_argument("--agent", required=True)
    p.add_argument("--model", required=True)
    p.add_argument("--tb-exit-code", type=int, default=0)
    args = p.parse_args()
    harvest_run(args.run_id, args.task_id, args.agent, args.model,
                args.tb_exit_code)
