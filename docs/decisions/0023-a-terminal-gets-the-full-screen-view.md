# A terminal gets the full-screen view

Recorded: 2026-09-28
Status: accepted

## Context

Decision 0022 removed the inline board, which left two front-ends: the
full-screen view behind `--tui`, and plain lines. A person who ran
`autoreview` at a terminal without the flag now got the plain lines, which
are written for a log. The view was the better answer for that person, and
they had to know a flag to get it.

## Decision

- `autoreview` with stdout on a terminal opens the full-screen view, as if
  `--tui` were passed. Everything decision 0020 says about a `--tui` run is
  true of this one.
- `--headless` prints the plain lines on a terminal as well, as a pipe
  would get them. The startup spinner on stderr still turns (decision 0014).
- `--tui` stays accepted. It still asks for the view by name, so off a
  terminal it still says `note: --tui needs a terminal; printing plain lines`.
  A run with no flag off a terminal prints no note: nobody asked it for a
  screen.
- `--tui` and `--headless` together are refused, with
  `error: --tui and --headless cannot be used together` and exit 1. Each
  asks for the opposite of the other, and neither is a default the other
  could quietly win over.
- The choice is `cli::View` (`Auto`, `Screen`, `Plain`). Whether stdout is a
  terminal is asked when the run starts, not when the flags are parsed.

## Consequences

- Off a terminal nothing changes: cron, CI, a pipe and the whole bash suite
  get the same plain lines, byte for byte.
- A run with no flag and nothing to review now makes a run directory and
  stages its skills on a terminal, because the view opens and `R` may ask
  for a review. Off a terminal, or with `--headless`, it still prints one
  line and exits before either.
- An empty `--pick` still exits at once, on a terminal too. The person chose
  nothing, and under `--pick` `R` refuses every PR outside the pick, so the
  view would have nothing to offer.
- A script that runs `autoreview` with stdout on a terminal and expects it
  to exit on its own now waits for `q`. It has to pass `--headless`, or
  send stdout somewhere other than the terminal.
- The view is tested on a pty in `tests/tui.test.sh`, with no flag, with
  `--tui` and with `--headless`.
