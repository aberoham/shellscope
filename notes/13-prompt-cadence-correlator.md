# Prompt: Cadence cross-correlator

## Mission

Given the outputs of `whodrove-cadence sweep teleport` and
`whodrove-cadence sweep gcp`, find cases where **the same workforce
identity** ran an agentic-cadence terminal session and an
agentic-cadence GCP burst that *overlap in time and match the same
model fingerprint*. These are the highest-signal flags: an analyst
can see both the keystroke side and the control-plane side of a
single agent's activity.

Read `10-cadence-fingerprints.md`, `11-prompt-teleport-cadence-sweep.md`,
and `12-prompt-gcp-cadence-sweep.md` first.

## Inputs

- JSONL from the Teleport sweep (one row per session segment).
- JSONL from the GCP sweep (one row per `gcloud` burst).
- An identity-bridge function that maps Teleport `teleport_user` to
  the workforce email used as `principalEmail` in GCP audit logs.
  At THG this is typically `<short-id>@thehutgroup.com`. The
  mapping logic lives in the existing audit-log plumbing — reuse
  it; the function should already exist in `whodrove/identity/`
  (verify before assuming).

## What to build

```
whodrove-cadence correlate \
  [--teleport-jsonl path]   # default: latest sweep output
  [--gcp-jsonl path]        # default: latest sweep output
  [--time-tolerance 60]     # seconds of allowed offset on either edge
  [--require-same-model]    # if set, both sides must classify to same label
  [--min-confidence 0.5]    # min score on either side
  [--format jsonl|parquet]
  [--out path]
```

Behaviour:

1. Load both JSONL streams.
2. Build a Teleport index keyed by `workforce_email`,
   `(started_at, ended_at)`.
3. For each GCP burst, find Teleport sessions where the workforce
   email matches and the time intervals overlap (with
   `--time-tolerance` slack on either edge).
4. For each overlap pair, emit a `correlation` row scoring how
   well they agree.

## Correlation row schema

```json
{
  "correlation_id": "sha1(sid||burst_id)",
  "workforce_identity": "abe@thehutgroup.com",
  "teleport": {
    "sid": "...",
    "label": "opus-4.6",
    "confidence": 0.95,
    "started_at": "...",
    "ended_at":   "...",
    "server_hostname": "...",
    "server_labels": { "...": "..." }
  },
  "gcp": {
    "burst_id": "...",
    "label": "opus-4.6",
    "confidence": 0.78,
    "started_at": "...",
    "ended_at":   "...",
    "principal":  "...",
    "caller_ip":  "...",
    "user_agent": "...",
    "method_examples": [ "..." ]
  },
  "overlap_seconds": 312.5,
  "model_agreement": true,
  "joint_score": 0.86,
  "tier": "high",
  "swept_at": "..."
}
```

`joint_score` and `tier` (`high` / `med` / `low`) computed:

```python
def joint(corr):
    t = corr["teleport"]["confidence"]
    g = corr["gcp"]["confidence"]
    agree = corr["model_agreement"]
    overlap_frac = corr["overlap_seconds"] / max(
        1.0,
        max(corr["teleport"]["duration_sec"], corr["gcp"]["duration_sec"]),
    )
    base = (t + g) / 2
    if agree:
        base += 0.10
    base *= 0.5 + 0.5 * min(1.0, overlap_frac)
    score = min(1.0, base)
    if score >= 0.75 and agree: tier = "high"
    elif score >= 0.55:         tier = "med"
    else:                       tier = "low"
    return score, tier
```

## Workflows the correlator must support

1. **Forensics:** "Show me every high-tier correlation in the last
   72 hours for user X." Filterable from the JSONL via `jq` —
   verify it works.

2. **Tabletop demos:** ingest a single recording + a synthetic GCP
   burst, output a single correlation row, render the markdown
   summary used in Identity Summit-style demos. Add
   `whodrove-cadence correlate --markdown` that emits a Slack/Notion-
   friendly summary per correlation.

3. **Continuous monitoring:** designed to be a fan-in step in a
   cron sweep (`sweep teleport` + `sweep gcp` + `correlate`),
   idempotent on inputs, safe to re-run. Use the deterministic
   `correlation_id` so downstream sinks dedupe naturally.

## CLI integration with whodrove

All three subcommands (`sweep teleport`, `sweep gcp`, `correlate`)
ship under a single entrypoint `whodrove-cadence`, installed
alongside `whodrove-teleport`. Use the same Typer/Click flavor the
rest of the repo uses (`whodrove-teleport` is the prior art; match
its argument style).

Add three convenience top-level scripts in `whodrove/scripts/cadence/`
(new directory; mirrors the existing `whodrove/scripts/recordings/`
and `whodrove/scripts/teleport-eas/` layout):

- `sweep-teleport.sh` — wraps the Teleport sweep with the cluster's
  default Athena project + S3 bucket.
- `sweep-gcp.sh` — wraps GCP sweep with default
  `--project thg-dev-audit-sink` (or whatever the actual sink is).
- `watch.sh` — chains all three and tails the correlation output.

Match the convention of the existing fixture wrapper at
`harnesses/terminal-bench-teleport/runtask.sh`: self-contained,
sources `.env`, no multi-line shell in comments
(global rule in `~/.claude/CLAUDE.md`).

## Tests

- A fixture-driven test that runs both sweeps over local fixture
  data and asserts the correlator emits a `high`-tier row for the
  one known case where the same identity has both signals.
- A confounder test: GCP burst from one identity, Teleport session
  from a different identity, same time window. Assert
  **no** correlation row is emitted (or tier `low` only with a
  clear reason field).

## Privacy and data handling

- Workforce identity mapping should pass through whatever the
  existing audit-log code already does — do not call new external
  identity services from this tool.
- The GIF/PNG cadence renders are NOT included in output JSONL.
  Path references only. A reviewer can fetch them via the existing
  whodrove labels lookup (they're already in `~/Desktop/` per the
  cadence-comparison work).
- Output JSONL contains identity info — write it to
  `whodrove/runs/cadence/<timestamp>/` (mode 0700) by default, not
  the working directory.

## Open questions to confirm with the user before shipping

1. **Identity bridge function** — does
   `whodrove/identity/teleport_to_workforce()` exist already? If
   not, agree on its location and signature with the user before
   adding to a shared module.
2. **GCP audit sink project** — there isn't a single THG-wide one;
   confirm which one to default to.
3. **Output sink** — should correlation rows feed back into
   `sessions.sqlite` (as additional labels on the Teleport sid) or
   stand alone as their own table? First cut: additional labels
   with `set-by cadence-correlator@v1`.
