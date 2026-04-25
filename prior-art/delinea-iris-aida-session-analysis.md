# Delinea Iris AI: AI-Driven Auditing (AIDA) of Session Recordings

## Source

- Primary docs: https://docs.delinea.com/online-help/delinea-platform/insights/session-record/analyze-record/aida.htm
- Companion overview: https://docs.delinea.com/online-help/delinea-platform/insights/session-record/analyze-record/index.htm
- Product page: https://delinea.com/products/ai-driven-auditing
- Iris umbrella product page: https://delinea.com/products/delinea-platform-iris-ai
- Demo page: https://delinea.com/demos/delinea-ai-driven-auditing-demo
- Press coverage of the August 2025 launch:
  https://siliconangle.com/2025/08/05/delinea-introduces-iris-ai-enhance-identity-security-real-time-access-control/
- Partner write-up:
  https://www.somerfordassociates.com/blog/how-delinea-iris-ai-is-automating-the-manual-nightmare-of-pam-auditing/
- Vendor: Delinea, Inc.
- Product area: Delinea Platform / Insights / Session Record (the cloud platform
  successor to Secret Server and Server PAM session recording).
- Reviewed: 2026-04-25
- Visible doc set reviewed: live Delinea Platform online help (no version
  string is exposed on the documentation pages themselves).

## What It Is

Delinea uses three overlapping names for what is, in practice, one feature
applied to recorded privileged sessions:

- **Iris AI** is the umbrella brand for Delinea's "natively integrated AI
  engine" inside the Delinea Platform. Iris currently has at least two named
  capability streams: **Authorization powered by Iris AI** (real-time access
  decisioning) and **Auditing powered by Iris AI** (analysis of recorded
  sessions). Iris was announced publicly at Black Hat USA 2025.
- **AI-Driven Auditing**, abbreviated **AIDA**, is the feature that performs
  the post-session analysis. The marketplace SKU and subscription metering
  ("AI-Driven Auditing hours") use this name.
- **Iris Auditing** is the in-product label for the same feature in the
  current docs. Delinea's documentation uses "Iris Auditing" and "Auditing
  powered by Iris AI" interchangeably with AIDA, without distinguishing them.

The thing being analyzed is a **Delinea Platform session recording**: a
captured privileged SSH or RDP session originally produced by the Server PAM
session-recording pipeline that the Platform inherits from Secret Server and
Server PAM.

## Relevant Capabilities

- Analyzes recorded interactive **SSH** sessions captured by the Delinea
  Platform.
- Analyzes recorded interactive **RDP** sessions, but with an explicitly
  documented restriction: for RDP, only activity inside a PowerShell terminal
  window is currently in scope. Pure GUI RDP work is not analyzed.
- Operates on three time-aligned input streams produced by the recorder:
  - **Visual frame OCR** over high-resolution screen captures, used to read
    on-screen commands, output, file paths, and SQL queries.
  - **Keystroke log** with timestamps and window-focus context.
  - **Process trace** of background processes spawned during the session.
- Emits per-session structured output:
  - A natural-language summary covering Summary of Activities, Critical
    Errors or Warnings, and Outcome of the Session.
  - A timeline-aligned **Activity panel** that groups captured commands by
    activity, with full output, timestamps, and AI-assigned labels.
  - A **heat map** rendered along the recording's timebar that highlights
    keystrokes, processes, and anomalous segments. Clicking a heat-map
    region jumps the player to that point in the recording.
  - A controlled **label vocabulary** (see Outputs section below).
- Provides keyword search and label-based filtering across analyzed sessions
  on the Session Recording list view.
- Drives the analysis from a **policy** that selects which sessions get
  analyzed. Policies are scoped by Subjects (initiating users), Targets
  (specific computers, servers, or collections), and optional Conditions
  (date range, day of week, time of day).
- Tracks consumption against a purchased pool of **AI-Driven Auditing hours**
  visible under Marketplace -> Subscriptions; analysis stops once the
  allocation is consumed.
- Positioned as supporting both completed sessions and recently completed
  ("near real-time") sessions; the product page claims "real-time alerts on
  high-risk activity," but the docs describe per-recording analysis rather
  than streaming.

## Requirements and Operating Model

- Requires the **Delinea Platform** (the cloud-native platform that is the
  successor to and integrator of Secret Server and Server PAM). AIDA is not
  documented as available for legacy on-prem Secret Server installations
  outside the Platform.
- Requires an **AI-Driven Auditing subscription** measured in analysis hours.
  Consumption is visible in the Marketplace UI and analysis halts when the
  allocation is exhausted.
- The session content must come from the Delinea Platform's own session
  recording pipeline; AIDA is not documented as accepting external
  recordings (no asciicast, no third-party SSH proxy capture, no
  customer-supplied video).
- An **analysis policy** must be configured before automatic analysis runs.
  Policies are matched by user/subject, target host, and time conditions.
- Analysis runs in Delinea-managed cloud infrastructure built on **Azure
  Computer Vision** (for OCR / frame analysis) and **Azure OpenAI** (for the
  LLM stages). The data path leaves the Delinea tenant boundary into Azure
  for processing.
- Tenant data is processed in the **same cloud region** the customer
  selected for their Delinea Platform tenant. The service is explicitly
  unavailable in the United Arab Emirates due to local data-residency
  constraints.
- Delinea's documentation states that customer recordings and data will not
  be used to train AI models without specific prior written authorization,
  and that data is deleted from Azure immediately after Azure OpenAI
  processing completes.
- Delinea also documents a "human on the loop" model: the feature is framed
  as augmenting reviewer oversight, not replacing it, and analyst feedback
  is part of the loop.
- Support access to recording data is restricted: when a user flags an Iris
  Auditing artifact (analysis, comment, or alert), the implicated data
  becomes visible to Delinea engineers for that troubleshooting case only.

## Outputs and Integration Points

- **Session Recording UI**: the analyzed session shows a Summary tab
  (narrative), an Activity panel (commands grouped by activity with output,
  timestamps, and labels), and a heat map across the recording timeline.
- **Label vocabulary**: the docs enumerate a controlled set of activity
  labels assigned per command/segment. Observed categories include:
  Administrative, Authentication, Backup & Restore, Cloud & Remote Services,
  Data Analysis & Visualization, Development & Compilation, File Operations
  & Transfer, File Directory Management, IAM, Logging & Auditing, Network
  Ops & Connectivity, Package Management, Performance Optimization,
  Privilege Elevation, SSH Key Management, Security & Encryption, Shell &
  Script Operations, Software Build & CI/CD, Storage & Disk Management,
  Suspicious, System Info & Monitoring, System Mgmt & Configuration, Text
  Processing & Search, Troubleshooting & Diagnostics, Virtualization &
  Containers. Different Delinea pages quote slightly different counts (the
  AIDA page's UI describes "25 categories"; the underlying enumeration we
  collected lists roughly 25-29 labels). The label set is fixed by Delinea,
  not customer-extensible from what is documented.
- **Anomaly highlighting**: the heat map's darker regions correspond to
  segments the model judged anomalous. The product page lists nine anomaly
  archetypes the system is tuned to surface: disabling firewalls, critical
  security modifications, creation of hidden users, unauthorized software
  installation, authorization failures, elevated privilege attempts,
  abnormal deletions, unexpected file transfers, and excessive secrets
  access.
- **Search and filter**: analysts can filter the session list by label and
  free-text search the transcribed content of analyzed sessions.
- **Policy resources**: analysis policy is itself a manageable object with
  Subjects, Targets, and Conditions. This is the Delinea analogue of
  Teleport's `inference_policy`.
- **Subscription telemetry**: AI-Driven Auditing hours allocated and
  consumed are surfaced under Marketplace -> Subscriptions.
- **SIEM/audit forwarding**: Delinea Platform supports syslog/SIEM
  integrations (Splunk, QRadar, Sentinel, etc.) for platform-level audit,
  but the public AIDA documentation we reviewed does not specify a distinct
  AIDA-event schema or `session.summarized`-style event payload analogous
  to Teleport's. See Open Questions.
- **Public API for AIDA outputs**: not documented in the materials
  reviewed. The Delinea Platform has APIs generally; whether AIDA summaries,
  labels, and anomaly markers are exposed as first-class API objects is
  unclear from the docs.

## Limitations and Risks

- **RDP coverage gap**: only the contents of PowerShell terminals inside RDP
  are analyzed. Native Windows GUI activity, MMC actions, browser activity
  inside an RDP session, and non-PowerShell consoles are out of scope as of
  the docs reviewed.
- **No documented support for non-Delinea recordings**: the pipeline assumes
  the Platform's own recorder and metadata streams. There is no documented
  ingest for raw asciicasts, OpenSSH session logs, kubectl audit, Tailscale
  SSH recordings, or third-party PAM capture.
- **Cloud/off-tenant processing**: recording content is sent to Azure
  Computer Vision and Azure OpenAI for analysis. Delinea publishes
  no-training and immediate-deletion commitments, but the feature is not a
  bring-your-own-model or on-prem inference path. Customers who require
  inference inside their own VPC or who cannot send privileged-session
  content to Azure will be excluded.
- **Region exclusion**: not available in the UAE.
- **Subscription cap is the cost control**: analysis is gated on a
  pre-purchased pool of hours rather than a per-policy budget the way an
  operator might prefer; once hours are exhausted, analysis halts.
- **Labels and anomaly categories are vendor-defined**: there is no
  documented mechanism for customers to extend, retrain, or override label
  semantics, and no documented MITRE ATT&CK technique mapping.
- **No on-demand re-analysis path documented**: the docs describe automated
  policy-driven analysis after a session ends; manual re-analysis,
  re-summarization with a different prompt, or A/B model comparison are not
  exposed.
- **Single LLM path**: Azure OpenAI is the only inference backend named.
  There is no documented choice of model family, no Bedrock path, no
  customer-provided OpenAI-compatible gateway path, and no offline path.
- **Vendor explainability claim is qualitative**: Delinea markets Iris as
  "transparent and explainable" with "evidence trails," but the docs do not
  document the evidence schema (e.g., which OCR frame, which keystroke
  range, which process event grounded each label). See Open Questions.
- **Accuracy is implicitly model-bounded**: the docs require a human in the
  loop, which is a reasonable acknowledgment that LLM/CV outputs can be
  wrong, but they do not publish accuracy metrics, false-positive rates, or
  evaluation methodology.

## Open Questions

- Does AIDA expose its analysis output (summary, labels, anomaly markers,
  per-segment evidence) through a documented API for SIEM, ticketing, or
  case-management integration, or only through the Delinea Platform UI?
- Is there an `aida.session.analyzed` style audit event analogous to
  Teleport's `session.summarized`, and what fields does it carry?
- What is the exact storage schema of the analysis artifact attached to a
  recording (labels, anomaly intervals, evidence pointers, model version,
  prompt version, token usage)?
- Which Azure OpenAI model family and version does AIDA use, and is the
  model pinned per tenant or rolled forward on Delinea's schedule?
- Does AIDA carry a per-segment evidence pointer (frame range, keystroke
  span, process event ID) that justifies each assigned label, or only
  document-level grouping?
- How does AIDA treat secrets, passwords, tokens, or PII visible on the
  recorded screen before sending content to Azure? Is there pre-redaction
  inside the tenant boundary, or is raw OCR content uploaded?
- How are failed analyses surfaced (timeouts, OCR failure, model errors,
  context-window overflow on long sessions)?
- What is the maximum supported session length, and how are long sessions
  chunked across model calls?
- Is there a documented MITRE ATT&CK mapping for the "Suspicious" label or
  the nine anomaly archetypes, or is the mapping marketing-only?
- Can customers extend, suppress, or rename the label vocabulary, or supply
  their own taxonomy?
- Is on-demand re-analysis or comparative re-analysis (e.g., new prompt,
  new model) exposed to the customer at all?
- Does AIDA ingest anything other than Delinea Platform native recordings
  (e.g., uploaded video, raw OpenSSH logs, kubectl audit, asciinema), or is
  it strictly tied to the Platform recorder?
- For RDP, beyond PowerShell, what is on the roadmap and what is the
  technical reason GUI activity is excluded today?
- Are AIDA outputs themselves auditable: who changed a label, who flagged
  an anomaly as false positive, who suppressed a session from analysis?
- What is the published unit price of an "AI-Driven Auditing hour," and is
  metering wall-clock recording length, processing time, or token-equivalent
  consumption?
