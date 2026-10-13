# The plan for version 2

## Why

- Version 1 is 24,500 lines of Rust: three binaries, a `stats` subcommand and a
  My PRs tab over one library.
- Its engine was written for plain lines, and the full-screen view was fitted on
  afterwards. The cost was fd redirection, 100 ms polls, a 590-line run loop in
  the binary, and hand-rolled lists, scrolling, markdown and key help.
- The product has settled on one experience: one place to review the team's PRs
  and handle your own.

## Decisions

- **Language:** Go, with Bubble Tea v2, Bubbles v2 and Lip Gloss v2.
- **One binary:** `autoreview` is the view on a terminal and one plain pass
  elsewhere. `autoreview 213 240` limits the run to those PRs.
  `autoreview stats` and `autoreview panel` are subcommands.
- **Kept from v1:** watching in the view, headless one pass, `--babysit`,
  `--once`, `--no-post`, `--continue`, the command override, claude and codex
  with the exit-10 fallback, CI and stack holds, the pass cap and rest, the eyes
  reaction, sessions, the activity tail, and the My PRs tasks.
- **Dropped:** `review-prs`, `--pick` and `gum`, `--auto`, the `pr-review-tab`
  skill, and byte-exact v1 wording.
- **Positional PRs:** the run is limited to those PRs and reviews each one now,
  also when it is SEEN. In the view it makes one pass and holds open.
- **Override:** one variable, `AUTOREVIEW_CMD`.
- **Tasks:** they share `--jobs` with reviews but are not part of a pass. A
  failed task still makes the exit status 1.
- **Second ctrl-C:** the first one kills every job, then removes reactions and
  worktrees. A second one skips the cleanup that is left and exits.
- **List:** a scrolling list of two-line rows, as in v1. `bubbles/list` pages,
  and its selection is a position, not a PR.
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
internal/term      opening a new terminal tab (Herdr, cmux, Ghostty)
internal/marks     the eyes reaction while a review runs
internal/activity  what a running review does, from its transcript
internal/tui       the view
internal/panel     `autoreview panel`: one change, several models, one synthesis
internal/task      the tasks on your own PRs, and their skills
internal/worktree  the worktree each task runs in
internal/babysit   when a babysat PR gets another fix
```

**The engine**

- One goroutine owns the run. It reads one inbox: commands, results and signals.
  One channel keeps "reaped" ahead of "settled".
- It publishes an immutable snapshot after each change. Typed events are a
  journal for the plain lines and the run log. Nothing rebuilds state from them.
- `Do(Command)` never blocks. `Snapshot()` is an atomic load. `Changed()` holds
  one token. `Run()` returns only after everything it started has stopped.
- Reading the lists is separate from reviewing: `Snapshot.Looking` and
  `Snapshot.Phase`. A run reads the lists while it reviews, and between reviews.
- A run under the view is held open (`Config.HoldOpen`): it makes its pass and
  stays for `ReviewNow`, `StopJob` and `SetFocus` until `Quit`.
- One-shot, watch, babysit and once are one loop: `Config.Loop`, with the pass
  cap, the rest and the push reset as a `queue.Policy`. What the run remembers
  about each PR is a `queue.Track`, which is pure.
- A review and an override are data: a pure `review.Spec`. A task will be too.
  The test fake sits where a process starts (`proc.Starter`).
- A monitor goroutine for each review sends two messages through the inbox: the
  process ended, then its results are read. Reading files and asking GitHub
  happen off the engine's goroutine.

**The view**

- `Init` returns a command that waits on `Changed()` and returns the snapshot.
  There is no `Program.Send` and no fd redirection.
- `Update` calls `eng.Do(...)` directly, so two keys cannot arrive reversed.
- The view runs no process. Opening a browser or a terminal tab is an `Actions`
  function that `cmd/autoreview` hands in.
- The view tails the transcript itself, once a second, through
  `internal/activity`. Activity never enters the engine.
- `cmd/autoreview` prints the summary after the alternate screen closes.

## Milestones

Each one leaves a binary that runs. One PR for each milestone.

| #   | Result                                                                                                                              | Status |
| --- | ----------------------------------------------------------------------------------------------------------------------------------- | ------ |
| 0   | Repo, module, lint, CI, `AGENTS.md`, `docs/rules.md`                                                                                | done   |
| 1   | Read-only view: both tabs from real `gh` data, detail pane, key help, mouse, resize, `o`, refresh, `q`. Headless prints both lists. | done   |
| 2   | `autoreview stats`, with ledger read and append                                                                                     | done   |
| 3   | Headless one pass: positional PRs, `--once`, `--no-post`, `--continue`, the override, summary, exit status, ctrl-C                  | done   |
| 4   | The view runs reviews: `R`, `x`, `f`, `r`, `l`, the rendered review, the summary after close, the pty tests                         | done   |
| 5   | Loop policies: watch, `--babysit`, rest, cap, CI hold and the wait for pending checks, stack hold, `--max-idle`, `w`                | done   |
| 6   | Eyes reaction, codex, the exit-10 fallback, the in-use check, the activity tail                                                     | done   |
| 7   | My PRs tasks, after v1 decision 0030: `f` fix, `u` conflicts, `c` comments, `b` babysit, `B` babysit all                            | done   |
| 8   | Formula, tap, README, cutover rename                                                                                                | next   |
| 9   | `panel` (optional): `autoreview panel`, in `internal/panel`                                                                         | done   |

## Cutover

v2 was built in `venabots/autoreview-v2` and lands in `venabots/autoreview` as
one merge of the two histories. The merge's tree is v2's tree, so v1's history,
tags and issues stay, and the Rust source is at the tag `v0.24.0`.

The formula is in this repo, at `packaging/autoreview.rb`, and
`cmd/autoreview/formula_test.go` checks it against the build. A tag makes a
GitHub release (`release.yml`) and writes that formula into
`venabots/homebrew-tap` (`bump-tap.yml`). `HOMEBREW_TAP_TOKEN` is a secret of
the organization, so every repo in it can write the tap: a tag pushed in any
copy of this repo replaces the formula.

1. Merge the pull request with a merge commit. A squash would drop v2's history.
2. Tag `main` `v0.25.0` and push the tag. v2 continues v1's numbering. The tap's
   formula then builds v2 with Go.
3. `brew update && brew upgrade venabots/tap/autoreview`. `review-prs` and `gum`
   go away, and `panel` becomes `autoreview panel`.
4. Archive `venabots/autoreview-v2`.

## Tests

- **Unit, in table form:** the pure packages. The v1 cases and fixtures are
  ported.
- **Engine:** fakes inside `testing/synctest`, which gives virtual time.
- **View:** `Update` and `View` called directly, asserting on the frame.
- **End to end:** `run()` against a fake `gh`, which is the test binary itself
  under another name.
- **Pty:** the real binary under a pty (`cmd/autoreview/pty_test.go`). Only what
  is outside the frame: the alternate screen, the exit status, the summary after
  close, a cooked terminal, no leftover input.
