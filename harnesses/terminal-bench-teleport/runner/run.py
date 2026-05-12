"""Drive a single Terminal-Bench task end-to-end inside the OSS Teleport
test cluster, producing one Teleport session recording labelled as agent
activity.

What this script does, in order:
  1. Sanity-check: cluster is up, agent identity exists, tsh binary
     resolves, base image is built, terminal_bench is importable.
  2. Patch the requested task with patch_task.py → temp dir.
  3. Apply the tsh_terminal monkey-patch.
  4. Set env vars the patch needs (proxy, identity, join token).
  5. Invoke `terminal_bench.cli.tb.main` in-process via runpy with the
     patched task path passed through --task-dir.
  6. Hand off to harvest.py to pull the resulting <sid>.tar from the
     cluster volume.

Run this from the repo root:
    uv run --with terminal-bench --with pyyaml --with pexpect \\
        python harnesses/terminal-bench-teleport/runner/run.py \\
        --task-id hello-world --agent terminus --model openrouter/deepseek/deepseek-chat

Dependencies the script needs at import time, beyond stdlib:
    terminal-bench  the harness itself
    pyyaml          for runner/patch_task.py to rewrite docker-compose.yaml
    pexpect         for runner/tsh_terminal.py to drive the persistent
                    tsh ssh session
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import uuid
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent.parent
TBENCH_TASKS = ROOT / "harnesses" / "terminal-bench" / "original-tasks"
CLUSTER_DIR = HERE.parent / "cluster"
RUNS_DIR = HERE.parent / "runs"


def _read(path: Path) -> str:
    if not path.exists():
        sys.exit(
            f"ERROR: {path} not found. Run cluster/up.sh && "
            f"cluster/bootstrap.sh first."
        )
    return path.read_text().strip()


def _check_image() -> None:
    proc = subprocess.run(
        ["docker", "image", "inspect", "tbench-teleport-base:v17.7.20"],
        capture_output=True,
    )
    if proc.returncode != 0:
        sys.exit(
            "ERROR: tbench-teleport-base:v17.7.20 not built. "
            "Run image/build.sh first."
        )


def _ensure_docker_host_env() -> None:
    """The Python docker SDK looks for /var/run/docker.sock and ignores
    Docker contexts. Colima/Rancher Desktop/etc. publish their socket
    elsewhere and rely on `docker context`. If DOCKER_HOST is unset,
    resolve it from the active context so the harness's
    `docker.from_env()` call works.
    """
    if os.environ.get("DOCKER_HOST"):
        return
    proc = subprocess.run(
        ["docker", "context", "inspect", "--format",
         "{{.Endpoints.docker.Host}}"],
        capture_output=True, text=True,
    )
    if proc.returncode == 0 and proc.stdout.strip():
        os.environ["DOCKER_HOST"] = proc.stdout.strip()


def _check_tsh() -> None:
    if shutil.which(os.environ.get("TBENCH_TSH_BIN", "tsh")) is None:
        sys.exit("ERROR: tsh binary not on PATH. `brew install teleport`.")


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--task-id", required=True,
                   help="Task ID under harnesses/terminal-bench/original-tasks/")
    p.add_argument("--agent", default="terminus",
                   help="Terminal-Bench agent name")
    p.add_argument("--model", required=True,
                   help="LiteLLM-compatible model id, e.g. "
                        "openrouter/deepseek/deepseek-chat")
    p.add_argument("--run-id", default=None,
                   help="Stable run id (default: random 8-hex)")
    p.add_argument("--no-harvest", action="store_true",
                   help="Skip the post-run recording harvest step")
    args = p.parse_args()

    run_id = args.run_id or uuid.uuid4().hex[:8]

    task_dir = TBENCH_TASKS / args.task_id
    if not task_dir.exists():
        sys.exit(f"ERROR: task not found: {task_dir}")

    _check_image()
    _check_tsh()
    _ensure_docker_host_env()

    proxy = os.environ.get("TBENCH_TELEPORT_PROXY", "localhost:3080")
    identity_path = (CLUSTER_DIR / ".agent-identity").resolve()
    if not identity_path.exists():
        sys.exit(f"ERROR: {identity_path} not found. Run cluster/bootstrap.sh.")

    join_token = _read(CLUSTER_DIR / ".node-join-token")

    # Patch the task. patch_task.py uses PyYAML.
    sys.path.insert(0, str(HERE))
    from patch_task import patch_task

    RUNS_DIR.mkdir(parents=True, exist_ok=True)
    patched = patch_task(task_dir, RUNS_DIR, run_id=run_id)
    print(f"Patched task at: {patched}")

    # Set env for the patched harness + entrypoint to consume.
    # TELEPORT_PROXY_ADDR (in-network) is the auth/proxy alias; from a
    # task container's POV the auth is reachable as
    # `tbench-fixture-auth:3025` since both join `tbench-fixture-net`.
    os.environ["TELEPORT_PROXY_ADDR"] = "tbench-fixture-auth:3025"
    os.environ["TELEPORT_JOIN_TOKEN"] = join_token
    os.environ["TELEPORT_NODE_LABELS"] = (
        f"fixture/run-id={run_id},"
        f"fixture/task-id={args.task_id},"
        f"fixture/agent={args.agent},"
        f"fixture/model={args.model.replace('/', '_')}"
    )

    # Set env for the tsh_terminal monkey-patch (controller-side).
    os.environ["TBENCH_TELEPORT_PROXY"] = proxy
    os.environ["TBENCH_TELEPORT_IDENTITY"] = str(identity_path)
    # Default UNIX login matches what unmodified Terminal-Bench tasks
    # expect: they're written assuming root (no USER directive in their
    # Dockerfiles), so /app and similar are root-owned and oracle/agent
    # solution scripts can fail with permission errors otherwise.
    # Override via TBENCH_TELEPORT_LOGIN if a task needs a non-root user.
    os.environ.setdefault("TBENCH_TELEPORT_LOGIN", "root")

    # Apply monkey-patch BEFORE importing/running the harness.
    import tsh_terminal  # noqa: F401  (side-effect)

    # Capture run start so harvest only picks up recordings produced by
    # *this* run, not earlier runs sitting in cluster/data.
    import time as _time
    run_started_at = _time.time()

    # Force a unique --run-id so two concurrent `tb run` invocations
    # don't collide on the wallclock-seconds default
    # (`YYYY-MM-DD__HH-MM-SS`). When they collide, tb sees an existing
    # dir with a lock file and interprets it as "resuming a previous
    # run" — then errors out on the config mismatch.
    from datetime import datetime as _dt
    tb_run_id = (
        _dt.now().strftime("%Y-%m-%d__%H-%M-%S")
        + "_" + uuid.uuid4().hex[:6]
    )

    # Invoke the harness in-process so our patch takes effect.
    import runpy
    saved_argv = sys.argv
    sys.argv = [
        "tb",
        "run",
        "--task-id", args.task_id,
        "--agent", args.agent,
        "--model", args.model,
        "--run-id", tb_run_id,
        # patched is .../runs/<run_id>/<task_id>/; --dataset-path is the parent.
        "--dataset-path", str(patched.parent),
    ]
    print(f"Invoking: {' '.join(sys.argv)}")
    try:
        runpy.run_module("terminal_bench.cli.tb.main", run_name="__main__")
        rc = 0
    except SystemExit as e:
        rc = e.code if isinstance(e.code, int) else 1
    finally:
        sys.argv = saved_argv

    if not args.no_harvest:
        from harvest import harvest_run
        harvest_run(
            run_id=run_id,
            task_id=args.task_id,
            agent=args.agent,
            model=args.model,
            tb_exit_code=rc,
            t0=run_started_at,
        )

    return rc


if __name__ == "__main__":
    sys.exit(main())
