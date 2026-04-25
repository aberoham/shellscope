# PAM Session Monitoring Vendors

Consolidated prior-art note covering the four major legacy Privileged Access
Management (PAM) vendors that ship session recording, monitoring, and replay
as part of their core platform. Each vendor's AI/analytics layer is noted
briefly here; deeper AI-specific products such as Delinea Privileged Behavior
Analytics or ManageEngine's OpenAI-backed summary integration are flagged for
their own separate notes.

## Sources

CyberArk
- https://www.cyberark.com/solutions/session-monitoring-and-recording/ (reviewed 2026-04-25)
- https://docs.cyberark.com/pam-self-hosted/latest/en/content/pasimp/monitoring-privileged-sessions.htm (reviewed 2026-04-25)
- https://docs.cyberark.com/pam-self-hosted/latest/en/content/pasimp/configuring-recording-and-audits-in-psmp.htm (reviewed 2026-04-25)
- https://docs.cyberark.com/pam-self-hosted/latest/en/content/pasimp/analyzing-high-risk-activities-during-psm-sessions.htm (reviewed 2026-04-25)
- https://docs.cyberark.com/pam-self-hosted/latest/en/content/pta/configuring-psm-for-pta-integration.htm (reviewed 2026-04-25)
- https://community.cyberark.com/s/article/PSM-How-to-set-the-retention-of-recordings-in-the-Vault (reviewed 2026-04-25)

BeyondTrust
- https://www.beyondtrust.com/products/privileged-remote-access (reviewed 2026-04-25)
- https://docs.beyondtrust.com/pra/docs/on-prem-reports (reviewed 2026-04-25)
- https://www.beyondtrust.com/products/password-safe/features/privileged-session-management (reviewed 2026-04-25)
- https://www.beyondtrust.com/docs/beyondinsight-password-safe/ps/admin/configure-session-monitoring.htm (reviewed 2026-04-25)

Delinea
- https://delinea.com/products/secret-server/features/session-recording (reviewed 2026-04-25)
- https://docs.delinea.com/online-help/secret-server/session-recording/index.htm (reviewed 2026-04-25)
- https://delinea.com/products/server-pam (reviewed 2026-04-25)
- https://delinea.com/products/privileged-behavior-analytics (reviewed 2026-04-25)

ManageEngine
- https://www.manageengine.com/privileged-access-management/privileged-session-monitoring-and-recording.html (reviewed 2026-04-25)
- https://www.manageengine.com/privileged-access-management/help/session-recording.html (reviewed 2026-04-25)
- https://www.manageengine.com/privileged-access-management/help/session-audit.html (reviewed 2026-04-25)
- https://www.manageengine.com/privileged-access-management/help/ai-powered-insights.html (reviewed 2026-04-25)

## What It Is

The legacy PAM session monitoring category covers vendor platforms that broker
privileged access to servers, network devices, databases, and remote desktops,
and record the resulting interactive sessions for audit, compliance, and
incident response. The recording function is conceptually similar across
vendors:

- a proxy or jump host sits between the operator and the target;
- the proxy captures either screen video (for graphical sessions such as RDP)
  or terminal text (for SSH and similar) plus, in many cases, keystrokes,
  process events, and chat;
- recordings are stored in a dedicated repository (a hardened vault, a
  database, or external object/file storage) and surfaced through a web
  console for replay, search, and SIEM forwarding;
- a real-time monitoring layer lets administrators "shadow" active sessions
  and intervene by locking, terminating, or in some cases blocking specific
  commands.

These products predate the LLM-summarization wave and are recording-centric:
analytics, where present, are mostly metadata- and behavior-driven. Two of
the four vendors (CyberArk PTA, Delinea Privileged Behavior Analytics) ship
behavioral analytics as a separate product layered on top of the base
recording. Only one (ManageEngine PAM360) currently advertises generative
session summarization in the base product, and it is delivered by integrating
with OpenAI rather than through native models.

## Per-Vendor Analysis

### CyberArk Privileged Session Manager (PSM and PSM for SSH)

#### Capabilities

- PSM is a jump-server proxy that brokers privileged sessions to Windows,
  database, mainframe, web, and SaaS targets. PSM for SSH (PSMP) is the
  separate connector for SSH and Telnet targets and supports SSH tunneling
  and SCP/SFTP file transfer.
- Two recording modes are available: text recording and video recording.
  Video recording applies to graphical sessions; text/keystroke recording
  applies to SSH, mainframe, and SQL clients.
- Documentation states that "each keystroke and command is recorded in the
  Vault for immediate auditing" for PSM for SSH, and a full text recording is
  written at session end. SSH keystroke auditing infers entered text from the
  target shell prompt rather than purely from the wire bytes.
- "Universal keystrokes" text recording is enabled by default for supported
  connection components (excluding PSM-RDP), so non-RDP graphical connections
  can still produce a searchable text track alongside video.
- Live monitoring lets authorized users watch active sessions and terminate
  them. Recordings are played back inside the PVWA web UI or downloaded for
  external playback.
- Search across recordings is supported via the PVWA Recording page;
  authorized users can search recordings for specific content, including
  audited keystrokes.
- Integration with CyberArk Privileged Threat Analytics (PTA) produces a
  per-session risk score that is written back to the Vault and surfaced in
  PVWA for both active and completed sessions.

#### Operating Model

- Deployed as a Windows-based jump host (PSM) plus a separate Linux-based
  PSMP component for SSH. Both components require connectivity to the Digital
  Vault, which holds recordings and metadata.
- Recordings are written to a temporary local folder during the session, then
  uploaded to the Vault when the session ends. The Vault enforces tamper-
  resistant storage; the marketing page describes "tamper-proof vault"
  storage as a control against operator log editing.
- External storage is supported as an alternative or supplement to in-Vault
  storage; the PSM uploads finished recordings over SMB to the configured
  share. Multiple PSM servers can share one storage backend.
- Retention is operator-configured. CyberArk publishes a sizing formula of
  the form `S = retention_days * sessions_per_day * session_minutes *
  recording_rate + 20 GB` (example: 90 days * 400 sessions/day * 180 minutes
  at 300 KB/min comes out to roughly 1.96 TB).
- Connection components are configured per platform; recording behavior is
  toggled per platform.

#### Outputs and Integrations

- Recording files (video and/or text) plus metadata stored in the Vault or on
  external SMB storage.
- PVWA web UI for search and playback, including PTA-derived risk scores per
  session.
- PTA emits incidents that can be forwarded to SIEM via syslog in CEF or LEEF
  format. Vendor-specific connectors exist for several SIEMs (e.g., the
  Rapid7 InsightIDR documentation describes the CEF/LEEF flow from EPV and
  PTA).
- Recordings can be exported and replayed in an external media player.

#### Limitations

- PTA only analyzes sessions from the time the integration was enabled;
  historic recordings are not retroactively scored.
- SSH keystroke auditing is prompt-aware. The vendor docs note this depends
  on recognizing the target shell prompt, which implies risk on unusual
  shells or prompt configurations (not directly verified in this round).
- AI-generated summaries are not part of the base PSM product as of the
  reviewed docs; analytics value comes through PTA, which scores sessions but
  does not produce a narrative summary.
- Sizing and retention discipline is the operator's responsibility; the
  Vault's tamper-resistance does not by itself bound storage growth.

### BeyondTrust (Privileged Remote Access and Password Safe)

BeyondTrust ships two adjacent products that both record sessions: Privileged
Remote Access (PRA, formerly Bomgar) for vendor and remote-employee access,
and Password Safe (within BeyondInsight) for credential-brokered SSH/RDP
sessions to managed assets. They share much of the recording and forensics
model; key differences are noted below.

#### Capabilities

- Session recording captures video playback for graphical sessions, with
  captions identifying who was in control of the mouse and keyboard at any
  point.
- Command shell recording, when enabled, produces both video and text
  transcripts of all command shells run during the session.
- Database connection recordings are described as full-desktop video.
- Real-time monitoring is supported through the access console; users with
  appropriate permissions can join, take over, or terminate active sessions.
- Command filtering / "command blacklisting" lets administrators define
  keyword groups that trigger actions: block command, lock session, block
  and lock, or terminate session. This is one of the more aggressive
  inline policy controls in the cohort.
- Session forensics search runs across all access sessions and can match
  against chat messages, command shell commands, file transfers, file system
  modifications, registry modifications, and foreground window titles.
- Password Safe documentation indicates SSH session recording captures full
  terminal output including keystrokes and responses, with keystroke logging
  on by default; auditors can search by user, system, date range, or keyword
  typed during the session.
- Recording can be selectively suppressed by Jump Policy via a "Disable
  Session Recordings" option that affects screen sharing, protocol tunnel
  Jump recording, and command shell recording.

#### Operating Model

- PRA is delivered as a hardened "B Series Appliance" (physical, virtual on
  vSphere/Hyper-V, or cloud) or as PRA Cloud. Recordings are stored on the
  appliance in a raw format and converted to compressed format only when
  viewed or downloaded.
- Password Safe is part of BeyondInsight and deploys as an appliance
  (virtual or physical) or in cloud (AWS, Azure). Recorded files are stored
  as encrypted binary files on the appliance or BeyondInsight server.
- Session recording is enabled by default for admin sessions in Password
  Safe; per-asset and per-policy configuration exists.

#### Outputs and Integrations

- Three report types in PRA: Session, Summary, and Session Forensics.
  Reports can be downloaded as Microsoft Excel or CSV, or viewed in HTML.
- Password Safe is documented as sending access events and session metadata
  to SIEMs in real time; named integrations include Splunk, IBM QRadar, and
  Microsoft Sentinel.
- Video and text transcripts are accessible via the web console; no native
  generative-AI summary feature was found in the reviewed pages.

#### Limitations

- Recording storage is tied to the appliance footprint by default; long
  retention or large user populations push customers toward archival
  pipelines that the vendor docs do not deeply prescribe.
- The forensics search appears to be substring/keyword based across recorded
  fields (chat, shell, transfers, registry, window titles); semantic or
  intent-level search is not advertised.
- AI/ML features are not advertised in the reviewed pages; the analytics
  story is essentially structured search plus inline command filtering.
- Disabling recording is policy-driven and reversible per Jump Policy, which
  means audit gaps are possible if policies are misconfigured.

### Delinea (Secret Server / Server PAM, with Privileged Behavior Analytics)

#### Capabilities

- Secret Server's session recording is offered in two tiers: Basic Session
  Recording and Advanced Session Recording (ASR).
- Basic recording works through the Secret Server Protocol Handler launcher
  and captures "second-by-second screenshots on the client machine"
  compiled into downloadable videos. It is cross-platform (Windows and Mac).
- Advanced Session Recording uses an installed agent (the ASRA) on the
  target and adds keystroke capture, process metadata (all processes
  started and stopped during a session), searchable video, and richer
  playback metadata. ASR is not available for Mac launchers.
- Protocol coverage in the documented launchers and proxies includes RDP,
  SSH, SQL, and PuTTY. RDP Proxy and SSH Proxy capture keystrokes; ASRA also
  captures keystrokes and process events.
- The replay UI shows an activity heatmap across the session ("process,
  screen, and keystroke activity across the entire session") so reviewers
  can jump to high-activity points; the web player exposes processes, key
  sequences, and metadata alongside the video.
- Cross-session search is supported, including searching for sessions in
  which a particular process (e.g., PowerShell) ran or a particular command
  (e.g., `sudo`) was typed. Keystrokes are indexed for in-playback jump.
- Real-time monitoring is supported: administrators can watch live sessions,
  send messages, and terminate sessions.
- Session Monitoring page filters by session data, secret, user, group,
  launcher type, date, and folder.

#### Operating Model

- Secret Server runs on Windows + IIS + SQL Server on-premises, or as a
  Delinea-hosted cloud instance. Server PAM is the broader Linux/Unix
  privilege-elevation product; the session recording features above are
  predominantly Secret Server features that Server PAM workflows feed into.
- Basic recording requires only the Protocol Handler. Advanced recording
  requires deploying ASRA per host where deeper telemetry is required.
- Recordings can be stored on disk and "archived based on your company's
  retention policy"; the docs cover storage configuration but do not in the
  reviewed pages dictate a specific tamper-resistance model the way
  CyberArk's Vault does.

#### Outputs and Integrations

- Web-based playback of video plus indexed keystrokes/processes; downloadable
  video for offline review.
- Syslog export is documented as the SIEM integration path; the marketing
  page also notes that network logon data can be enriched with the actual
  Secret Server username, helping correlate downstream SIEM alerts to
  specific recorded sessions.

#### Privileged Behavior Analytics (separate add-on)

- Delinea Privileged Behavior Analytics (PBA) is a separate analytics
  product that consumes Secret Server activity (logins, secret access,
  session launches, file transfers, admin actions) and applies machine
  learning to flag anomalies.
- PBA appears to operate over metadata and audit events rather than the
  recorded session content itself; the reviewed product page does not
  describe ingestion of video frames or keystroke text into the model.
- Outputs include behavioral alerts, user activity-spike alerts,
  authentication threat alerts, and risk scores. Automated responses include
  MFA prompts, session termination, or alert-only.
- Detailed PBA characterization belongs in its own note (see Open Questions).

#### Limitations

- Basic recording cannot reliably capture sessions launched outside the
  managed flow ("Show SSH Proxy Credentials" path) or in tabbed SSH clients,
  where only the first launched session is recorded.
- ASR's deeper telemetry requires per-host agents; environments that cannot
  deploy agents lose process metadata and the richer search experience.
- The base recording product does not advertise generative summarization;
  the AI surface is PBA, which is metadata-based behavior analytics rather
  than session-content analysis.
- SIEM integration is via syslog; richer event-level streaming is not
  highlighted in the reviewed pages.

### ManageEngine PAM360

#### Capabilities

- Browser-based recording of remote sessions over Windows RDP, SSH/Telnet,
  VNC, SQL, and HTTPS web access. No third-party client is required for the
  base recording flow.
- Recording formats observed in the docs: RDPV, SSHV, VNCV, TELNTEV.
- Three categories of monitored sessions: Managed Sessions (launched through
  PAM360 with stored credentials), Unmanaged Sessions (direct logins
  observed via PAM360 agents), and Recorded Website Connections (browser-
  based privileged access).
- Session shadowing supports real-time joining of active managed sessions
  and termination of suspicious sessions.
- Keystroke tracking is captured when Windows or Domain agents are deployed;
  agent telemetry also feeds system event logging (login/logoff, application
  activity).
- Replay is offered in the PAM360 console or via the Remote Spark player;
  large SSH/Telnet recordings are split into smaller fragments to make
  playback smoother.
- Audit exports to PDF and CSV are supported for compliance documentation.
- AI-generated session summaries are available for SSH, Telnet, and RDP
  sessions starting at build 7510, implemented by integrating PAM360 with
  OpenAI. Supported models listed include GPT-3.5 Turbo, GPT-4, GPT-4 Turbo,
  GPT-4o mini, and GPT-4o. The summary is described as highlighting
  commands executed, intent, and possible anomalies. RDP summarization
  requires the PAM360 agent on the target.

#### Operating Model

- Centralized PAM360 server with a web console; optional Windows/Domain
  agents on targets enable keystroke capture, system event collection, and
  RDP session activity for the AI summary path.
- Default recording storage path is local to the PAM360 install
  (`<PAM360>/recorded_files`), with configurable directories or UNC paths.
- All recordings are encrypted by default. Files larger than 10 MB are split
  into <=10 MB segments; segment splitting applies to Legacy SSH/Telnet, not
  video-format recordings.
- Recording can be enabled globally or per-resource for granular control.
- Deletion of recordings requires approval from at least one other
  administrator; locally stored files are deleted immediately on approval,
  while external-storage deletion is scheduled and depends on device
  availability.

#### Outputs and Integrations

- Web console for replay and search; audit log exports to PDF and CSV.
- AI summaries surfaced in the recording detail view alongside the video or
  terminal recording.
- EventLog Analyzer integration is referenced for detailed action tracking;
  broader SIEM forwarding is implied by ManageEngine's product family but
  was not characterized in the reviewed pages.

#### Limitations

- AI summarization is restricted to SSH, Telnet, and RDP sessions today, and
  the RDP path requires an installed agent.
- The AI feature delegates inference to OpenAI, which is a hard dependency
  for customers that require fully on-premises or BYO-model pipelines.
- Search and alerting capabilities were not deeply characterized in the
  reviewed pages beyond filterable session lists and event review.
- The reviewed pages describe anomaly mentions ("anomalous sessions",
  "suspicious user sessions") but do not detail the underlying detection
  mechanism.

## Cross-Vendor Themes

- **Recording-centric, not analysis-centric.** All four vendors center on
  capturing video and/or keystroke streams and exposing them in a web
  player. Search is mostly substring/keyword across recorded fields plus
  metadata filters (user, asset, time, secret). None of the base products
  positions itself as a session-understanding system.
- **Real-time controls are common, not uniform.** Live shadowing and
  termination are present everywhere. BeyondTrust's command blacklisting
  with block/lock/terminate actions is the most aggressive inline policy
  surface in the cohort.
- **Storage models diverge.** CyberArk anchors on a tamper-resistant Vault
  with optional external SMB storage. BeyondTrust stores on the appliance
  (raw, compressed on retrieval). Delinea stores on disk subject to
  customer retention. ManageEngine stores in a configurable directory or
  UNC path with mandatory encryption and split-file handling for large
  text recordings.
- **AI surfaces are uneven and mostly bolt-on.** CyberArk's PTA scores
  sessions for risk but does not summarize them. Delinea's PBA is
  metadata-driven anomaly detection delivered as a separate product.
  BeyondTrust's reviewed pages did not advertise an AI summarization or
  scoring layer in the base product. ManageEngine ships AI summaries in
  the base product but only for SSH/Telnet/RDP and only by calling
  OpenAI-hosted models. None of the four advertises a vendor-neutral
  analysis pipeline that ingests session content from another PAM, raw SSH
  logs, asciicast files, or Kubernetes audit streams.
- **Deployment burden is non-trivial.** All four require operator
  infrastructure: jump hosts (CyberArk PSM), appliances (BeyondTrust),
  Windows/IIS/SQL plus optional per-host agents (Delinea), or a central
  PAM360 server plus optional agents (ManageEngine). Adoption of AI
  features generally adds further requirements (PTA, PBA, OpenAI keys, RDP
  agents).
- **Evidence linkage is shallow.** Even where keystrokes or processes are
  indexed, the connection between an alert/score/summary and the specific
  spans of a recording that justify it is mostly implicit (jump-to-time
  in the player) rather than explicit.

## Open Questions

- Does CyberArk's universal-keystrokes text recording capture all entered
  text on all supported non-RDP graphical connection components, or is
  there a meaningful coverage gap for components not listed in the docs
  reviewed here?
- What exactly does PTA consume from PSM sessions to compute risk scores
  (text recording, structured events, metadata only), and is that data
  exposed via a documented API?
- Does BeyondTrust have any AI/ML scoring or summarization layer in
  Password Safe or PRA beyond keyword forensics search? The reviewed
  pages did not surface one, but this should be re-checked against current
  release notes.
- For BeyondTrust PRA on the B Series Appliance, what is the
  recommended/supported pattern for long-term recording archival off the
  appliance? The reviewed pages describe storage and report export but not
  a clear archival workflow.
- Does Delinea PBA at any point ingest the recorded session video,
  keystroke stream, or process events, or is it strictly an audit-event /
  metadata UEBA product?
- What exact session content does ManageEngine's OpenAI integration send
  for SSH/Telnet/RDP summarization (full transcript, command list,
  processed extract)?
- For all four vendors, what is the documented behavior when a recording
  cannot be uploaded to its primary store (Vault unavailable, appliance
  disk full, external store unreachable)?
- Are there documented APIs (REST, gRPC, or otherwise) on each platform
  that expose session recordings and metadata for external batch retrieval
  without scraping the web UI?
