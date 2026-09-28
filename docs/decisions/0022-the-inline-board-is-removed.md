# The inline board is removed

Recorded: 2026-09-28
Status: accepted

## Context

`autoreview` had four ways to show one pass: plain lines through a pipe, the
inline board on a terminal (decision 0015), the plain lines again when the
board could not open, and the full-screen view under `--tui` (decision
0020). Each was a front-end over the same engine, and each had its own idea
of what a row says.

The board cost the most of the four for what it gave:

- **Cursor-query machinery.** An inline viewport has to ask the terminal
  where its rows went after every resize. That query shares a lock with
  crossterm's event reader, so events could only be read on the main
  thread, and a terminal that did not answer within two seconds got the
  plain lines instead. The suite needed a pty driver that answers the query
  to test any of it.
- **A second renderer for every row.** The board and the view each built
  a running row, a finished row, a detail block and a "not approved yet"
  block, with their own width arithmetic. A change to what a review shows
  had to be made twice, or the two drifted.
- **Raw mode around the plain lines.** While the board was up, a bare
  `println!` landed inside the live area, so every print in the pass had to
  know whether a board was open.

The full-screen view does everything the board did, and keeps the reviews
that finished, which the board scrolled away.

## Decision

- Interactive means the full-screen view. Headless means plain lines.
  There is nothing in between.
- A pass without the view prints the plain lines, on a terminal or not. The
  same lines go to the run log while the view is up, so there is one plain
  form and it is the one the suite pins.
- `src/board.rs` and the board's row builders in `src/ui.rs` are deleted.
  The keys the view sends the pass (`Action`) live in `src/tui/keys.rs`.
- The view hides and shows the cursor itself, in `src/tui/terminal.rs`.
  `Ui` no longer writes cursor escapes; its `Drop` still gives the terminal
  back, so a `?` that returns early does not leave the alternate screen up.
- The summary is the plain table everywhere: after a headless pass, and
  after the view closes. The styled comfy-table summary and its OSC 8
  links to each PR are gone. It was a third rendering of the same facts,
  and the only one the suite could not grep.
- `ui.ticking()` is whether the view is open. Without it the pass waits on
  its channel until the nearest deadline and follows no transcript, because
  no row would show what it read.

## Consequences

- A person at a terminal who runs `autoreview` without `--tui` sees the
  plain lines, not a live display. Decision 0023 changed this the same day:
  a terminal now gets the full-screen view unless `--headless` is passed. Space, digits and esc no longer expand a
  running row; the view's detail pane is where that lives now.
- The plain output contract is unchanged, byte for byte.
- A `#N` in the summary no longer opens the PR on cmd-click. In the view,
  `o` opens the selected PR in the browser.
- The panel's models are named on the `panel #N:` lines, not in a table of
  their own. comfy-table stays a dependency, for `autoreview stats`.
- Decision 0006 (board rows carry no hyperlinks) and decision 0015 (the
  board is an inline ratatui viewport) are superseded: the thing they
  governed is gone.
- `tests/board.test.sh` is deleted. `tests/pty.py` still answers the cursor
  query, so nothing that asks can stall a test for two seconds.
- The view reads events on the main thread as well. Nothing forces that now,
  but the pass loop is where the keys are acted on, and moving the read is
  a change of its own.
