"""Rewrite a Terminal-Bench task directory so it (a) builds FROM our
Teleport-enrolled base image and (b) joins the test cluster's docker
network with the right env vars set.

Idempotent: re-running on an already-patched temp dir produces an identical
output. Does not touch the original task directory.

Usage:
    from patch_task import patch_task
    patched = patch_task("harnesses/terminal-bench/original-tasks/hello-world",
                         out_root="harnesses/terminal-bench-teleport/runs")
"""

from __future__ import annotations

import re
import shutil
import uuid
from pathlib import Path

import yaml


BASE_IMAGE = "tbench-teleport-base:v17.7.20"
NETWORK_NAME = "tbench-fixture-net"

_FROM_RE = re.compile(r"^\s*FROM\s+\S+", re.IGNORECASE)


def _rewrite_dockerfile(src: Path, dst: Path) -> None:
    """Replace the FIRST FROM line. Multi-stage builds keep their later FROMs."""
    lines = src.read_text().splitlines(keepends=True)
    rewrote = False
    out_lines: list[str] = []
    for line in lines:
        if not rewrote and _FROM_RE.match(line):
            out_lines.append(f"FROM {BASE_IMAGE}\n")
            rewrote = True
            continue
        out_lines.append(line)
    if not rewrote:
        # No FROM found — prepend ours.
        out_lines.insert(0, f"FROM {BASE_IMAGE}\n")
    dst.write_text("".join(out_lines))


def _rewrite_compose(src: Path, dst: Path) -> None:
    """Inject Teleport env vars + the shared docker network into every service.

    Tasks always have a `client` service per the t-bench convention, but
    a few tasks add sidecars; we patch all services uniformly so any of
    them can be the one the harness shells into.
    """
    doc = yaml.safe_load(src.read_text())
    services = doc.setdefault("services", {})

    extra_env = [
        "TELEPORT_PROXY_ADDR=${TELEPORT_PROXY_ADDR}",
        "TELEPORT_JOIN_TOKEN=${TELEPORT_JOIN_TOKEN}",
        "TELEPORT_NODE_LABELS=${TELEPORT_NODE_LABELS}",
        # The entrypoint reads this for the nodename. The harness sets
        # T_BENCH_TASK_DOCKER_CLIENT_CONTAINER_NAME on its host process;
        # we pass it through into the container too.
        "T_BENCH_TASK_DOCKER_CLIENT_CONTAINER_NAME="
        "${T_BENCH_TASK_DOCKER_CLIENT_CONTAINER_NAME}",
    ]

    for svc in services.values():
        env = svc.get("environment", [])
        if isinstance(env, dict):
            # Convert dict form to list form for uniform append.
            env = [f"{k}={v}" for k, v in env.items()]
        env = list(env) + extra_env
        svc["environment"] = env
        # Make the container's hostname match its name so the in-container
        # entrypoint can pass it as `nodename` to teleport.
        svc.setdefault(
            "hostname", "${T_BENCH_TASK_DOCKER_CLIENT_CONTAINER_NAME}"
        )
        # Join the shared network alongside whatever the service had.
        # If the service had no explicit `networks:`, Compose put it on
        # the project's auto-created default network — we have to name
        # `default` explicitly here, since listing any networks at the
        # service level opts out of the auto-attach.
        existing_nets = svc.get("networks")
        if existing_nets is None:
            nets = ["default"]
        elif isinstance(existing_nets, dict):
            nets = list(existing_nets.keys())
        else:
            nets = list(existing_nets)
        if NETWORK_NAME not in nets:
            nets.append(NETWORK_NAME)
        svc["networks"] = nets

    networks = doc.setdefault("networks", {})
    networks[NETWORK_NAME] = {"external": True, "name": NETWORK_NAME}

    dst.write_text(yaml.safe_dump(doc, sort_keys=False))


def patch_task(
    task_dir: str | Path,
    out_root: str | Path,
    run_id: str | None = None,
) -> Path:
    """Copy `task_dir` into `out_root/<run_id>/<task_id>/` and patch in place.

    The two-level layout (run-id parent, task-id child) is what the t-bench
    `--dataset-path` flag expects: dataset-path is the parent and the harness
    looks up `<dataset-path>/<task-id>/`.

    Returns the path to the patched task directory.
    """
    task_dir = Path(task_dir).resolve()
    out_root = Path(out_root).resolve()
    run_id = run_id or uuid.uuid4().hex[:8]

    task_id = task_dir.name
    dst = out_root / run_id / task_id
    if dst.exists():
        shutil.rmtree(dst)
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copytree(task_dir, dst)

    _rewrite_dockerfile(dst / "Dockerfile", dst / "Dockerfile")
    _rewrite_compose(dst / "docker-compose.yaml", dst / "docker-compose.yaml")

    return dst


if __name__ == "__main__":
    import argparse

    p = argparse.ArgumentParser()
    p.add_argument("task_dir")
    p.add_argument("--out-root", default="runs")
    p.add_argument("--run-id")
    args = p.parse_args()
    print(patch_task(args.task_dir, args.out_root, args.run_id))
