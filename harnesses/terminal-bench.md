# Terminal-Bench

## Source

- Canonical repository: <https://github.com/harbor-framework/terminal-bench>
  (the older `github.com/laude-institute/terminal-bench` URL redirects here)
- Successor framework: <https://github.com/laude-institute/harbor> (separate
  repo; recommended for running Terminal-Bench 2.0)
- Leaderboard, task gallery, and docs: <https://www.tbench.ai>
- Paper: Merrill, Shaw et al., *"Terminal-Bench: Benchmarking Agents on Hard,
  Realistic Tasks in Command Line Interfaces"*, arXiv:2601.11868
  (submitted 2026-01-17)
- Submodule path: `harnesses/terminal-bench/`
- Pinned commit: `1a6ffa9674b571da0ed040c470cb40c4d85f9b9b` (`main` HEAD,
  2026-01-21). Upstream publishes no release tags, so we pin to a commit on
  `main` rather than a tagged release.
- Access date: 2026-04-25

To re-pin to a different commit, from the project root:

```bash
cd harnesses/terminal-bench
git fetch
git checkout <commit-or-branch>
cd ../..
git add harnesses/terminal-bench
```

Update the pinned-commit line above when you do.

## What it is

Open-source benchmark for AI agents operating in a Linux terminal. Each task
is a Docker environment plus a natural-language instruction, a reference
("oracle") solution, and a pytest-based test script that scores whatever the
agent leaves behind in the container. The benchmark deliberately separates
the **task suite** from the **agent**: a reference agent called *Terminus*
operates purely through tmux without any dedicated tool calls, which lets
different scaffolds (Claude Code, Codex, Goose, Droid, etc.) be compared on
equal footing, and lets different models be compared inside the same
scaffold. The harness ships as a pip package exposing a `tb` CLI; the newer
*Harbor* framework (separate repository) is the upstream-recommended way to
run Terminal-Bench 2.0.

## What is in this submodule

Pinned at `1a6ffa9`, the relevant top-level layout is:

| Path                  | What it is |
|-----------------------|------------|
| `terminal_bench/`     | Python package — CLI (`cli/tb/`), agents, harness, dataset/registry, terminal handling. |
| `original-tasks/`     | The v1-era task corpus, 241 task directories at this pin. |
| `registry.json`       | Dataset manifest. Names each published dataset, the branch it lives on, and the commit hash to fetch. Confirms `terminal-bench-core` v0.2.x lives on the branch `dataset/terminal-bench-core/v0.2.x`, *not* on `main`. |
| `adapters/`           | Adapters for other benchmarks (SWE-bench, USACO). |
| `docker/`             | Base Docker images used by tasks. |
| `dashboard/`, `discord-bot/` | Community-facing surfaces; not load-bearing for running the harness. |

A typical task directory under `original-tasks/<task-id>/` contains:

- `task.yaml` — instruction text and metadata
- `Dockerfile` and `docker-compose.yaml` — environment definition
- `solution.sh` — the reference oracle solution
- `run-tests.sh` — entry point for the per-task verifier
- `tests/` — pytest scripts that score the agent's container state

The Terminal-Bench 2.0 task set (89 tasks per the paper) is **not** present
on this pinned commit; it lives on the upstream branch
`dataset/terminal-bench-core/v0.2.x` and is fetched on demand by the harness
when `--dataset-version 0.2.x` is passed.

## What a run produces

Per `tb run` invocation, for each task:

- A Docker container instantiated from the task's image.
- A tmux session that the agent drives. Terminal output is captured.
- A pytest verdict per test inside `tests/` after the agent terminates.
- Aggregated JSON results, plus optional asciinema-style recordings depending
  on flags.

These are the artifacts that make Terminal-Bench attractive as a fixture for
a Teleport-side classifier: each session has a known agent, a known model, a
known task instruction, and a ground-truth pass/fail label.

## History

- Originated at Laude Institute alongside the K Prize (Konwinski Prize), a
  $1M continually-updating SWE-Bench variant. Terminal-Bench is the
  generalisation: rather than "GitHub issue + Python PR," each task is a
  Dockerised environment with arbitrary success criteria.
- Beta release April 2025 as `terminal-bench-core v0.1.1`, ~80 hand-built
  tasks. Mike A. Merrill and Alexander G. Shaw led.
- Terminal-Bench 2.0 plus the Harbor framework launched November 2025. The
  2.0 task set was crowd-sourced from 93 contributors who proposed 229
  candidates, of which 89 survived a multi-stage curation pass.
- The arXiv paper appeared January 2026 (2601.11868). Frontier model+agent
  combinations score under 65% on Terminal-Bench 2.0 per the paper.

## Why this is in the repo

This file's framing is what `prior-art/AGENTS.md` forbids elsewhere, and
[`harnesses/README.md`](./README.md) explains why the rule is relaxed here:
the reason the submodule exists at all is to be runnable inside our setup.

What Terminal-Bench gives this project, concretely, is a **reproducible
source of agent-driven shell sessions with ground-truth labels** — task ID,
agent, model, pass/fail. Driving its containers from inside a Teleport SSH
session converts that into labeled Teleport audit/recording traffic, which is
exactly what step 2 / step 3 (`notes/06-pipeline-design-stub.md`) need to
evaluate against.

Open experimental angles, posed as questions, not commitments:

- Run a single Terminal-Bench task end-to-end inside a Teleport SSH session.
  Which of the four tap points enumerated in
  [`notes/05-tap-points-for-detection.md`](../notes/05-tap-points-for-detection.md)
  produces the cleanest "this is an agent, not a human" signal? Audit events?
  Recording payload? gRPC stream? Something else?
- Does detection accuracy correlate with task difficulty as scored by the
  upstream leaderboard?
- Does swapping the agent (Terminus vs. Claude Code vs. Codex vs. Goose) or
  the underlying model change the detector's signal in ways that imply we are
  detecting *the scaffold* rather than *agency*?
- Are per-task transcripts stable enough across reruns of the same agent +
  model + task to be usable as labeled training data, or does
  non-determinism (model sampling, network, container scheduling) wash that
  out?

These belong here as questions only. Concrete experiment design and any
detection logic belong in `notes/06-pipeline-design-stub.md`.

## How to run it (pointer only)

We do not redocument the upstream README. The minimum is, per the upstream
quickstart:

```bash
# install
uv tool install terminal-bench   # or: pip install terminal-bench

# from inside this submodule, run a single task with the reference agent
cd harnesses/terminal-bench
tb run \
  --agent terminus \
  --model anthropic/claude-3-7-latest \
  --task-id hello-world
```

For Terminal-Bench 2.0 specifically, follow the Harbor repo's quickstart
rather than this submodule's `tb` CLI.

## Open questions

Things upstream docs do not pin down at the access date above and that we
would want to confirm before treating Terminal-Bench output as a stable
fixture:

- The exact replay determinism guarantees of a `tb run` against the same
  task + agent + model. The README does not state this.
- Whether the harness emits a stable, machine-readable manifest for each run
  that we could use as the ground-truth join key against Teleport-side
  audit/recording artifacts, or whether we would need to wrap `tb run` in our
  own emitter.
- Whether the v0.2.x dataset is intended to be merged onto `main` over time
  or to remain on its dataset branch. `registry.json` only documents the
  current state.
- License / redistribution constraints for individual task definitions and
  Docker images, especially if we end up baking them into a fixture pipeline
  rather than fetching upstream at run time.
