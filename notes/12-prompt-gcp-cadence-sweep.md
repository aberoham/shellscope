# Prompt: GCP audit-log cadence sweep

## Mission

Detect agentic behaviour in GCP control-plane traffic — specifically
sequences of `gcloud`-driven API calls that, when grouped by
principal and time, match one of the cadence fingerprints documented
in `10-cadence-fingerprints.md`. The hypothesis: when an agent
drives `gcloud` instead of a terminal directly, the API-call timing
inherits the agent's per-turn latency pattern, even though the
terminal layer is bypassed.

**Read `10-cadence-fingerprints.md` first** for features, scoring
functions, and the qualitative signatures. This prompt only covers
GCP-specific I/O and re-calibration.

## Inputs

1. A BigQuery sink containing Cloud Audit Logs
   (`cloudaudit_googleapis_com_activity_*` and optionally
   `cloudaudit_googleapis_com_data_access_*`). Verify the actual
   sink in the user's GCP org; do not assume.
2. ADC creds with `bigquery.jobs.create` on whichever project owns
   the sink.

## What to build

A `whodrove-cadence sweep gcp` subcommand:

```
whodrove-cadence sweep gcp \
  --project <bq-project> \
  --since 24h \
  [--principal <email>] \
  [--user-agent-regex <regex>] \
  [--burst-gap 60] \
  [--min-events 30] \
  [--limit N] \
  [--format jsonl|parquet] \
  [--out path]
```

Behaviour:

1. Query BigQuery for audit log entries matching
   `userAgent regex google-cloud-sdk|gcloud` (default; overridable
   via `--user-agent-regex`).
2. Group entries by `(principalEmail, callerIp, userAgent)` and
   split into **bursts** where the gap between consecutive entries
   exceeds `--burst-gap` seconds (default 60s). Each burst becomes
   one cadence-scoring unit.
3. For each burst with `n_events >= --min-events`, compute features
   and classify using the same functions from note 10.
4. Apply the **re-calibration adjustments below** before reporting.
5. Emit JSONL with one row per burst.

## Re-calibration vs. terminal sessions

GCP audit cadence is **coarser** than terminal SessionPrint cadence:

- One terminal SessionPrint event ≈ one chunk of streamed command
  output. A model turn produces many of these.
- One audit-log entry ≈ one API call, which typically equals one
  agent-tool-use turn.

So the *absolute* thresholds in note 10 will misfire on audit logs
(everything will look "Kimi-style slow"). Apply a per-feature
scaling factor before classification:

```python
# Audit-log entries are ~5-10x sparser than terminal SessionPrint
# events. Scale gap-derived features by 1/8 before applying the
# scorers from note 10.

AUDIT_GAP_SCALE = 1.0 / 8.0  # tune from labeled corpus

def features_for_audit(timestamps_sec):
    f = features(timestamps_sec, byte_counts=np.zeros_like(timestamps_sec))
    for k in ("gap_p50","gap_p90","gap_p99","gap_max"):
        f[k] *= AUDIT_GAP_SCALE
    # events_per_sec scales the other direction:
    f["events_per_sec"] /= AUDIT_GAP_SCALE
    return f
```

Document the scaling factor in the output row so reviewers can see
it (`"gap_scale_factor": 0.125`). When you've calibrated against a
labelled GCP corpus, replace the heuristic with provider-specific
scorers — the *shape* (bimodal vs. metronome vs. sparse) will hold,
the magnitudes won't.

## Why bytes are missing

Audit log entries don't carry a payload-bytes equivalent that
parallels terminal SessionPrint byte counts. Drop
`total_bytes` and `burst_frac` from the byte-related scoring
heuristics, but keep the gap-based ones. The signature differences
between Gemini's bimodality and Opus's metronome are gap-driven, so
this is fine.

## BigQuery — seed pattern

```sql
WITH gcloud_calls AS (
  SELECT
    protoPayload.authenticationInfo.principalEmail   AS principal,
    protoPayload.requestMetadata.callerIp            AS caller_ip,
    protoPayload.requestMetadata.callerSuppliedUserAgent AS user_agent,
    protoPayload.methodName                          AS method,
    protoPayload.serviceName                         AS service,
    resource.type                                    AS resource_type,
    timestamp
  FROM `<bq-project>.<dataset>.cloudaudit_googleapis_com_activity_*`
  WHERE _PARTITIONTIME >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 1 DAY)
    AND REGEXP_CONTAINS(
          protoPayload.requestMetadata.callerSuppliedUserAgent,
          r'google-cloud-sdk|gcloud'
        )
)
SELECT *
FROM gcloud_calls
ORDER BY principal, timestamp;
```

Then group into bursts client-side:

```python
def burst_partition(rows, gap_seconds=60):
    """Yield (principal, caller_ip, user_agent, [rows]) for each burst."""
    rows = sorted(rows, key=lambda r: (r["principal"], r["caller_ip"], r["timestamp"]))
    cur_key, cur_burst, cur_last_ts = None, [], None
    for r in rows:
        key = (r["principal"], r["caller_ip"], r["user_agent"])
        if cur_key != key or (cur_last_ts and (r["timestamp"] - cur_last_ts).total_seconds() > gap_seconds):
            if cur_burst:
                yield (*cur_key, cur_burst)
            cur_key, cur_burst = key, [r]
        else:
            cur_burst.append(r)
        cur_last_ts = r["timestamp"]
    if cur_burst:
        yield (*cur_key, cur_burst)
```

## Output row schema

```json
{
  "burst_id": "sha1(principal||caller_ip||first_ts||last_ts)",
  "principal": "abe@thehutgroup.com",
  "caller_ip": "192.0.2.1",
  "user_agent": "google-cloud-sdk/472.0.0 ...",
  "started_at": "2026-05-12T08:42:06.968Z",
  "ended_at":   "2026-05-12T08:54:30.123Z",
  "n_events": 87,
  "services": ["compute.googleapis.com","iam.googleapis.com"],
  "method_examples": ["compute.instances.list","iam.serviceAccounts.get","..."],
  "features": { ...same shape as terminal sweep },
  "gap_scale_factor": 0.125,
  "scores": { "kimi-k2.6": 0.20, "gemini-3.1": 0.85, "opus-4.6": 0.05, "gpt-5.5": 0.30, "human": 0.45 },
  "label": "gemini-3.1",
  "confidence": 0.85,
  "schema_version": 1,
  "swept_at": "2026-05-12T11:00:00Z"
}
```

## Hand-off

Unlike the Teleport sweep, GCP bursts don't have a Teleport `sid` to
label. Either:

a. Write the rows into a new `whodrove` table (`gcp_cadence_bursts`)
   alongside `sessions`, with the same labels schema so analysts
   can pivot.

b. Push the rows into a Cloud Logging custom log so the existing
   GCP-side monitoring workflows can alert on them.

Pick (a) for the first cut; it stays inside the existing whodrove
data model.

## Tests

- Synthetic burst fixtures: hand-craft three burst sequences with
  timing patterns matching Gemini bimodality / Opus metronome /
  human variance. Assert classifier returns the expected label.
- A "no-agent" fixture: a long, slow burst of `gcloud
  compute instances list` calls from a Terraform `apply` run.
  Assert classifier returns `human` or `ambiguous` (Terraform's
  cadence shouldn't match any agent fingerprint).

## Edge cases unique to GCP

- **Service accounts:** when an agent runs under a service-account
  ADC, `principalEmail` looks like
  `<sa-name>@<project>.iam.gserviceaccount.com`. Don't filter these
  out — agents using SA creds are exactly what this is meant to
  catch.
- **Workload Identity Federation:** principal can be a federated
  external identity (`principal://iam.googleapis.com/projects/...`).
  Treat as opaque; classify by cadence regardless.
- **Bursts spanning a Cloud-Logging partition boundary:** dual-read
  yesterday + today and de-duplicate by `insertId` before grouping.
- **API method classes:** if the burst is 100% reads of one
  resource type (`compute.instances.list` repeated), de-prioritise
  the agent label — that's almost certainly a polling script, not
  agent reasoning. Add a `dominant_method_share` feature; if
  `>0.95`, downgrade scores or label `polling`.

## Out of scope for this tool

- **Not** Cloud Trace spans. They're useful for application-level
  performance debugging, but Cloud Audit Logs are the right
  surface for authenticated-action cadence.
- **Not** the cross-correlation with Teleport sessions — that's
  prompt 13.
- **Not** alerting / paging. Emit JSONL; let downstream pipelines
  fan out to alerts as they already do for whodrove labels.
