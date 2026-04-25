# Prior-Art Research Instructions

These instructions apply to files in `prior-art/`.

## Purpose

Capture prior art relevant to ShellScope without copying third-party materials
into this repository. Notes should help future readers understand the source,
compare it to ShellScope, and cite it later in talks or papers.

## Source Handling

- Use primary sources whenever possible: official documentation, official blog
  posts, product pages, conference papers, standards, or source repositories.
- Record the canonical source URL, access date, product/doc version if visible,
  and author or vendor where known.
- Do not commit wholesale copies of third-party documentation, papers, web
  pages, screenshots, or generated "Copy for LLM" markdown.
- Keep direct quotes rare and short. Prefer paraphrase. If a short quote is
  necessary, quote only the minimum phrase and attribute it.
- If a local scratch copy exists, use it only as temporary input. The committed
  artifact must be our analysis, not a source mirror.

## Note Structure

Use the template in `prior-art/README.md` unless a source clearly needs a
different shape. Keep the analysis focused on facts that affect ShellScope:

- supported session types and data sources;
- whether the system analyzes terminal output, keystrokes, process traces,
  database queries, Kubernetes API activity, or audit events;
- model/provider choices and AI enablement controls;
- policy/routing model;
- produced summaries, classifications, risk labels, events, or evidence;
- deployment and credential requirements;
- stated limitations, cost controls, retention, privacy, and safety claims;
- how ShellScope should position itself relative to the work.

## Style

- Write in neutral, technical language.
- Distinguish verified source facts from our interpretation.
- Avoid marketing language except when explicitly identifying vendor
  positioning.
- Prefer stable nouns: "feature", "paper", "product", "service", "library",
  or "tool".
- Add `Open questions` instead of guessing when a source is ambiguous.

## File Naming

- Use lowercase kebab-case: `vendor-feature-name.md`,
  `paper-short-name.md`, or `project-name.md`.
- If multiple sources describe one product area, use one note and list all
  sources at the top.

