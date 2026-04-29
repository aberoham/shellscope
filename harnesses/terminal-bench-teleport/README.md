# terminal-bench-teleport

Drives Terminal-Bench tasks through a self-hosted Teleport OSS cluster on
local Docker (Colima on macOS), so each completed task produces exactly one
`<sid>.tar` session recording labelled `operator.type=agent` by
construction.

Architecture, scope, and the design decisions behind the layout below are
in [`notes/07-terminal-bench-teleport-fixture.md`](../../notes/07-terminal-bench-teleport-fixture.md).
Read that first.

## What is here

```
terminal-bench-teleport/
├── cluster/             Single-host Teleport OSS test cluster
│   ├── docker-compose.yaml   auth+proxy container on tbench-fixture-net
│   ├── teleport.yaml         server config; node-sync recording
│   ├── up.sh                 start cluster
│   ├── down.sh               stop cluster (--wipe to drop state)
│   └── bootstrap.sh          one-time: role + user + token + identity
├── image/               Base image for patched task containers
│   ├── Dockerfile.base       FROM t-bench python + teleport binary
│   ├── entrypoint.sh         joins cluster as Node, then exec's task CMD
│   └── build.sh
└── runner/              Python orchestration
    ├── patch_task.py         rewrites a task's Dockerfile + compose
    ├── tsh_terminal.py       monkey-patches TmuxSession to use tsh
    ├── run.py                end-to-end driver
    └── harvest.py            pulls recordings, emits label sidecars
```

## Prerequisites

- macOS host with Colima running (`colima start`).
- `tsh` binary on PATH: `brew install teleport`.
- Python 3.11+ with `terminal-bench` and `pyyaml` installable. The
  recommended invocation uses `uv` to pull deps transiently.

## End-to-end run (single task)

From the repo root:

```bash
# 1. Bring up the test cluster (idempotent).
harnesses/terminal-bench-teleport/cluster/up.sh

# 2. Bootstrap the cluster (creates role/user/token/identity files).
#    Re-runnable; rotates the token and identity each time.
harnesses/terminal-bench-teleport/cluster/bootstrap.sh

# 3. Build the base image once.
harnesses/terminal-bench-teleport/image/build.sh

# 4. Drive a single Terminal-Bench task end-to-end.
#    Use whichever LiteLLM-compatible model is current and cheap.
uv run --with terminal-bench --with pyyaml \
  python harnesses/terminal-bench-teleport/runner/run.py \
    --task-id hello-world \
    --agent terminus \
    --model openrouter/deepseek/deepseek-chat
```

After the run completes, recordings land at
`harnesses/terminal-bench-teleport/runs/<run-id>/<task-id>/recordings/<sid>.tar`,
each with a `<sid>.labels.json` sidecar. The runner prints suggested
`teleport-analyze parse` + `teleport-analyze label set` invocations to
ingest into shellscope's `sessions.sqlite`.

## Tear-down

```bash
harnesses/terminal-bench-teleport/cluster/down.sh         # stop, keep state
harnesses/terminal-bench-teleport/cluster/down.sh --wipe  # also drop state
```

## How the monkey-patch works

`tsh_terminal.py` swaps `TmuxSession._exec_run` so every tmux interaction
goes via `tsh ssh agent@<container-name>` instead of `docker exec
<container-name>`. The container management half of Terminal-Bench
(`DockerComposeManager`, file copies, post-agent test execution) keeps
using `docker exec` — that's harness scaffolding, not part of the operator
session, and must NOT pollute the recording.

The patch surface is small enough to maintain across submodule re-pins:
two methods of one class, with line-number references documented in the
file. If upstream renames or restructures `TmuxSession`, the runner will
fail loudly on import.

## Known gotchas

- **Self-signed cert.** The OSS test cluster auto-generates a self-signed
  cert on first start. The runner passes `--insecure` to `tsh`. Don't
  copy that flag into anything pointing at production Teleport.
- **Colima port forwarding.** The compose file publishes 3080 (multiplexed
  proxy/SSH/auth), 3024 (reverse tunnel), and 3025 (auth gRPC). If Colima
  isn't forwarding, `tsh --proxy=localhost:3080` won't connect.
- **`tsh` and `teleport` versions must match the cluster.** The cluster
  pins to v17.7.20 (matching `upstream-repo/`). v18 `tsh` against a v17
  proxy returns "missing required parameter ProxyAddress" even with
  `--skip-version-check`. `brew install teleport@17` if you have a newer
  brew tsh.
- **First call race.** When a task container starts, its in-container
  Teleport node takes ~3-10s to register with auth. The patched
  `_exec_run` retries with backoff for up to 30s on tsh exit-code 255
  ("could not connect"). If you see 255 after 30s, the entrypoint failed
  to start `teleport`; check `docker exec <container> cat /var/log/teleport.log`.
- **PTY required for recordings.** Non-PTY exec sessions emit `session.start`
  + `session.end` audit events but no recording bytes — the OSS file
  backend writes nothing when there's no `SessionPrint` stream. The
  patch forces `tsh ssh -t` so every `_exec_run` becomes a recorded PTY
  session. Side effect: a single Terminal-Bench task produces *many*
  short `<sid>.tar` files (one per harness call), not one. Aggregate
  downstream by the `fixture/run-id=<id>` Teleport node label.
- **TERM passthrough.** The host's `TERM` env var is forwarded into the
  remote PTY. Exotic terminals (Ghostty's `xterm-ghostty`, Kitty's
  `xterm-kitty`) aren't in the container's terminfo and tmux refuses to
  start. The patch overrides to `TERM=xterm-256color` for every tsh
  invocation.
- **Asciinema vs Teleport recording.** Both record. They capture different
  views (asciinema = the agent's perspective inside the container, written
  to a `.cast` file; Teleport = the audit-server-side recording, written
  to `<sid>.tar`). They do not collide because asciinema runs inside tmux
  while Teleport's recorder lives in the SSH service that brokered the
  session.

## What this does NOT do

- No batch / multi-task runner. The MVP is one task at a time. A
  parallel runner is a follow-up.
- No automatic ingest into `sessions.sqlite`. `harvest.py` prints the
  commands; the user runs them when ready.
- No live (frontier) model integration. Pass any LiteLLM-compatible
  model id; the runner does not care.
- No Cloud / EAS path — recordings stay on the local Docker volume.
