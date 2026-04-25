# Prior Art Notes

This directory tracks concise analysis of third-party work related to
ShellScope. These files are not mirrors of vendor documentation, papers, or
blog posts. They are local research notes that record why a source matters,
what it appears to provide, and how it compares with the ShellScope direction.

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
- `What it is`: short description in our own words.
- `Relevant capabilities`: what the prior art can do.
- `Requirements and operating model`: what has to be deployed, enabled, or
  configured.
- `Outputs and integration points`: artifacts, events, APIs, dashboards, logs,
  or data formats.
- `Limitations and risks`: stated limitations plus practical concerns.
- `Relevance to ShellScope`: overlap, differences, and design implications.
- `Open questions`: claims that need follow-up verification.

