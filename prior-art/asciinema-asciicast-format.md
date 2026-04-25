# asciinema and the asciicast File Format

## Source

- Primary sources: https://docs.asciinema.org/manual/cli/,
  https://docs.asciinema.org/manual/asciicast/v1/,
  https://docs.asciinema.org/manual/asciicast/v2/,
  https://docs.asciinema.org/manual/asciicast/v3/,
  https://docs.asciinema.org/manual/player/parsers/,
  https://github.com/asciinema/asciinema,
  https://github.com/asciinema/asciinema-server,
  https://github.com/asciinema/agg, https://asciinema.org,
  https://blog.asciinema.org/post/three-point-o/.
- Project: asciinema (CLI, file format, player, server).
- Original author: Marcin Kulik, first released in 2011.
- License: GPL v3+ for the CLI and `agg`; Apache 2.0 for `asciinema-server`.
- Current CLI generation reviewed: 3.x (Rust rewrite). Python 2.x line lives
  on the `python` branch.
- Reviewed: 2026-04-25.

## What It Is

asciinema is an open-source terminal session recorder, player, and live-streaming
toolchain. It captures what a terminal would display by wrapping the user's
shell or command in a pseudoterminal, then writes the byte stream plus a small
amount of metadata to an `asciicast` file. The file is text JSON, not video, so
recordings are small, diffable, and trivially shareable.

`asciicast` is the on-disk format. Three numbered versions exist: v1
(deprecated), v2 (long-time default, used by asciinema CLI 2.x), and v3
(introduced with the asciinema CLI 3.0 release in September 2025 and current
default for new recordings).

## Relevant Capabilities

- CLI subcommands documented for the 3.x line include at least: `rec` (record
  into a `.cast` file), `play` (replay inside a terminal), `stream` (live-stream
  locally or via a server), `cat` (dump the captured output stream to stdout),
  `convert` (translate between formats, e.g. asciicast v3 to v2), and
  `upload` / `auth` for talking to an asciinema server.
- Recording mechanism: asciinema spawns the target shell or command under a
  PTY and copies bytes between the real terminal and that PTY, recording the
  output side. SIGWINCH is observed so resize events are written into the file.
- Captured by default: the terminal output byte stream (including escape
  sequences) plus metadata and resize events. Keystroke input is not captured
  by default; an opt-in flag enables it, represented by a distinct event code.
- gzip-friendly; the docs claim recordings compress to roughly 8% of their
  original size on average.
- Live streaming and offline recording share the same event vocabulary, so the
  format is usable both for stored archives and for stream replay.

## Requirements and Operating Model

- Distributed as a single binary. Installable from major Linux package managers,
  Homebrew and MacPorts on macOS, ports/pkg on FreeBSD and OpenBSD, container
  images, or built from source with `cargo`. The legacy 2.x Python
  implementation is still on PyPI for `pipx` install.
- Runs entirely client-side. No network is required to record or play back
  locally; uploads to a server are optional.
- The hosted service at `asciinema.org` is the canonical backend, but the
  `asciinema-server` project (Elixir / Phoenix, Apache 2.0, ships a Dockerfile)
  is self-hostable, and the CLI can be pointed at any compatible server.
  Self-hosted visibility controls (private, unlisted, public), editable
  metadata, transcript download, and full-text search across recording content
  are documented.
- `asciinema-player` is a JavaScript player that embeds in a web page or via
  an `<iframe>` served from a server.

## Outputs and Integration Points

### File shape (v2 and v3)

- `.cast` file extension. MIME type `application/x-asciicast`.
- Newline-delimited JSON. Line 1 is a JSON object (the header). Lines 2..N are
  events. UTF-8 throughout. Non-printable characters in string fields are
  encoded with `\uXXXX` escapes.
- v3 additionally allows `#`-prefixed comment lines in the event stream.

### Header fields

v2 header: `version` (integer `2`, required), `width` and `height` (terminal
cols and rows, required), `timestamp` (Unix start), `duration` (seconds),
`idle_time_limit` (playback idle cap), `command`, `title`, `env` (selected env
vars, commonly `SHELL` and `TERM`), `theme` (object with `fg`, `bg`, palette).

v3 header changes: `version` must be `3`; terminal info moves into a `term`
object with required `cols`/`rows` and optional `type`/`version`/`theme`; a
`tags` array is added; `duration` is dropped because timing is now relative
(implied by the sum of intervals).

### Event stream

Each event is a 3-element JSON array. v2 uses `[time, code, data]` with
absolute seconds since recording start (monotonic float). v3 uses
`[interval, code, data]` with seconds since the previous event (relative
delta); the official CLI rounds to millisecond precision and the spec warns
that naive rounding accumulates drift and recommends error diffusion.

Event codes: `"o"` terminal output (bytes the program wrote to the PTY);
`"i"` terminal input (bytes read from the user, only present when input
recording is explicitly enabled); `"r"` resize (`data` is `"COLSxROWS"`);
`"m"` marker / breakpoint with an optional label; `"x"` exit (v3-only;
`data` is the recorded process's numeric exit status — v2 has no equivalent
and the recording simply ends).

### v1, for completeness

v1 was a single JSON document (not NDJSON) with top-level `version`, `width`,
`height`, `duration`, `command`, `title`, `env`, and a `stdout` array of
`[delay, data]` 2-tuples. Deprecated; current tooling reads it but does not
produce it.

### What the format does NOT contain

The format is a faithful capture of the terminal byte stream plus minimal
metadata. It does not include: command boundaries
(all output and prompt redrawing is one stream of `"o"` events); per-command
exit codes (v3 records a single session-level exit status, v2 records none);
prompt detection or shell semantics (the recorder does not know what the
prompt looks like); a structured timeline (sections, steps, intents); process
tree, syscall, or BPF data; any notion of "what the user meant" (even when
input is captured, it is raw key bytes including control characters and
bracketed-paste markers); network identifiers, source IP, auth principal, or
credential context (the recorder runs as the local user and writes only what
it sees in the PTY).

### Tooling ecosystem

- `asciinema-player` — official JavaScript player; supports asciicast v1/v2/v3
  and, via pluggable parsers, also `typescript` and `ttyrec` recordings.
- `asciinema-server` — Elixir/Phoenix self-hostable server; bundles the `avt`
  virtual terminal for thumbnail and stream rendering.
- `agg` — official Rust tool, successor to `asciicast2gif`, renders an
  asciicast (v2) into an animated GIF via `gifski`. GPL v3+.
- The asciinema docs do not endorse third-party parser libraries by name, but
  the format is simple enough that ad-hoc parsers exist in many languages.
  The legacy Python 2.x asciinema package exposes `asciinema.asciicast`
  modules (`v1`, `v2`) reusable as a parser. Gitea renders asciicast inline.

## Limitations and Risks

- The recorder captures whatever bytes the terminal emits. Anything on screen
  lands in the file: secrets echoed by mistake, tokens printed by tooling,
  files dumped with `cat`, password prompts that misbehave and echo. No
  built-in redaction.
- Input recording, when enabled, captures raw keystrokes verbatim, including
  any password typed into a non-`noecho` prompt.
- The format has no concept of session participants, source IP, or authn/z
  context. Provenance must come from whatever wrapper invoked `asciinema`.
- v3 stores deltas; aggressive rounding can accumulate drift.
- v3 is not backwards-compatible with v2 (`term` object, relative intervals,
  `"x"` exit event); a v2-only parser will misread a v3 file and vice versa.
- Hosted asciinema.org retention, deletion, and visibility behavior are
  product policies that can change. Self-hosting on `asciinema-server` is the
  durable answer for organizations with retention requirements.

## Open Questions

- v3 spec stability: first published 2025-09-10, revised 2025-10-20 (timing
  precision). Implemented across the official CLI, player, and server, but
  the docs do not mark a "stable" tag. Pin to a dated revision and re-check.
- Are v3 event codes beyond `o`/`i`/`r`/`m`/`x` reserved or planned? The
  reviewed material lists only those five.
- What does the `i` event capture in practice for bracketed-paste and IME
  composition? The spec says "stdin"; terminal-mode operational semantics
  are not detailed in the pages reviewed.
- Hosted asciinema.org retention, deletion on takedown, indexing, and
  default visibility policies are not enumerated on the homepage. Self-hosted
  `asciinema-server` is the safer assumption for customer recordings.
- Does a canonical, maintained Rust or Go parser crate exist? The docs do
  not endorse one.
- Is there a documented convention for embedding shell-integration markers
  (e.g. OSC 133 prompt/command markers) inside `o` events?
