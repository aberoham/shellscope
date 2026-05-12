# Prompt: Teleport cadence sweep

## Mission

Build a tool that scans Teleport session recordings in Athena/S3 and
labels each one with the model fingerprint it most resembles (or
`human`, or `ambiguous`, or `insufficient_data`). Output is JSONL
per session, plus a hand-off that lets the rows be ingested via the
existing `whodrove-teleport label set` flow so analysts can pivot
in the same sessions.sqlite they already use.

**Read `10-cadence-fingerprints.md` first.** It defines the feature
set, the per-model scoring functions, and the edge cases. This
prompt only specifies the I/O, glue, and ergonomics.

## Inputs

1. An Athena database with Teleport audit events (parquet on S3,
   partitioned by year/month/day). Verify the actual table name and
   schema in `whodrove/notes/04-cloud-and-external-audit-storage.md`
   before assuming; do not hardcode the schema from training data.
2. An S3 prefix where session recordings (`<sid>.tar`) live, also
   documented in note 04.
3. Existing `whodrove/scripts/recordings/plot_cadence.py` — reuse its
   `parse_tar_events()` to extract `SessionPrint` events from a
   `.tar`. Do not reimplement.

## What to build

A `whodrove-cadence sweep teleport` subcommand with:

```
whodrove-cadence sweep teleport \
  --since 24h \
  [--user <teleport-user>] \
  [--label key=value] \
  [--cluster <name>] \
  [--limit N] \
  [--format jsonl|parquet] \
  [--out path]
```

Behaviour:

1. Query Athena for candidate sessions in the time window, filtered
   by `proto=ssh`, `event in ('session.start','session.end')`,
   honoring the optional `--user` and `--label` filters. Use
   `session.start` / `session.end` to compute the recording window
   bounds; SessionPrint timing comes from the `.tar`, not Athena.

2. For each candidate, `boto3 s3.get_object` the `<sid>.tar`,
   stream-parse with the existing `parse_tar_events()`, build the
   `(timestamps, byte_counts)` arrays, and run `features(...)` +
   `classify(...)` from note 10.

3. Pre-classification housekeeping (defined in note 10):
   - Strip leading/trailing idle >120s before computing features.
   - If `n_events < 30`, label `insufficient_data`, skip features.
   - Run CUSUM on `events_per_sec` over 30-event windows; if a
     break is found with effect size > 1.5× pooled stddev, segment
     and classify each piece separately. Emit one row per segment
     with a `segment_index` column.

4. Emit one JSONL row per (session, segment) with:

   ```json
   {
     "sid": "...",
     "cluster": "...",
     "teleport_user": "...",
     "unix_login": "...",
     "server_hostname": "...",
     "server_labels": {"...": "..."},
     "started_at": "2026-05-12T08:42:06.968Z",
     "ended_at":   "2026-05-12T08:54:30.123Z",
     "segment_index": 0,
     "segment_started_at": "...",
     "segment_ended_at": "...",
     "features": { ...all keys from features() in note 10 },
     "scores": { "kimi-k2.6": 0.40, "gemini-3.1": 0.05, "opus-4.6": 0.95, "gpt-5.5": 0.10, "human": 0.20 },
     "label": "opus-4.6",
     "confidence": 0.95,
     "recording_s3_uri": "s3://.../recordings/<sid>.tar",
     "schema_version": 1,
     "swept_at": "2026-05-12T11:00:00Z"
   }
   ```

5. Hand-off to whodrove labels: emit a sibling script (`--apply` or
   a follow-up subcommand `whodrove-cadence apply teleport
   <jsonl>`) that issues, for each row above its threshold:

   ```
   whodrove-teleport label set --session <sid> --key agent.suspected_model --value <label>   --set-by cadence-sweep@v1
   whodrove-teleport label set --session <sid> --key agent.suspicion_score --value <conf>    --set-by cadence-sweep@v1
   whodrove-teleport label set --session <sid> --key operator.type         --value agent     --set-by cadence-sweep@v1
   ```

   Match the existing `set-by` convention (`fixture@v0` already used
   by harvest; use `cadence-sweep@v1` to namespace this tool).

## Module layout

```
whodrove/
  cadence/
    __init__.py
    features.py       # parse_tar_events (lifted from plot_cadence.py),
                      # features(), classify(), SCORERS, scoring fns
    teleport.py       # athena_query, s3_fetch_tar, sweep_teleport()
    gcp.py            # (sibling, see 12-prompt-gcp-cadence-sweep.md)
    correlate.py      # (sibling, see 13-prompt-cadence-correlator.md)
    cli.py            # `whodrove-cadence` typer/click entrypoint
```

## Athena query — seed pattern

```sql
WITH bounds AS (
  SELECT
    sid,
    cluster_name AS cluster,
    "user"            AS teleport_user,
    login             AS unix_login,
    server_hostname,
    server_labels,
    MIN(time) FILTER (WHERE event = 'session.start') AS started_at,
    MAX(time) FILTER (WHERE event = 'session.end')   AS ended_at
  FROM teleport_audit
  WHERE year  = '<YYYY>'
    AND month = '<MM>'
    AND day   IN (<days-in-window>)
    AND event IN ('session.start','session.end')
    AND proto = 'ssh'
  GROUP BY 1,2,3,4,5,6
)
SELECT *
FROM bounds
WHERE date_diff('second', started_at, ended_at) >= 30
  AND teleport_user LIKE COALESCE(:user_filter, '%')
ORDER BY started_at DESC
LIMIT :limit;
```

If `server_labels` is a Map type in your schema, filter via
`server_labels[:label_key] = :label_value`. Confirm before
committing — note 04 has the exact column type.

## S3 fetch

The recording URI scheme is in note 04. Reference example:

```python
import boto3, io, tarfile
s3 = boto3.client("s3")

def fetch_tar(bucket: str, key: str) -> bytes:
    obj = s3.get_object(Bucket=bucket, Key=key)
    return obj["Body"].read()

def events_from_s3(bucket: str, key: str):
    raw = fetch_tar(bucket, key)
    return parse_tar_events(io.BytesIO(raw))  # reused from plot_cadence.py
```

For large clusters, prefer `boto3.resource` with multipart concurrent
fetch only if you actually observe throughput problems; the default
single-threaded `get_object` will handle most recordings (they're
KB-MB, not GB).

## Cost guardrails

- Always pass `--limit` in the CLI default (e.g., 500) so a runaway
  scan doesn't dump every session in the cluster.
- The Athena query is cheap (audit parquet is small) but the S3
  GET-per-session is the dominant cost driver. Cache features by
  `<sid>` in `sessions.sqlite` so re-runs of the sweep don't refetch
  the `.tar`. Schema: `(sid TEXT PRIMARY KEY, features_json TEXT,
  computed_at TIMESTAMP)`.

## Tests

- A small fixture corpus committed under
  `whodrove/cadence/tests/fixtures/` with one `.tar` per model
  (lift the four runs cited in note 10). Each fixture has a known
  expected label; `pytest` asserts `classify()` returns it with
  `confidence >= 0.5`.
- Add a `human-reference.tar` fixture: the human run from
  `whodrove/runs/.../human-vulnerable-secret-20260511-*`. Confirm
  human-label scoring on it.
- A regression test for the segmentation path: synthesize a
  recording that's the first half of Kimi appended to the second
  half of Opus, assert the classifier emits two segments with
  different labels.

## Out of scope for this tool

- Do **not** ingest into `sessions.sqlite` directly. Emit JSONL,
  let `whodrove-teleport label set` consume it. Single
  responsibility per binary, matches the existing pipeline shape.
- Do **not** build a UI or graph output. The cadence PNG renderer
  already exists at `whodrove/scripts/recordings/plot_cadence.py`; reuse it for any
  per-session debug rendering (`whodrove-cadence plot <sid>` if you
  want a one-line wrapper, otherwise skip).
- Do **not** attempt to detect *non-terminal* agent activity here.
  GCP audit logs are the sibling prompt (12); cross-correlation is
  prompt 13.
