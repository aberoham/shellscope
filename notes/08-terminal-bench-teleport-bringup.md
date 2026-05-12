# 08 — Terminal-Bench-Teleport bring-up (2026-05-10)

Operational status of the fixture pipeline designed in
[`07-terminal-bench-teleport-fixture.md`](07-terminal-bench-teleport-fixture.md).
Read 07 first if you want the *why*; this file is the *what runs* and
*how to run it*.

## Goal

One Teleport `<sid>.tar` recording per Terminal-Bench task, labelled
`operator.type=agent`, suitable for human-vs-agent cadence analysis.

## State as of 2026-05-10

End-to-end working: `hello-world` × `moonshot/moonshot-v1-8k` ran 100%
accurate and produced exactly one `.tar` recording (3.7 KB, 267 lines
when replayed via `tsh play`). Cluster, image, identity, token, and
runner are all current.

## Architecture (one paragraph)

Single auth+proxy Teleport OSS container on `tbench-fixture-net` (Docker
network). Each Terminal-Bench task container joins the same network and
runs an in-container `teleport` node that registers as a Node, named
after the container. The host runs the patched harness; its monkey-patch
opens **one persistent `tsh ssh -t agent@<node>` session per task**,
shared across the harness's "agent" and "tests" `TmuxSession` instances
via `container.id`, so the whole task produces a single Teleport
recording. tmux still runs inside the container; only the entry path
changed from `docker exec` to `tsh ssh`.

## Files that exist

```
harnesses/terminal-bench-teleport/
├── cluster/
│   ├── docker-compose.yaml          auth+proxy, ports 3080/3024/3025
│   ├── teleport.yaml                node-sync recording, debug_service: false
│   ├── up.sh / down.sh / bootstrap.sh
│   ├── .agent-identity              (gitignored — bootstrap output)
│   ├── .node-join-token             (gitignored — bootstrap output)
│   └── data/                        (gitignored — Teleport state + recordings)
├── image/
│   ├── Dockerfile.base              FROM t-bench python-3-13 + teleport binary + Netskope CA
│   ├── entrypoint.sh                joins cluster as Node, propagates env, exec's CMD
│   ├── build.sh                     stages netskope.crt and builds tbench-teleport-base:v17.7.20
│   └── netskope.crt                 (gitignored — staged from ~/.docker/certs.d/)
├── runner/
│   ├── patch_task.py                rewrites task Dockerfile + docker-compose.yaml
│   ├── tsh_terminal.py              the persistent-PTY monkey-patch (~310 LOC)
│   ├── run.py                       end-to-end driver (single task)
│   └── harvest.py                   collects <sid>.tar + writes labels.json sidecars
└── runs/<run-id>/<task-id>/         (gitignored — patched task + recordings + harvest output)
```

Plus, in `whodrove/tmp/`:
- `tbench-teleport-bringup.sh` — `up.sh` + `bootstrap.sh` + `build.sh`
- `tbench-teleport-runtask.sh <task-id> <model>` — sources `.env`,
  runs the harness via `uv run --with terminal-bench --with pyyaml --with pexpect`

## What changed in this session vs. the April scaffold

The April scaffold issued one `tsh ssh` per tmux subcommand, producing
many short recordings per task. This session:

1. **`runner/tsh_terminal.py` — full rewrite (per-call → persistent PTY).**
   New `_TshSession` class wraps a `pexpect.spawn` of `tsh ssh -t agent@<node>`.
   Output framing protocol: configure shell once with
   `stty -echo; bind 'set enable-bracketed-paste off'; set +o emacs +o vi; export PS1=''`,
   then per call send `({cmd}); printf '\n__TBEND_<uuid>_%d__\n' $?` and
   `expect()` for the unique end-marker. Strip readline noise
   (`\x1b[?2004[hl]`) from output. Sessions are keyed by
   `container.id` so the harness's "agent" and "tests" `TmuxSession`
   instances share one SSH session = one Teleport recording per task.
   Patches `TmuxSession._exec_run`, `_send_blocking_keys`, `stop`, and
   `Terminal.stop` (the Terminal patch is what closes the SSH session
   *after* every TmuxSession.stop runs but *before* docker-compose
   teardown — node-sync recording finalization needs a clean SSH close).
   Open-time retry budget: 60s on transient failures (exit 255,
   "could not connect", etc.). The `pexpect.spawn` waits for the bash
   prompt before sending input, because bash's readline runs
   `tcflush(TCIFLUSH)` on startup and silently drops any pre-typed-ahead
   bytes.

2. **`image/Dockerfile.base` — added Netskope CA layer.**
   `COPY netskope.crt → /usr/local/share/ca-certificates/`,
   `RUN update-ca-certificates`, `ENV UV_NATIVE_TLS=1 SSL_CERT_FILE=...`.
   This is the same fix the user already had at
   `harnesses/terminal-bench/tmp/patch_task_cert.sh` but applied once
   at the base layer so every patched task inherits.

3. **`image/build.sh` — stages the cert.**
   Copies `${NETSKOPE_CERT:-~/.docker/certs.d/netskope.crt}` into the
   build context as `netskope.crt`, builds, then removes it. Empty
   fallback if the host file isn't present.

4. **`image/entrypoint.sh` — propagates `UV_NATIVE_TLS` / `SSL_CERT_FILE`
   into `/etc/tbench-env.sh`.**
   Dockerfile `ENV` doesn't reach the tmux subshells the harness opens;
   without this, `uv` (which uses rustls and ignores the system store
   by default) can't reach pypi.org through Netskope. Was the only
   thing preventing `hello-world`'s test phase from passing.

5. **`cluster/teleport.yaml` — `debug_service: { enabled: false }`.**
   Teleport v17 tries to bind a UNIX socket in the data dir; Colima's
   9p/virtiofs bind mount on macOS rejects socket creation with EPERM,
   so the auth container crash-looped without this. The web UI / gRPC
   path is unaffected.

6. **`runner/run.py` — docstring + dep list updated** to mention
   `pexpect`.

7. **`README.md` — "How the monkey-patch works" + "Known gotchas"
   updated** to reflect the persistent-PTY shape and the four patched
   methods (was three; added `Terminal.stop`).

8. **`.gitignore` — added `image/netskope.crt`.**

## How to bring it up from cold

Prereqs: macOS host, Colima running (`colima start`),
`brew install teleport@17` (`tsh` 17.x — 18.x will fail
"missing required parameter ProxyAddress" against the v17 cluster),
`~/.docker/certs.d/netskope.crt` present, `uv` on PATH,
`harnesses/terminal-bench/.env` populated with API keys.

```
whodrove/harnesses/terminal-bench-teleport/bringup.sh        # cluster up + bootstrap + image build
whodrove/harnesses/terminal-bench-teleport/runtask.sh hello-world moonshot/moonshot-v1-8k
```

Tear down with `harnesses/terminal-bench-teleport/cluster/down.sh
[--wipe]`. The cluster keeps state across runs; only `--wipe` drops it.

## Verification checklist (single task)

- `cluster/data/log/records/<sid>.tar` — exactly one new `.tar` per
  task run (use `find … -newer` if you've run multiple).
- `tctl sessions ls` (via `docker compose exec teleport-auth tctl …`)
  shows the same SID with `Target=<sanitized-container-name>`.
- `tsh --insecure --skip-version-check --identity=<path> --proxy=localhost:3080
   play <sid> --format=text` produces clean text replay (no
   `\x1b[?2004[hl]` noise should be visible).
- `runs/<run-id>/<task-id>/recordings/<sid>.labels.json` has
  `operator.type=agent`, `fixture.task_passed=1`, etc.
- Terminal-Bench's `runs/<TS>/<task>/results.json` reports
  `is_resolved: true`.

## Known gotchas (live, hit during this bring-up)

- **`tsh` version skew** — v18 brew tsh against v17 cluster fails even
  with `--skip-version-check`. Pin to `brew install teleport@17`.
- **Colima debug-socket EPERM** — fixed via `debug_service.enabled:false`.
- **bash readline flushes pre-typed input** — the patch waits for the
  prompt (`# `, `$ `, or `\x1b[?2004h`) before sending the configure
  line, then sleeps 0.3s.
- **Bracketed-paste bytes leak into stdout** — disabled via `bind` AND
  stripped post-hoc (`_READLINE_NOISE_RE` in tsh_terminal.py); bash
  emits them at least once on the first prompt regardless of `bind`.
- **Netskope SSL inspection** — host MITMs HTTPS; container needs the
  Netskope CA in its trust store AND `UV_NATIVE_TLS=1` because uv uses
  rustls.
- **Dockerfile `ENV` does not propagate to tmux subshells** — the
  entrypoint writes the env into `/etc/tbench-env.sh` which is sourced
  from `/etc/profile.d`, `/etc/bash.bashrc`, and per-user `.bashrc`.

## Cost so far

One successful `hello-world` run via Moonshot is on the order of
$0.001. The fixture cluster is local (no cloud spend).

## What's still out of scope

- **Batch / parallel runs.** Driver is single-task by design.
- **Live ingest into `sessions.sqlite`.** `harvest.py` prints the
  suggested `whodrove-teleport parse` + `label set` invocations; the
  user runs them.
- **Submodule modifications.** The Terminal-Bench submodule is
  unchanged; all hooks are runtime monkey-patches.
- **Recordings on a remote / Cloud Teleport tenant.** Recordings live
  on the local Docker volume only.

## Cross-references

- [`07-terminal-bench-teleport-fixture.md`](07-terminal-bench-teleport-fixture.md) — design.
- [`harnesses/terminal-bench-teleport/README.md`](../harnesses/terminal-bench-teleport/README.md) — operator-facing docs (kept in sync with this).
- [`harnesses/terminal-bench-teleport/runner/tsh_terminal.py`](../harnesses/terminal-bench-teleport/runner/tsh_terminal.py) — the meaningful code surface.
