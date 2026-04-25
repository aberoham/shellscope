# Teleport Session Recording Summaries

## Source

- Primary source: https://goteleport.com/docs/identity-security/session-summaries/
- Source title: "Session Recording Summaries"
- Vendor: Teleport / Gravitational, Inc.
- Product area: Teleport Identity Security
- Reviewed: 2026-04-25
- Visible docs version reviewed: 18.x
- Local input used: `notes/teleport-hosted-session-summaries.md`, an untracked
  scratch copy of the upstream documentation. Do not commit that source copy.

## What It Is

Teleport Session Recording Summaries is an Enterprise / Identity Security
feature that uses LLM inference to generate summaries for recorded interactive
sessions. It covers shell-oriented sessions and database sessions, and is
presented as a way for security or compliance reviewers to understand a
session before replaying the whole recording.

This is direct prior art for ShellScope because it already applies generative
AI to Teleport-recorded operational sessions.

## Relevant Capabilities

- Summarizes recorded interactive SSH sessions.
- Summarizes recorded Kubernetes sessions, including `kubectl exec` style
  activity.
- Summarizes recorded database sessions.
- Applies summarization automatically after a session finishes, if an
  inference policy matches.
- Supports policy routing by session kind and metadata such as resource labels,
  session participants, user traits, and related session/resource fields.
- Can use OpenAI, OpenAI-compatible gateways such as LiteLLM, and Amazon
  Bedrock depending on deployment mode.
- In Teleport Enterprise Cloud, newer tenants can use a Teleport-managed
  Bedrock-backed model named `teleport-cloud-default`.
- Produces a UI-visible summary attached to the session recording.
- Emits a `session.summarized` audit event after summarization, including risk
  level, model name, session type, username, session ID, and a short
  description.
- Exposes Prometheus metrics for monitoring the summarizer, labeled by
  inference model and, for OpenAI errors, API error code.

## Requirements and Operating Model

- Requires Teleport Enterprise with Identity Security.
- Teleport Enterprise Cloud requires a tenant on v18.2.0 or later; the
  Teleport-managed model path requires v18.7.1 or later.
- Self-hosted Teleport Enterprise requires v18.2.0 or later and access to an
  LLM API.
- AI summarization is disabled by default and must be explicitly enabled.
- Configuration is built around three Teleport resource types:
  `inference_model`, `inference_secret`, and `inference_policy`.
- Administrators need RBAC permissions to manage inference configuration.
  Teleport's preset `editor` role grants this by default; the docs also show a
  narrower role with read/list/create/update/delete on the inference resource
  types.
- Viewers need access to the relevant `session` resources. Teleport's preset
  `auditor` role is the simple path for viewing all recordings and summaries.
- OpenAI-compatible model configuration stores API credentials in an
  `inference_secret` resource referenced by an `inference_model`.
- Amazon Bedrock configuration requires the Teleport Auth Service to have AWS
  credentials and IAM permission for `bedrock:InvokeModel` against the selected
  model or inference profile.
- Policies choose which sessions are summarized and which model is used.
  Allowed session kinds are `ssh`, `k8s`, and `db`.

## Outputs and Integration Points

- Session recording UI: summary appears from the Audit -> Session Recordings
  flow after the recorded session has uploaded and inference has completed.
- Audit stream: summarized sessions generate `session.summarized` events.
  These can be forwarded to a SIEM for alerting on high-severity sessions.
- Policy language: policy filters use Teleport predicate language and can
  match against resource, session, and user fields.
- LLM gateway option: OpenAI-compatible routing can point at a custom
  `base_url`, making LiteLLM or another gateway a supported integration
  pattern.
- Monitoring: Prometheus metrics expose summarizer health/error signals.

## Limitations and Risks

- No on-demand summarization: summaries are generated automatically after a
  session ends when a matching policy applies.
- No re-summarization path is documented for a session that has already been
  processed.
- Session size is bounded by the model context window. Teleport lets operators
  configure `max_session_length_bytes`, but the byte-to-token ratio is
  approximate.
- Token spend controls are not built into the feature; the operator is
  responsible for controlling LLM budget.
- Summary accuracy is explicitly caveated because the feature relies on an LLM.
- Broad policies that summarize every session can create unnecessary cost and
  data exposure.
- For Teleport Cloud plus a self-managed OpenAI-compatible gateway, the gateway
  must be reachable by the Teleport Auth Service and protected like any other
  LLM API endpoint.

## Relevance to ShellScope

Teleport's feature is the closest known product prior art for the Teleport
slice of ShellScope. It means ShellScope should not be positioned merely as
"LLM summaries for Teleport sessions." That capability already exists in
Teleport Enterprise Identity Security.

Potential ShellScope differentiation:

- Vendor-neutral analysis across Teleport, raw SSH logs, Tailscale SSH,
  Kubernetes audit streams, asciinema/asciicast files, PAM session recordings,
  and other session sources.
- Research-grade comparison of data taps and evidence quality, not only a
  product summarization feature.
- Operator classification beyond summary: human vs bot vs AI agent, work type,
  automation candidates, and confidence.
- Batch and backfill workflows over existing object storage or audit archives.
- Explicit evidence model that records the source events/recording spans behind
  each summary or classification.
- Offline or bring-your-own-model pipelines where the analysis can remain
  inside a customer's environment.
- Evaluation methodology: false positives, omitted critical behavior,
  hallucinated findings, privacy leakage, and prompt-injection resistance.

## Open Questions

- What exact schema is used for the stored summary object attached to a
  recording?
- Are risk levels limited to a fixed enum, and where is that enum documented?
- Does the summary include structured timeline segments, or only a single
  narrative/risk artifact in the currently documented release?
- What session content is sent to the inference provider for each protocol:
  terminal output, BPF events, database queries, metadata, or a normalized
  intermediate representation?
- How are failed summarization attempts represented in audit events and
  metrics?
- What retention and deletion behavior applies to generated summaries when the
  underlying recording expires or is deleted?

