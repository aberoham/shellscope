# Prior-Art Research Instructions

These instructions apply to files in `prior-art/`.

## Purpose

Capture prior art without copying third-party materials into this repository.
Notes are factual records of what a source provides — useful later for
citation, comparison, and lookup.

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

## Scope: facts only

Prior-art notes describe what a third-party source does. They do **not**:

- name this repository, this project, or any internal product;
- compare a source to our work, position our work relative to it, or speculate
  about gaps a future build of ours might fill;
- editorialize about whether a source is good, bad, weak, strong, "the closest
  competitor", "directly relevant", etc.;
- forecast use cases, markets, customer fit, or product strategy;
- contain a "Relevance to <our project>" section, a "Differentiation" section,
  a "How we should position" section, or any equivalent framing.

Synthesis, comparison, gap analysis, and design work belong in design docs and
issues elsewhere — not here. A reader of `prior-art/` should be able to learn
what each source does without ever knowing what we are building.

If you find yourself writing a sentence that mentions our project, internal
goals, or "differentiation": delete it.

## Note Structure

Use the template in `prior-art/README.md` unless a source clearly needs a
different shape. Cover the source factually:

- supported session types and data sources;
- whether the system analyzes terminal output, keystrokes, process traces,
  database queries, Kubernetes API activity, or audit events;
- model/provider choices and AI enablement controls;
- policy/routing model;
- produced summaries, classifications, risk labels, events, or evidence;
- deployment and credential requirements;
- stated limitations, cost controls, retention, privacy, and safety claims.

## Style

- Write in neutral, technical language.
- Report what the source says or shows. Where the source is ambiguous, say so
  with hedged wording ("the docs do not state", "appears to") or move the
  unresolved point into Open Questions.
- Avoid marketing language except when explicitly identifying vendor
  positioning, and even then quote sparingly.
- Prefer stable nouns: "feature", "paper", "product", "service", "library",
  or "tool".
- Use `Open questions` for anything the source does not pin down. Do not guess
  and do not editorialize about what the absence implies for us.

## File Naming

- Use lowercase kebab-case: `vendor-feature-name.md`,
  `paper-short-name.md`, or `project-name.md`.
- If multiple sources describe one product area, use one note and list all
  sources at the top.
