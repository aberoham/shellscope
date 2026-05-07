# whodrove

Tools for studying who or what drove privileged principals.

`whodrove` mines audit events and session recordings from Teleport
and GCP (AWS is on the roadmap, less fertile) and, for each
privileged session, asks: was the driver a human, or a
**ghostdriver**?

> A **ghostdriver** is non-human activity operating through a human
> principal — in practice, an LLM-driven coding-agent harness issuing
> commands via the human's credentials on a high-privilege operator
> surface (kubectl, gcloud, aws-cli, SSH/Teleport, terraform, and
> similar). The audit log shows the human's identity; the steering
> wheel is held by something else.

Once a session is classified as ghostdriven, the secondary goal is
fingerprinting *which* model and *which* harness from cadence and
call-graph shape. A bonus target is the human-pumped-up-by-an-LLM
operator — someone exercising privileged credentials they'd never
wield without an agent dictating the next flag — whose cadence
deviates the same way ghostdrivers' does.

## Layout

| Directory          | What's in it                                                                                                          | Entry point                                                                  |
|--------------------|-----------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------|
| `notes/`           | Durable research notes — plumbing, storage topology, tap options, multi-step pipeline design, open questions. | [`notes/README.md`](./notes/README.md)                                       |
| `notes-gcp/`       | Parallel notes for the GCP substrate — Cloud Audit Logs, org-aggregated sink to BigQuery, tap points, GCP-side pipeline design. | [`notes-gcp/README.md`](./notes-gcp/README.md)                               |
| `prior-art/`       | Bare factual notes on third-party prior art. | [`prior-art/README.md`](./prior-art/README.md)                               |
| `harnesses/`       | Third-party benchmarks or harnesses that could be used as labeled data or test fixtures. | [`harnesses/README.md`](./harnesses/README.md)                               |
| `upstream-repo/`   | Pinned read-only Teleport source (currently `2797910` = `Release 17.7.20`).                            | [github.com/gravitational/teleport](https://github.com/gravitational/teleport) |

## Step model

The work has a multi-step plan; each step's output is a durable
artifact under `notes/` (Teleport substrate) or `notes-gcp/` (GCP
substrate). Both substrates feed the same `sessions.sqlite` /
labels schema used in later steps.

- **Step 1 — Plumbing research.**
  - *Teleport:* See [`notes/01-audit-log-plumbing.md`](./notes/01-audit-log-plumbing.md)
    through [`notes/05-tap-points-for-detection.md`](./notes/05-tap-points-for-detection.md),
    with what couldn't be answered from source alone collected in
    [`notes/99-open-questions.md`](./notes/99-open-questions.md).
  - *GCP:* See [`notes-gcp/01-cloud-audit-logs.md`](./notes-gcp/01-cloud-audit-logs.md)
    through [`notes-gcp/05-tap-points-for-detection.md`](./notes-gcp/05-tap-points-for-detection.md),
    with live-tenant questions in
    [`notes-gcp/99-open-questions.md`](./notes-gcp/99-open-questions.md).
- **Step 2 — Pipeline design.**
  - *Teleport:* See [`notes/06-pipeline-design.md`](./notes/06-pipeline-design.md):
    a single static Go binary `whodrove-teleport` that taps Athena for
    `session.upload` events and S3 directly for recordings, parses
    ProtoStreamV1 via `lib/events.NewProtoReader`, and upserts
    per-session features plus Kubernetes-style classification labels
    into a local `sessions.sqlite`.
  - *GCP:* See [`notes-gcp/06-pipeline-design.md`](./notes-gcp/06-pipeline-design.md):
    a sibling Go binary `whodrove-gcp` that queries BigQuery for
    per-`(principal, minute)` feature rows, synthesises sessions by
    gluing adjacent buckets, and writes into the same
    `sessions.sqlite` with GCP-flavoured labels
    (`substrate.kind=gcp-cloud-audit`, `gcp.ua.tool`, etc.).
- **Step 3 — Classifier.** Outstanding. Reads from the shared SQLite
  extract; phase-1 is rules-only ("ghostdriver y/n", with cohort
  routing for substrates the rules don't apply to), phase-2 is
  LLM-on-call-graph (or PTY bytes, on the Teleport side) for sessions
  phase-1 routes to it. Phase-2 is also where harness fingerprinting
  lives — *which* model, *which* agent harness.

## Conventions

Shared across `notes/`, `prior-art/`, and `harnesses/`:

- `path:line` references in `notes/` are relative to `upstream-repo/`
  unless prefixed. `notes-gcp/` has no equivalent pin (GCP is a
  managed service); it cites product names + log names + documented
  schemas instead.
- Tenant-specific values use placeholders. Teleport: `<your-tenant>`
  for the Cloud hostname, `<uuid>` for AWS resource nonces. GCP:
  `<org-id>`, `<logging-project>`, `<bq-dataset>`,
  `<gcs-archive-bucket>`, `<your-domain>`.
- Code blocks marked `// v17.7.20` are copied directly from
  `upstream-repo/` at the pinned commit; everything else is
  illustrative.
- New facts that can't be sourced get added to the relevant
  `99-open-questions.md` with a verification note, not inlined as
  if known.

## Cloning

`upstream-repo` is gravitational/teleport, which itself declares a
nested submodule `e` pointing at `git@github.com:gravitational/teleport.e.git`
— Teleport's proprietary enterprise half, private to Gravitational
employees. Nothing in this study reads from `e`, so the recommended
clone sequence inits the direct submodules first, marks `e` inactive,
then recurses for everything else:

```bash
git clone <this-repo>
cd <this-repo>
git submodule update --init                        # direct submodules only
git -C upstream-repo config submodule.e.update none
git submodule update --init --recursive            # safe to recurse now
```

The submodules are shallow by design; pinning is what guarantees
reproducibility, not history depth. The `submodule.e.update = none`
setting must live in `upstream-repo`'s local config because that is
the only repo where `e` is a direct submodule — setting it at the
whodrove or outer-project level has no effect. Subsequent recursive
updates will report `Skipping submodule 'upstream-repo/e'`.
