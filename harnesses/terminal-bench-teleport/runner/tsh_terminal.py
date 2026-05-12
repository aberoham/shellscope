"""Monkey-patch terminal_bench.terminal so each Terminal-Bench task
produces exactly ONE Teleport <sid>.tar recording, by routing every
TmuxSession exec call through a single persistent `tsh ssh -t` session
shared across all TmuxSession instances of the same container.

Why a monkey-patch and not a subclass: the patch surface is small (three
methods on TmuxSession plus one on Terminal) and we want to slip the
routing in without forking the harness or the submodule.

Why one session shared per container: by default the harness creates two
TmuxSession instances per task — one named "agent" and one named "tests"
(unless task.yaml sets `run_tests_in_same_shell: true`). Each instance
goes through `_exec_run` for tmux subcommand routing. If we opened a
fresh `tsh ssh` per TmuxSession we'd get two Teleport recordings per
task. Sharing one persistent session keyed by container.id collapses
this back to a single recording per task, which is what the
human-vs-agent cadence analysis wants.

Output framing: the persistent shell runs with `stty -echo` and
`PS1=''`, and each call sends
`({cmd}); printf '\\n__TBEND_<uuid>_%d__\\n' $?`
followed by `expect` for the unique end-marker. Exit code is captured
in the marker; everything before it is the command's stdout.

Importing this module triggers `apply()`. Idempotent.

Required env at the time the patched code runs:
    TBENCH_TELEPORT_IDENTITY  path to the agent identity file
                              (cluster/.agent-identity from bootstrap.sh)
    TBENCH_TELEPORT_PROXY     proxy host:port (default: localhost:3080)

Optional:
    TBENCH_TELEPORT_LOGIN     UNIX user inside the container (default: root)
    TBENCH_TSH_BIN            tsh binary path (default: tsh)
"""

from __future__ import annotations

import os
import re
import shlex
import subprocess
import sys
import time
import uuid
from collections import namedtuple

import pexpect

from terminal_bench.terminal import terminal as _terminal
from terminal_bench.terminal import tmux_session as _tmux_session


_TshExecResult = namedtuple("TshExecResult", ["exit_code", "output"])
_PATCHED_SENTINEL = "_tbench_teleport_patched"

# Persistent SSH sessions keyed by container.id, so the "agent" and
# "tests" TmuxSession instances of the same task share one SSH session
# (and therefore one Teleport recording).
_SHARED_TSH_SESSIONS: dict[str, "_TshSession"] = {}

# 180s budget gives concurrent runs slack: dockerd serializes network
# attachments per network, so when two `tb run` invocations spin up
# containers on `tbench-fixture-net` at the same time, the second
# container's DNS entry may not appear in the proxy resolver for tens
# of seconds. Headroom is cheap; per-attempt time is dominated by tsh
# spawning + EOF.
_OPEN_RETRY_BUDGET_SEC = 180.0
_OPEN_RETRY_INTERVAL_SEC = 2.0
_DNS_PRECHECK_NETWORK = os.environ.get(
    "TBENCH_TELEPORT_NETWORK", "tbench-fixture-net"
)
_DNS_PRECHECK_POLL_SEC = 0.5
_DEFAULT_EXEC_TIMEOUT_SEC = 600.0

_TRANSIENT_OPEN_RE = re.compile(
    rb"(offline or does not exist"
    rb"|is not online"
    rb"|target host .* is offline"
    rb"|could not connect"
    rb"|connection refused)",
    re.IGNORECASE,
)

_READLINE_NOISE_RE = re.compile(rb"\x1b\[\?2004[hl]")


def _sanitize_nodename(raw: str) -> str:
    """No-op. Teleport v17 accepts the Terminal-Bench container name
    verbatim (including the `__` between the date and time), so we
    keep the raw name as the Teleport nodename. This matters because
    the OSS auth-server-mode proxy resolves nodes via docker DNS in
    `tbench-fixture-net`, where the container is only registered under
    its actual name — any transform here would create a name the proxy
    can't dial.
    """
    return raw


def _wait_for_docker_dns(nodename: str, deadline: float) -> bool:
    """Block until docker has attached the container named `nodename` to
    `_DNS_PRECHECK_NETWORK` with a non-empty IP. Returns True on success,
    False if the deadline elapses.

    Why: the OSS proxy resolves Teleport nodes via docker's embedded DNS
    in the shared network. If `docker compose up -d` returns before the
    container's network entry is registered, the first `tsh ssh` lookup
    sees NXDOMAIN. The harness then retries — but with concurrent runs
    we've observed network attachment taking longer than the previous
    60s retry budget. This precheck makes the wait explicit and cheap.

    A missing container or missing network simply keeps polling within
    budget; both are normal during container startup.
    """
    fmt = (
        '{{index .NetworkSettings.Networks "'
        + _DNS_PRECHECK_NETWORK
        + '" "IPAddress"}}'
    )
    while time.time() < deadline:
        try:
            out = subprocess.check_output(
                ["docker", "inspect", nodename, "--format", fmt],
                stderr=subprocess.DEVNULL,
                timeout=5,
            ).decode().strip()
        except (
            subprocess.CalledProcessError,
            subprocess.TimeoutExpired,
            FileNotFoundError,
        ):
            out = ""
        if out and out != "<no value>":
            return True
        time.sleep(_DNS_PRECHECK_POLL_SEC)
    return False


def _tsh_argv(nodename: str) -> list[str]:
    bin_ = os.environ.get("TBENCH_TSH_BIN", "tsh")
    identity = os.environ["TBENCH_TELEPORT_IDENTITY"]
    proxy = os.environ.get("TBENCH_TELEPORT_PROXY", "localhost:3080")
    login = os.environ.get("TBENCH_TELEPORT_LOGIN", "root")

    return [
        bin_,
        f"--identity={identity}",
        f"--proxy={proxy}",
        "--insecure",            # self-signed cert on the OSS test cluster
        "--skip-version-check",  # brew tsh may be ahead of test cluster
        "ssh",
        # PTY required: OSS Teleport with node-sync recording mode only
        # produces a <sid>.tar for sessions that have a SessionPrint
        # stream, which means a PTY-backed session.
        "-t",
        f"{login}@{nodename}",
    ]


class _TshSession:
    """One persistent `tsh ssh -t` session per container, multiplexed
    via pexpect with delimiter-based output framing.
    """

    def __init__(self, nodename: str):
        self.nodename = nodename
        self._proc: pexpect.spawn | None = None
        self._open_with_retry()

    def _open_with_retry(self) -> None:
        deadline = time.time() + _OPEN_RETRY_BUDGET_SEC
        env = {**os.environ, "TERM": "xterm-256color"}
        attempts = 0
        last_buf: bytes = b""

        # Precheck: don't dial tsh until docker has registered the
        # container on the shared network. Saves a bunch of pointless
        # tsh spawns + EOFs during startup, and (under concurrent runs)
        # avoids the dockerd-DNS-not-yet-populated race we documented.
        dns_ready = _wait_for_docker_dns(self.nodename, deadline)
        if not dns_ready:
            print(
                f"[tsh_terminal] WARN: docker DNS for {self.nodename!r} "
                f"on {_DNS_PRECHECK_NETWORK!r} did not become ready "
                f"within budget; falling through to tsh anyway",
                file=sys.stderr,
            )
        else:
            print(
                f"[tsh_terminal] docker DNS ready for {self.nodename}",
                file=sys.stderr,
            )

        while True:
            attempts += 1
            argv = _tsh_argv(self.nodename)
            print(
                f"[tsh_terminal] opening session attempt {attempts}: "
                f"nodename={self.nodename}",
                file=sys.stderr,
            )

            try:
                proc = pexpect.spawn(
                    argv[0],
                    argv[1:],
                    env=env,
                    dimensions=(40, 200),
                    timeout=30,
                )
            except pexpect.ExceptionPexpect as e:
                if time.time() >= deadline:
                    raise RuntimeError(
                        f"tsh failed to spawn after "
                        f"{_OPEN_RETRY_BUDGET_SEC}s: {e}"
                    ) from e
                time.sleep(_OPEN_RETRY_INTERVAL_SEC)
                continue

            # First, wait for bash to finish initializing before we
            # send anything: bash with readline calls tcflush(TCIFLUSH)
            # at startup to discard typed-ahead input, so anything we
            # send before the prompt appears is silently dropped (the
            # PTY echoes it but bash never executes it). The signal
            # we look for is the trailing `# ` or `$ ` of the first
            # prompt; bash always prints the bracketed-paste-enable
            # escape `\x1b[?2004h` immediately before the prompt, so
            # match either to be robust to PS1 variations.
            # Note: pexpect.EOF is deliberately NOT in the pattern list.
            # When EOF is listed as a sentinel, pexpect returns its index
            # instead of raising, and the `except pexpect.EOF` block
            # below would never fire — leaving us to fall through to
            # sendline() against a dead PTY and crash with
            # `OSError: [Errno 5] Input/output error`. Letting EOF raise
            # routes us into the transient-retry path where it belongs.
            try:
                proc.expect(
                    [rb"[#$] $", rb"\x1b\[\?2004h"],
                    timeout=15,
                )
            except pexpect.TIMEOUT:
                pass
            except pexpect.EOF:
                last_buf = proc.before if isinstance(proc.before, bytes) else b""
                exit_status = proc.exitstatus
                proc.close(force=True)
                print(
                    f"[tsh_terminal] open attempt {attempts} EOF before "
                    f"prompt: exit_status={exit_status} "
                    f"buf_tail={last_buf[-300:]!r}",
                    file=sys.stderr,
                )
                if (
                    _TRANSIENT_OPEN_RE.search(last_buf)
                    or exit_status == 255
                ) and time.time() < deadline:
                    time.sleep(_OPEN_RETRY_INTERVAL_SEC)
                    continue
                raise RuntimeError(
                    f"tsh session open EOF before prompt: "
                    f"exit_status={exit_status} "
                    f"buf_tail={last_buf[-500:]!r}"
                )
            # Settle briefly so bash is firmly inside readline before
            # we type at it.
            time.sleep(0.3)

            # Configure the remote shell once: turn off line-discipline
            # echo so input doesn't bleed into captured output, hide
            # PS1 so prompts don't pollute stdout, then emit a ready
            # sentinel on its own line. The `\\n` in the printf format
            # is what produces actual newlines around the sentinel —
            # the input echo only contains literal backslash-n, not
            # newline bytes, so the regex below matches the printf
            # output and not the echoed input.
            # Disable line-discipline echo, readline's bracketed-paste
            # mode (which injects `\x1b[?2004h/l` around every command
            # line and would otherwise contaminate captured output),
            # readline command-line editing, and PS1 so the prompt
            # doesn't print on the next line of output.
            ready_sentinel = b"__TBSESS_READY__"
            configure_line = (
                b"stty -echo 2>/dev/null; "
                b"bind 'set enable-bracketed-paste off' 2>/dev/null; "
                b"set +o emacs +o vi 2>/dev/null; "
                b"export PS1=''; "
                b"printf '\\n" + ready_sentinel + b"\\n'"
            )
            try:
                proc.sendline(configure_line)
                idx = proc.expect(
                    [
                        b"\n" + ready_sentinel + b"\r?\n",
                        pexpect.EOF,
                        pexpect.TIMEOUT,
                    ],
                    timeout=15,
                )
            except OSError as e:
                # Child died between the readiness-expect and sendline,
                # so writing to the PTY fails with EIO. Treat as a
                # transient open failure and retry within budget.
                last_buf = proc.before if isinstance(proc.before, bytes) else b""
                proc.close(force=True)
                print(
                    f"[tsh_terminal] open attempt {attempts} sendline "
                    f"failed ({e}); treating as transient. "
                    f"buf_tail={last_buf[-300:]!r}",
                    file=sys.stderr,
                )
                if time.time() >= deadline:
                    raise RuntimeError(
                        f"tsh session open OSError after "
                        f"{_OPEN_RETRY_BUDGET_SEC}s: {e}; "
                        f"buf_tail={last_buf[-300:]!r}"
                    ) from e
                time.sleep(_OPEN_RETRY_INTERVAL_SEC)
                continue
            except pexpect.ExceptionPexpect as e:
                last_buf = proc.before if isinstance(proc.before, bytes) else b""
                proc.close(force=True)
                if time.time() >= deadline:
                    raise RuntimeError(
                        f"tsh session open errored after "
                        f"{_OPEN_RETRY_BUDGET_SEC}s: {e}; "
                        f"buf_tail={last_buf[-300:]!r}"
                    ) from e
                time.sleep(_OPEN_RETRY_INTERVAL_SEC)
                continue

            if idx == 0:
                self._proc = proc
                print(
                    f"[tsh_terminal] session open: nodename={self.nodename} "
                    f"attempts={attempts}",
                    file=sys.stderr,
                )
                return

            # idx == 1 (EOF) or idx == 2 (TIMEOUT). proc.after in those
            # cases is the pexpect EOF/TIMEOUT *class*, not bytes — only
            # use proc.before for diagnostics.
            last_buf = proc.before if isinstance(proc.before, bytes) else b""
            exit_status = proc.exitstatus
            proc.close(force=True)
            print(
                f"[tsh_terminal] open attempt {attempts} did not reach "
                f"ready: idx={idx} exit_status={exit_status} "
                f"buf_tail={last_buf[-300:]!r}",
                file=sys.stderr,
            )
            transient = (
                _TRANSIENT_OPEN_RE.search(last_buf)
                or exit_status == 255
            )
            if not transient:
                raise RuntimeError(
                    f"tsh session open failed (non-transient): "
                    f"buf={last_buf[-500:]!r}"
                )
            if time.time() >= deadline:
                raise RuntimeError(
                    f"tsh session open did not become ready within "
                    f"{_OPEN_RETRY_BUDGET_SEC}s; "
                    f"buf={last_buf[-500:]!r}"
                )
            time.sleep(_OPEN_RETRY_INTERVAL_SEC)

    def exec_run(
        self,
        cmd: list[str] | str,
        timeout: float = _DEFAULT_EXEC_TIMEOUT_SEC,
    ) -> _TshExecResult:
        """Run a command in the persistent shell, returning exit code +
        stdout bytes.
        """
        if self._proc is None:
            raise RuntimeError("tsh session not open")

        if isinstance(cmd, list):
            cmd_str = " ".join(shlex.quote(c) for c in cmd)
        else:
            cmd_str = cmd

        marker = uuid.uuid4().hex
        line = (
            f"({cmd_str}); printf '\\n__TBEND_{marker}_%d__\\n' $?"
        ).encode()
        end_pat = re.compile(
            rb"\n__TBEND_" + marker.encode() + rb"_(\d+)__\r?\n"
        )

        self._proc.sendline(line)
        try:
            self._proc.expect(end_pat, timeout=timeout)
        except pexpect.TIMEOUT as e:
            raise TimeoutError(
                f"tsh exec timed out after {timeout}s: cmd={cmd_str!r}"
            ) from e
        except pexpect.EOF as e:
            buf = (self._proc.before or b"")[-300:]
            raise RuntimeError(
                f"tsh session ended unexpectedly during exec: "
                f"cmd={cmd_str!r}, buf_tail={buf!r}"
            ) from e

        exit_code = int(self._proc.match.group(1))
        output = self._proc.before or b""
        # bash with readline emits bracketed-paste enable/disable
        # escapes (`\x1b[?2004h`/`l`) around every command line. Even
        # with `bind 'set enable-bracketed-paste off'` configured, some
        # bash builds still emit them once on the first interactive
        # prompt. Strip globally so callers like the asciinema timestamp
        # parser get pure command stdout.
        output = _READLINE_NOISE_RE.sub(b"", output)
        return _TshExecResult(exit_code=exit_code, output=output)

    def close(self) -> None:
        if self._proc is None:
            return
        proc = self._proc
        self._proc = None
        try:
            # Politely exit the remote shell so Teleport sees a clean
            # session close — required for node-sync recording to
            # finalize the <sid>.tar promptly.
            proc.sendline(b"exit")
            try:
                proc.expect(pexpect.EOF, timeout=5)
            except pexpect.TIMEOUT:
                pass
        except (OSError, pexpect.ExceptionPexpect):
            pass
        finally:
            try:
                proc.close(force=True)
            except Exception:
                pass


def _get_or_open_session(self) -> _TshSession:
    """Look up the persistent session for this TmuxSession's container,
    opening one if absent. Keyed by container.id so the agent + tests
    TmuxSession instances of the same task share one SSH session.
    """
    cid = self.container.id
    sess = _SHARED_TSH_SESSIONS.get(cid)
    if sess is not None and sess._proc is not None:
        return sess
    nodename = _sanitize_nodename(self.container.name)
    sess = _TshSession(nodename)
    _SHARED_TSH_SESSIONS[cid] = sess
    return sess


def _patched_exec_run(self, cmd):
    """Replacement for TmuxSession._exec_run. Routes through the
    shared persistent tsh session for this TmuxSession's container.
    """
    return _get_or_open_session(self).exec_run(cmd)


def _patched_send_blocking_keys(self, keys, max_timeout_sec):
    """Replacement for TmuxSession._send_blocking_keys.

    Upstream calls `self.container.exec_run(...)` directly for the
    send-keys step (line 213 in tmux_session.py at the pinned commit),
    which bypasses our `_exec_run` patch. Re-route through `_exec_run`
    so this also flows over the persistent tsh session.
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


def _patched_tmux_stop(self):
    """Replacement for TmuxSession.stop.

    Mirrors upstream stop() (sends C-d to asciinema) but does NOT close
    the shared SSH session — that happens in _patched_terminal_stop
    after every TmuxSession in the Terminal has had a chance to send
    its C-d.
    """
    if self._recording_path:
        self._logger.debug("Stopping recording.")
        self.send_keys(keys=["C-d"], min_timeout_sec=0.1)


def _patched_terminal_stop(self):
    """Replacement for Terminal.stop.

    Sequence:
      1. Stop each TmuxSession (send C-d to asciinema).
      2. Close the shared persistent tsh session for this container —
         this is what causes Teleport to finalize the <sid>.tar.
      3. Tear down docker-compose (kills the container, including its
         in-container teleport node).
      4. Stop the livestreamer if any.
      5. Clear the sessions dict.

    Mirrors the original Terminal.stop body line-for-line; only step 2
    is new. If upstream changes Terminal.stop, this patch will diverge
    and needs updating — same maintenance posture as the TmuxSession
    patches.
    """
    for session in self._sessions.values():
        session.stop()

    if self.container is not None:
        sess = _SHARED_TSH_SESSIONS.pop(self.container.id, None)
        if sess is not None:
            sess.close()

    self._compose_manager.stop()

    if self._livestreamer:
        self._livestreamer.stop()

    self._sessions.clear()


def apply() -> None:
    """Monkey-patch TmuxSession + Terminal. Idempotent."""
    tmux_cls = _tmux_session.TmuxSession
    term_cls = _terminal.Terminal
    if getattr(tmux_cls, _PATCHED_SENTINEL, False):
        return
    tmux_cls._exec_run = _patched_exec_run
    tmux_cls._send_blocking_keys = _patched_send_blocking_keys
    tmux_cls.stop = _patched_tmux_stop
    term_cls.stop = _patched_terminal_stop
    setattr(tmux_cls, _PATCHED_SENTINEL, True)
    setattr(term_cls, _PATCHED_SENTINEL, True)
    print(
        "[tsh_terminal] TmuxSession + Terminal monkey-patch applied "
        "(persistent PTY, one recording per task)",
        file=sys.stderr,
    )


apply()
