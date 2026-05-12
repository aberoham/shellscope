# Agentic cadence fingerprints — measured reference

This file is the empirical knowledge base for the cadence-detection
work. The three sibling prompts (11–13) each cite it. Do not edit
it without re-running the underlying benchmark; the numbers below
are anchored to specific recordings under
`harnesses/terminal-bench-teleport/runs/*/vulnerable-secret/recordings/`.

## Source of the data

All four agent runs used:

- The same Terminal-Bench task (`vulnerable-secret`): reverse-engineer
  a stripped binary to extract a FLAG token, write it to
  `/app/results.txt`.
- The same agent framework (`terminus_1`).
- The same Teleport-instrumented `tsh ssh -t` PTY-backed session
  (`node-sync` recording mode).
- A persistent SSH session per task (one Teleport `<sid>.tar` per
  agent attempt — see `runner/tsh_terminal.py`).

Differences in cadence are therefore primarily a function of (a)
the provider's network and inference latency, (b) the model's
reasoning style, and (c) the granularity of commands the model
chose to issue.

## Per-model cadence (vulnerable-secret, May 2026)

| Model              | Provider           | Duration | Events | Bytes | E/s   | gap p50 | gap p90 | gap p99 | gap max | >1s | >5s | >30s |
|--------------------|-------------------:|---------:|-------:|------:|------:|--------:|--------:|--------:|--------:|----:|----:|-----:|
| Kimi K2.6          | Moonshot           |    738s  |    214 | 55 KB | 0.29  | 0.06s   | 11.2s   | 34.3s   |  127s   |  43 |  37 |    4 |
| Gemini 3.1 pp      | Google AI Studio   |    296s  |    219 | 36 KB | 0.74  | 0.00s   |  0.13s  | 29.7s   |  34.4s  |  18 |  15 |    2 |
| Opus 4.6           | Anthropic / Vertex |    268s  |    347 | 43 KB | 1.30  | 0.00s   |  4.7s   | 10.1s   |  15.8s  |  39 |  34 |    0 |
| GPT-5.5            | OpenAI             |    185s  |    277 | 58 KB | 1.49  | 0.00s   |  0.07s  | 18.4s   |  42.8s  |  16 |  12 |    1 |
| Human (reference)  | n/a                |    782s  |    619 | mixed | 0.79  | ~0.05–0.5s typing rhythm + 2–30s thought pauses; rarely >60s |

## Qualitative signatures

### Kimi K2.6 — "deliberative, sparse, very slow round-trips"

- **Killer feature:** at least one inter-event gap >60s (max 127s in
  the sample). Humans rarely freeze that long mid-task; other
  agents rarely either, because their provider latency is lower.
- Events/sec **0.15–0.5** — extremely low for an agent (humans
  range ~0.5–1.5 when actively typing).
- 4–6 pauses >30s in a typical multi-minute session.
- Few sub-100ms bursts. Kimi pattern: one command, long wait, one
  command. Output is dense per turn but turns are slow.

### Gemini 3.1 Pro Preview — "burst-then-think, sharp bimodality"

- **Killer feature:** strong bimodal gap distribution. p50 ≈ 0ms and
  p90 ≈ 100–300ms (command output streams out fast in a burst),
  but p99 jumps to 20–30s — sparsely populated middle band.
- Events/sec **0.5–1.0**.
- Two distinct pause classes: <250ms (within-burst) and >20s
  (between-burst thinking). Few pauses in the 1–4s
  "human-deliberation" band.
- Typical pause-count signature: 12–20 pauses >5s, of which 1–3 are
  >30s.

### Opus 4.6 — "metronome, no long thinking pauses"

- **Killer feature:** zero or near-zero pauses >30s, max gap
  typically <20s. Opus's tool-use planning spreads cognition
  across many small steps, so cadence never breaks down into a
  long-silent reasoning pause.
- Events/sec **1.0–1.5** — highest of any model.
- p99 inter-event gap typically <15s.
- 30–40 pauses >1s and 30+ pauses >5s, but **0** pauses >30s in a
  successful run.

### GPT-5.5 — "explosive bursts plus rare deep thought"

- **Killer feature:** tightest within-burst timing of any model —
  p90 under 100ms. When GPT-5.5 produces output, it's near-instant;
  when it's thinking, you'll see a single 20–45s pause and very few
  medium pauses.
- Events/sec **1.3–1.7**.
- Pause histogram has a fat-tailed shape: much smaller pause count
  than Opus, but with at least one >30s pause where Opus has none.
- Output bursts can dump 200+ bytes in <100ms — denser than Opus's
  even pacing.

### Human (negative control)

- Inter-keystroke gaps cluster around 50–300ms (typing rhythm).
- Thought pauses 1–10s scattered, occasionally 20–60s when reading
  documentation. Rarely the abrupt
  *0ms burst → 35s silence → 0ms burst* pattern that
  characterises Gemini/GPT-5.5.
- Bytes per event higher than any agent (humans paste; agents type
  one command at a time).
- High *local* variance of inter-event gaps over a sliding window
  (humans are uneven over short timescales), low *global*
  bimodality (humans don't have a single tight burst regime and a
  single far-off thinking regime).

## Features to compute per recording

For a sequence of SessionPrint event timestamps `t[]` (seconds since
session start) and byte counts `b[]`:

```python
import numpy as np

def features(t: np.ndarray, b: np.ndarray) -> dict:
    if len(t) < 2:
        return {"insufficient_data": True}
    gaps = np.diff(t)
    return {
        "duration":          float(t[-1] - t[0]),
        "n_events":          int(len(t)),
        "total_bytes":       int(b.sum()),
        "events_per_sec":    len(t) / max(1e-9, t[-1] - t[0]),
        "gap_p50":           float(np.percentile(gaps, 50)),
        "gap_p90":           float(np.percentile(gaps, 90)),
        "gap_p99":           float(np.percentile(gaps, 99)),
        "gap_max":           float(np.max(gaps)),
        "n_pause_gt_1s":     int((gaps > 1).sum()),
        "n_pause_gt_5s":     int((gaps > 5).sum()),
        "n_pause_gt_30s":    int((gaps > 30).sum()),
        # bimodality: fraction of gaps in burst band vs think band
        "burst_frac":        float((gaps < 0.2).mean()),
        "think_frac":        float((gaps > 5).mean()),
        # local variance over sliding 10-event windows
        "local_gap_std":     float(np.mean([
                                  np.std(gaps[i:i+10])
                                  for i in range(0, max(1, len(gaps) - 10))
                              ])),
    }
```

## Scoring functions (v0 thresholds, tune against labeled data)

```python
def score_kimi(f):
    s = 0.0
    if f["gap_max"] > 60:                 s += 0.4   # strongest signal
    if f["n_pause_gt_30s"] >= 2:          s += 0.2
    if 0.15 < f["events_per_sec"] < 0.5:  s += 0.2
    if f["burst_frac"] < 0.3:             s += 0.2
    return min(1.0, s)

def score_gemini(f):
    s = 0.0
    if f["gap_p90"] < 0.3 and f["gap_p99"] > 15: s += 0.5  # bimodality
    if 0.5 < f["events_per_sec"] < 1.0:          s += 0.2
    if f["n_pause_gt_30s"] >= 1:                 s += 0.15
    if f["think_frac"] > 0.06:                   s += 0.15
    return min(1.0, s)

def score_opus(f):
    s = 0.0
    if f["n_pause_gt_30s"] == 0 and f["gap_max"] < 20: s += 0.5  # killer
    if 1.0 < f["events_per_sec"] < 1.7:                s += 0.2
    if f["n_pause_gt_5s"] > 25:                        s += 0.15
    if f["gap_p99"] < 12:                              s += 0.15
    return min(1.0, s)

def score_gpt5(f):
    s = 0.0
    if f["gap_p90"] < 0.1 and f["gap_max"] > 20:       s += 0.5
    if f["events_per_sec"] > 1.3:                      s += 0.2
    if f["n_pause_gt_5s"] < 20 and f["n_pause_gt_30s"] <= 2: s += 0.15
    if f["burst_frac"] > 0.6:                          s += 0.15
    return min(1.0, s)

def score_human(f):
    s = 0.0
    if f["local_gap_std"] > 0.5:                       s += 0.3
    if 0.3 < f["burst_frac"] < 0.6:                    s += 0.2
    if f["n_pause_gt_30s"] <= 1 and f["gap_max"] < 60: s += 0.2
    if 0.4 < f["events_per_sec"] < 1.5:                s += 0.2
    if f["think_frac"] < 0.06:                         s += 0.1
    return min(1.0, s)

SCORERS = {
    "kimi-k2.6":  score_kimi,
    "gemini-3.1": score_gemini,
    "opus-4.6":   score_opus,
    "gpt-5.5":    score_gpt5,
    "human":      score_human,
}

def classify(f, threshold: float = 0.5):
    if f.get("insufficient_data"):
        return {"label": "insufficient_data", "scores": {}}
    scores = {k: fn(f) for k, fn in SCORERS.items()}
    label, top = max(scores.items(), key=lambda kv: kv[1])
    return {
        "label": label if top >= threshold else "ambiguous",
        "scores": scores,
        "confidence": top,
    }
```

## Edge cases to handle universally

- **Short sessions (<30 events):** percentile features collapse;
  emit `insufficient_data` rather than guess.
- **Idle prefix/suffix:** humans walk away. Strip any leading or
  trailing gap >120s before computing features.
- **Mixed sessions:** humans handing off to agents are real. Run a
  CUSUM or binary-segmentation pass on `events_per_sec` to find
  abrupt shifts in cadence; if found, score each segment
  independently.
- **Provider time-of-day variation:** Moonshot in particular gets
  slower during Asia/Pacific business hours; calibrate the Kimi
  thresholds with a per-hour multiplier if false negatives cluster
  by time.
- **Recording-mode mismatch:** SessionPrint only flows for
  PTY-backed sessions under `node-sync` or `proxy` recording. If
  the cluster runs `off`, the cadence sweep is blind — surface
  this as a coverage gap, not a clean run.

## Provenance and reuse

- The reference cadence extractor is
  `whodrove/scripts/recordings/plot_cadence.py`. Lift `parse_tar_events` and
  feature helpers from it; do not reimplement.
- All four GIFs/PNGs that anchor the visual narrative live on
  `~/Desktop/vulnerable-secret-<model>-{cadence,cast}.{png,gif}`.
- The benchmarked run-ids:
  - Kimi:   `whodrove/runs/2026-05-12__09-42-14/`
  - Gemini: `whodrove/runs/2026-05-12__11-23-13_3359cd/`
  - Opus:   `whodrove/runs/2026-05-12__10-57-50_b9fac3/`
  - GPT-5:  `whodrove/runs/2026-05-12__10-30-25_1bf852/`
