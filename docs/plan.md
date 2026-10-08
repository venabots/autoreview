# The plan for version 2

## Why

- Version 1 is 24,500 lines of Rust: three binaries, a `stats` subcommand and
  a My PRs tab over one library.
- Its engine was written for plain lines, and the full-screen view was fitted
  on afterwards. The cost was fd redirection, 100 ms polls, a 590-line run
  loop in the binary, and hand-rolled lists, scrolling, markdown and key
  help.
- The product has settled on one experience: one place to review the team's
  PRs and handle your own.

## Decisions

- **Language:** Go, with Bubble Tea v2, Bubbles v2 and Lip Gloss v2.
- **One binary:** `autoreview` is the view on a terminal and one plain pass
  elsewhere. `autoreview 213 240` limits the run to those PRs.
  `autoreview stats` and `autoreview panel` are subcommands.
- **Kept from v1:** watching in the view, headless one pass, `--babysit`,
  `--once`, `--no-post`, `--continue`, the command override, claude and
  codex with the exit-10 fallback, CI and stack holds, the pass cap and
  rest, the eyes reaction, sessions, the activity tail, and the My PRs
  tasks.
- **Dropped:** `review-prs`, `--pick` and `gum`, `--auto`, the
  `pr-review-tab` skill, and byte-exact v1 wording.
- **Positional PRs:** the run is limited to those PRs and reviews each one
  now, also when it is SEEN. In the view it makes one pass and holds open.
- **Override:** one variable, `AUTOREVIEW_CMD`.
- **Tasks:** they share `--jobs` with reviews but are not part of a pass. A
  failed task still makes the exit status 1.
- **Second ctrl-C:** the first one kills every job, then removes reactions
  and worktrees. A second one skips the cleanup that is left and exits.
- **List:** a scrolling list of two-line rows, as in v1. `bubbles/list`
  pages, and its selection is a position, not a PR.
- **Markdown:** Glamour v2, with its output cached for each text and width.
- **Formula:** build from the source tarball with `go build` and a version
  ldflag.

## Architecture

```
cmd/autoreview     dispatch, wiring, signals, exit code
internal/cli       pflag parsing, environment defaults
internal/repo      owner, name, login, root
internal/gh        gh with a deadline, in its own process group
internal/prs       the list query, NEW/UPDATED/SEEN, filters, CI, stacks
internal/mine      the My PRs query and what blocks each PR
internal/queue     pure: what the run does about each PR, and why
internal/engine    the loop, Snapshot, Command
internal/words     the vocabulary for a PR's state
internal/sanitize  other people's text, made safe to draw
internal/plain     snapshots as plain lines
internal/ledger    the v1 ledger: read and append, byte for byte
internal/stats     the `stats` report
internal/session   the session id, the transcript lookup, pin or resume
internal/review    one review: the dash-p command line, exit classes, trailer, verdict
internal/findings  the findings in a review's text, for the ledger
internal/proc      process groups and the stop sequence
internal/rundir    the run directory
internal/skills    staging the bundled skills
internal/report    the summary table and the lines under it
internal/tui       the view
```

Still to come: `marks` (the eyes reaction), `task` (worktrees), `term`
(the new-tab spawner) and `panel`.

**The engine**

- One goroutine owns the run. It reads one inbox: commands, results and
  signals. One channel keeps "reaped" ahead of "settled".
- It publishes an immutable snapshot after each change. Typed events are a
  journal for the plain lines and the run log. Nothing rebuilds state from
  them.
- `Do(Command)` never blocks. `Snapshot()` is an atomic load. `Changed()`
  holds one token. `Run()` returns only after everything it started has
  stopped.
- One-shot, watch, babysit and once will be one loop with a `Policy` value.
- A review and an override are data: a pure `review.Spec`. A task will be
  too. The test fake sits where a process starts (`proc.Starter`).
- A monitor goroutine for each review sends two messages through the inbox:
  the process ended, then its results are read. Reading files and asking
  GitHub happen off the engine's goroutine.

**The view**

- `Init` returns a command that waits on `Changed()` and returns the
  snapshot. There is no `Program.Send` and no fd redirection.
- `Update` calls `eng.Do(...)` directly, so two keys cannot arrive reversed.
- The view will tail the transcript itself. Activity never enters the
  engine.
- `cmd/autoreview` prints the summary after the alternate screen closes.

## Milestones

Each one leaves a binary that runs. One PR for each milestone.

| # | Result | Status |
|---|---|---|
| 0 | Repo, module, lint, CI, `AGENTS.md`, `docs/rules.md` | done |
| 1 | Read-only view: both tabs from real `gh` data, detail pane, key help, mouse, resize, `o`, refresh, `q`. Headless prints both lists. | done |
| 2 | `autoreview stats`, with ledger read and append | done |
| 3 | Headless one pass: positional PRs, `--once`, `--no-post`, `--continue`, the override, summary, exit status, ctrl-C | done |
| 4 | The view runs reviews: `R`, `x`, `f`, `r`, `l`, the rendered review, the summary after close, the pty tests | next |
| 5 | Loop policies: watch, `--babysit`, rest, cap, CI hold and the wait for pending checks, stack hold, `--max-idle`, `w` | |
| 6 | Eyes reaction, codex, the exit-10 fallback, the in-use check, the activity tail | |
| 7 | My PRs tasks, after v1 decision 0030: `f` fix, `u` conflicts, `c` comments, `b` babysit, `B` babysit all | |
| 8 | Formula, tap, README, cutover rename | |
| 9 | `panel` (optional) | |

## Tests

- **Unit, in table form:** the pure packages. The v1 cases and fixtures are
  ported.
- **Engine:** fakes inside `testing/synctest`, which gives virtual time.
- **View:** `Update` and `View` called directly, asserting on the frame.
- **End to end:** `run()` against a fake `gh`, which is the test binary
  itself under another name.
- **Pty, from milestone 4:** only what is outside the frame: the alternate
  screen, the exit status, the summary after close, a cooked terminal, no
  leftover input.
