"""Monkey-patch terminal_bench.terminal.tmux_session.TmuxSession so its
container-exec calls route through `tsh ssh` to a Teleport-enrolled Node.

Why a monkey-patch and not a subclass: the patch surface is exactly two
methods (`_exec_run` and the one stray `self.container.exec_run` inside
`_send_blocking_keys`), and we want to slip the routing in without forking
either the harness or the submodule. Subclassing `TmuxSession` would
require subclassing `Terminal` and `Harness` to thread the new class type
through, which is much more invasive.

Importing this module triggers `apply()`. Idempotent.

Required env at the time the patched code runs:
    TBENCH_TELEPORT_IDENTITY  path to the agent identity file
                              (cluster/.agent-identity from bootstrap.sh)
    TBENCH_TELEPORT_PROXY     proxy host:port (default: localhost:3080)

Optional:
    TBENCH_TELEPORT_LOGIN     UNIX user inside the container (default: agent)
    TBENCH_TSH_BIN            tsh binary path (default: tsh)
"""

from __future__ import annotations

import os
import re
import shlex
import subprocess
import time
from collections import namedtuple

from terminal_bench.terminal import tmux_session as _tmux_session


def _sanitize_nodename(raw: str) -> str:
    """Mirror entrypoint.sh: Teleport nodenames must be DNS-1123-ish
    (lowercase, alphanumeric, hyphens). Terminal-Bench's container names
    use `__` as a date/time separator which Teleport rejects. The
    in-container entrypoint applies the same transform when registering
    the node, so both sides agree on the resulting name.
    """
    s = raw.lower().replace("_", "-")
    s = re.sub(r"[^a-z0-9-]+", "-", s)
    s = re.sub(r"-+", "-", s).strip("-")
    return s


_TshExecResult = namedtuple("TshExecResult", ["exit_code", "output"])
_PATCHED_SENTINEL = "_tbench_teleport_patched"

_FIRST_CALL_RETRY_BUDGET_SEC = 60.0
_RETRY_INTERVAL_SEC = 2.0

# tsh failure modes that indicate the node is still coming online.
# We retry these for up to _FIRST_CALL_RETRY_BUDGET_SEC; everything
# else returns immediately so legitimate task-side failures aren't
# masked.
_TRANSIENT_STDERR_RE = re.compile(
    r"(offline or does not exist"
    r"|is not online"
    r"|target host .* is offline"
    r"|could not connect"
    r"|connection refused)",
    re.IGNORECASE,
)


def _tsh_argv(nodename: str, cmd: list[str] | str) -> list[str]:
    bin_ = os.environ.get("TBENCH_TSH_BIN", "tsh")
    identity = os.environ["TBENCH_TELEPORT_IDENTITY"]
    proxy = os.environ.get("TBENCH_TELEPORT_PROXY", "localhost:3080")
    login = os.environ.get("TBENCH_TELEPORT_LOGIN", "agent")

    if isinstance(cmd, list):
        cmd_str = " ".join(shlex.quote(c) for c in cmd)
    else:
        cmd_str = cmd

    return [
        bin_,
        f"--identity={identity}",
        f"--proxy={proxy}",
        "--insecure",            # self-signed cert on the OSS test cluster
        "--skip-version-check",  # brew tsh may be ahead of test cluster
        "ssh",
        # Force PTY allocation (-t is an `ssh` subcommand flag, not a tsh
        # global). OSS Teleport with node-sync recording mode only
        # produces a <sid>.tar for sessions that have a PTY — non-PTY
        # exec sessions emit session.start + session.end audit events
        # but no recording bytes, so the fixture pipeline gets nothing to
        # parse. The trade-off is that every _exec_run becomes its own
        # short PTY session; aggregation lives downstream in shellscope.
        "-t",
        f"{login}@{nodename}",
        cmd_str,
    ]


def _patched_exec_run(self, cmd):
    """Replacement for TmuxSession._exec_run. Routes through tsh.

    The first call after the container comes up may race the node-join
    handshake; we retry with backoff for ~30s before giving up.
    """
    nodename = _sanitize_nodename(self.container.name)
    deadline = time.time() + _FIRST_CALL_RETRY_BUDGET_SEC
    last_proc = None
    # Force a portable TERM. tsh forwards the host's TERM into the
    # remote PTY; if the host is running Ghostty (xterm-ghostty) or any
    # exotic emulator, the remote /lib/terminfo won't recognise it and
    # tmux/ncurses programs fail with "missing or unsuitable terminal".
    env = {**os.environ, "TERM": "xterm-256color"}
    first = True
    attempts = 0
    while True:
        try:
            argv = _tsh_argv(nodename, cmd)
            attempts += 1
            last_proc = subprocess.run(
                argv,
                capture_output=True,
                check=False,
                env=env,
            )
            if first:
                _sys = __import__("sys")
                first = False
                print(f"[tsh_terminal] first call: nodename={nodename} cmd={cmd!r}",
                      file=_sys.stderr)
                print(f"  rc={last_proc.returncode} stdout={last_proc.stdout[:200]!r} stderr={last_proc.stderr[:300].decode(errors='replace')!r}",
                      file=_sys.stderr)

            stderr_text = last_proc.stderr.decode(errors="replace")
            transient = (last_proc.returncode == 255
                         or _TRANSIENT_STDERR_RE.search(stderr_text))
            if not transient or time.time() >= deadline:
                return _TshExecResult(
                    exit_code=last_proc.returncode, output=last_proc.stdout
                )
        except FileNotFoundError as e:
            raise RuntimeError(
                "tsh binary not found. `brew install teleport`, or set "
                "TBENCH_TSH_BIN."
            ) from e

        time.sleep(_RETRY_INTERVAL_SEC)


def _patched_send_blocking_keys(self, keys, max_timeout_sec):
    """Replacement for TmuxSession._send_blocking_keys.

    Upstream calls `self.container.exec_run(...)` directly for the
    send-keys step (line 213 in tmux_session.py at the pinned commit),
    which bypasses our `_exec_run` patch. Re-route through `_exec_run`.
    """
    start_time_sec = time.time()

    self._exec_run(self._tmux_send_keys(keys))

    result = self._exec_run(
        ["timeout", f"{max_timeout_sec}s", "tmux", "wait", "done"]
    )
    if result.exit_code != 0:
        raise TimeoutError(
            f"Command timed out after {max_timeout_sec} seconds"
        )

    elapsed_time_sec = time.time() - start_time_sec
    self._logger.debug(
        f"Blocking command completed in {elapsed_time_sec:.2f}s."
    )


def apply() -> None:
    """Monkey-patch TmuxSession. Idempotent."""
    cls = _tmux_session.TmuxSession
    if getattr(cls, _PATCHED_SENTINEL, False):
        return
    cls._exec_run = _patched_exec_run
    cls._send_blocking_keys = _patched_send_blocking_keys
    setattr(cls, _PATCHED_SENTINEL, True)
    print("[tsh_terminal] TmuxSession monkey-patch applied", file=__import__("sys").stderr)


apply()
