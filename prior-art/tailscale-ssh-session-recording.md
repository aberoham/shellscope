# Tailscale SSH and Kubernetes Session Recording

## Source

- Primary sources:
  - https://tailscale.com/docs/features/tailscale-ssh/tailscale-ssh-session-recording
  - https://tailscale.com/docs/features/tailscale-ssh/how-to/session-recording-s3
  - https://tailscale.com/docs/reference/multiple-recorder-nodes
  - https://tailscale.com/docs/features/kubernetes-operator/how-to/session-recording
  - https://tailscale.com/docs/features/kubernetes-operator/how-to/tsrecorder
  - https://tailscale.com/docs/features/logging
  - https://tailscale.com/docs/reference/logging-streaming-events
  - https://tailscale.com/blog/session-recording-beta
  - https://tailscale.com/blog/auditable-infrastructure-access
- Vendor: Tailscale Inc. Product area: Tailscale SSH, Tailscale Kubernetes
  Operator, `tsrecorder`.
- Reviewed: 2026-04-25.
- Initial SSH session recording: announced 2023-05-11 (beta). Kubernetes
  session recording: Tailscale 1.70. Kubernetes API request audit logging:
  announced 2026-02-20 (beta).

## What It Is

Tailscale SSH lets nodes in a tailnet accept SSH connections where
authentication and authorization come from Tailscale identity and the tailnet
policy file (ACLs) instead of SSH keys; `tailscaled` on the remote host
terminates the SSH protocol when policy allows.

Session recording is an optional layer on top of Tailscale SSH and on top of
the Tailscale Kubernetes Operator's API server proxy. Matched sessions are
streamed over the tailnet to one or more recorder nodes (`tsrecorder`) which
persist them to local disk or S3-compatible object storage. Replay is offered
through an optional web UI on the recorder node and through the `asciinema` CLI.

## Relevant Capabilities

### Tailscale SSH session recording

- Records terminal *output* only (PTY writes from the remote process).
  Keystrokes / `stdin` are explicitly not captured.
- Recording is performed by the destination node (SSH server side), not the
  client; this is not configurable.
- Files are `asciicast` v2 (newline-delimited JSON), the same format as
  `asciinema`. Header object includes `version`, `width`, `height`,
  `timestamp`, `env`, plus Tailscale-specific `srcNode` and a source node
  ID. Subsequent lines are 3-element arrays `[t, "o", data]`.
- Cast files are searchable as text (`grep`) and replayable via
  `asciinema play`, GIF conversion with `agg`, or the recorder's optional
  web UI.
- Only Tailscale SSH sessions are recorded; OpenSSH sessions tunneled over
  Tailscale are not. Binary protocols layered on SSH (e.g. `scp`) are
  written verbatim as output bytes.

### Tailscale Kubernetes session and API recording

- Available via the Kubernetes Operator's API server proxy from Tailscale 1.70,
  on both legacy SPDY and the newer WebSockets streaming transports.
- Records `kubectl exec`, `debug`, `attach`, and `run` session contents as
  `stdout` and `stderr` from the attached terminal. `stdin` is explicitly
  excluded to keep typed credentials out of the recording.
- Captures session metadata: pod, container, namespace, source Tailscale
  device hostname and tags, and Tailscale user identity.
- When `enableEvents` is set on the `tailscale.com/cap/kubernetes` grant,
  every API request through the proxy is also recorded as a structured audit
  event (verb, resource, namespace, user, timestamp), in addition to
  interactive `kubectl` sessions.

### Policy / ACL controls

- Opt-in per ACL rule. SSH access rules add `"recorder": ["tag:tsrecorder"]`
  pointing at a tailnet tag; the same pattern appears inside the
  `tailscale.com/cap/kubernetes` grant.
- Different rules can route to different recorder tags (e.g. dev vs prod)
  for storage separation. When multiple rules match, the first matching
  rule in the policy file wins.
- `enforceRecorder` controls failure mode (see below).
- Configuring recording requires Owner, Admin, or Network Admin. Replay is
  gated by tailnet ACLs to the recorder node plus OS- or bucket-level
  access to storage; the docs recommend scoping recorder access tightly.

### Recorder node (`tsrecorder`)

- Container image; joins the tailnet via `tsnet`, listens on port 80 for
  incoming recordings, optional UI on 443.
- Storage backends: local filesystem, AWS S3, and S3-compatible services
  (MinIO, Wasabi, GCS with compatibility flags, Cloudflare R2 with
  `S3_SEND_CONTENT_MD5`). On-disk path:
  `/<dir>/<stableNodeID>/<timestamp>.cast`, where `stableNodeID` is the
  SSH server node.
- Multiple recorders can share a tag for redundancy; Tailscale picks the
  lowest IP-address value first and falls back through the rest.
- A Kubernetes `Recorder` CRD runs `tsrecorder` as a StatefulSet; in that
  mode S3 is the only documented durable backend.
- Default is "fail open": if no recorder is reachable, the session is still
  allowed. `enforceRecorder: true` switches to "fail closed": new matching
  sessions are refused and active ones terminated when the recorder is
  unreachable. The Kubernetes docs note that an in-flight session whose
  `tsrecorder` connection fails mid-stream may still continue even under
  fail-closed policy.

### Audit and SIEM integration

Distinct from cast files, Tailscale exposes configuration audit logs (who
changed the tailnet, ~90-day retention, all tiers); network flow logs
(Premium/Enterprise), recently enriched with user/device identity rather
than just IP/port tuples; Kubernetes API request audit logs from the API
server proxy (beta as of early 2026), structured per request; and SSH
login integration with `auditd` / `journald` so local Linux auditing
records the Tailscale identity behind a session, including for plain
OpenSSH sessions on a tailnet host.

Configuration audit logs and network flow logs can stream to SIEMs and to
S3 / S3-compatible storage. The docs reviewed do not enumerate specific
SIEM destinations.

### Edition / pricing

SSH session recording is documented today as Personal and Enterprise (the
2023 announcement said Free and Enterprise; either way plain Free is
excluded). Kubernetes session recording inherits the same availability and
additionally requires the operator with API server proxy. Network flow
logs require Premium or Enterprise.

## Requirements and Operating Model

- A tailnet with Tailscale SSH on destination hosts; Kubernetes recording
  also requires the operator with API server proxy.
- One or more tagged `tsrecorder` instances. For durable storage in the
  Kubernetes deployment path, an S3-compatible bucket plus credentials
  (static keys, IAM role, or IRSA on EKS).
- Policy file edits to add `recorder` to SSH access rules and to set
  `recorder` / `enforceRecorder` / optional `enableEvents` inside the
  `tailscale.com/cap/kubernetes` grant. ACLs must let matched source nodes
  reach the recorder tag on port 80, plus give replay reviewers tailnet
  access to the recorder UI.
- The customer owns the recorder host and storage; Tailscale states
  recordings are end-to-end encrypted over the tailnet and not visible to
  Tailscale itself.

## Outputs and Integration Points

- File format: `asciicast` v2, written as newline-delimited JSON `.cast`
  files. The header is a JSON object; each subsequent line is a JSON array
  `[seconds_since_start, stream_label, payload_string]` where
  `stream_label` is `"o"` for output. Tailscale extends the header with
  source-node fields, apparently without breaking asciicast replay.
- Storage layout (filesystem): `<base>/<stableNodeID>/<timestamp>.cast`.
  S3 key layout is not documented in detail in the materials reviewed.
- Replay UX: optional web UI at `https://<recorder>.<tailnet>.ts.net`, plus
  `asciinema play` directly against the `.cast` file.
- Audit integration: structured Kubernetes API audit events from the API
  server proxy and configuration audit events can stream to SIEM / S3;
  the recordings themselves are cast files, not events.
- Tailscale identity (user, device, tags) propagates into these logs and
  into Linux audit subsystems for SSH sessions.

## Limitations and Risks

- Output-only capture for both SSH and Kubernetes; typed input is absent
  from the cast file. Intent must be inferred from on-screen output.
- No filtering: pasted passwords, echoed tokens, and query results with
  PII land in the cast verbatim.
- Recording happens on the destination node, so a compromised destination
  could suppress its own recording. Fail-closed is the strongest documented
  mitigation, and even there a mid-session `tsrecorder` failure can allow
  the session to continue.
- No documented tamper-evidence (no hash chaining, signing, or notarization)
  on recording files in the materials reviewed.
- Retention is the operator's responsibility; no built-in lifecycle is
  documented. Expired recordings are removed manually or by a
  customer-configured S3 lifecycle rule.
- OpenSSH sessions tunneled over Tailscale are not recorded - only
  Tailscale SSH sessions are - which matters in mixed environments.
- No native LLM analysis or classification; the product stops at
  record-and-replay-with-audit.

## Open Questions

- Exact S3 object key layout written by `tsrecorder` (prefix per node, per
  day, per session ID)? Disk layout is `<dir>/<stableNodeID>/<timestamp>.cast`;
  the S3 mapping is not spelled out.
- Are Kubernetes session recordings one `.cast` per session, or split per
  stream (`stdout` vs `stderr`)? What session metadata appears in the
  `kubectl exec` cast header?
- Is the structured Kubernetes API audit log written to the same bucket /
  prefix as cast files, or a separate stream requiring its own ingestion?
- Does Tailscale add any tamper-evidence (signature, hash chain, manifest)
  to recording files or surrounding metadata?
- What retention or rotation behavior does `tsrecorder` itself enforce on
  disk before falling back to S3 lifecycle policies?
- Does the cast header differ between the SPDY and WebSockets Kubernetes
  recording paths, or are they normalized?
- Which specific SIEM destinations (Splunk, Datadog, Elastic, etc.) does
  Tailscale log streaming actually support, vs. generic S3 / webhook?
