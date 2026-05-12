# Cadence fingerprinting

Detect agentic activity in Teleport session recordings and GCP
Cloud Audit Logs by per-model timing signature. The goal is not
"agent vs. human": it is naming the model when the fingerprint
matches one already measured, with a calibrated confidence on
either side of that call.

## What's here

notes/10-cadence-fingerprints.md
  Empirical reference. Measured cadence stats for Kimi K2.6,
  Gemini 3.1 Pro Preview, Opus 4.6, and GPT-5.5 against the
  vulnerable-secret task. Feature extraction, per-model scoring
  functions, edge-case handling. Read first.

notes/11-prompt-teleport-cadence-sweep.md
  Build prompt: Athena/S3 sweep of Teleport recordings, JSONL
  output, labels hand-off via the existing whodrove-teleport
  label-set flow.

notes/12-prompt-gcp-cadence-sweep.md
  Build prompt: BigQuery sweep of Cloud Audit Logs for
  gcloud-driven bursts, with re-calibration for the coarser cadence
  of API calls vs. terminal SessionPrint events.

notes/13-prompt-cadence-correlator.md
  Build prompt: cross-source correlator joining the Teleport and
  GCP sweeps by workforce identity and time overlap.

## Tooling

scripts/recordings/ holds utilities for working with Teleport
`<sid>.tar` recordings and the asciinema `.cast` files produced by
some agent harnesses. `plot_cadence.py` is the reference feature
extractor; the prompts above tell the implementation agent to
reuse it directly rather than reimplement.

## Reference artefacts

The four benchmark recordings, their cadence PNGs, and the 4x-sped
GIFs that anchor the visual story live on `~/Desktop` (gitignored,
not in the repo). Regenerate any of them with:

```
harnesses/terminal-bench-teleport/runtask.sh vulnerable-secret <model>
```

then post-process with `scripts/recordings/cast-to-slides.sh`.

## LiteLLM overrides

Some models in the comparison need provider-specific request
shaping that upstream LiteLLM does not yet handle. Claude Opus 4.7
rejects `temperature`. GPT-5.x only accepts `temperature=1` and
rejects nested `$ref` schemas. Vertex AI's Anthropic gateway
translates `response_format` into a tool_use call whose extraction
drops most fields. Those patches live in the forked terminal-bench
submodule at `harnesses/terminal-bench`; the fork URL is in
`.gitmodules`.
