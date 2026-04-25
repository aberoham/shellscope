# Prior Art Notes

This directory tracks concise factual analysis of third-party work. These
files are not mirrors of vendor documentation, papers, or blog posts. They
are local research notes that record what a source is, what it does, and how
it is documented.

See `AGENTS.md` for the binding rules — notably: no comparisons to this
project, no positioning, no gap analysis. Prior-art notes describe what
exists in the source. Nothing more.

## Layout

- One Markdown file per project, paper, product feature, or closely related
  source cluster.
- Use kebab-case filenames, e.g. `teleport-session-recording-summaries.md`.
- Put source links and access dates near the top of each note.
- Prefer primary sources: product docs, official blogs, papers, standards, or
  source repositories.

## Baseline Template

Each note should usually include:

- `Source`: canonical upstream URLs, with access date and source version if
  known.
- `What it is`: short factual description in our own words.
- `Relevant capabilities`: what the source can do, per its own documentation.
- `Requirements and operating model`: what has to be deployed, enabled, or
  configured.
- `Outputs and integration points`: artifacts, events, APIs, dashboards, logs,
  or data formats.
- `Limitations and risks`: limitations stated by the source.
- `Open questions`: claims the source does not pin down and that need
  follow-up verification against the source itself.
