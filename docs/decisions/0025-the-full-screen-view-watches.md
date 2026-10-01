# The full-screen view watches

Recorded: 2026-10-01
Status: accepted

## Context

Decision 0023 gave a terminal the full-screen view. The view stays up until
`q`, but the run under it made one pass and then waited. A person who started
`autoreview` and left the screen open missed every PR opened after that pass,
unless they knew `--watch` or the `w` key. The tool is named for the opposite
behavior.

## Decision

- A run that opens the full-screen view watches, as if `--watch` were passed.
  It polls on `$AUTOREVIEW_WATCH_INTERVAL` (default 2m) and rests a reviewed
  PR for `$AUTOREVIEW_BABYSIT_INTERVAL` (default 30m). These are the
  intervals the `w` key uses, and they are read leniently, as `w` reads them.
- A run that does not open the view is one pass, as before. That is a pipe,
  cron, CI, `--headless`, and `--tui` off a terminal.
- `--pick` and `--babysit` are not changed by the view. A pick may only hold
  what was picked, and a babysit run is a loop already.
- `--once` asks for one pass in the view. With `--watch` or `--babysit` it is
  refused, with `error: --once and --watch cannot be used together` (or
  `--babysit`) and exit 1.
- The choice is made in `Config::watching_on_a_screen`, when the run starts.
  That is where it is known whether there is a terminal (decision 0023).

## Consequences

- Off a terminal nothing changes. The bash suite and every cron line get the
  same lines and the same exit status (decision 0003).
- In the view, `q` now ends a watch run, so the exit status is 130 and not 0.
  `--once` keeps the old status.
- The view opens at once. A watch run holds a PR with pending checks and
  looks again, so the wait of up to 30 minutes before the view opened is gone
  (decision 0011 still describes the one-pass run).
- A default run on a terminal is unattended for as long as the screen is up.
  The cost bounds still apply: `--max-passes`, the rest per PR, `--budget`.
- Under codex the run says once that every pass reviews fresh, as `--watch`
  does.
- `w` still stops and starts the looking for work.
